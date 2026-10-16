"""Bounded, fail-closed provider evidence. No cloud provisioning operations."""
import argparse
import importlib.util
import ipaddress
import json
import math
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import time
import uuid
from datetime import datetime, timezone

REGION = 'ap-southeast-1'
PROFILE = 'infra-ops-poc'
GROUP_ID = 'asg-t4necb4dfmu6o3z2rn0g'
SERVER_GROUP_ID = 'sgp-xs5s42tihtl6ja2a67'
VPC_ID = 'vpc-t4n4fi6r1a7bi6n93ftq3'
WORKER_ID = 'i-t4nj1bitmcz7cuoch82h'
LISTENER_ID = 'lsn-7kuvcqkaz5o3z2w376@6432'
FIELDS = ('observedAt', 'desired', 'members', 'healthyRegistered')
RETRYABLE_READS = frozenset({
    ('ess', 'DescribeScalingGroups'),
    ('ess', 'DescribeScalingInstances'),
    ('ecs', 'DescribeInstances'),
    ('nlb', 'ListServerGroupServers'),
    ('nlb', 'GetListenerAttribute'),
    ('nlb', 'GetListenerHealthStatus'),
})

_spec = importlib.util.spec_from_file_location('provider_inventory', Path(__file__).parents[1] / 'metrics/provider_inventory.py')
_inventory = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_inventory)


def require(condition):
    if not condition:
        raise ValueError('Provider evidence rejected')


def collect_backends(fetch, server_group_id):
    rows, seen, tokens = [], set(), set()
    token, total = '', None
    for _ in range(100):
        body = fetch(token)
        count = body.get('TotalCount')
        require(type(count) is int and 0 <= count <= 5000)
        if total is None:
            total = count
        require(total == count and isinstance(body.get('Servers'), list))
        batch = body['Servers']
        for row in batch:
            node = row.get('ServerId')
            require(isinstance(node, str) and node and node not in seen)
            require(row.get('ServerGroupId') == server_group_id and row.get('ServerType') == 'Ecs')
            require(row.get('Port') == 6432 and row.get('Status') == 'Available')
            require(type(row.get('Weight')) is int and 0 <= row['Weight'] <= 100)
            seen.add(node)
            rows.append(row)
        require(len(rows) <= total)
        token = body.get('NextToken', '')
        require(isinstance(token, str))
        if not token:
            require(len(rows) == total)
            return rows
        require(batch and token not in tokens and len(rows) < total)
        tokens.add(token)
    raise ValueError('Provider evidence rejected')


def snapshot(fetch_ess, fetch_group, fetch_ecs, fetch_nlb, *, group_id, server_group_id, now):
    """Read adapters return raw, complete scoped provider observations.

    fetch_nlb returns Servers (fully paginated), Listener and Health. Addresses
    are transient; publish strips them before the worker evidence write.
    """
    try:
        require(now.tzinfo is not None)
        group = fetch_group()
        require(group.get('ScalingGroupId') == group_id and group.get('VpcId') == VPC_ID)
        desired = group.get('DesiredCapacity')
        require(type(desired) is int and 2 <= desired <= 4)
        rows = _inventory.collect_members(fetch_ess, group_id)
        ids = sorted(row['InstanceId'] for row in rows)
        require(2 <= len(ids) <= 4)
        addresses = {}
        for row in fetch_ecs(ids):
            node = row.get('InstanceId')
            require(node in ids and node not in addresses)
            vpc = row.get('VpcAttributes', {})
            require(vpc.get('VpcId') == VPC_ID)
            ips = vpc.get('PrivateIpAddress', {}).get('IpAddress', [])
            require(isinstance(ips, list) and len(ips) == 1)
            address = ipaddress.ip_address(ips[0])
            require(address.version == 4 and address.is_private and not address.is_loopback and not address.is_link_local and not address.is_unspecified)
            require(str(address) not in addresses.values())
            addresses[node] = str(address)
        require(set(addresses) == set(ids))
        nlb = fetch_nlb()
        # Validate the injected adapter's complete list using the same validator.
        backends = collect_backends(lambda token: {'TotalCount':len(nlb['Servers']), 'Servers':nlb['Servers']}, server_group_id)
        registered = {row['ServerId']:row['Weight'] > 0 for row in backends}
        require(set(registered) <= set(ids))
        listener = nlb['Listener']
        require(isinstance(listener.get('ListenerId'), str) and listener['ListenerId'])
        require(listener.get('ServerGroupId') == server_group_id and listener.get('ListenerPort') == 6432 and listener.get('ListenerStatus') == 'Running')
        health = nlb['Health']
        require(not health.get('NextToken') and len(health['ListenerHealthStatus']) == 1)
        observed = health['ListenerHealthStatus'][0]
        require(observed.get('ListenerId') == listener['ListenerId'] and observed.get('ListenerPort') == 6432)
        require(len(observed['ServerGroupInfos']) == 1)
        info = observed['ServerGroupInfos'][0]
        require(info.get('ServerGroupId') == server_group_id and info.get('HeathCheckEnabled') is True and isinstance(info.get('NonNormalServers'), list))
        bad = set()
        for row in info['NonNormalServers']:
            node = row.get('ServerId')
            require(node in registered and node not in bad and row.get('Port') == 6432 and row.get('Status') in ('Initial','Unhealthy','Unavailable'))
            bad.add(node)
        return {'observedAt':now.timestamp(), 'desired':desired, 'members':ids,
                'healthyRegistered':sorted(node for node in ids if registered.get(node) and node not in bad),
                'privateAddresses':addresses}
    except Exception:
        raise ValueError('Provider refresh failed') from None


class Deadline:
    """One absolute limit shared by every operation in a refresh."""
    def __init__(self, when, *, monotonic=time.monotonic):
        self.when = when
        self.monotonic = monotonic

    def remaining(self):
        remaining = self.when - self.monotonic()
        require(remaining > 0)
        return remaining


def publish(snapshot, previous_targets, atomic_worker_write, update_targets, *, deadline=None):
    if deadline is not None:
        deadline.remaining()
    targets = tuple(sorted(snapshot['privateAddresses'].items()))
    if targets != previous_targets:
        update_targets(targets)
    if deadline is not None:
        deadline.remaining()
    atomic_worker_write({key:snapshot[key] for key in FIELDS})
    return targets


def atomic_write(path, value, *, deadline=None, clock=time.monotonic):
    """Write mode 0600 then fsync and replace in the destination directory."""
    path = Path(path)
    fd, temporary = tempfile.mkstemp(prefix='.'+path.name+'.', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, separators=(',', ':'))
            stream.flush()
            os.fsync(stream.fileno())
        if deadline is not None and clock() >= deadline:
            raise ValueError('Worker publication deadline exceeded')
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def run_loop(read, write, *, interval=15, max_seconds=5400, monotonic=time.monotonic, sleep=time.sleep):
    require(type(interval) is int and 1 <= interval <= 15 and type(max_seconds) is int and 1 <= max_seconds <= 5400)
    started = monotonic()
    runtime_end = started + max_seconds
    while monotonic() < runtime_end:
        tick = monotonic()
        deadline = Deadline(min(tick + interval, runtime_end), monotonic=monotonic)
        try:
            value = read(deadline=deadline)
            deadline.remaining()
            write(value, deadline=deadline)
            deadline.remaining()
        except Exception:
            raise ValueError('Provider feed stopped; refresh or publication failed') from None
        delay = min(tick + interval, runtime_end) - monotonic()
        if delay > 0:
            sleep(delay)


class Operator:
    """CLI adapters use argv locally; remote code is fixed and arguments quoted."""
    def __init__(self, args):
        self.args = args
        self.previous = ()
        self.deadline = None

    def command(self, argv):
        try:
            timeout = min(10, self.deadline.remaining()) if self.deadline is not None else 10
            result = subprocess.run(argv, check=True, capture_output=True, text=True, timeout=timeout).stdout
            if self.deadline is not None:
                self.deadline.remaining()
            return result
        except Exception:
            raise ValueError('Provider or Workbench command failed') from None

    def aliyun(self, service, operation, **kwargs):
        argv = ['aliyun', service, operation, '--profile', PROFILE, '--region', REGION]
        if (service, operation) in RETRYABLE_READS:
            argv += ['--retry-count', '2']
        for key, value in kwargs.items():
            argv += ['--'+key, str(value)]
        try:
            return json.loads(self.command(argv))
        except Exception:
            raise ValueError('Provider read failed') from None

    def fetch_group(self):
        body = self.aliyun('ess', 'DescribeScalingGroups', **{'ScalingGroupId.1':GROUP_ID, 'PageNumber':1, 'PageSize':1})
        require(body.get('PageNumber') == 1 and body.get('TotalCount') == 1)
        rows = body['ScalingGroups']['ScalingGroup']
        require(len(rows) == 1)
        return rows[0]

    def fetch_ess(self, page):
        return self.aliyun('ess', 'DescribeScalingInstances', ScalingGroupId=GROUP_ID, PageNumber=page, PageSize=50)

    def fetch_ecs(self, ids):
        body = self.aliyun('ecs', 'DescribeInstances', InstanceIds=json.dumps(ids), VpcId=VPC_ID, PageNumber=1, PageSize=100)
        require(body.get('TotalCount') == len(ids) and body.get('PageNumber') == 1)
        return body['Instances']['Instance']

    def fetch_nlb(self):
        servers = collect_backends(lambda token:self.aliyun('nlb','ListServerGroupServers',ServerGroupId=SERVER_GROUP_ID,MaxResults=100,NextToken=token), SERVER_GROUP_ID)
        listener = self.aliyun('nlb','GetListenerAttribute',ListenerId=self.args.listener_id)
        require(listener.get('ListenerId') == self.args.listener_id)
        health = self.aliyun('nlb','GetListenerHealthStatus',ListenerId=self.args.listener_id)
        return {'Servers':servers,'Listener':listener,'Health':health}

    def read(self, *, deadline=None):
        self.deadline = deadline
        # Timestamp at start, never after a slow provider read.
        return snapshot(self.fetch_ess,self.fetch_group,self.fetch_ecs,self.fetch_nlb,group_id=GROUP_ID,server_group_id=SERVER_GROUP_ID,now=datetime.now(timezone.utc))

    def workbench(self, script, *arguments):
        command = shlex.join(['python3','-c',script,*arguments])
        return self.command([self.args.workbench,'exec','-P',PROFILE,'-r',REGION,'-i',WORKER_ID,'-c',command])

    def remote(self, script, *arguments):
        # Worker monotonic time is independent of Mac/worker wall-clock skew.
        # A sample happened between send and receive. Subtract the entire RTT
        # from the budget at send, choosing the earliest possible worker expiry.
        deadline = self.deadline or Deadline(time.monotonic() + 10)
        sent = deadline.monotonic()
        budget = deadline.remaining()
        sample = json.loads(self.workbench(_CLOCK_PROBE))
        round_trip = deadline.monotonic() - sent
        deadline.remaining()
        require(0 <= round_trip <= 2)
        require(set(sample) == {'monotonic','boot'})
        worker_time = sample['monotonic']
        require(type(worker_time) in (int,float) and math.isfinite(worker_time) and worker_time >= 0)
        boot = sample['boot']
        require(isinstance(boot,str) and str(uuid.UUID(boot)) == boot)
        expires = worker_time + budget - round_trip - 0.1
        require(expires > worker_time)
        prefix = ('import time\n_remote_deadline = '+repr(expires)+
                  '\nif open(\"/proc/sys/kernel/random/boot_id\").read().strip() != '+repr(boot)+
                  ': raise ValueError(\"Worker clock identity changed\")'+
                  '\nif time.monotonic() >= _remote_deadline: raise ValueError(\"Remote deadline exceeded\")\n')
        return self.workbench(prefix + script,*arguments)

    def atomic_worker_write(self, value):
        # The remote worker needs only stdlib Python; no repository deployment.
        script = 'import json,os,tempfile,sys,time; from pathlib import Path\n' + _ATOMIC_SOURCE + '\natomic_write(sys.argv[1],json.loads(sys.argv[2]),deadline=globals().get(\"_remote_deadline\"))'
        self.remote(script,self.args.worker_inventory,json.dumps(value,separators=(',',':')))

    def existing_targets(self):
        rows = json.loads(self.remote(_TARGET_SCRIPT,self.args.kubeconfig))
        targets = {}
        require(isinstance(rows, list))
        for row in rows:
            labels = row['labels']
            require(all(labels.get(k) == v for k,v in {'env':'rdev.ali','db_proxy_code':'DP00001','target_db_code':'D00002'}.items()))
            node = labels['ecs_instance_id']
            require(isinstance(node, str) and node and node not in targets)
            require(len(row['targets']) == 1 and row['targets'][0].endswith(':9930'))
            address = str(ipaddress.IPv4Address(row['targets'][0][:-5]))
            require(ipaddress.ip_address(address).is_private)
            targets[node] = address
        return tuple(sorted(targets.items()))

    def update_targets(self, targets):
        values = [{'targets':[address+':9930'],'labels':{'env':'rdev.ali','db_proxy_code':'DP00001','target_db_code':'D00002','ecs_instance_id':node}} for node,address in targets]
        self.remote(_TARGET_SCRIPT,self.args.kubeconfig,json.dumps(values,separators=(',',':')))

    def write(self, value, *, deadline=None):
        self.deadline = deadline
        self.previous = publish(value,self.previous,self.atomic_worker_write,self.update_targets,deadline=deadline)


# Keep the remote write identical to the locally tested implementation.
import inspect
_ATOMIC_SOURCE = inspect.getsource(atomic_write)
_CLOCK_PROBE = '''import json,time
boot=open('/proc/sys/kernel/random/boot_id').read().strip()
print(json.dumps({'monotonic':time.monotonic(),'boot':boot}))
'''
_TARGET_SCRIPT = '''import json,os,stat,subprocess,sys,time
path=sys.argv[1]
assert stat.S_ISREG(os.stat(path).st_mode) and stat.S_IMODE(os.stat(path).st_mode)==0o600
args=['kubectl','--kubeconfig',path,'-n','raptor-test']
def run(extra):
 remaining=_remote_deadline-time.monotonic()
 if remaining<=0: raise ValueError('Remote deadline exceeded')
 return subprocess.run(args+extra,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=min(8,remaining)).stdout
body=json.loads(run(['get','configmap','pgcat-provider-inventory','-o','json']))
assert body['metadata']['name']=='pgcat-provider-inventory' and body['metadata']['namespace']=='raptor-test'
assert 'targets.json' in body['data']
if len(sys.argv)==2:
 print(body['data']['targets.json']); sys.exit(0)
patch={'metadata':{'resourceVersion':body['metadata']['resourceVersion']},'data':{'targets.json':sys.argv[2]}}
run(['patch','configmap','pgcat-provider-inventory','--type=merge','-p',json.dumps(patch)])
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--interval',type=int,default=15)
    parser.add_argument('--max-seconds',type=int,default=5400)
    parser.add_argument('--listener-id',default=LISTENER_ID)
    parser.add_argument('--workbench',required=True)
    parser.add_argument('--worker-inventory',required=True)
    parser.add_argument('--kubeconfig',required=True,help='already authorized private worker path; never transferred by this tool')
    args = parser.parse_args()
    try:
        require(args.worker_inventory.startswith('/') and args.kubeconfig.startswith('/') and args.worker_inventory != args.kubeconfig)
        operator = Operator(args)
        operator.previous = operator.existing_targets()
        run_loop(operator.read,operator.write,interval=args.interval,max_seconds=args.max_seconds)
    except Exception:
        parser.exit(1,'Provider feed stopped; evidence is no longer refreshed.\n')


if __name__ == '__main__':
    main()
