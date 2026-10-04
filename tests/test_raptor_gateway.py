"""Real durable orchestration, replacing only external cloud operations."""
from dataclasses import asdict
import multiprocessing
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid
from test_raptor_store import APP
from test_sandbox_lifecycle_state import config_data
from test_sandbox_lifecycle_lifecycle import ExternalCloud
from tools.task_store.store import Store
from tools.task_store.models import TaskBinding
from tools.sandbox_lifecycle.config import Config, CheckpointRef
from tools.sandbox_lifecycle.cloud import CloudError
from tools.sandbox_lifecycle.state import load_ledger, save_ledger, account_lock


class ApplicationCloud(ExternalCloud):
    def __init__(self, ledger):
        super().__init__(ledger)
        self.crash = None

    def assert_storage(self):
        if self.crash == 'before-create':
            raise SystemExit('synthetic crash')

    def run_job(self, identity, job, key, *, result_sink=None):
        if job['phase'] != 'inference':
            return super().run_job(identity, job, key)
        self.calls.append('inference')
        request = job['request']
        ref = dict(archive_key='auth/lifecycle/'+job['generation']+'.tgz', checksum_key='auth/lifecycle/'+job['generation']+'.sha256',sha256='b'*64,bytes=200,pi_version='0.99.2')
        raw = dict(phase='inference',kind='app-question',passed=True,binding=request['binding'],
                   actual_model='gpt-5.6-luna',tool_succeeded=True,answer_generated=True,
                   context_sha256=request['binding']['snapshot_sha256'],answer='The registered database is planned.',
                   usage=[dict(input=3,output=2,total_tokens=5)],checkpoint=ref)
        self.pending_result = raw
        if self.crash == 'before-artifact':
            raise SystemExit('synthetic crash')
        return result_sink.accept(raw)

    def read_completed_result(self, identity, key, generation=None, phase='inference', *, result_sink=None):
        self.calls.append('read-result')
        if self.pending_result and result_sink:
            return result_sink.accept(self.pending_result)
        return None

    def terminate_and_confirm(self, identity, key):
        if self.crash == 'before-cleanup':
            raise SystemExit('synthetic crash')
        return super().terminate_and_confirm(identity, key)


def hold_worker(root, ready, release):
    from tools.task_store.gateway import worker_lock
    with worker_lock(Path(root)):
        ready.set()
        release.wait(10)


class GatewayTests(unittest.TestCase):
    def setUp(self):
        from tools.task_store.gateway import Gateway
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        self.store = Store(self.root/'raptor')
        self.app = self.store.create_app(APP)
        d = config_data();d['bootstrap_checkpoint']=CheckpointRef(**d['bootstrap_checkpoint'])
        self.cfg = Config(**d)
        self.ledger = self.root/'lifecycle'/self.cfg.account_id/'ledger.json'
        self.cloud = ApplicationCloud(self.ledger)
        self.gateway = Gateway(self.store,self.cfg,self.cloud,self.ledger)

    def submit(self):
        return self.store.submit(dict(app_id=self.app['app_id'],question='What is missing?',model='gpt-5.6-luna',idempotency_key=str(uuid.uuid4())))

    def binding(self, task):
        attempt = self.store.get_task(task['task_id'])['attempts'][-1]
        return TaskBinding(task['task_id'],attempt['attempt_id'],task['snapshot_sha256'])

    def test_serial_queue_and_correct_association(self):
        first, second = self.submit(), self.submit()
        self.assertTrue(self.gateway.tick())
        saved = self.store.get_task(first['task_id'])
        self.assertEqual(saved['attempts'][0]['state'],'completed')
        self.assertEqual(saved['attempts'][0]['outcomes']['checkpoint'],'saved')
        self.assertEqual(saved['attempts'][0]['outcomes']['cleanup'],'confirmed')
        self.assertEqual(saved['attempts'][0]['answer'],'The registered database is planned.')
        self.assertEqual(saved['attempts'][0]['evidence']['actual_model'],'gpt-5.6-luna')
        self.assertTrue(saved['attempts'][0]['evidence']['tool_succeeded'])
        self.assertEqual(saved['attempts'][0]['evidence']['context_sha256'],first['snapshot_sha256'])
        self.assertEqual(self.store.get_task(second['task_id'])['attempts'][0]['state'],'queued')
        self.assertTrue(self.gateway.tick())
        self.assertEqual(self.cloud.calls.count('create'),2)
        self.assertLess(self.cloud.calls.index('kill'),len(self.cloud.calls)-1-self.cloud.calls[::-1].index('create'))
        self.assertEqual(load_ledger(self.ledger,self.cfg)['task_binding']['task_id'],second['task_id'])

    def test_direct_cli_lock_keeps_task_queued(self):
        task = self.submit()
        with account_lock(self.ledger.parent.parent,self.cfg.account_id):
            self.assertFalse(self.gateway.tick())
        self.assertEqual(self.cloud.calls,[])
        self.assertEqual(self.store.get_task(task['task_id'])['attempts'][0]['state'],'queued')

    def test_real_second_worker_is_excluded(self):
        from tools.task_store.gateway import worker_lock
        context=multiprocessing.get_context('spawn')
        ready,release=context.Event(),context.Event()
        process=context.Process(target=hold_worker,args=(str(self.store.root),ready,release))
        process.start()
        try:
            self.assertTrue(ready.wait(5))
            with self.assertRaisesRegex(ValueError,'Busy'):
                with worker_lock(self.store.root):
                    self.fail('overlapping worker')
            self.assertFalse(self.gateway.tick())
        finally:
            release.set();process.join(10)
        self.assertEqual(process.exitcode,0)

    def test_claim_before_ledger_requires_explicit_recovery_without_replay(self):
        task=self.submit()
        self.store.claim_next()
        self.gateway.reconcile()
        self.assertFalse(self.gateway.tick())
        self.assertEqual(self.cloud.calls,[])
        self.store.request_recovery(self.binding(task),str(uuid.uuid4()))
        self.gateway.tick()
        saved=self.store.get_task(task['task_id'])['attempts'][0]
        self.assertEqual(saved['outcomes']['answer'],'unknown')
        self.assertEqual(saved['outcomes']['cleanup'],'confirmed')
        self.assertEqual(saved['state'],'failed')
        self.assertNotIn('inference',self.cloud.calls)

    def test_crash_matrix_never_replays_claimed_inference(self):
        from tools.task_store.gateway import Gateway
        for crash in ('before-create','before-artifact','before-ledger-result','before-cleanup','before-finish'):
            with self.subTest(crash=crash),tempfile.TemporaryDirectory() as temp:
                root=Path(temp).resolve();store=Store(root/'store');app=store.create_app(APP)
                task=store.submit(dict(app_id=app['app_id'],question='synthetic',model='gpt-5.6-luna',idempotency_key=str(uuid.uuid4())))
                ledger=root/'lifecycle'/self.cfg.account_id/'ledger.json';cloud=ApplicationCloud(ledger)
                gateway=Gateway(store,self.cfg,cloud,ledger);cloud.crash=crash
                if crash=='before-finish':
                    with patch.object(store,'finish',side_effect=SystemExit('crash')):
                        with self.assertRaises(SystemExit):gateway.tick()
                elif crash=='before-ledger-result':
                    from tools.sandbox_lifecycle.lifecycle import persist
                    def crash_after_artifact(state,path):
                        if state.get('result',{}).get('kind')=='app-question':raise SystemExit('crash')
                        persist(state,path)
                    with patch('tools.sandbox_lifecycle.lifecycle.persist',side_effect=crash_after_artifact):
                        with self.assertRaises(SystemExit):gateway.tick()
                else:
                    with self.assertRaises(SystemExit):gateway.tick()
                count=cloud.calls.count('inference');cloud.crash=None
                restarted=Gateway(Store(root/'store'),self.cfg,cloud,ledger)
                restarted.reconcile();self.assertFalse(restarted.tick())
                self.assertEqual(cloud.calls.count('inference'),count)
                binding=TaskBinding(task['task_id'],task['attempts'][0]['attempt_id'],task['snapshot_sha256'])
                store.request_recovery(binding,str(uuid.uuid4()))
                restarted.tick()
                saved=store.get_task(task['task_id'])['attempts'][0]
                self.assertEqual(saved['outcomes']['cleanup'],'confirmed')
                self.assertEqual(saved['outcomes']['answer'],'unknown' if crash=='before-create' else 'completed')
                self.assertEqual(cloud.calls.count('inference'),count)

    def test_foreign_binding_blocks_queue(self):
        task=self.submit()
        state=load_ledger(self.ledger,self.cfg)
        foreign=TaskBinding(str(uuid.uuid4()),str(uuid.uuid4()),'a'*64)
        state.update(task_binding=asdict(foreign),attempt_id=foreign.attempt_id)
        save_ledger(self.ledger,state)
        self.assertFalse(self.gateway.tick())
        self.assertTrue(self.store.get_task(task['task_id'])['blocked'])
        self.assertNotIn('create',self.cloud.calls)

    def test_interrupted_recovery_reuses_control_without_inference(self):
        task=self.submit();self.cloud.fail='kill';self.gateway.tick();self.cloud.fail=None
        key=str(uuid.uuid4());binding=self.binding(task)
        self.store.request_recovery(binding,key)
        with patch.object(self.store,'finish_recovery',side_effect=SystemExit('crash')):
            with self.assertRaises(SystemExit):self.gateway.tick()
        self.assertTrue(self.store.get_task(task['task_id'])['blocked'])
        self.store.request_recovery(binding,key)
        self.gateway.tick()
        self.assertFalse(self.store.get_task(task['task_id'])['blocked'])
        self.assertEqual(self.cloud.calls.count('inference'),1)

    def test_persistent_store_failure_during_recovery_keeps_gate(self):
        task=self.submit();self.cloud.fail='kill';self.gateway.tick();self.cloud.fail=None
        self.store.request_recovery(self.binding(task),str(uuid.uuid4()))
        with patch.object(self.store,'finish',side_effect=OSError('private failure')):
            with self.assertRaises(OSError):self.gateway.tick()
        self.assertTrue(self.store.get_task(task['task_id'])['blocked'])
        self.assertEqual(self.cloud.calls.count('inference'),1)

    def test_completed_private_artifact_adopted_only_for_matching_attempt(self):
        task=self.submit();self.cloud.crash='before-artifact'
        with self.assertRaises(SystemExit):self.gateway.tick()
        self.cloud.crash=None
        self.cloud.pending_result['binding']['attempt_id']=str(uuid.uuid4())
        self.gateway.reconcile()
        self.store.request_recovery(self.binding(task),str(uuid.uuid4()))
        self.gateway.tick()
        saved=self.store.get_task(task['task_id'])['attempts'][0]
        self.assertNotEqual(saved['outcomes']['answer'],'completed')
        self.assertIsNone(saved['answer'])
        self.assertIn('kill',self.cloud.calls)
        self.assertEqual(self.cloud.calls.count('inference'),1)

    def test_completed_answer_failed_checkpoint_or_cleanup(self):
        task=self.submit();self.cloud.reject_checkpoint=True
        self.gateway.tick()
        saved=self.store.get_task(task['task_id'])['attempts'][0]
        self.assertEqual(saved['outcomes']['answer'],'completed')
        self.assertEqual(saved['outcomes']['checkpoint'],'failed')
        self.assertEqual(saved['outcomes']['cleanup'],'confirmed')
        self.assertEqual(saved['state'],'blocked')
        self.cloud.reject_checkpoint=False
        self.store.request_recovery(self.binding(task),str(uuid.uuid4()));self.gateway.tick()
        self.assertFalse(self.store.get_task(task['task_id'])['blocked'])
        other=self.submit();self.cloud.fail='kill'
        self.gateway.tick()
        failed=self.store.get_task(other['task_id'])['attempts'][0]
        self.assertEqual(failed['outcomes']['cleanup'],'failed')
        self.assertEqual(failed['state'],'blocked')
        before=self.cloud.calls.count('create');self.submit();self.gateway.tick()
        self.assertEqual(self.cloud.calls.count('create'),before)

    def test_checkpoint_candidate_requires_recovery_before_next_dispatch(self):
        first,second=self.submit(),self.submit();self.cloud.reject_checkpoint=True
        self.gateway.tick()
        state=load_ledger(self.ledger,self.cfg);candidate=state['candidate']
        self.assertTrue(self.store.get_task(first['task_id'])['blocked'])
        self.cloud.reject_checkpoint=False
        self.gateway.tick()
        self.assertEqual(self.cloud.calls.count('create'),1)
        self.assertEqual(load_ledger(self.ledger,self.cfg)['candidate'],candidate)
        self.assertEqual(self.store.get_task(second['task_id'])['attempts'][0]['state'],'queued')
        self.store.request_recovery(self.binding(first),str(uuid.uuid4()));self.gateway.tick()
        self.assertEqual(load_ledger(self.ledger,self.cfg)['checkpoint'],candidate)
        self.assertFalse(self.store.get_task(first['task_id'])['blocked'])
        self.gateway.tick();self.assertEqual(self.cloud.calls.count('create'),2)

    def test_confirmed_generation_survives_unavailable_local_answer(self):
        from tools.sandbox_lifecycle.task_results import TaskResultSink
        task=self.submit()
        with patch.object(TaskResultSink,'_publish',side_effect=OSError('synthetic storage failure')):
            self.gateway.tick()
        saved=self.store.get_task(task['task_id'])['attempts'][0]
        self.assertEqual(saved['outcomes']['answer'],'completed')
        self.assertIsNone(saved['answer'])
        self.assertEqual(saved['state'],'blocked')
        self.assertEqual(saved['outcomes']['error'],'AnswerArtifactFailed')
        self.assertEqual(saved['outcomes']['checkpoint'],'saved')
        self.assertEqual(saved['outcomes']['cleanup'],'confirmed')

    def test_store_event_or_finish_write_failure_still_cleans_resources(self):
        task=self.submit()
        original=self.store.append_event
        def observe(binding,stage):
            if stage=='asking-agent':raise OSError('synthetic-private-error')
            original(binding,stage)
        with patch.object(self.store,'append_event',side_effect=observe):self.gateway.tick()
        self.assertIn('kill',self.cloud.calls)
        self.assertIn('remove-key',self.cloud.calls)
        self.assertNotIn('inference',self.cloud.calls)
        self.assertTrue(self.store.get_task(task['task_id'])['blocked'])
        self.cloud.calls=[]
        self.store.request_recovery(self.binding(task),str(uuid.uuid4()));self.gateway.tick()
        other=self.submit()
        with patch.object(self.store,'finish',side_effect=OSError('synthetic-private-error')):self.gateway.tick()
        self.assertIn('kill',self.cloud.calls)
        self.assertTrue(self.store.get_task(other['task_id'])['blocked'])

    def test_uncertain_create_keeps_identifiers_and_blocks_replacement(self):
        task=self.submit();self.cloud.fail='create';self.gateway.tick()
        state=load_ledger(self.ledger,self.cfg)
        self.assertEqual(state['pending'],'sandbox')
        self.cloud.fail=None
        self.store.request_recovery(self.binding(task),str(uuid.uuid4()))
        self.gateway.tick();self.submit();self.gateway.tick()
        self.assertEqual(self.cloud.calls.count('create'),1)
        self.assertTrue(self.store.get_task(task['task_id'])['blocked'])
