"""First-time rdev backend check. Never print state, credentials, or raw errors."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import uuid


def validate_state(raw):
    try:
        state = json.loads(raw)
        resources = state['resources']
        if state['version'] != 4 or not isinstance(resources, list):
            raise ValueError()
        if any(not isinstance(r, dict) or r.get('mode') != 'data' for r in resources):
            raise ValueError()
    except (KeyError, TypeError, ValueError):
        raise ValueError('Probe requires valid empty or data-only state') from None


def validate_plan(plan):
    if not isinstance(plan, dict) or plan.get('errored') or plan.get('deferred_changes'):
        raise ValueError('Probe plan is incomplete')
    def inspect(value):
        if isinstance(value, dict):
            if 'mode' in value and value['mode'] != 'data':
                raise ValueError('Probe plan contains managed resources')
            for child in value.values():
                inspect(child)
        elif isinstance(value, list):
            for child in value:
                inspect(child)
    inspect(plan)


def validate_write_plan(plan, nonce):
    change = plan.get('output_changes', {}).get('state_probe_nonce', {})
    if change.get('actions') not in [['create'], ['update']] or change.get('after') != nonce:
        raise ValueError('Probe plan does not require a new state marker')


def main():
    bucket = os.environ.get('RDEV_STATE_BUCKET')
    endpoint = os.environ.get('RDEV_LOCK_ENDPOINT')
    if bucket != 'raptor-iap-tfstate-sg-200743' or endpoint != 'https://raptor-tf-lock.ap-southeast-1.ots.aliyuncs.com':
        raise ValueError('Expected POC backend configuration is missing')
    repository = Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix='rdev-state-check-') as name:
        directory = Path(name)
        directory.chmod(0o700)
        shutil.copy(repository / 'terraform-module/rdev-foundation/.terraform.lock.hcl', directory)
        (directory / 'main.tf').write_text('''terraform {
  required_version = "= 1.13.3"
  required_providers {
    alicloud = { source = "aliyun/alicloud", version = "1.293.0" }
  }
  backend "oss" {
    region = "ap-southeast-1"
    prefix = "rdev.ali"
    key = "terraform.tfstate"
    acl = "private"
    encrypt = true
    tablestore_table = "terraform_lock"
  }
}
provider "alicloud" { region = "ap-southeast-1" }
data "alicloud_account" "current" {}
''')
        nonce = str(uuid.uuid4())
        with (directory / 'main.tf').open('a') as configuration:
            configuration.write('output "state_probe_nonce" { value = ' + json.dumps(nonce) + ' }\n')
        def run(args, allow_missing=False):
            result = subprocess.run(['terraform', '-chdir=' + name] + args, capture_output=True, text=True, timeout=180)
            if result.returncode:
                if allow_missing and result.stdout == '' and result.stderr.strip() == 'No state file was found!':
                    return ''
                raise ValueError('Backend check failed at ' + args[0] + '; raw diagnostics withheld')
            return result.stdout
        run(['init', '-input=false', '-lockfile=readonly', '-backend-config=bucket=' + bucket,
             '-backend-config=tablestore_endpoint=' + endpoint])
        raw = run(['state', 'pull'], allow_missing=True)
        if raw.strip():
            validate_state(raw)
        run(['plan', '-refresh-only', '-input=false', '-lock-timeout=30s', '-out=probe.tfplan'])
        plan = json.loads(run(['show', '-json', 'probe.tfplan']))
        validate_plan(plan)
        validate_write_plan(plan, nonce)
        run(['apply', '-input=false', '-lock-timeout=30s', 'probe.tfplan'])
        persisted = run(['state', 'pull'])
        validate_state(persisted)
        if json.loads(persisted).get('outputs', {}).get('state_probe_nonce', {}).get('value') != nonce:
            raise ValueError('Fresh state marker was not persisted')
        print(json.dumps({'passed': True, 'state_read_write': True, 'terraform_locking': True,
                          'managed_resources': 0, 'infrastructure_provisioned': False}))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, subprocess.SubprocessError):
        print('Remote state verification failed; credentials and raw state withheld.')
        raise SystemExit(1)
