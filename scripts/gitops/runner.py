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
STACK = ROOT / 'infra-terraform/environments/rdev-test-foundation'

def binding(sha):
    if not re.fullmatch('[0-9a-f]{40}', sha): raise ValueError('Invalid source SHA')
    return ('raptor-test-foundation-v1:'+sha).encode()

def seal(plan, key, sha):
    if len(key)!=32: raise ValueError('Expected 256-bit key')
    nonce=os.urandom(12)
    return nonce+AESGCM(key).encrypt(nonce,plan,binding(sha))

def unseal(blob,key,sha):
    if len(key)!=32 or len(blob)<28: raise ValueError('Invalid encrypted plan')
    return AESGCM(key).decrypt(blob[:12],blob[12:],binding(sha))

def summary(plan,sha):
    binding(sha)
    if plan.get('complete') is not True or plan.get('errored') or plan.get('deferred_changes'):raise ValueError('Incomplete plan')
    rows=[]
    for r in plan.get('resource_changes',[]):
        if r.get('mode')!='managed':continue
        address=r['address']; actions=r['change']['actions']
        if not re.fullmatch(r'[A-Za-z0-9_.\[\]"-]{1,240}',address):raise ValueError('Unsafe address')
        if not actions or any(a not in ('create','update','delete','no-op','read') for a in actions):raise ValueError('Unknown action')
        if actions==['no-op']:continue
        address=re.sub(r'\["[^"]*"\]', '[redacted-key]', address)
        rows.append('| `'+address+'` | '+', '.join(actions)+' |')
    return '<!-- raptor-terraform-plan -->\nTerraform test foundation plan for `'+sha+'`\n\n| Resource | Action |\n|---|---|\n'+('\n'.join(rows) if rows else '| — | No resource changes |')+'\n\nValues are withheld; raw plan/state are never posted.\n'

def require_protection(document):
    rules=document.get('protection_rules',[])
    if not any(r.get('type')=='required_reviewers' and r.get('reviewers') for r in rules):
        raise ValueError('Required environment reviewer missing')

def command(args,cwd):
    r=subprocess.run(args,cwd=cwd,capture_output=True)
    if r.returncode:raise RuntimeError('Terraform command failed; raw output withheld')
    return r.stdout

def main():
    mode=sys.argv[1]
    if mode=='guard':
        require_protection(json.load(sys.stdin));return
    sha=os.environ['TF_SOURCE_SHA'];binding(sha)
    if command(['git','rev-parse','HEAD'],ROOT).decode().strip()!=sha:raise ValueError('Checkout mismatch')
    if mode not in ('plan','apply'):raise ValueError('Invalid mode')
    config=json.loads(os.environ['TF_STACK_INPUTS'])
    expected={'account_id','vpc_id','db_vswitch_id','config_bucket','db_code'}
    if set(config)!=expected:raise ValueError('Invalid stack inputs')
    for k,v in config.items():
        if not isinstance(v,str) or not re.fullmatch(r'[A-Za-z0-9-]{1,100}',v):raise ValueError('Invalid input')
    bucket=os.environ['TF_STATE_BUCKET'];endpoint=os.environ['TF_LOCK_ENDPOINT']
    if not re.fullmatch('[a-z0-9-]{3,63}',bucket) or not re.fullmatch(r'https://[a-z0-9-]+\.ap-southeast-1\.ots\.aliyuncs\.com',endpoint):raise ValueError('Invalid backend')
    # No raw logs or state artifacts; temp private inputs are deleted on exit.
    variable=STACK/'gitops.auto.tfvars.json'
    try:
        variable.write_text(json.dumps(config));variable.chmod(0o600)
        command(['terraform','init','-input=false','-lockfile=readonly',
                 '-backend-config=bucket='+bucket,'-backend-config=region=ap-southeast-1',
                 '-backend-config=prefix=rdev.ali/test-pgcat','-backend-config=key=terraform.tfstate',
                 '-backend-config=tablestore_endpoint='+endpoint,'-backend-config=tablestore_table=terraform_lock'],STACK)
        command(['terraform','validate'],STACK)
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp)/'saved.tfplan'
            if mode=='plan':
                command(['terraform','plan','-input=false','-lock-timeout=5m','-out='+str(path)],STACK)
                plan=json.loads(command(['terraform','show','-json',str(path)],STACK))
                Path(os.environ['TF_SUMMARY_PATH']).write_text(summary(plan,sha))
                if os.environ.get('TF_ENCRYPTED_PLAN_PATH'):
                    key=base64.b64decode(os.environ['TF_PLAN_KEY'],validate=True)
                    Path(os.environ['TF_ENCRYPTED_PLAN_PATH']).write_bytes(seal(path.read_bytes(),key,sha))
            else:
                if os.environ.get('GITHUB_REF')!='refs/heads/main':raise ValueError('Apply requires main')
                key=base64.b64decode(os.environ['TF_PLAN_KEY'],validate=True)
                path.write_bytes(unseal(Path(os.environ['TF_ENCRYPTED_PLAN_PATH']).read_bytes(),key,sha));path.chmod(0o600)
                command(['terraform','apply','-input=false',str(path)],STACK)
                print('Reviewed saved plan applied successfully.')
    finally:variable.unlink(missing_ok=True)

if __name__=='__main__':
    try:main()
    except Exception:
        print('GitOps execution failed; private output withheld.',file=sys.stderr);sys.exit(1)
