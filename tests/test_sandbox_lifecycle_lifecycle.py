import json,tempfile,unittest
from pathlib import Path
from dataclasses import asdict
from test_sandbox_lifecycle_state import config_data
from tools.sandbox_lifecycle.config import Config,CheckpointRef,Request
from tools.sandbox_lifecycle.cloud import KeyHandle,CloudError
from tools.sandbox_lifecycle.state import load_ledger,save_ledger
from tools.sandbox_lifecycle.lifecycle import execute,recover
class ExternalCloud:
 def __init__(self,ledger):self.ledger=ledger;self.calls=[];self.fail=None;self.reject_checkpoint=False;self.model_pass=True;self.pending_result=None
 def assert_identity(self):pass
 def assert_storage(self):pass
 def verify_checkpoint(self,ref):
  self.calls.append('verify')
  if self.reject_checkpoint and '/lifecycle/' in ref.archive_key:raise CloudError('CheckpointVerificationFailed')
 def create_key(self,name,expiry):
  assert json.loads(self.ledger.read_text())['phase']=='creating-key';self.calls.append('key');return KeyHandle('key-test','synthetic-secret')
 def remove_key(self,key):
  self.calls.append('remove-key')
  if self.fail=='key-cleanup':raise CloudError('KeyCleanupUnconfirmed')
 def resolve_key_intent(self,name):self.calls.append('resolve-key')
 def create_sandbox(self,attempt,key):
  assert json.loads(self.ledger.read_text())['phase']=='creating';self.calls.append('create')
  if self.fail=='create':raise CloudError('SandboxCreateUnconfirmed')
  return 'sbx-test'
 def prepare(self,identity,key):self.calls.append('prepare')
 def run_job(self,identity,job,key):
  self.calls.append(job['phase'])
  if self.fail=='restore' and job['phase']=='restore':raise CloudError('RunnerOutcomeUnknown')
  if job['phase']=='restore':return {'phase':'restore','passed':True}
  ref=dict(archive_key='auth/lifecycle/'+job['generation']+'.tgz',checksum_key='auth/lifecycle/'+job['generation']+'.sha256',sha256='b'*64,bytes=200,pi_version='0.99.2')
  if job['phase']=='refresh':return {'phase':'refresh','passed':True,'refresh_succeeded':True,'refresh_token_changed':True,'checkpoint':ref}
  return {'phase':'inference','passed':self.model_pass,'tool_succeeded':self.model_pass,'answer_matches':self.model_pass,'usage':[],'checkpoint':ref}
 def terminate_and_confirm(self,identity,key):
  self.calls.append('kill')
  if self.fail=='kill':raise CloudError('TerminationUnconfirmed')
 def read_completed_result(self,identity,key,generation=None,phase='inference'):return self.pending_result
class LifecycleTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name).resolve();d=config_data();d['bootstrap_checkpoint']=CheckpointRef(**d['bootstrap_checkpoint']);self.cfg=Config(**d);self.ledger=self.root/self.cfg.account_id/'ledger.json';self.cloud=ExternalCloud(self.ledger)
 def test_success_publishes_verified_checkpoint_then_cleans_up(self):
  result=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);s=load_ledger(self.ledger,self.cfg);self.assertTrue(result['passed']);self.assertEqual(s['phase'],'finished');self.assertNotIn('sandbox_id',s);self.assertNotIn('key_id',s);self.assertIn('/lifecycle/',s['checkpoint']['archive_key']);self.assertNotIn('synthetic-secret',self.ledger.read_text())
 def test_uncertain_create_blocks_replacement(self):
  self.cloud.fail='create';self.assertFalse(execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)['passed']);self.cloud.fail=None;self.assertFalse(execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)['passed']);self.assertEqual(self.cloud.calls.count('create'),1)
 def test_failed_restoration_prevents_inference(self):
  self.cloud.fail='restore';self.assertFalse(execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)['passed']);self.assertNotIn('inference',self.cloud.calls);self.assertIn('kill',self.cloud.calls)
 def test_successful_answer_failed_checkpoint_retains_previous_ref(self):
  self.cloud.reject_checkpoint=True;r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);self.assertFalse(r['passed']);self.assertEqual(r['request_outcome'],'completed');self.assertEqual(load_ledger(self.ledger,self.cfg)['checkpoint']['archive_key'],'auth/checkpoint-5.tgz');self.assertIn('kill',self.cloud.calls)
 def test_refresh_checkpoint_saved_even_when_inference_fails(self):
  self.cloud.model_pass=False;self.assertFalse(execute(self.cfg,Request('read-probe',True),self.ledger,self.cloud)['passed']);self.assertIn('/lifecycle/',load_ledger(self.ledger,self.cfg)['checkpoint']['archive_key']);self.assertLess(self.cloud.calls.index('refresh'),self.cloud.calls.index('inference'))
 def test_unconfirmed_termination_preserves_ids_and_blocks_new_run(self):
  self.cloud.fail='kill';r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);self.assertFalse(r['passed']);s=load_ledger(self.ledger,self.cfg);self.assertEqual(s['sandbox_id'],'sbx-test');self.assertIn('key_id',s);self.assertFalse(execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)['passed']);self.assertEqual(self.cloud.calls.count('create'),1)
 def test_recover_cleans_live_run_without_replaying(self):
  self.cloud.fail='kill';execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);self.cloud.fail=None;before=self.cloud.calls.count('inference');r=recover(self.cfg,self.ledger,self.cloud);self.assertTrue(r['passed']);self.assertEqual(self.cloud.calls.count('inference'),before);self.assertNotIn('sandbox_id',load_ledger(self.ledger,self.cfg))
 def test_cleanup_key_failure_is_recoverable(self):
  self.cloud.fail='key-cleanup';self.assertFalse(execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)['passed']);self.assertIn('key_id',load_ledger(self.ledger,self.cfg));self.cloud.fail=None;self.assertTrue(recover(self.cfg,self.ledger,self.cloud)['passed'])
