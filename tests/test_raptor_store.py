"""Persistence tests catch snapshot mutation, duplicate dispatch and unsafe paths."""
import copy
import json
import multiprocessing
from pathlib import Path
import sqlite3
import tempfile
import unittest
import uuid
from unittest.mock import patch


APP = dict(name='db-client', owner='synthetic-owner', environment='poc',
           region='ap-southeast-1', ack_cluster_id=None, namespace=None,
           deployment=None, dependencies=[dict(name='database', type='postgres',
                                              lifecycle='planned', notes='Not deployed')])


def claim(root, queue):
    from tools.task_store.store import Store
    result = Store(Path(root)).claim_next()
    queue.put(result['attempt_id'] if result else None)


class StoreTests(unittest.TestCase):
    def setUp(self):
        from tools.task_store.store import Store
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / 'state'
        self.store = Store(self.root)
        self.app = self.store.create_app(APP)

    def submit(self, **changes):
        value = dict(app_id=self.app['app_id'], question='What is missing?',
                     model='gpt-5.6-luna', idempotency_key=str(uuid.uuid4()))
        value.update(changes)
        return self.store.submit(value)

    def test_submission_snapshot_survives_edit_and_store_restart(self):
        from tools.task_store.store import Store
        task = self.submit()
        value = copy.deepcopy(APP)
        value['dependencies'][0]['lifecycle'] = 'present'
        self.store.update_app(self.app['app_id'], 1, value)
        saved = Store(self.root).get_task(task['task_id'])
        self.assertEqual(saved['snapshot']['version'], 1)
        self.assertEqual(saved['snapshot']['dependencies'][0]['lifecycle'], 'planned')
        self.assertEqual(saved['snapshot']['provenance'], 'owner-entered')
        self.assertEqual(Store(self.root).get_app(self.app['app_id'])['version'], 2)

    def test_duplicate_submit_and_conflicting_idempotency(self):
        from tools.task_store.models import Conflict
        key = str(uuid.uuid4())
        first = self.submit(idempotency_key=key)
        second = self.submit(idempotency_key=key)
        self.assertEqual(first['task_id'], second['task_id'])
        with self.assertRaises(Conflict):
            self.submit(idempotency_key=key, question='Different question')
        self.assertEqual(len(self.store.list_tasks()), 1)
        self.assertEqual(len(first['attempts']), 1)

    def test_real_process_claim_is_exclusive(self):
        self.submit()
        context = multiprocessing.get_context('spawn')
        queue = context.Queue()
        workers = [context.Process(target=claim, args=(str(self.root), queue)) for _ in range(2)]
        for worker in workers:
            worker.start()
        for worker in workers:
            worker.join(10)
            self.assertEqual(worker.exitcode, 0)
        self.assertEqual(sum(queue.get(timeout=2) is not None for _ in workers), 1)
        queue.close()

    def test_fifo_cancel_and_retry_history(self):
        from tools.task_store.models import TaskBinding, OutcomeReport, Conflict
        first, second, third = self.submit(), self.submit(), self.submit()
        self.store.cancel(first['task_id'], first['attempts'][0]['attempt_id'])
        attempt = self.store.claim_next()
        self.assertEqual(attempt['task_id'], second['task_id'])
        with self.assertRaises(Conflict):
            self.store.cancel(second['task_id'], attempt['attempt_id'])
        binding = TaskBinding(second['task_id'], attempt['attempt_id'], second['snapshot_sha256'])
        self.store.finish(binding, OutcomeReport('failed', 'saved', 'confirmed', 'failed', 'ModelFailed'), None)
        key = str(uuid.uuid4())
        retry = self.store.retry(second['task_id'], key)
        self.assertEqual(self.store.retry(second['task_id'], key)['attempt_id'], retry['attempt_id'])
        self.assertNotEqual(retry['attempt_id'], attempt['attempt_id'])
        self.assertEqual(self.store.get_task(second['task_id'])['question'], 'What is missing?')
        self.assertEqual(self.store.claim_next()['task_id'], third['task_id'])

    def test_utf8_and_exact_schema_limits(self):
        from tools.task_store.models import ValidationError, NotFound, validate_snapshot
        self.submit(question='😀' * 2000)
        for changes in ({'question': '😀' * 2001}, {'question': ' '},
                        {'model': 'unverified'}, {'extra': 'secret'},
                        {'idempotency_key': '../elsewhere'}):
            with self.subTest(changes=list(changes)):
                with self.assertRaises(ValidationError):
                    self.submit(**changes)
        with self.assertRaises(NotFound):
            self.submit(app_id=str(uuid.uuid4()))
        with self.assertRaises(ValidationError):
            self.store.update_app(self.app['app_id'], True, APP)
        for change in ({'unknown': 'value'}, {'namespace': 'postgresql://user:pass@host'},
                       {'dependencies': APP['dependencies'] * 11},
                       {'dependencies': [dict(APP['dependencies'][0], type='invalid')]}):
            with self.assertRaises(ValidationError):
                self.store.create_app(dict(APP, **change))
        self.store.create_app(dict(APP, dependencies=APP['dependencies'] * 10))
        oversized = dict(self.app, dependencies=[dict(APP['dependencies'][0], notes='😀' * 1000)] * 10)
        with self.assertRaises(ValidationError):
            validate_snapshot(oversized)

    def test_private_paths_and_symlinks(self):
        from tools.task_store.store import Store
        self.assertEqual(self.root.stat().st_mode & 0o777, 0o700)
        for path in self.root.glob('tasks.sqlite*'):
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        other = self.root.parent / 'link'
        other.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(ValueError):
            Store(other)

    def test_failed_transaction_leaves_no_partial_task(self):
        with sqlite3.connect(self.root / 'tasks.sqlite3') as db:
            db.execute("CREATE TRIGGER fail_queue BEFORE INSERT ON attempts BEGIN SELECT RAISE(ABORT, 'synthetic-secret'); END")
        with self.assertRaises(Exception) as caught:
            self.submit()
        self.assertNotIn('synthetic-secret', str(caught.exception))
        self.assertEqual(self.store.list_tasks(), [])

    def test_binding_gate_recovery_and_stale_versions(self):
        from tools.task_store.models import TaskBinding, OutcomeReport, Conflict, Blocked
        task = self.submit()
        attempt = self.store.claim_next()
        binding = TaskBinding(task['task_id'], attempt['attempt_id'], task['snapshot_sha256'])
        self.store.append_event(binding, 'starting')
        self.store.block(binding, 'TaskPersistenceFailed')
        self.submit()
        self.assertIsNone(self.store.claim_next())
        with self.assertRaises(Blocked):
            self.store.retry(task['task_id'], str(uuid.uuid4()))
        self.store.request_recovery(binding, str(uuid.uuid4()))
        self.assertEqual(self.store.claim_recovery(), binding)
        self.store.finish(binding, OutcomeReport('unknown', 'unknown', 'confirmed', 'failed', None), None)
        self.store.clear_gate(binding)
        self.assertIsNotNone(self.store.claim_next())
        self.store.update_app(self.app['app_id'], 1, APP)
        with self.assertRaises(Conflict):
            self.store.update_app(self.app['app_id'], 1, APP)

    def test_canonical_paths_are_shared_with_old_cli(self):
        from tools.sandbox_lifecycle.paths import lifecycle_root, canonical_local_root
        from tools.sandbox_lifecycle.__main__ import state_root
        self.assertEqual(lifecycle_root(), state_root())
        self.assertEqual(lifecycle_root().parent, canonical_local_root())
        self.assertNotIn('/worktrees/', str(canonical_local_root()))
