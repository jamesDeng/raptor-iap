"""Private runner transport must never leak text or accept another attempt."""
from dataclasses import asdict
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid
from types import SimpleNamespace
from test_sandbox_lifecycle_state import config_data
from tools.sandbox_lifecycle.config import Config, CheckpointRef
from tools.sandbox_lifecycle.cloud import Cloud, CloudError, KeyHandle, safe_result
from tools.sandbox_lifecycle.contracts import inference_outcome
from tools.sandbox_lifecycle.state import load_ledger, save_ledger


class TaskResultTests(unittest.TestCase):
    def setUp(self):
        from tools.task_store.models import TaskBinding
        from tools.sandbox_lifecycle.task_results import TaskResultSink
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        self.binding = TaskBinding(str(uuid.uuid4()), str(uuid.uuid4()), 'b' * 64)
        self.sink = TaskResultSink(self.root, self.binding)
        d = config_data()
        d['bootstrap_checkpoint'] = CheckpointRef(**d['bootstrap_checkpoint'])
        self.cfg = Config(**d)
        ref = dict(archive_key='auth/lifecycle/test.tgz', checksum_key='auth/lifecycle/test.sha256',
                   sha256='c'*64, bytes=200, pi_version='0.99.2')
        self.raw = dict(phase='inference', passed=True, kind='app-question',
                        binding=asdict(self.binding), actual_model='gpt-5.6-luna',
                        context_sha256='b'*64, tool_succeeded=True, answer_generated=True,
                        usage=[dict(input=2, output=2, total_tokens=4)],
                        answer='synthetic-private-answer', checkpoint=ref)

    def test_private_result_split_and_bound_read(self):
        result = self.sink.accept(self.raw)
        safe_result(result, self.cfg, 'inference')
        self.assertNotIn('synthetic-private-answer', json.dumps(result))
        self.assertEqual(self.sink.read(result['answer_ref']), 'synthetic-private-answer')
        state = load_ledger(self.root/self.cfg.account_id/'ledger.json', self.cfg)
        state.update(task_binding=asdict(self.binding), attempt_id=self.binding.attempt_id, result=result)
        save_ledger(self.root/self.cfg.account_id/'ledger.json', state)
        self.assertNotIn('synthetic-private-answer', (self.root/self.cfg.account_id/'ledger.json').read_text())
        file = self.root / result['answer_ref']['relative_path']
        self.assertEqual(file.stat().st_mode & 0o777, 0o600)
        self.assertEqual(file.parent.stat().st_mode & 0o777, 0o700)
        self.assertEqual(self.sink.accept(self.raw), result)

    def test_wrong_binding_hash_model_and_unknown_fields_rejected(self):
        for change in ({'binding': dict(asdict(self.binding), task_id=str(uuid.uuid4()))},
                       {'context_sha256': 'a'*64}, {'actual_model':'other'},
                       {'answer':'x'*16385}, {'extra':'synthetic-secret'},
                       {'error':'synthetic-provider-secret'}):
            with self.subTest(fields=list(change)), self.assertRaises(ValueError):
                self.sink.accept(dict(self.raw, **change))

    def test_result_reference_cannot_switch_task_or_escape(self):
        result = self.sink.accept(self.raw)
        for change in ({'relative_path':'../auth.json'}, {'relative_path':'task-results/'+str(uuid.uuid4())+'/file.json'},
                       {'sha256':'a'*64}, {'bytes':True}, {'extra':'secret'}):
            with self.assertRaises(ValueError):
                self.sink.read(dict(result['answer_ref'], **change))
        file = self.root / result['answer_ref']['relative_path']
        file.unlink()
        file.symlink_to(self.root/'elsewhere')
        with self.assertRaises(ValueError):
            self.sink.read(result['answer_ref'])

    def test_result_write_failure_preserves_checkpoint_and_cleanup_obligations(self):
        with patch('tools.sandbox_lifecycle.task_results.os.replace', side_effect=OSError('synthetic-secret')):
            result = self.sink.accept(self.raw)
        self.assertFalse(result['passed'])
        self.assertEqual(result['error'], 'AnswerArtifactFailed')
        self.assertEqual(result['checkpoint'], self.raw['checkpoint'])
        self.assertNotIn('answer_ref', result)
        self.assertNotIn('synthetic-secret', json.dumps(result))
        safe_result(result, self.cfg, 'inference')

    def test_answer_retained_on_checkpoint_failure(self):
        raw = dict(self.raw, passed=False, error='CheckpointFailed')
        raw.pop('checkpoint')
        result = self.sink.accept(raw)
        self.assertEqual(self.sink.read(result['answer_ref']), 'synthetic-private-answer')
        self.assertEqual(inference_outcome(result), 'completed')

    def test_artifact_conflict_and_tampering_never_overwrite(self):
        first = self.sink.accept(self.raw)
        changed = self.sink.accept(dict(self.raw, answer='different'))
        self.assertFalse(changed['passed'])
        self.assertEqual(self.sink.read(first['answer_ref']), 'synthetic-private-answer')
        file = self.root / first['answer_ref']['relative_path']
        file.write_text('{}')
        with self.assertRaises(ValueError):
            self.sink.read(first['answer_ref'])

    def test_recovery_reads_only_known_result_uuid_and_enforces_size(self):
        generation = str(uuid.uuid4())
        reads, commands = [], []
        def read(name):
            reads.append(name)
            return json.dumps(self.raw)
        def run(command, timeout):
            commands.append((command, timeout))
            return SimpleNamespace(stdout=str(len(json.dumps(self.raw).encode())), exit_code=0)
        cloud = Cloud(self.cfg, None, None, None, None)
        cloud.sandbox = lambda *_: SimpleNamespace(files=SimpleNamespace(read=read), commands=SimpleNamespace(run=run))
        result = cloud.read_completed_result('sbx', KeyHandle('id','synthetic'), generation, 'inference', result_sink=self.sink)
        self.assertEqual(self.sink.read(result['answer_ref']), 'synthetic-private-answer')
        self.assertEqual(reads, ['/tmp/raptor-harness/job-'+generation+'-result.json'])
        self.assertEqual(commands[0][1], 10)
        cloud.sandbox = lambda *_: SimpleNamespace(files=SimpleNamespace(read=read), commands=SimpleNamespace(run=lambda *a,**k:SimpleNamespace(stdout='65537',exit_code=0)))
        with self.assertRaises(CloudError):
            cloud.read_completed_result('sbx',KeyHandle('id','synthetic'),generation,'inference',result_sink=self.sink)
        self.assertEqual(len(reads), 1)

    def test_failed_context_result_has_no_answer_artifact(self):
        raw = dict(self.raw, passed=False, tool_succeeded=False, answer_generated=False, error='ContextUnavailable')
        raw.pop('answer');raw.pop('context_sha256')
        result=self.sink.accept(raw)
        self.assertNotIn('answer_ref', result)
        self.assertEqual(inference_outcome(result),'failed')

    def test_old_ledger_and_read_probe_still_validate(self):
        old = dict(phase='inference',passed=True,tool_succeeded=True,answer_matches=True,usage=[],checkpoint=self.raw['checkpoint'])
        self.assertEqual(safe_result(old,self.cfg,'inference'),old)
        self.assertEqual(inference_outcome(old),'completed')

    def test_application_binding_survives_execution_and_recovery(self):
        from test_raptor_store import APP
        from test_sandbox_lifecycle_lifecycle import ExternalCloud
        from tools.task_store.store import Store
        from tools.sandbox_lifecycle.config import Request
        from tools.sandbox_lifecycle.lifecycle import execute, recover
        store = Store(self.root/'store')
        app = store.create_app(APP)
        task = store.submit(dict(app_id=app['app_id'], question='synthetic-private-question',
                                 model='gpt-5.6-luna',idempotency_key=str(uuid.uuid4())))
        attempt = task['attempts'][0]
        from tools.task_store.models import TaskBinding
        binding = TaskBinding(task['task_id'],attempt['attempt_id'],task['snapshot_sha256'])
        application = dict(kind='app-question', question=task['question'], snapshot=task['snapshot'],
                           model=task['model'], binding=asdict(binding))
        path = self.root/self.cfg.account_id/'ledger.json'
        template = self.raw
        class AppCloud(ExternalCloud):
            def run_job(inner, identity, job, key, *, result_sink=None):
                if job['phase'] != 'inference':
                    return super().run_job(identity,job,key)
                inner.calls.append('inference')
                self.assertEqual(job['request'], application)
                self.assertIsNotNone(result_sink)
                raw = dict(template,binding=asdict(binding),context_sha256=binding.snapshot_sha256)
                inner.pending_result = raw
                return result_sink.accept(raw)
            def read_completed_result(inner, identity, key, generation=None, phase='inference', *, result_sink=None):
                self.assertIsNotNone(result_sink)
                return result_sink.accept(inner.pending_result)
        cloud = AppCloud(path)
        cloud.fail='kill'
        first = execute(self.cfg,Request('app-question',application=application),path,cloud)
        self.assertFalse(first['passed'])
        state = load_ledger(path,self.cfg)
        self.assertEqual(state['attempt_id'],binding.attempt_id)
        self.assertEqual(state['task_binding'],asdict(binding))
        self.assertNotIn('synthetic-private-question',path.read_text())
        self.assertNotIn('synthetic-private-answer',path.read_text())
        cloud.fail=None
        result = recover(self.cfg,path,cloud)
        self.assertTrue(result['passed'])
        self.assertEqual(cloud.calls.count('inference'),1)
        self.assertTrue(result['cleanup_confirmed'])
