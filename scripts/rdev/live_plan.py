"""Protected initial foundation plan. No apply, raw logs, state or plan artifacts."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

EXPECTED = {
    'alicloud_vpc.env': {'cidr_block': '10.70.0.0/16'},
    'alicloud_vswitch.workers': {'zone_id': 'ap-southeast-1a', 'cidr_block': '10.70.1.0/24'},
    'alicloud_vswitch.database': {'zone_id': 'ap-southeast-1a', 'cidr_block': '10.70.2.0/24'},
    'alicloud_nat_gateway.outbound': {'nat_type': 'Enhanced', 'payment_type': 'PayAsYouGo', 'internet_charge_type': 'PayByLcu', 'deletion_protection': True},
    'alicloud_eip_address.outbound': {'payment_type': 'PayAsYouGo', 'internet_charge_type': 'PayByTraffic', 'bandwidth': '5'},
    'alicloud_eip_association.outbound': {'instance_type': 'Nat'},
    'alicloud_snat_entry.workers': {},
    'alicloud_cs_managed_kubernetes.cluster': {'cluster_spec': 'ack.standard', 'profile': 'Default', 'version': '1.35.7-aliyun.1', 'new_nat_gateway': False, 'slb_internet_enabled': False, 'deletion_protection': True, 'skip_set_certificate_authority': True},
    'alicloud_cs_kubernetes_node_pool.platform': {'instance_types': ['ecs.e-c1m2.xlarge'], 'desired_size': '1', 'instance_charge_type': 'PostPaid', 'system_disk_category': 'cloud_essd', 'system_disk_size': 40, 'internet_max_bandwidth_out': 0, 'spot_strategy': 'NoSpot'},
    'alicloud_db_instance.platform': {'engine': 'PostgreSQL', 'engine_version': '14.0', 'category': 'Basic', 'instance_type': 'pg.n2e.1c.1m', 'instance_storage': 10, 'db_instance_storage_type': 'general_essd', 'instance_charge_type': 'Postpaid', 'zone_id': 'ap-southeast-1a', 'security_ips': ['10.70.1.0/24', '10.72.0.0/16'], 'storage_auto_scale': 'Disable', 'deletion_protection': True},
    'terraform_data.account_guard': {},
    'terraform_data.service_role_guard': {},
}


def validate_plan(plan, account):
    try:
        if not isinstance(plan, dict) or plan.get('errored') or plan.get('complete') is not True or plan.get('deferred_changes') or plan.get('resource_drift'):
            raise ValueError()
        if plan['variables']['account_id']['value'] != account or plan['variables']['kubernetes_version']['value'] != '1.35.7-aliyun.1':
            raise ValueError()
        changes = plan['resource_changes']
        expected_addresses = {'module.foundation.' + key for key in EXPECTED}
        managed = [r for r in changes if r.get('mode') == 'managed']
        if len(managed) != len(EXPECTED) or {r['address'] for r in managed} != expected_addresses:
            raise ValueError()
        for resource in managed:
            key = resource['address'][len('module.foundation.') :]
            change = resource['change']
            if resource['type'] != key.split('.')[0] or change['actions'] != ['create']:
                raise ValueError()
            after = change['after']
            if any(after.get(field) != value for field, value in EXPECTED[key].items()):
                raise ValueError()
        return {'passed': True, 'cloud_creates': 10, 'local_guards': 2, 'updates': 0, 'deletes': 0, 'apply_executed': False}
    except (KeyError, TypeError, AttributeError, ValueError):
        raise ValueError('Plan does not match the reviewed initial foundation') from None


def main():
    if os.environ.get('GITHUB_REF') != 'refs/heads/main':
        raise ValueError('Only main may run the protected plan')
    sha = os.environ.get('GITHUB_SHA', '')
    role = re.fullmatch(r'acs:ram::(\d+):role/raptor-iap-rdev-apply', os.environ.get('RDEV_APPLY_ROLE_ARN', ''))
    if not re.fullmatch(r'[0-9a-f]{40}', sha) or not role:
        raise ValueError('Reviewed source or deployment identity is missing')
    bucket = os.environ.get('RDEV_STATE_BUCKET')
    endpoint = os.environ.get('RDEV_LOCK_ENDPOINT')
    if bucket != 'raptor-iap-tfstate-sg-200743' or endpoint != 'https://raptor-tf-lock.ap-southeast-1.ots.aliyuncs.com':
        raise ValueError('Expected backend configuration is missing')
    os.umask(0o077)
    repository = Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix='rdev-live-plan-') as name:
        root = Path(name)
        directory = root / 'infra-terraform/environments/rdev.ali'
        shutil.copytree(repository / 'infra-terraform/environments/rdev.ali', directory, ignore=shutil.ignore_patterns('.terraform', '*.tfstate*', '*.tfplan', '*.tfvars'))
        shutil.copytree(repository / 'terraform-module/rdev-foundation', root / 'terraform-module/rdev-foundation', ignore=shutil.ignore_patterns('.terraform', '*.tfstate*', '*.tfplan', '*.tfvars'))
        (directory / 'terraform.tfvars.json').write_text(json.dumps({'account_id': role.group(1), 'kubernetes_version': '1.35.7-aliyun.1'}))
        def run(args):
            result = subprocess.run(['terraform', '-chdir=' + str(directory)] + args, capture_output=True, text=True, timeout=600)
            if result.returncode:
                raise ValueError('Protected planning failed at ' + args[0] + '; raw diagnostics withheld')
            return result.stdout
        run(['init', '-input=false', '-lockfile=readonly', '-backend-config=bucket=' + bucket,
             '-backend-config=tablestore_endpoint=' + endpoint, '-backend-config=tablestore_table=terraform_lock'])
        run(['plan', '-input=false', '-no-color', '-lock-timeout=60s', '-out=foundation.tfplan'])
        summary = validate_plan(json.loads(run(['show', '-json', 'foundation.tfplan'])), role.group(1))
        summary.update(source_sha=sha, region='ap-southeast-1', identity='raptor-iap-rdev-apply', raw_artifacts_published=False)
        print(json.dumps(summary))
        path = os.environ.get('GITHUB_STEP_SUMMARY')
        if path:
            with Path(path).open('a') as output:
                output.write('### Initial foundation plan\n\nSource: `' + sha + '`\n\n10 cloud creates; 2 local guards; 0 updates/deletes. No apply. Raw plan/state/logs withheld.\n\nPlanning verifies reads and locking; creation authorization and resource readiness remain untested.\n')
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, subprocess.SubprocessError):
        print('Protected initial plan failed; credentials and raw diagnostics withheld.')
        raise SystemExit(1)
