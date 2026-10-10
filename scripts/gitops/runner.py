"""Private Terraform execution; only action summaries may leave the runner."""
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

ROOT = Path(os.environ.get('TF_CANDIDATE_ROOT', Path(__file__).resolve().parents[2])).resolve()
def select_stack(name):
    choices = {
        'test-foundation': {'root':'infra-terraform/environments/rdev-test-foundation','prefix':'rdev.ali/test-pgcat','key':'terraform.tfstate','label':'Terraform test foundation','binding':'raptor-test-foundation-v1'},
        'infra-api': {'root':'infra-terraform/environments/rdev-infra-api','prefix':'rdev.ali','key':'infra-api.tfstate','label':'Terraform Infra API','binding':'raptor-infra-api-v1'},
        'rdev-kong': {'root':'infra-terraform/environments/rdev.ali','prefix':'rdev.ali','key':'terraform.tfstate','label':'Terraform rdev Kong DNS','binding':'raptor-rdev-kong-v1'},
    }
    if name not in choices:raise ValueError('Unknown stack')
    return choices[name]


def binding(sha, stack="test-foundation"):
    if not re.fullmatch('[0-9a-f]{40}', sha): raise ValueError('Invalid source SHA')
    return (select_stack(stack)['binding']+':'+sha).encode()

def seal(plan, key, sha, stack="test-foundation"):
    if len(key)!=32: raise ValueError('Expected 256-bit key')
    nonce=os.urandom(12)
    return nonce+AESGCM(key).encrypt(nonce,plan,binding(sha,stack))

def unseal(blob,key,sha,stack="test-foundation"):
    if len(key)!=32 or len(blob)<28: raise ValueError('Invalid encrypted plan')
    return AESGCM(key).decrypt(blob[:12],blob[12:],binding(sha,stack))

def summary(plan,sha,stack="test-foundation"):
    binding(sha,stack)
    if plan.get('complete') is not True or plan.get('errored') or plan.get('deferred_changes'):raise ValueError('Incomplete plan')
    rows=[]
    for r in plan.get('resource_changes',[]):
        if r.get('mode')!='managed':continue
        address=r['address']; actions=r['change']['actions']
        if not re.fullmatch(r'[A-Za-z0-9_.\[\]"/-]{1,240}',address):raise ValueError('Unsafe address')
        if not actions or any(a not in ('create','update','delete','no-op','read') for a in actions):raise ValueError('Unknown action')
        importing=bool(r['change'].get('importing'))
        if actions==['no-op'] and not importing:continue
        if importing:actions=['import']+([] if actions==['no-op'] else actions)
        address=re.sub(r'\["[^"]*"\]', '[redacted-key]', address)
        rows.append('| `'+address+'` | '+', '.join(actions)+' |')
    return '<!-- raptor-terraform-plan -->\n'+select_stack(stack)['label']+' plan for `'+sha+'`\n\n| Resource | Action |\n|---|---|\n'+('\n'.join(rows) if rows else '| — | No resource changes |')+'\n\nValues are withheld; raw plan/state are never posted.\n'

def require_protection(document):
    rules=document.get('protection_rules',[])
    if not any(r.get('type')=='required_reviewers' and r.get('reviewers') for r in rules):
        raise ValueError('Required environment reviewer missing')

def command(args,cwd):
    stage=args[1] if args[0]=='terraform' else 'checkout'
    print('GitOps step: '+stage, flush=True)
    r=subprocess.run(args,cwd=cwd,capture_output=True)
    if r.returncode:
        diagnostic = (r.stdout+r.stderr).lower()
        categories = [label for label, patterns in {
            'access-denied': (b'accessdenied', b'access denied', b'status code: 403', b'statuscode: 403'),
            'missing-backend-object': (b'nosuchbucket', b'nosuchkey', b'no such bucket'),
            'backend-lock': (b'error acquiring the state lock', b'lockid'),
            'provider-download': (b'failed to query available provider packages', b'failed to install provider'),
            'module-path': (b'unreadable module directory', b'module not installed'),
        }.items() if any(pattern in diagnostic for pattern in patterns)]
        codes=re.findall(rb'(?:ErrorCode|Code|code)[\s:=\"]+([A-Za-z][A-Za-z0-9_.]{2,80})', r.stdout+r.stderr)
        actions=re.findall(rb'\b(?:ram|ess|nlb|ecs|vpc|rds|oss|ots):[A-Za-z][A-Za-z0-9]{1,80}\b', r.stdout+r.stderr)
        print('Failed step: '+stage+'; category: '+(', '.join(categories) or 'unclassified')+'; error codes: '+(', '.join(sorted({c.decode() for c in codes})) or 'unavailable')+'; permission actions: '+(', '.join(sorted({a.decode() for a in actions})) or 'unavailable'), file=sys.stderr)
        raise RuntimeError('Terraform command failed; raw output withheld')
    return r.stdout

def validate_inputs(config,stack="test-foundation"):
    select_stack(stack)
    if stack == 'rdev-kong':
        if set(config) != {'account_id','kubernetes_version'}:raise ValueError('Invalid stack inputs')
        if not isinstance(config['account_id'],str) or not re.fullmatch(r'\d{8,20}',config['account_id']):raise ValueError('Invalid account ID')
        if config['kubernetes_version'] != '1.35.7-aliyun.1':raise ValueError('Invalid Kubernetes version')
        return
    if stack == 'infra-api':
        if set(config) != {'trigger_url','gateway_instance_id'}:raise ValueError('Invalid stack inputs')
        if not isinstance(config['trigger_url'],str) or not re.fullmatch(r'https://[a-zA-Z0-9.-]+\.ap-southeast-1\.fcapp\.run/?',config['trigger_url']):raise ValueError('Invalid trigger')
        if not isinstance(config['gateway_instance_id'],str) or not re.fullmatch(r'api-shared-vpc-[a-z0-9-]+',config['gateway_instance_id']):raise ValueError('Invalid gateway')
        return
    required={'account_id','vpc_id','db_vswitch_id','config_bucket','db_code'}
    optional={'proxy_enabled','proxy_code','proxy_worker_vswitch_id','proxy_secret_version'}
    if not required.issubset(config) or set(config)-required-optional:raise ValueError('Invalid stack inputs')
    for k,v in config.items():
        if k=='proxy_enabled':
            if not isinstance(v,bool):raise ValueError('Invalid enable flag')
        elif not isinstance(v,str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,200}',v):raise ValueError('Invalid input')
    if config.get('proxy_enabled') and not (optional-{'proxy_enabled'}).issubset(config):raise ValueError('Incomplete proxy binding')

def main():
    mode=sys.argv[1]
    if mode=='guard':
        require_protection(json.load(sys.stdin));return
    stack_name=os.environ.get('TF_STACK_NAME','test-foundation');stack=select_stack(stack_name);STACK=ROOT/stack['root']
    sha=os.environ['TF_SOURCE_SHA'];binding(sha,stack_name)
    if command(['git','rev-parse','HEAD'],ROOT).decode().strip()!=sha:raise ValueError('Checkout mismatch')
    if mode not in ('plan','apply'):raise ValueError('Invalid mode')
    config=json.loads(os.environ['TF_STACK_INPUTS'])
    validate_inputs(config,stack_name)
    bucket=os.environ['TF_STATE_BUCKET'];endpoint=os.environ['TF_LOCK_ENDPOINT']
    if not re.fullmatch('[a-z0-9-]{3,63}',bucket) or not re.fullmatch(r'https://[a-z0-9-]+\.ap-southeast-1\.ots\.aliyuncs\.com',endpoint):raise ValueError('Invalid backend')
    # No raw logs or state artifacts; temp private inputs are deleted on exit.
    variable=STACK/'gitops.auto.tfvars.json'
    try:
        variable.write_text(json.dumps(config));variable.chmod(0o600)
        command(['terraform','init','-input=false','-lockfile=readonly',
                 '-backend-config=bucket='+bucket,'-backend-config=region=ap-southeast-1',
                 '-backend-config=prefix='+stack['prefix'],'-backend-config=key='+stack['key'],
                 '-backend-config=tablestore_endpoint='+endpoint,'-backend-config=tablestore_table=terraform_lock'],STACK)
        command(['terraform','validate'],STACK)
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp)/'saved.tfplan'
            if mode=='plan':
                command(['terraform','plan','-input=false','-lock-timeout=5m','-out='+str(path)],STACK)
                plan=json.loads(command(['terraform','show','-json',str(path)],STACK))
                Path(os.environ['TF_SUMMARY_PATH']).write_text(summary(plan,sha,stack_name))
                if os.environ.get('TF_ENCRYPTED_PLAN_PATH'):
                    key=base64.b64decode(os.environ['TF_PLAN_KEY'],validate=True)
                    Path(os.environ['TF_ENCRYPTED_PLAN_PATH']).write_bytes(seal(path.read_bytes(),key,sha,stack_name))
            else:
                if os.environ.get('GITHUB_REF')!='refs/heads/main':raise ValueError('Apply requires main')
                key=base64.b64decode(os.environ['TF_PLAN_KEY'],validate=True)
                path.write_bytes(unseal(Path(os.environ['TF_ENCRYPTED_PLAN_PATH']).read_bytes(),key,sha,stack_name));path.chmod(0o600)
                command(['terraform','apply','-input=false',str(path)],STACK)
                print('Reviewed saved plan applied successfully.')
    finally:variable.unlink(missing_ok=True)

if __name__=='__main__':
    try:main()
    except Exception:
        print('GitOps execution failed; private output withheld.',file=sys.stderr);sys.exit(1)
