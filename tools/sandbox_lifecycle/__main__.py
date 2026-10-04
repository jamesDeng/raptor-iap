"""Local POC CLI. Default status never starts compute."""
import argparse,json
from pathlib import Path
from .config import load_config,load_request
from .cloud import Cloud,CloudError
from .lifecycle import execute,recover,status,error_code
from .paths import lifecycle_root

def state_root():
 return lifecycle_root()
def main(argv=None):
 parser=argparse.ArgumentParser(description='Run and recover a single-account Pi sandbox lifecycle.')
 parser.add_argument('command',nargs='?',default='status',choices=['status','run','recover']);parser.add_argument('--config',type=Path,required=True);parser.add_argument('--request',type=Path);parser.add_argument('--profile',default='infra-ops-poc')
 args=parser.parse_args(argv)
 try:
  cfg=load_config(args.config);request=None
  if args.command=='run':
   if args.request is None:raise ValueError('InvalidRequest')
   request=load_request(args.request)
  cloud=Cloud.from_operator_profile(cfg,args.profile);ledger=state_root()/cfg.account_id/'ledger.json'
  if args.command=='run':result=execute(cfg,request,ledger,cloud)
  elif args.command=='recover':result=recover(cfg,ledger,cloud)
  else:result=status(cfg,ledger,cloud)
  print(json.dumps(result,sort_keys=True));return 0 if result['passed'] else (2 if args.command=='status' or result.get('error')=='SandboxCreateUnconfirmed' else 1)
 except ValueError as e:
  safe={'InvalidConfig','InvalidRequest','InvalidLedger','ForeignLedger','UnsafeStatePath','InvalidCheckpoint','InvalidAccount','InvalidLedgerLocation','Busy'}
  print(json.dumps({'passed':False,'error':str(e) if str(e) in safe else 'LocalValidationFailed'}));return 2
 except Exception as e:
  print(json.dumps({'passed':False,'error':error_code(e) if isinstance(e,CloudError) else 'LocalOperationFailed'}));return 1
if __name__=='__main__':raise SystemExit(main())
