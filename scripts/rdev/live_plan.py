"""Protected initial foundation plan. No apply, raw logs, state or plan artifacts."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from datetime import datetime, timedelta, timezone

EXPECTED = {
    'alicloud_vpc.env': {'cidr_block': '10.70.0.0/16'},
    'alicloud_vswitch.workers': {'zone_id': 'ap-southeast-1a', 'cidr_block': '10.70.1.0/24'},
    'alicloud_vswitch.database': {'zone_id': 'ap-southeast-1a', 'cidr_block': '10.70.2.0/24'},
    'alicloud_nat_gateway.outbound': {'nat_type': 'Enhanced', 'payment_type': 'PayAsYouGo', 'internet_charge_type': 'PayByLcu', 'deletion_protection': True},
    'alicloud_eip_address.outbound': {'payment_type': 'PayAsYouGo', 'internet_charge_type': 'PayByTraffic', 'bandwidth': '5'},
    'alicloud_eip_association.outbound': {'instance_type': 'Nat'},
    'alicloud_snat_entry.workers': {},
    'alicloud_cs_managed_kubernetes.cluster': {'cluster_spec': 'ack.standard', 'profile': 'Default', 'version': '1.35.7-aliyun.1', 'new_nat_gateway': False, 'slb_internet_enabled': False, 'deletion_protection': True, 'skip_set_certificate_authority': True, 'pod_cidr': '10.72.0.0/16', 'service_cidr': '10.73.0.0/16', 'addons': [{'name': 'flannel', 'disabled': False, 'config': None, 'version': None}]},
    'alicloud_cs_kubernetes_node_pool.platform': {'instance_types': ['ecs.e-c1m2.xlarge'], 'desired_size': '1', 'instance_charge_type': 'PostPaid', 'system_disk_category': 'cloud_essd', 'system_disk_size': 40, 'internet_max_bandwidth_out': 0, 'spot_strategy': 'NoSpot'},
    'alicloud_db_instance.platform': {'engine': 'PostgreSQL', 'engine_version': '14.0', 'category': 'Basic', 'instance_type': 'pg.n2e.1c.1m', 'instance_storage': 10, 'db_instance_storage_type': 'general_essd', 'instance_charge_type': 'Postpaid', 'zone_id': 'ap-southeast-1a', 'security_ips': ['10.70.1.0/24', '10.72.0.0/16'], 'storage_auto_scale': 'Disable', 'deletion_protection': True},
    'terraform_data.account_guard': {},
    'terraform_data.service_role_guard': {},
}


PARENT_REFERENCES = {
    'alicloud_vswitch.workers': {'vpc_id': 'alicloud_vpc.env.id'},
    'alicloud_vswitch.database': {'vpc_id': 'alicloud_vpc.env.id'},
    'alicloud_nat_gateway.outbound': {'vpc_id': 'alicloud_vpc.env.id', 'vswitch_id': 'alicloud_vswitch.workers.id'},
    'alicloud_cs_managed_kubernetes.cluster': {'vswitch_ids': 'alicloud_vswitch.workers.id'},
    'alicloud_cs_kubernetes_node_pool.platform': {'cluster_id': 'alicloud_cs_managed_kubernetes.cluster.id', 'vswitch_ids': 'alicloud_vswitch.workers.id'},
    'alicloud_db_instance.platform': {'vpc_id': 'alicloud_vpc.env.id', 'vswitch_id': 'alicloud_vswitch.database.id'},
    'alicloud_eip_association.outbound': {'allocation_id': 'alicloud_eip_address.outbound.id', 'instance_id': 'alicloud_nat_gateway.outbound.id'},
    'alicloud_snat_entry.workers': {'snat_table_id': 'alicloud_nat_gateway.outbound.snat_table_ids', 'source_vswitch_id': 'alicloud_vswitch.workers.id', 'snat_ip': 'alicloud_eip_address.outbound.ip_address'},
}


def validate_plan(plan, account):
    try:
        if not isinstance(plan, dict) or plan.get('errored') or plan.get('complete') is not True or plan.get('deferred_changes') or plan.get('resource_drift'):
            raise ValueError()
        if plan['variables']['account_id']['value'] != account or plan['variables']['kubernetes_version']['value'] != '1.35.7-aliyun.1':
            raise ValueError()
        configuration = plan['configuration']
        provider = configuration['provider_config']['alicloud']
        if provider['full_name'] != 'registry.terraform.io/aliyun/alicloud' or provider['version_constraint'] != '1.293.0' or provider['expressions']['region'] != {'constant_value': 'ap-southeast-1'}:
            raise ValueError()
        calls = configuration['root_module']['module_calls']
        if set(calls) != {'foundation'} or calls['foundation']['source'] != '../../../terraform-module/rdev-foundation':
            raise ValueError()
        config_resources = calls['foundation']['module']['resources']
        configured = {r['address']: r for r in config_resources if r['mode'] == 'managed'}
        if set(configured) != set(EXPECTED):
            raise ValueError()
        for address, fields in PARENT_REFERENCES.items():
            resource = configured[address]
            if resource['provider_config_key'] != 'alicloud':
                raise ValueError()
            for field, ref in fields.items():
                expression = resource['expressions'][field]
                if set(expression) != {'references'} or set(expression['references']) != {ref, ref.rsplit('.', 1)[0]}:
                    raise ValueError()
        changes = plan['resource_changes']
        if not isinstance(changes, list) or any(not isinstance(r, dict) or r.get('mode') not in ('managed', 'data') for r in changes):
            raise ValueError()
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
            if not isinstance(after, dict):
                raise ValueError()
            for field in PARENT_REFERENCES.get(key, {}):
                if change.get('after_unknown', {}).get(field) is not True or after.get(field) is not None:
                    raise ValueError()
            if any(after.get(field) != value for field, value in EXPECTED[key].items()):
                raise ValueError()
        return {'passed': True, 'cloud_creates': 10, 'local_guards': 2, 'updates': 0, 'deletes': 0, 'apply_executed': False}
    except (KeyError, TypeError, AttributeError, ValueError):
        raise ValueError('Plan does not match the reviewed initial foundation') from None


def main(provision=False):
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
    evidence = None
    if provision:
        from initial_apply import validate_release
        repository = Path(__file__).resolve().parents[2]
        evidence = json.loads((repository / 'docs/setup/rdev-foundation-preflight.json').read_text())
        validate_release(evidence, sha, os.environ.get('RDEV_EXPECTED_SOURCE_SHA', ''))
    os.umask(0o077)
    repository = Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix='rdev-live-plan-') as name:
        root = Path(name)
        directory = root / 'infra-terraform/environments/rdev.ali'
        shutil.copytree(repository / 'infra-terraform/environments/rdev.ali', directory, ignore=shutil.ignore_patterns('.terraform', '*.tfstate*', '*.tfplan', '*.tfvars'))
        shutil.copytree(repository / 'terraform-module/rdev-foundation', root / 'terraform-module/rdev-foundation', ignore=shutil.ignore_patterns('.terraform', '*.tfstate*', '*.tfplan', '*.tfvars'))
        (directory / 'terraform.tfvars.json').write_text(json.dumps({'account_id': role.group(1), 'kubernetes_version': '1.35.7-aliyun.1'}))
        def run(args, timeout=600):
            result = subprocess.run(['terraform', '-chdir=' + str(directory)] + args, capture_output=True, text=True, timeout=timeout)
            if result.returncode:
                raise ValueError('Protected planning failed at ' + args[0] + '; raw diagnostics withheld')
            return result.stdout
        run(['init', '-input=false', '-lockfile=readonly', '-backend-config=bucket=' + bucket,
             '-backend-config=tablestore_endpoint=' + endpoint, '-backend-config=tablestore_table=terraform_lock'])
        run(['plan', '-input=false', '-no-color', '-lock-timeout=60s', '-out=foundation.tfplan'])
        summary = validate_plan(json.loads(run(['show', '-json', 'foundation.tfplan'])), role.group(1))
        if provision:
            from initial_apply import apply_saved_plan
            print(json.dumps({'event': 'initial_apply_started', 'source_sha': sha, 'apply_started_at': datetime.now(timezone.utc).isoformat(), 'estimated_total_cny': evidence['estimated_total_cny']}))
            apply_saved_plan(run, evidence, sha, os.environ.get('RDEV_EXPECTED_SOURCE_SHA', ''))
            state = json.loads(run(['state', 'pull']))
            managed = [r for r in state.get('resources', []) if r.get('mode') == 'managed']
            if len(managed) != 12 or any(len(r.get('instances', [])) != 1 for r in managed):
                raise ValueError('Post-apply state requires reconciliation')
            started = datetime.now(timezone.utc)
            summary.update(apply_executed=True, terraform_apply_passed=True, managed_resources=12,
                           provisioning_completed_at=started.isoformat(), review_due_at=(started + timedelta(hours=72)).isoformat(),
                           runtime_acceptance_passed=False)
        summary.update(source_sha=sha, region='ap-southeast-1', identity='raptor-iap-rdev-apply', raw_artifacts_published=False)
        print(json.dumps(summary))
        path = os.environ.get('GITHUB_STEP_SUMMARY')
        if path:
            with Path(path).open('a') as output:
                output.write('### Initial foundation ' + ('apply' if provision else 'plan') + '\n\nSource: `' + sha + '`\n\n10 cloud creates; 2 local guards; 0 updates/deletes. Raw plan/state/logs withheld.\n\n')
                if provision:
                    output.write('Terraform apply passed; independent cloud/runtime acceptance is still required. Review continuation or teardown by `' + summary['review_due_at'] + '`.\n')
                else:
                    output.write('No apply. Planning verifies reads and locking; creation authorization and resource readiness remain untested.\n')
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, subprocess.SubprocessError):
        print('Protected initial plan failed; credentials and raw diagnostics withheld.')
        raise SystemExit(1)
