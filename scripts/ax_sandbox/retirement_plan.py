"""PR-only sandbox retirement plan: suppress values, state and provider logs."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
STACK = ROOT / 'infra-terraform/environments/rdev.ali/ax-sandbox'
OWNED = {
    'alicloud_cs_managed_kubernetes.cluster': 'c7de05cd5a9b84512925b1fc7a24e1bc9',
    'alicloud_vswitch.workers': 'vsw-t4nn4krsmkia5omq8urik',
    'alicloud_eip_address.api[0]': 'eip-t4nqsqavymz5vr33u1n3g',
    'alicloud_eip_association.api[0]': 'eip-t4nqsqavymz5vr33u1n3g:lb-t4n9rk0g7sfk5ocmghllb',
    'alicloud_ram_policy.gitops_plan': None,
    'alicloud_ram_role_policy_attachment.gitops_plan': None,
    'terraform_data.account_guard': None,
}

def changes(plan):
    rows = []
    for r in plan.get('resource_changes', []):
        if r.get('mode') != 'managed' or r['change']['actions'] == ['no-op']:
            continue
        address = r['address']
        key = address.removeprefix('module.sandbox.')
        if not address.startswith('module.sandbox.') or key not in OWNED:
            raise ValueError('Resource outside the dedicated sandbox')
        c = r['change']
        if c['actions'] not in (['create'], ['update'], ['delete']):
            raise ValueError('Replacement action is not authorized')
        value = c.get('before') or c.get('after') or {}
        if OWNED[key] is not None and value.get('id') != OWNED[key]:
            raise ValueError('Dedicated resource identity mismatch')
        if c['actions'] == ['create'] and key not in ('alicloud_ram_policy.gitops_plan', 'alicloud_ram_role_policy_attachment.gitops_plan'):
            raise ValueError('Retirement cannot create infrastructure')
        if key == 'alicloud_ram_policy.gitops_plan' and value.get('policy_name') != 'raptor-ax-sandbox-retirement-plan':
            raise ValueError('Unexpected policy')
        if key == 'alicloud_ram_role_policy_attachment.gitops_plan' and (value.get('role_name') != 'raptor-iap-rdev-plan' or value.get('policy_name') != 'raptor-ax-sandbox-retirement-plan'):
            raise ValueError('Unexpected role attachment')
        rows.append('| `' + address + '` | ' + ', '.join(c['actions']) + ' |')
    return rows

def run(args):
    p = subprocess.run(args, cwd=STACK, capture_output=True)
    if p.returncode:
        raise RuntimeError('Terraform ' + args[1] + ' failed; private output withheld')
    return p.stdout

def main():
    sha = os.environ['TF_SOURCE_SHA']
    if not re.fullmatch('[a-f0-9]{40}', sha):
        raise ValueError('Invalid source revision')
    run(['terraform', 'init', '-input=false', '-lockfile=readonly',
         '-backend-config=bucket=raptor-iap-tfstate-sg-200743',
         '-backend-config=tablestore_endpoint=https://raptor-tf-lock.ap-southeast-1.ots.aliyuncs.com'])
    with tempfile.TemporaryDirectory() as temp:
        saved = Path(temp) / 'private.tfplan'
        run(['terraform', 'plan', '-input=false', '-lock-timeout=60s',
             '-var=account_id=1360282071200743', '-var=vpc_id=vpc-t4n4fi6r1a7bi6n93ftq3', '-out=' + str(saved)])
        plan = json.loads(run(['terraform', 'show', '-json', str(saved)]))
    if plan.get('errored') or plan.get('complete') is not True or plan.get('deferred_changes'):
        raise ValueError('Incomplete retirement plan')
    rows = changes(plan)
    summary = ('Sandbox retirement plan for `' + sha + '`\n\n| Resource | Action |\n|---|---|\n'
               + ('\n'.join(rows) or '| — | No resource changes |')
               + '\n\nShared VPC and ECS AX node are outside this state. Values and raw state/plan/logs withheld.\n')
    Path(os.environ['TF_SUMMARY_PATH']).write_text(summary)

if __name__ == '__main__':
    try:
        main()
    except Exception:
        raise SystemExit('Sandbox retirement plan failed; private output withheld')
