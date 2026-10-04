"""Strict nonsecret runtime contracts."""
from dataclasses import dataclass,asdict
from pathlib import Path,PurePosixPath
import hashlib,json,re
LIMIT=16*1024*1024
@dataclass(frozen=True)
class CheckpointRef:
 archive_key:str
 checksum_key:str
 sha256:str
 bytes:int
 pi_version:str
@dataclass(frozen=True)
class Request:
 kind:str
 force_refresh:bool=False
@dataclass(frozen=True)
class Config:
 account_id:str
 region:str
 team_id:str
 template_id:str
 bucket:str
 bucket_prefix:str
 volume_name:str
 volume_id:str
 execution_role_arn:str
 bootstrap_checkpoint:CheckpointRef
 model:str='gpt-5.6-luna'
 sandbox_seconds:int=900
 model_seconds:int=90
 max_turns:int=3
 max_output_tokens:int=1024
 @property
 def fingerprint(self):return hashlib.sha256(json.dumps(asdict(self),sort_keys=True).encode()).hexdigest()
def _key(v):
 return isinstance(v,str) and bool(re.fullmatch(r'[a-zA-Z0-9_./-]{1,256}',v)) and not v.startswith('/') and all(x not in ('','.', '..') for x in v.split('/'))
def validate_checkpoint(value,cfg,bootstrap=False):
 try:
  if set(value)!=set(CheckpointRef.__dataclass_fields__):raise ValueError()
  ref=CheckpointRef(**value)
  prefix=cfg.bucket_prefix+'/' if bootstrap else cfg.bucket_prefix+'/lifecycle/'
  if not all(_key(v) and v.startswith(prefix) for v in (ref.archive_key,ref.checksum_key)):raise ValueError()
  if not ref.archive_key.endswith('.tgz') or ref.checksum_key!=ref.archive_key[:-4]+'.sha256':raise ValueError()
  if not re.fullmatch('[a-f0-9]{64}',ref.sha256) or type(ref.bytes) is not int or not 0<ref.bytes<=LIMIT or ref.pi_version!='0.99.2':raise ValueError()
  return ref
 except (TypeError,KeyError,ValueError,AttributeError):raise ValueError('InvalidCheckpoint') from None
def load_config(path):
 try:
  raw=json.loads(Path(path).read_text())
  if set(raw)!=set(Config.__dataclass_fields__):raise ValueError()
  c=Config(**raw)
  for k in ('account_id','team_id','template_id','bucket','volume_name','volume_id','execution_role_arn'):
   if not isinstance(getattr(c,k),str) or not re.fullmatch('[a-zA-Z0-9:_./-]{1,256}',getattr(c,k)):raise ValueError()
  if not re.fullmatch('[0-9]{12,20}',c.account_id) or c.region!='ap-southeast-1' or c.model!='gpt-5.6-luna' or not _key(c.bucket_prefix):raise ValueError()
  if not c.execution_role_arn.startswith('acs:ram::'+c.account_id+':role/'):raise ValueError()
  for k,cap in [('sandbox_seconds',900),('model_seconds',90),('max_turns',3),('max_output_tokens',1024)]:
   if type(getattr(c,k)) is not int or not 1<=getattr(c,k)<=cap:raise ValueError()
  object.__setattr__(c,'bootstrap_checkpoint',validate_checkpoint(raw['bootstrap_checkpoint'],c,True));return c
 except (TypeError,KeyError,ValueError,AttributeError):raise ValueError('InvalidConfig') from None
def load_request(path):
 try:
  raw=json.loads(Path(path).read_text())
  if not isinstance(raw,dict) or set(raw)!={'kind','force_refresh'} or raw['kind']!='read-probe' or type(raw['force_refresh']) is not bool:raise ValueError()
  return Request(**raw)
 except (TypeError,KeyError,ValueError):raise ValueError('InvalidRequest') from None
