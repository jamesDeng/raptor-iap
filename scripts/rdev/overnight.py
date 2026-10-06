#!/usr/bin/env python3
"""Explicit, data-preserving rdev.ali sleep/wake. Default is read-only preview."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time

REGION = 'ap-southeast-1'
TAGS = {'Project': 'raptor-iap', 'Environment': 'rdev.ali', 'Owner': 'rdev-foundation'}


def scope_from_state(state):
    if state.get('version') != 4:
        raise ValueError('Expected Terraform state version 4')
    resources = state.get('resources', [])
    def one(kind, name, mode='managed', optional=False):
        matches = [r for r in resources if r.get('module') == 'module.foundation' and r.get('mode') == mode and r.get('type') == kind and r.get('name') == name]
        if optional and not matches:
            return None
        if len(matches) != 1 or len(matches[0].get('instances', [])) != 1:
            raise ValueError('Expected one owned resource in state: ' + kind)
        instance = matches[0]['instances'][0]
        if instance.get('deposed'):
            raise ValueError('Deposed resource needs reconciliation')
        return instance['attributes']
    account = one('alicloud_account', 'current', 'data')['id']
    vpc = one('alicloud_vpc', 'env')
    db = one('alicloud_db_instance', 'platform')
    for obj in (vpc, db):
        if any(obj.get('tags', {}).get(k) != v for k, v in TAGS.items()):
            raise ValueError('State ownership tags do not match rdev.ali')
    cluster = one('alicloud_cs_managed_kubernetes', 'cluster', optional=True)
    pool = one('alicloud_cs_kubernetes_node_pool', 'platform', optional=True)
    if bool(cluster) != bool(pool):
        raise ValueError('Incomplete ACK state; reconcile before sleeping workers')
    return {'account':account, 'vpc':vpc['id'], 'database':db['id'],
            'cluster':cluster['id'] if cluster else None,
            'pool':pool['id'].split(':')[-1] if pool else None}


class CLI:
    def __init__(self, profile):
        self.profile = profile
        self.env = dict(os.environ)
        for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','http_proxy','https_proxy','all_proxy'):
            self.env.pop(key, None)
    def call(self, service, action, parameters=None):
        args = ['aliyun', service, action, '--profile', self.profile, '--region', REGION, '--retry-count', '0']
        if service == 'cs':
            args.append(parameters['path'])
        else:
            for key, value in (parameters or {}).items():
                args.extend(['--' + key, str(value)])
        result = subprocess.run(args, env=self.env, capture_output=True, text=True, timeout=60)
        try:
            data = json.loads(result.stdout or result.stderr)
        except ValueError:
            raise ValueError(service + ':' + action + ' returned an unreadable response') from None
        if result.returncode:
            code = data.get('error_code', 'RequestFailed')
            if not isinstance(code, str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', code):
                code = 'RequestFailed'
            raise ValueError(service + ':' + action + ' failed: ' + code)
        return data


def tags_match(tags):
    return all(tags.get(k) == v for k, v in TAGS.items())


def automation_safe(pool, cluster):
    management = pool.get('management', {})
    upgrade = cluster.get('operation_policy', {}).get('cluster_auto_upgrade', {})
    return (pool.get('auto_scaling', {}).get('enable') is False
            and management.get('auto_repair') is False
            and management.get('auto_vul_fix') is False
            and management.get('auto_upgrade', False) is False
            and management.get('upgrade_config', {}).get('auto_upgrade', False) is False
            and management.get('drift_enabled', False) is False
            and upgrade.get('enabled') is False)


def collect(api, scope):
    identity = api.call('sts', 'GetCallerIdentity')
    if identity.get('AccountId') != scope['account']:
        raise ValueError('Authenticated account differs from Terraform state')
    dbs = api.call('rds', 'DescribeDBInstanceAttribute', {'RegionId':REGION, 'DBInstanceId':scope['database']})['Items']['DBInstanceAttribute']
    if len(dbs) != 1:
        raise ValueError('Expected one database')
    db = dbs[0]
    if (db.get('DBInstanceId') != scope['database'] or db.get('RegionId') != REGION or db.get('VpcId') != scope['vpc'] or db.get('Engine') != 'PostgreSQL' or db.get('PayType') != 'Postpaid' or db.get('DBInstanceType') != 'Primary' or db.get('DBInstanceStorageType') != 'general_essd' or db.get('ReadOnlyDBInstanceIds', {}).get('ReadOnlyDBInstanceId') != []):
        raise ValueError('Database scope or pause prerequisites differ from the POC')
    tagrows = api.call('rds', 'DescribeTags', {'RegionId':REGION, 'DBInstanceId':scope['database']})['Items']['TagInfos']
    tags = {t['TagKey']:t['TagValue'] for t in tagrows if scope['database'] in t.get('DBInstanceIds', {}).get('DBInstanceIds', [])}
    if not tags_match(tags):
        raise ValueError('Live database ownership tags differ')
    response = api.call('ecs', 'DescribeInstances', {'RegionId':REGION, 'VpcId':scope['vpc'], 'PageSize':100})
    instances = response['Instances']['Instance']
    if response['TotalCount'] != len(instances):
        raise ValueError('Incomplete worker inventory')
    safe = True
    if instances:
        if not scope['cluster'] or not scope['pool'] or len(instances) != 1:
            raise ValueError('Expected exactly one worker with completed ACK state')
        pool = api.call('cs', 'GET', {'path':'/clusters/' + scope['cluster'] + '/nodepools/' + scope['pool']})
        if pool.get('nodepool_info', {}).get('nodepool_id') != scope['pool']:
            raise ValueError('Node pool identity differs')
        cluster = api.call('cs', 'GET', {'path':'/clusters/' + scope['cluster']})
        if cluster.get('cluster_id') != scope['cluster'] or cluster.get('region_id') != REGION or cluster.get('vpc_id') != scope['vpc']:
            raise ValueError('Live cluster identity differs')
        safe = automation_safe(pool, cluster)
        group_id = pool.get('scaling_group', {}).get('scaling_group_id')
        if not group_id:
            raise ValueError('Scaling-group identity is missing')
        groups = api.call('ess', 'DescribeScalingGroups', {'RegionId':REGION, 'ScalingGroupId.1':group_id})['ScalingGroups']['ScalingGroup']
        if len(groups) != 1 or groups[0].get('ScalingGroupId') != group_id or groups[0].get('VpcId') != scope['vpc']:
            raise ValueError('Scaling-group identity differs')
        group = groups[0]
        health = group.get('HealthCheckTypes', {}).get('HealthCheckType', [group.get('HealthCheckType')])
        safe = safe and (group.get('LifecycleState') == 'Inactive' or (group.get('LifecycleState') == 'Active' and health == ['NONE']))
    nodes = []
    for node in instances:
        tags = {t['TagKey']:t['TagValue'] for t in node.get('Tags', {}).get('Tag', [])}
        if not tags_match(tags) or tags.get('ack.aliyun.com') != scope['cluster'] or tags.get('ack.alibabacloud.com/nodepool-id') != scope['pool'] or node.get('VpcAttributes', {}).get('VpcId') != scope['vpc'] or node.get('RegionId') != REGION or node.get('InstanceChargeType') != 'PostPaid' or node.get('InstanceType') != 'ecs.e-c1m2.xlarge':
            raise ValueError('Worker ownership or economical-mode prerequisites differ')
        nodes.append({'id':node['InstanceId'], 'status':node['Status'], 'stopped_mode':node.get('StoppedMode')})
    return {'account':scope['account'], 'vpc':scope['vpc'], 'database':{'id':scope['database'],'status':rds_status(db['DBInstanceStatus'])}, 'nodes':nodes, 'safe_to_stop_nodes':safe}


def rds_status(status):
    return {'STOPPED':'Stopped', 'STOPPING':'Stopping', 'STARTING':'Starting'}.get(status, status)


def build_actions(inventory, mode):
    if mode not in ('sleep', 'wake'):
        raise ValueError('Expected sleep or wake')
    if mode == 'sleep' and inventory['nodes'] and not inventory['safe_to_stop_nodes']:
        raise ValueError('ACK automation or ESS health checks could replace stopped workers; no changes made')
    db = {**inventory['database'], 'status':rds_status(inventory['database']['status'])}
    actions = []
    for n in inventory['nodes']:
        if n['status'] not in ('Running','Stopped'):
            raise ValueError('Worker is transitioning; inspect status and try later')
        if mode == 'sleep' and n['status'] == 'Stopped' and n['stopped_mode'] != 'StopCharging':
            raise ValueError('Worker is stopped but compute savings are not verified')
        if n['status'] == ('Running' if mode == 'sleep' else 'Stopped'):
            parameters = {'RegionId':REGION, 'InstanceId':n['id']}
            if mode == 'sleep':
                parameters.update(StoppedMode='StopCharging', ForceStop='false')
            actions.append({'service':'ecs','api':'StopInstance' if mode == 'sleep' else 'StartInstance', 'parameters':parameters,'desired':'Stopped' if mode == 'sleep' else 'Running'})
    if db['status'] not in ('Running','Stopped'):
        raise ValueError('Database is transitioning; inspect status and try later')
    if db['status'] == ('Running' if mode == 'sleep' else 'Stopped'):
        action = {'service':'rds','api':'StopDBInstance' if mode == 'sleep' else 'StartDBInstance','parameters':{'RegionId':REGION,'DBInstanceId':db['id']},'desired':'Stopped' if mode == 'sleep' else 'Running'}
        if mode == 'sleep': actions.append(action)
        else: actions.insert(0, action)
    return actions


def execute_actions(actions, mutate, read, sleep=time.sleep, clock=time.monotonic, timeout=900):
    for action in actions:
        mutate(action)  # Exactly one mutation. Never retry an uncertain outcome.
        deadline = clock() + timeout
        last_status = None
        while True:
            status = dict(read(action))
            if action['service'] == 'rds':
                status['status'] = rds_status(status.get('status'))
            if status != last_status:
                # Emit only known status enums, never arbitrary provider text.
                label = status.get('status')
                if label not in ('Running','Stopped','Starting','Stopping'):
                    label = 'Transitioning'
                print(json.dumps({'action':action['api'], 'status':label}), flush=True)
                last_status = dict(status)
            if status.get('status') == action['desired']:
                if action['api'] == 'StopInstance' and status.get('stopped_mode') != 'StopCharging':
                    raise ValueError('ECS stopped in standard mode; compute savings not confirmed')
                print(json.dumps({'verified':action['api'], 'status':status['status']}), flush=True)
                break
            if clock() >= deadline:
                raise ValueError('Status polling timed out; inspect before running again')
            sleep(10)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('sleep','wake','status'))
    parser.add_argument('--state-file', required=True, type=Path, help='Private, freshly pulled rdev.ali Terraform state')
    parser.add_argument('--profile', default='infra-ops-poc')
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--confirm', help='Must be rdev.ali when executing')
    args = parser.parse_args()
    if args.execute and (args.confirm != 'rdev.ali' or args.mode == 'status'):
        parser.error('Execution requires sleep/wake and --confirm rdev.ali')
    scope = scope_from_state(json.loads(args.state_file.read_text()))
    api = CLI(args.profile)
    inventory = collect(api, scope)
    actions = [] if args.mode == 'status' else build_actions(inventory, args.mode)
    print(json.dumps({'mode':args.mode,'preview':not args.execute,'inventory':inventory,'actions':actions,'note':'Storage, backups, NAT/EIP and load-balancer charges continue. No deletions or scheduled runs.'}, indent=2))
    if args.execute:
        # Recheck ownership and automation immediately before the first mutation.
        if collect(api, scope) != inventory:
            raise ValueError('Inventory changed since preview; no changes made')
        def read(action):
            if action['service'] == 'rds':
                d = api.call('rds','DescribeDBInstanceAttribute',action['parameters'])['Items']['DBInstanceAttribute'][0]
                return {'status':d['DBInstanceStatus']}
            parameters = {'RegionId':REGION,'InstanceIds':json.dumps([action['parameters']['InstanceId']])}
            rows = api.call('ecs','DescribeInstances',parameters)['Instances']['Instance']
            if len(rows) != 1 or rows[0]['InstanceId'] != action['parameters']['InstanceId']:
                raise ValueError('Worker identity changed during polling')
            return {'status':rows[0]['Status'],'stopped_mode':rows[0].get('StoppedMode')}
        execute_actions(actions, lambda a:api.call(a['service'],a['api'],a['parameters']), read)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.TimeoutExpired):
        # API errors and state may contain credentials; report only our sanitized ValueErrors.
        import sys
        error = sys.exc_info()[1]
        print(str(error) if isinstance(error, ValueError) and not isinstance(error, json.JSONDecodeError) else 'Local input or CLI operation failed; inspect privately', file=sys.stderr)
        raise SystemExit(1)
