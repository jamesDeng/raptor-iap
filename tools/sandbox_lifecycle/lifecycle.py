"""Sequential lifecycle; unknown cloud outcomes never authorize replacement."""
from dataclasses import asdict
from datetime import datetime,timedelta,timezone
from pathlib import Path
import uuid
from .config import validate_checkpoint
from .state import account_lock,load_ledger,save_ledger
from .cloud import CloudError,safe_result
ERRORS={'AccountMismatchOrUnavailable','StoragePreflightFailed','KeyCreateUnconfirmed','KeyCleanupUnconfirmed','KeyInventoryUnavailable','SandboxCreateUnconfirmed','SandboxPreparationFailed','RunnerOutcomeUnknown','InvalidRunnerResult','TerminationUnconfirmed','CheckpointVerificationFailed','RecoveryResultUnavailable','NeedsSignIn','RunnerFailed','CheckpointFailed','ModelFailed','TurnLimit','Timeout','InvalidJob'}
def error_code(e):return str(e) if isinstance(e,CloudError) and str(e) in ERRORS else 'ControllerFailed'
def output(s,passed=False):
 return {'passed':passed,'phase':s['phase'],'request_outcome':s.get('request_outcome','unknown'),'checkpoint':s['checkpoint'],'cleanup_confirmed':not any(k in s for k in ('sandbox_id','key_id','cleanup_key_ids','pending')),'error':s.get('failure'),**({'result':s['result']} if 'result' in s else {})}
def locked_path(cfg,path):
 path=Path(path).absolute();root=path.parent.parent
 if path!=root/cfg.account_id/'ledger.json':raise ValueError('InvalidLedgerLocation')
 return account_lock(root,cfg.account_id)
def new_key(s,path,cloud):
 if 'key_id' in s:
  s['cleanup_key_ids']=list(dict.fromkeys(s.get('cleanup_key_ids',[])+[s.pop('key_id')]))
 s['key_name']='raptor-lifecycle-'+uuid.uuid4().hex;s['key_expiry']=(datetime.now(timezone.utc)+timedelta(minutes=60)).strftime('%Y-%m-%dT%H:%M:%SZ');s['phase']='creating-key';s['pending']='key';save_ledger(path,s)
 key=cloud.create_key(s['key_name'],s['key_expiry']);s['key_id']=key.id;s.pop('pending');save_ledger(path,s);return key

def cleanup(s,path,cloud,key):
 if 'sandbox_id' in s:
  s['phase']='terminating';save_ledger(path,s)
  cloud.terminate_and_confirm(s['sandbox_id'],key);s.pop('sandbox_id');save_ledger(path,s)
 ids=s.get('cleanup_key_ids',[])+([s['key_id']] if 'key_id' in s else [])
 for identity in list(dict.fromkeys(ids)):
  cloud.remove_key(identity)
  if s.get('key_id')==identity:s.pop('key_id')
  if identity in s.get('cleanup_key_ids',[]):s['cleanup_key_ids'].remove(identity)
  if not s.get('cleanup_key_ids'):s.pop('cleanup_key_ids',None)
  save_ledger(path,s)
 if s.get('pending')=='key':cloud.resolve_key_intent(s['key_name']);s.pop('pending');save_ledger(path,s)

def publish(s,path,cloud,result,cfg):
 if 'checkpoint' in result:
  ref=validate_checkpoint(result['checkpoint'],cfg);s['candidate']=asdict(ref);save_ledger(path,s)
  cloud.verify_checkpoint(ref);s['checkpoint']=asdict(ref);s.pop('candidate');save_ledger(path,s)

def recover_locked(cfg,path,cloud,s):
 if s.get('pending')=='sandbox' and 'sandbox_id' not in s:
  s['phase']='blocked';s['failure']='SandboxCreateUnconfirmed';save_ledger(path,s);return output(s)
 key=None
 try:
  cloud.assert_identity()
  if s.get('pending')=='key':cloud.resolve_key_intent(s['key_name']);s.pop('pending');save_ledger(path,s)
  if 'sandbox_id' in s:
   key=new_key(s,path,cloud)
   # Only a completed runner result may be adopted; missing result never triggers replay.
   result=cloud.read_completed_result(s['sandbox_id'],key,s.get('job_generation'),s.get('job_phase','inference'))
   if result:
    result=safe_result(result,cfg,s.get('job_phase','inference'));s['result']=result;s['request_outcome']='completed' if result['passed'] else 'failed';save_ledger(path,s)
    if 'checkpoint' in result:s['candidate']=result['checkpoint'];save_ledger(path,s)
  cleanup(s,path,cloud,key)
  if 'candidate' in s:
   cloud.verify_checkpoint(validate_checkpoint(s['candidate'],cfg));s['checkpoint']=s.pop('candidate');save_ledger(path,s)
  s['phase']='finished';s.pop('failure',None);save_ledger(path,s);return output(s,True)
 except Exception as e:
  s['phase']='blocked';s['failure']=error_code(e);save_ledger(path,s);return output(s)

def execute(cfg,request,ledger_path,cloud):
 with locked_path(cfg,ledger_path) as path:
  s=load_ledger(path,cfg)
  if any(k in s for k in ('sandbox_id','key_id','cleanup_key_ids','pending')):
   recovered=recover_locked(cfg,path,cloud,s)
   if not recovered['passed']:return recovered
  if s['phase']=='blocked':return output(s)
  s={k:v for k,v in s.items() if k in ('schema_version','fingerprint','checkpoint')};s.update(phase='idle',attempt_id=str(uuid.uuid4()),request_outcome='unknown');save_ledger(path,s)
  key=None;passed=False
  try:
   cloud.assert_storage();cloud.verify_checkpoint(validate_checkpoint(s['checkpoint'],cfg,True))
   key=new_key(s,path,cloud);s['phase']='creating';s['pending']='sandbox';save_ledger(path,s)
   s['sandbox_id']=cloud.create_sandbox(s['attempt_id'],key);s.pop('pending');save_ledger(path,s)
   cloud.prepare(s['sandbox_id'],key)
   phases=['restore']+(['refresh'] if request.force_refresh else [])+['inference']
   for phase in phases:
    s['phase']='restoring' if phase=='restore' else 'running';s['job_generation']=str(uuid.uuid4());s['job_phase']=phase;save_ledger(path,s)
    job={'phase':phase,'generation':s['job_generation'],'reference':s['checkpoint'],'limits':{'model_seconds':cfg.model_seconds,'max_turns':cfg.max_turns,'max_output_tokens':cfg.max_output_tokens}}
    result=safe_result(cloud.run_job(s['sandbox_id'],job,key),cfg,phase);s['result']=result;save_ledger(path,s)
    if phase=='inference':s['request_outcome']='completed' if result['passed'] else 'failed';save_ledger(path,s)
    if phase!='restore':s['phase']='checkpointing';save_ledger(path,s);publish(s,path,cloud,result,cfg)
    if not result['passed']:s['request_outcome']='failed';raise CloudError(result.get('error','ModelFailed'))
   s['request_outcome']='completed';passed=True;save_ledger(path,s)
  except Exception as e:s['failure']=error_code(e);s['phase']='blocked';save_ledger(path,s)
  try:cleanup(s,path,cloud,key)
  except Exception as e:s['failure']=error_code(e);s['phase']='blocked';save_ledger(path,s);return output(s)
  if 'pending' in s:s['phase']='blocked'
  else:s['phase']='finished'
  save_ledger(path,s);return output(s,passed and 'failure' not in s and s['phase']=='finished')

def recover(cfg,ledger_path,cloud):
 with locked_path(cfg,ledger_path) as path:
  s=load_ledger(path,cfg);return recover_locked(cfg,path,cloud,s)
def status(cfg,ledger_path,cloud):
 s=load_ledger(ledger_path,cfg);cloud.assert_identity();cloud.verify_checkpoint(validate_checkpoint(s['checkpoint'],cfg,True));return output(s,not any(k in s for k in ('sandbox_id','key_id','pending','cleanup_key_ids')))
