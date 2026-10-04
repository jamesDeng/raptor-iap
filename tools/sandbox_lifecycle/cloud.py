"""Aliyun SDK boundary: secrets remain in memory; errors are fixed enums."""
from dataclasses import dataclass,field,asdict
from pathlib import Path
import json,os,re,shlex,subprocess,uuid
from .config import validate_checkpoint
class CloudError(Exception):pass
@dataclass
class KeyHandle:
 id:str
 value:str=field(repr=False)

def safe_result(raw,cfg,phase):
 from .contracts import validate_result
 try:return validate_result(raw,phase,cfg)
 except (ValueError,TypeError,KeyError):raise CloudError('InvalidRunnerResult') from None

class Cloud:
 def __init__(self,cfg,client,sts,oss,sandbox_cls):self.cfg=cfg;self.client=client;self.sts=sts;self.oss=oss;self.sandbox_cls=sandbox_cls
 @classmethod
 def from_operator_profile(cls,cfg,profile):
  try:
   from alibabacloud_credentials.client import Client as Credentials
   from alibabacloud_credentials.models import Config as CredentialConfig
   from alibabacloud_tea_openapi.models import Config as SDKConfig
   from alibabacloud_fcsandbox20260509.client import Client
   from alibabacloud_sts20150401.client import Client as STS
   from e2b import Sandbox
   import oss2
   env=dict(os.environ)
   for name in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','http_proxy','https_proxy','all_proxy'):env.pop(name,None);os.environ.pop(name,None)
   subprocess.run(['aliyun','sts','GetCallerIdentity','--profile',profile,'--region',cfg.region],env=env,capture_output=True,check=True,timeout=30)
   out=subprocess.run(['aliyun','configure','get','--profile',profile],env=env,capture_output=True,text=True,check=True,timeout=10)
   c=json.loads(out.stdout);credentials=Credentials(CredentialConfig(type='sts',access_key_id=c['access_key_id'],access_key_secret=c['access_key_secret'],security_token=c['sts_token']))
   client=Client(SDKConfig(credential=credentials,region_id=cfg.region,endpoint='fcsandbox.'+cfg.region+'.aliyuncs.com',connect_timeout=10000,read_timeout=30000))
   sts=STS(SDKConfig(credential=credentials,endpoint='sts.'+cfg.region+'.aliyuncs.com'))
   oss=oss2.Bucket(oss2.StsAuth(c['access_key_id'],c['access_key_secret'],c['sts_token']),'https://oss-'+cfg.region+'.aliyuncs.com',cfg.bucket)
   cloud=cls(cfg,client,sts,oss,Sandbox);cloud.assert_identity();return cloud
  except Exception:raise CloudError('OperatorAuthenticationFailed') from None
 def connection(self,key):return dict(api_key=key.value,api_url='https://api.ap-southeast-1.e2b.fc.aliyuncs.com',domain='ap-southeast-1.e2b.fc.aliyuncs.com',request_timeout=30)
 def assert_identity(self):
  try:
   if self.sts.get_caller_identity().body.account_id!=self.cfg.account_id:raise ValueError()
  except Exception:raise CloudError('AccountMismatchOrUnavailable') from None
 def assert_storage(self):
  from alibabacloud_fcsandbox20260509 import models as m
  try:
   self.assert_identity()
   v=self.client.get_volume(self.cfg.volume_id,m.GetVolumeRequest(team_id=self.cfg.team_id)).body.volume
   expected_endpoint='https://oss-'+self.cfg.region+'.aliyuncs.com'
   oss=v.oss_volume_config
   if (v.status!='AVAILABLE' or v.volume_name!=self.cfg.volume_name
       or v.volume_id!=self.cfg.volume_id or v.user_id!=self.cfg.account_id
       or v.team_id!=self.cfg.team_id or v.storage_class!='OSS' or oss is None
       or oss.bucket_name!=self.cfg.bucket or oss.bucket_path!='/'+self.cfg.bucket_prefix
       or oss.endpoint!=expected_endpoint or oss.read_only is not False):raise ValueError()
   # Existing OSS Volumes may omit mount_config; create explicitly supplies the role.
   if v.mount_config is not None and v.mount_config.role!=self.cfg.execution_role_arn:raise ValueError()
   self.verify_checkpoint(self.cfg.bootstrap_checkpoint)
  except Exception:raise CloudError('StoragePreflightFailed') from None
 def create_key(self,name,expires_at):
  from alibabacloud_fcsandbox20260509 import models as m
  try:
   k=self.client.create_api_key(m.CreateApiKeyRequest(body=m.CreateApiKeyInput(api_key_name=name,team_id=self.cfg.team_id,expire_time=expires_at))).body.api_key
   if not k.api_key_id or not k.api_key_value:raise ValueError()
   return KeyHandle(k.api_key_id,k.api_key_value)
  except Exception:raise CloudError('KeyCreateUnconfirmed') from None
 def list_keys(self):
  from alibabacloud_fcsandbox20260509 import models as m
  try:
   body=self.client.list_api_keys(m.ListApiKeysRequest(team_id=self.cfg.team_id,page_size=100)).body.to_map()
   if body.get('total',0)>100 or not isinstance(body.get('apiKeys'),list):raise ValueError()
   return body['apiKeys']
  except Exception:raise CloudError('KeyInventoryUnavailable') from None
 def remove_key(self,key_id):
  from alibabacloud_fcsandbox20260509 import models as m
  try:
   if not any(x['apiKeyID']==key_id for x in self.list_keys()):return
   self.client.update_api_key(key_id,m.UpdateApiKeyRequest(body=m.UpdateApiKeyInput(status='inactive')))
   self.client.delete_api_key(key_id,m.DeleteApiKeyRequest())
   if any(x['apiKeyID']==key_id for x in self.list_keys()):raise ValueError()
  except Exception:raise CloudError('KeyCleanupUnconfirmed') from None
 def resolve_key_intent(self,name):
  keys=[x['apiKeyID'] for x in self.list_keys() if x.get('apiKeyName')==name]
  for identity in keys:self.remove_key(identity)
 def create_sandbox(self,attempt_id,key):
  try:
   sb=self.sandbox_cls.create(template=self.cfg.template_id,timeout=self.cfg.sandbox_seconds,volume_mounts={'/mnt/oss':self.cfg.volume_name},metadata={'fc.sandbox.auth.role':self.cfg.execution_role_arn,'raptor.attempt':attempt_id},**self.connection(key));return sb.sandbox_id
  except Exception:raise CloudError('SandboxCreateUnconfirmed') from None
 def sandbox(self,identity,key):return self.sandbox_cls.connect(identity,**self.connection(key))
 def prepare(self,identity,key):
  root=Path(__file__).resolve().parents[2]/'components/agent-harness'
  command="""set -eu
+cd /tmp
+curl -fsSLO https://nodejs.org/dist/v22.23.3/node-v22.23.3-linux-x64.tar.xz
+curl -fsSL https://nodejs.org/dist/v22.23.3/SHASUMS256.txt | awk '$2=="node-v22.23.3-linux-x64.tar.xz"' > node.sha
+sha256sum -c node.sha
+tar -xf node-v22.23.3-linux-x64.tar.xz
+export PATH=/tmp/node-v22.23.3-linux-x64/bin:$PATH
+cd /tmp/raptor-harness
+npm ci --ignore-scripts --no-audit --no-fund >/tmp/raptor-install.log 2>&1
+""".replace('\n+','\n')
  try:
   sb=self.sandbox(identity,key);sb.commands.run('mkdir -m 700 -p /tmp/raptor-harness',timeout=20)
   for name in ('archive.py','checkpoint.mjs','pi-adapter.mjs','runner.mjs','package.json','package-lock.json'):sb.files.write('/tmp/raptor-harness/'+name,(root/name).read_text())
   sb.commands.run(command,timeout=180)
  except Exception:raise CloudError('SandboxPreparationFailed') from None
 def run_job(self,identity,job,key):
  try:
   sb=self.sandbox(identity,key);generation=job['generation']
   if not re.fullmatch('[a-f0-9-]{36}',generation):raise ValueError()
   job={**job,'state_root':'/tmp/raptor-pi-live','mount_root':'/mnt/oss','prefix':self.cfg.bucket_prefix}
   stem='/tmp/raptor-harness/job-'+generation
   sb.files.write(stem+'.json',json.dumps(job));sb.commands.run('chmod 600 '+shlex.quote(stem+'.json'),timeout=10)
   try:sb.commands.run('/tmp/node-v22.23.3-linux-x64/bin/node /tmp/raptor-harness/runner.mjs '+shlex.quote(stem+'.json')+' '+shlex.quote(stem+'-result.json'),timeout=self.cfg.model_seconds+45)
   except Exception:pass
   raw=json.loads(sb.files.read(stem+'-result.json'));return safe_result(raw,self.cfg,job['phase'])
  except Exception as e:
   if isinstance(e,CloudError):raise
   raise CloudError('RunnerOutcomeUnknown') from None
 def read_completed_result(self,identity,key,generation=None,phase='inference'):
  from e2b.exceptions import NotFoundException,SandboxNotFoundException
  if not generation:return None
  if not re.fullmatch('[a-f0-9-]{36}',generation):raise CloudError('InvalidJobReference')
  try:return safe_result(json.loads(self.sandbox(identity,key).files.read('/tmp/raptor-harness/job-'+generation+'-result.json')),self.cfg,phase)
  except (NotFoundException,SandboxNotFoundException):return None
  except Exception:raise CloudError('RecoveryResultUnavailable') from None
 def terminate_and_confirm(self,identity,key):
  from e2b.exceptions import NotFoundException,SandboxNotFoundException
  try:
   try:self.sandbox_cls.kill(identity,**self.connection(key))
   except (NotFoundException,SandboxNotFoundException):pass
   try:self.sandbox_cls.get_info(identity,**self.connection(key))
   except (NotFoundException,SandboxNotFoundException):return
   raise ValueError()
  except Exception:raise CloudError('TerminationUnconfirmed') from None
 def verify_checkpoint(self,ref):
  try:
   validate_checkpoint(asdict(ref),self.cfg,True)
   if self.oss.get_bucket_encryption().sse_algorithm!='AES256':raise ValueError()
   for name,size in ((ref.archive_key,ref.bytes),(ref.checksum_key,64)):
    h=self.oss.head_object(name)
    if h.headers.get('x-oss-server-side-encryption')!='AES256' or h.content_length!=size:raise ValueError()
   if self.oss.get_object(ref.checksum_key).read(65).decode().strip()!=ref.sha256:raise ValueError()
  except Exception:raise CloudError('CheckpointVerificationFailed') from None
