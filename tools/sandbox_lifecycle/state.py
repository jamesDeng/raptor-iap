"""Protected single-host ownership and crash-safe nonsecret ledger."""
from contextlib import contextmanager
from dataclasses import asdict
from pathlib import Path
import fcntl,json,os,tempfile,re
from .config import validate_checkpoint,checkpoint_shape
from .contracts import validate_result,ERRORS
FIELDS={'schema_version','fingerprint','phase','attempt_id','sandbox_id','key_id','key_name','key_expiry','pending','checkpoint','candidate','request_outcome','failure','result','refresh_result','job_generation','job_phase','cleanup_key_ids','persistence_error'}
FIELDS.add('task_binding')
PHASES={'idle','creating-key','creating','restoring','running','checkpointing','terminating','finished','blocked'}
def safe_path(path):
 p=Path(path).absolute()
 if any(x.is_symlink() for x in (p,*p.parents)):raise ValueError('UnsafeStatePath')
 return p
def protect_dir(path):
 p=safe_path(path);p.mkdir(parents=True,exist_ok=True,mode=0o700);os.chmod(p,0o700);return p
def validate_ledger(s):
 if not isinstance(s,dict) or set(s)-FIELDS or s.get('schema_version')!=1 or s.get('phase') not in PHASES:raise ValueError('InvalidLedger')
 if not re.fullmatch('[a-f0-9]{64}',s.get('fingerprint','')):raise ValueError('InvalidLedger')
 for k in ('attempt_id','sandbox_id','key_id','key_name','key_expiry','pending','request_outcome','failure','job_generation','job_phase'):
  if k in s and (not isinstance(s[k],str) or not re.fullmatch('[a-zA-Z0-9_:./+-]{1,256}',s[k])):raise ValueError('InvalidLedger')
 if 'cleanup_key_ids' in s and (not isinstance(s['cleanup_key_ids'],list) or any(not isinstance(x,str) or not re.fullmatch('[a-zA-Z0-9-]{1,128}',x) for x in s['cleanup_key_ids'])):raise ValueError('InvalidLedger')
 if 'persistence_error' in s and type(s['persistence_error']) is not bool:raise ValueError('InvalidLedger')
 if 'task_binding' in s:
  from tools.task_store.models import TaskBinding
  try:
   binding=TaskBinding(**s['task_binding'])
   if s.get('attempt_id')!=binding.attempt_id:raise ValueError()
  except (ValueError,TypeError):raise ValueError('InvalidLedger') from None
 for key,values in [('failure',ERRORS),('request_outcome',{'completed','failed','unknown'}),('pending',{'key','sandbox'}),('job_phase',{'restore','refresh','inference'})]:
  if key in s and s[key] not in values:raise ValueError('InvalidLedger')
 for key in ('checkpoint','candidate'):
  if key in s:checkpoint_shape(s[key])
 for result_field in ('result','refresh_result'):
  if result_field not in s:continue
  try:validate_result(s[result_field], 'refresh' if result_field=='refresh_result' else None)
  except (ValueError,TypeError):raise ValueError('InvalidLedger') from None
 return s
def load_ledger(path,cfg):
 p=safe_path(path)
 if not p.exists():return {'schema_version':1,'fingerprint':cfg.fingerprint,'phase':'idle','checkpoint':asdict(cfg.bootstrap_checkpoint)}
 try:s=validate_ledger(json.loads(p.read_text()))
 except (TypeError,ValueError):raise ValueError('InvalidLedger') from None
 if s['fingerprint']!=cfg.fingerprint:raise ValueError('ForeignLedger')
 for k in ('checkpoint','candidate'):
  if k in s:validate_checkpoint(s[k],cfg,True)
 for key in ('result','refresh_result'):
  if key in s:
   try:validate_result(s[key],cfg=cfg)
   except (TypeError,ValueError):raise ValueError('InvalidLedger') from None
 return s

def save_ledger(path,value):
 validate_ledger(value);p=safe_path(path);protect_dir(p.parent)
 fd,name=tempfile.mkstemp(prefix='.ledger-',dir=p.parent)
 try:
  with os.fdopen(fd,'w') as f:
   os.fchmod(f.fileno(),0o600);json.dump(value,f,sort_keys=True);f.flush();os.fsync(f.fileno())
  os.replace(name,p)
  d=os.open(p.parent,os.O_RDONLY)
  try:os.fsync(d)
  finally:os.close(d)
 finally:
  if os.path.exists(name):os.unlink(name)
@contextmanager
def account_lock(state_root,account_id):
 if not re.fullmatch('[0-9]{12,20}',account_id):raise ValueError('InvalidAccount')
 parent=protect_dir(Path(state_root)/account_id);path=safe_path(parent/'account.lock')
 fd=os.open(path,os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
 try:
  try:fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
  except BlockingIOError:raise ValueError('Busy') from None
  yield parent/'ledger.json'
 finally:os.close(fd)
