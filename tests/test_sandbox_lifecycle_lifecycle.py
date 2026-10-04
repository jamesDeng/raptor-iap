import json,tempfile,unittest
from unittest.mock import patch
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

 def test_force_refresh_evidence_retained_in_final_report(self):
  r=execute(self.cfg,Request("read-probe",True),self.ledger,self.cloud)
  self.assertTrue(r["refresh_result"]["refresh_succeeded"])
  self.assertTrue(r["refresh_result"]["refresh_token_changed"])
  self.assertEqual(r["result"]["phase"],"inference")

 def test_recovery_read_failure_still_cleans_compute_and_keys(self):
  self.cloud.fail='kill';execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);self.cloud.fail=None
  with patch.object(self.cloud,'read_completed_result',side_effect=CloudError('RecoveryResultUnavailable')):
   before=len(self.cloud.calls);r=recover(self.cfg,self.ledger,self.cloud)
  self.assertFalse(r['passed']);self.assertIn('kill',self.cloud.calls[before:]);self.assertIn('remove-key',self.cloud.calls[before:]);self.assertTrue(r['cleanup_confirmed'])
 def test_persistent_write_failure_after_resource_creation_still_cleans(self):
  for boundary in ('key_id','sandbox_id'):
   with self.subTest(boundary=boundary):
    if self.ledger.exists():self.ledger.unlink()
    self.cloud.calls=[];started=False
    def failing_save(path,value):
     nonlocal started
     started|=boundary in value
     if started:raise OSError('synthetic-secret-disk-error')
     save_ledger(path,value)
    with patch('tools.sandbox_lifecycle.lifecycle.save_ledger',side_effect=failing_save):
     r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)
    self.assertFalse(r['passed']);self.assertIn('remove-key',self.cloud.calls)
    if boundary=='sandbox_id':self.assertIn('kill',self.cloud.calls)
    self.assertTrue(r['cleanup_confirmed']);self.assertNotIn('synthetic-secret',json.dumps(r))
 def test_failed_publication_does_not_select_candidate_during_cleanup_saves(self):
  once=False
  def fail_publication(path,value):
   nonlocal once
   if '/lifecycle/' in value['checkpoint']['archive_key'] and not once:
    once=True;raise OSError('injected publication')
   save_ledger(path,value)
  with patch('tools.sandbox_lifecycle.lifecycle.save_ledger',side_effect=fail_publication):r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)
  self.assertFalse(r['passed']);self.assertEqual(load_ledger(self.ledger,self.cfg)['checkpoint']['archive_key'],'auth/checkpoint-5.tgz')
 def test_only_inference_recovery_can_establish_request_completion(self):
  for phase in ('restore','refresh','inference'):
   with self.subTest(phase=phase):
    s=load_ledger(self.ledger,self.cfg);s.update(phase='running',sandbox_id='sbx-test',key_id='old-key',job_generation='11111111-1111-4111-8111-111111111111',job_phase=phase,request_outcome='unknown');save_ledger(self.ledger,s)
    self.cloud.pending_result=self.cloud.run_job('sbx-test',{'phase':phase,'generation':s['job_generation']},None)
    r=recover(self.cfg,self.ledger,self.cloud);self.assertTrue(r['passed']);self.assertEqual(r['request_outcome'],'completed' if phase=='inference' else 'unknown')
 def test_complete_answer_failed_checkpoint_generation_keeps_request_completed(self):
  original=self.cloud.run_job
  def run(*args):
   r=original(*args)
   if r['phase']=='inference':r.pop('checkpoint');r.update(passed=False,error='CheckpointFailed')
   return r
  with patch.object(self.cloud,'run_job',side_effect=run):r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)
  self.assertFalse(r['passed']);self.assertEqual(r['request_outcome'],'completed');self.assertEqual(r['checkpoint']['archive_key'],'auth/checkpoint-5.tgz')
 def test_incomplete_success_result_fails_and_retains_old_checkpoint(self):
  original=self.cloud.run_job
  def run(*args):
   r=original(*args)
   if r['phase']=='inference':r.pop('checkpoint')
   return r
  with patch.object(self.cloud,'run_job',side_effect=run):r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)
  self.assertFalse(r['passed']);self.assertEqual(r['checkpoint']['archive_key'],'auth/checkpoint-5.tgz');self.assertTrue(r['cleanup_confirmed'])
 def test_uncertain_key_create_reconciles_unique_intent_before_replacement(self):
  with patch.object(self.cloud,'create_key',side_effect=CloudError('KeyCreateUnconfirmed')):r=execute(self.cfg,Request('read-probe'),self.ledger,self.cloud)
  self.assertFalse(r['passed']);self.assertNotIn('create',self.cloud.calls);self.assertIn('resolve-key',self.cloud.calls)

 def test_recovery_selects_verified_descriptor_only_after_confirmed_termination(self):
  self.cloud.fail='kill';execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);self.cloud.fail=None
  s=load_ledger(self.ledger,self.cfg);s['checkpoint']=dict(self.cfg.bootstrap_checkpoint.__dict__);save_ledger(self.ledger,s)
  self.cloud.pending_result=self.cloud.run_job('sbx-test',{'phase':'inference','generation':'44444444-4444-4444-8444-444444444444'},None)
  before=len(self.cloud.calls);r=recover(self.cfg,self.ledger,self.cloud)
  self.assertTrue(r['passed']);self.assertEqual(r['checkpoint'],self.cloud.pending_result['checkpoint'])
  calls=self.cloud.calls[before:];self.assertLess(calls.index('kill'),calls.index('verify'))
 def test_recovery_failed_publication_keeps_previous_checkpoint(self):
  self.cloud.fail='kill';execute(self.cfg,Request('read-probe'),self.ledger,self.cloud);self.cloud.fail=None
  s=load_ledger(self.ledger,self.cfg);s['checkpoint']=dict(self.cfg.bootstrap_checkpoint.__dict__);save_ledger(self.ledger,s)
  self.cloud.pending_result=self.cloud.run_job('sbx-test',{'phase':'inference','generation':'55555555-5555-4555-8555-555555555555'},None)
  once=False
  def fail_publication(path,value):
   nonlocal once
   if '/lifecycle/' in value['checkpoint']['archive_key'] and not once:
    once=True;raise OSError('injected')
   save_ledger(path,value)
  with patch('tools.sandbox_lifecycle.lifecycle.save_ledger',side_effect=fail_publication):r=recover(self.cfg,self.ledger,self.cloud)
  self.assertFalse(r['passed']);self.assertTrue(r['cleanup_confirmed']);self.assertEqual(load_ledger(self.ledger,self.cfg)['checkpoint']['archive_key'],'auth/checkpoint-5.tgz')
