import json,unittest
from types import SimpleNamespace
from test_sandbox_lifecycle_state import config_data
from tools.sandbox_lifecycle.config import Config,CheckpointRef
from tools.sandbox_lifecycle.cloud import Cloud,KeyHandle,CloudError,safe_result
class Transport:
 def __init__(self):self.calls=[];self.fail=False;self.keys=[{'apiKeyID':'key-test','apiKeyName':'attempt-test','status':'active','apiKeyMask':'redacted','teamID':'team-test'}]
 def create_api_key(self,request):
  self.calls.append(request.to_map())
  if self.fail:raise RuntimeError('synthetic-secret')
  return SimpleNamespace(body=SimpleNamespace(api_key=SimpleNamespace(api_key_id='key-test',api_key_value='synthetic-control-secret')))
 def list_api_keys(self,request):return SimpleNamespace(body=SimpleNamespace(to_map=lambda:{'apiKeys':self.keys,'total':len(self.keys)}))
 def update_api_key(self,identity,request):self.calls.append(('update',identity,request.to_map()))
 def delete_api_key(self,identity,request):self.keys=[];self.calls.append(('delete',identity))
class SandboxPort:
 calls=[]
 @classmethod
 def create(cls,**kwargs):cls.calls.append(kwargs);return SimpleNamespace(sandbox_id='sbx-test')
class ObjectStore:
 def __init__(self):self.encryption='AES256';self.size=200;self.calls=[]
 def get_bucket_encryption(self):return SimpleNamespace(sse_algorithm='AES256')
 def head_object(self,key):self.calls.append(('head',key));return SimpleNamespace(headers={'x-oss-server-side-encryption':self.encryption,'Content-Length':str(self.size if key.endswith('.tgz') else 64)},content_length=self.size if key.endswith('.tgz') else 64)
 def get_object(self,key):self.calls.append(('get',key));return SimpleNamespace(read=lambda n=None:'a'.encode()*64)
class CloudTests(unittest.TestCase):
 def setUp(self):
  d=config_data();d['bootstrap_checkpoint']=CheckpointRef(**d['bootstrap_checkpoint']);self.cfg=Config(**d);self.transport=Transport();self.oss=ObjectStore();self.cloud=Cloud(self.cfg,self.transport,SimpleNamespace(get_caller_identity=lambda:SimpleNamespace(body=SimpleNamespace(account_id=self.cfg.account_id))),self.oss,SandboxPort)
 def test_actual_sdk_key_payload_and_secret_not_in_repr(self):
  key=self.cloud.create_key('attempt-test','2026-10-04T02:00:00Z');self.assertNotIn('synthetic-control-secret',repr(key));self.assertEqual(self.transport.calls[0]['body']['teamID'],'team-test')
 def test_create_configuration_keeps_control_secret_outside_metadata(self):
  self.assertEqual(self.cloud.create_sandbox('attempt-test',KeyHandle('key','synthetic-control-secret')),'sbx-test');v=SandboxPort.calls[-1];self.assertEqual(v['timeout'],900);self.assertEqual(v['volume_mounts'],{'/mnt/oss':'test-volume'});self.assertEqual(v['metadata']['fc.sandbox.auth.role'],self.cfg.execution_role_arn);self.assertNotIn('synthetic-control-secret',json.dumps(v['metadata']));self.assertEqual(v['api_url'],'https://api.ap-southeast-1.e2b.fc.aliyuncs.com')
 def test_account_mismatch_stops_writes(self):
  self.cloud.sts=SimpleNamespace(get_caller_identity=lambda:SimpleNamespace(body=SimpleNamespace(account_id='other')))
  with self.assertRaises(CloudError):self.cloud.assert_identity()
  self.assertEqual(self.transport.calls,[])
 def test_metadata_verification_never_downloads_auth_archive(self):
  self.cloud.verify_checkpoint(self.cfg.bootstrap_checkpoint);self.assertEqual([k for action,k in self.oss.calls if action=='get'],['auth/checkpoint-5.sha256'])
  for attr,value in [('encryption',None),('size',201)]:
   setattr(self.oss,attr,value)
   with self.assertRaises(CloudError):self.cloud.verify_checkpoint(self.cfg.bootstrap_checkpoint)
   self.oss.encryption='AES256';self.oss.size=200
 def test_raw_sdk_error_is_redacted(self):
  self.transport.fail=True
  with self.assertRaises(CloudError) as e:self.cloud.create_key('attempt','date')
  self.assertNotIn('synthetic-secret',str(e.exception))
 def test_safe_result_rejects_unknown_fields_and_secret_values(self):
  for result in [{'phase':'inference','passed':False,'error':'synthetic-secret'},{'phase':'inference','passed':True,'answer':'synthetic-secret'},{'phase':'inference','passed':True,'usage':[{'input':'synthetic-secret','output':1,'total_tokens':2}]}]:
   with self.assertRaises(CloudError):safe_result(result,self.cfg,'inference')

 def test_cleanup_uses_actual_api_key_id_casing_and_verifies_absence(self):
  self.cloud.remove_key('key-test');self.assertEqual(self.transport.keys,[]);self.assertEqual(self.transport.calls[-1],('delete','key-test'))
