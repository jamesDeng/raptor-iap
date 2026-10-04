import contextlib,json,multiprocessing,os,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
from tools.sandbox_lifecycle.config import load_config,load_request,validate_checkpoint
from tools.sandbox_lifecycle.state import account_lock,load_ledger,save_ledger

def config_data():
 return dict(account_id="1234567890123456",region="ap-southeast-1",team_id="team-test",template_id="template-test",bucket="test-credential-bucket",bucket_prefix="auth",volume_name="test-volume",volume_id="volume-test",execution_role_arn="acs:ram::1234567890123456:role/test",bootstrap_checkpoint=dict(archive_key="auth/checkpoint-5.tgz",checksum_key="auth/checkpoint-5.sha256",sha256="a"*64,bytes=200,pi_version="0.99.2"),model="gpt-5.6-luna",sandbox_seconds=900,model_seconds=90,max_turns=3,max_output_tokens=1024)
def child_lock(root,q):
 try:
  with account_lock(Path(root),"1234567890123456"):q.put("acquired")
 except ValueError as e:q.put(str(e))
class StateTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name).resolve()
 def cfg(self,data=None):
  p=self.root/'cfg.json';p.write_text(json.dumps(data or config_data()));return load_config(p)
 def test_wrong_region_role_account_or_unknown_secret_field_rejected(self):
  for key,value in [('region','cn-hangzhou'),('execution_role_arn','acs:ram::9999999999999999:role/test'),('access_token','synthetic'),('bucket_prefix','../outside')]:
   d=config_data();d[key]=value
   with self.subTest(key=key),self.assertRaises(ValueError):self.cfg(d)
 def test_boolean_or_excessive_limits_rejected(self):
  for key,value in [('max_turns',True),('model_seconds',91),('sandbox_seconds',901),('max_output_tokens',1025),('max_turns',0)]:
   d=config_data();d[key]=value
   with self.subTest(key=key),self.assertRaises(ValueError):self.cfg(d)
 def test_request_rejects_unknown_fields(self):
  p=self.root/'request.json';p.write_text('{"kind":"read-probe","force_refresh":false}');self.assertFalse(load_request(p).force_refresh)
  p.write_text('{"kind":"shell","command":"echo x"}')
  with self.assertRaises(ValueError):load_request(p)
 def test_second_process_cannot_acquire_same_account_lock(self):
  ctx=multiprocessing.get_context('fork');q=ctx.Queue()
  with account_lock(self.root,"1234567890123456"):
   child=ctx.Process(target=child_lock,args=(str(self.root),q));child.start();child.join(4);self.assertEqual(q.get(timeout=1),'Busy')
 def test_fingerprint_blocks_foreign_ledger(self):
  c=self.cfg();p=self.root/'state/ledger.json';s=load_ledger(p,c);save_ledger(p,s)
  d=config_data();d['bucket']='other-bucket'
  with self.assertRaisesRegex(ValueError,'ForeignLedger'):load_ledger(p,self.cfg(d))
 def test_failed_replace_preserves_previous_checkpoint(self):
  c=self.cfg();p=self.root/'state/ledger.json';s=load_ledger(p,c);save_ledger(p,s)
  with patch('os.replace',side_effect=OSError('injected')):
   with self.assertRaises(OSError):save_ledger(p,dict(s,phase='creating'))
  self.assertEqual(load_ledger(p,c)['phase'],'idle');self.assertEqual(len(list(p.parent.iterdir())),1)
 def test_symlink_state_path_rejected(self):
  target=self.root/'outside';target.mkdir();p=self.root/'state';p.symlink_to(target,target_is_directory=True)
  with self.assertRaises(ValueError):save_ledger(p/'ledger.json',load_ledger(self.root/'missing',self.cfg()))
 def test_state_permissions(self):
  c=self.cfg();p=self.root/'state/ledger.json';save_ledger(p,load_ledger(p,c));self.assertEqual(p.stat().st_mode&511,384);self.assertEqual(p.parent.stat().st_mode&511,448)
 def test_checkpoint_cannot_escape_bucket_prefix(self):
  c=self.cfg();d=config_data()['bootstrap_checkpoint'];d['archive_key']='other/state.tgz'
  with self.assertRaises(ValueError):validate_checkpoint(d,c,bootstrap=True)
 def test_corrupt_ledger_never_becomes_idle(self):
  p=self.root/'ledger';p.write_text('{')
  with self.assertRaises(ValueError):load_ledger(p,self.cfg())
