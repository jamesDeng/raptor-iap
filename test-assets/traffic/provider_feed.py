"""Bounded, fail-closed provider evidence. No cloud provisioning operations."""
import argparse
import copy
from capture_window import InventoryMode
import ipaddress
import json
import math
import os
from pathlib import Path
import shlex
import stat
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
FIELDS = ('schemaVersion', 'feedSession', 'sequence', 'observedAt', 'desired',
          'members', 'memberStates', 'backendStates', 'healthyRegistered')
LIFECYCLES = frozenset(('InService', 'Pending', 'Pending:Wait', 'Protected',
                        'Standby', 'Stopped', 'Removing', 'Removing:Wait'))
RETRYABLE_READS = frozenset({
    ('ess', 'DescribeScalingGroups'),
    ('ess', 'DescribeScalingInstances'),
    ('ecs', 'DescribeInstances'),
    ('nlb', 'ListServerGroupServers'),
    ('nlb', 'GetListenerAttribute'),
    ('nlb', 'GetListenerHealthStatus'),
})
FAILURE_STAGES = frozenset({
    'startup', 'ess-group', 'ess-members', 'ecs-instances', 'nlb-servers',
    'nlb-listener', 'nlb-health', 'targets-read', 'targets-update',
    'inventory-write', 'read', 'publish', 'loop', 'ess-transition',
    'nlb-backend-state', 'status-write',
})


class FeedStop(ValueError):
    """Only a fixed stage name may leave the local provider process."""
    def __init__(self, stage):
        self.stage = stage if stage in FAILURE_STAGES else 'loop'
        super().__init__('Provider feed stopped at '+self.stage+'; evidence is no longer refreshed')



def require(condition):
    if not condition:
        raise ValueError('Provider evidence rejected')


def collect_transition_members(fetch, group_id):
    """Read every ESS page without discarding pending or draining identities."""
    rows, seen, total = [], set(), None
    for page in range(1, 5):
        body = fetch(page)
        count = body.get('TotalCount')
        require(type(count) is int and 2 <= count <= 4)
        require(type(body.get('PageNumber')) is int and body['PageNumber'] == page)
        if total is None:
            total = count
        require(total == count)
        batch = body.get('ScalingInstances', {}).get('ScalingInstance')
        require(isinstance(batch, list) and batch)
        for row in batch:
            node = row.get('InstanceId')
            require(isinstance(node, str) and node and node not in seen)
            require(row.get('ScalingGroupId') == group_id)
            if row.get('LifecycleState') not in LIFECYCLES or row.get('HealthStatus') not in ('Healthy', 'Unhealthy'):
                raise FeedStop('ess-transition')
            seen.add(node)
            rows.append(row)
        require(len(rows) <= total)
        if len(rows) == total:
            return rows
    raise ValueError('Provider evidence rejected')


def collect_backends(fetch, server_group_id):
    rows, seen, tokens = [], set(), set()
    token, total = '', None
    for _ in range(100):
        body = fetch(token)
        count = body.get('TotalCount')
        require(type(count) is int and 0 <= count <= 4)
        if total is None:
            total = count
        require(total == count and isinstance(body.get('Servers'), list))
        batch = body['Servers']
        for row in batch:
            node = row.get('ServerId')
            require(isinstance(node, str) and node and node not in seen)
            require(row.get('ServerGroupId') == server_group_id and row.get('ServerType') == 'Ecs')
            require(row.get('Port') == 6432 and isinstance(row.get('Status'), str))
            if row['Status'] not in ('Adding', 'Available', 'Configuring', 'Removing'):
                raise FeedStop('nlb-backend-state')
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


def snapshot(fetch_ess, fetch_group, fetch_ecs, fetch_nlb, *, group_id, server_group_id, now,
             known_ids=(), allowed_departures=0, required_address_ids=()):
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
        rows = collect_transition_members(fetch_ess, group_id)
        ids = sorted(row['InstanceId'] for row in rows)
        require(2 <= len(ids) <= 4)
        known = set(known_ids)
        require(all(isinstance(node, str) and node for node in known))
        require(type(allowed_departures) is int and 0 <= allowed_departures <= 2)
        require(len(known | set(ids)) <= 4 and len(known - set(ids)) <= allowed_departures)
        nlb = fetch_nlb()
        # Validate the injected adapter's complete list using the same validator.
        backends = collect_backends(lambda token: {'TotalCount':len(nlb['Servers']), 'Servers':nlb['Servers']}, server_group_id)
        registered = {row['ServerId']:row for row in backends}
        require(set(registered) <= set(ids) | known)
        required_ids = set(ids) | set(registered)
        optional = {row['InstanceId'] for row in rows
                    if row['LifecycleState'] in ('Pending', 'Pending:Wait')
                    and row['InstanceId'] not in registered and row['InstanceId'] not in required_address_ids}
        addresses, seen_ecs = {}, set()
        ecs_rows = fetch_ecs(sorted(required_ids))
        require(isinstance(ecs_rows, list))
        for row in ecs_rows:
            node = row.get('InstanceId')
            require(node in required_ids and node not in seen_ecs)
            seen_ecs.add(node)
            vpc = row.get('VpcAttributes', {})
            require(vpc.get('VpcId') == VPC_ID)
            ips = vpc.get('PrivateIpAddress', {}).get('IpAddress', [])
            require(isinstance(ips, list))
            if not ips and node in optional:
                continue
            require(len(ips) == 1)
            address = ipaddress.ip_address(ips[0])
            require(address.version == 4 and address.is_private and not address.is_loopback and not address.is_link_local and not address.is_unspecified)
            require(str(address) not in addresses.values())
            addresses[node] = str(address)
        require(required_ids - set(addresses) <= optional)
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
        bad = {}
        for row in info['NonNormalServers']:
            node = row.get('ServerId')
            require(node in registered and node not in bad and row.get('Port') == 6432 and row.get('Status') in ('Initial','Unhealthy','Unavailable'))
            bad[node] = row['Status']
        return {'schemaVersion':2, 'observedAt':now.timestamp(), 'desired':desired, 'members':ids,
                'memberStates':[{'id':row['InstanceId'], 'lifecycle':row['LifecycleState'],
                                 'health':row['HealthStatus'], 'addressKnown':row['InstanceId'] in addresses}
                                for row in sorted(rows, key=lambda row:row['InstanceId'])],
                'backendStates':[{'id':node, 'status':registered[node]['Status'],
                                  'weight':registered[node]['Weight'], 'listenerHealth':bad.get(node, 'Normal')}
                                 for node in sorted(registered)],
                'healthyRegistered':sorted(node for node in ids if node in registered
                                           and registered[node]['Status'] == 'Available'
                                           and registered[node]['Weight'] > 0 and node not in bad),
                'privateAddresses':addresses}
    except FeedStop:
        raise
    except ValueError:
        raise ValueError('Provider refresh failed') from None
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


def publish(snapshot, previous_targets, atomic_worker_write, update_targets, *, deadline=None, retain_targets=()):
    if deadline is not None:
        deadline.remaining()
    addresses = dict(retain_targets)
    require(len(addresses) == len(retain_targets))
    for node, address in snapshot['privateAddresses'].items():
        require(node not in addresses or addresses[node] == address)
        addresses[node] = address
    require(len(set(addresses.values())) == len(addresses))
    targets = tuple(sorted(addresses.items()))
    if targets != previous_targets:
        update_targets(targets)
    if deadline is not None:
        deadline.remaining()
    inventory={key:snapshot[key] for key in FIELDS}
    if 'mode' in snapshot:inventory['mode']=snapshot['mode']
    atomic_worker_write(inventory)
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


def write_status_receipt(path, value, *, deadline=None):
    """Commit an operational receipt with no fallible durability step afterward.

    Unlike worker inventory, this local liveness hint needs no crash-durability
    guarantee. All flushes and deadline checks precede the atomic replacement,
    so a failed local write leaves the last successful receipt unchanged.
    """
    if deadline is not None:
        deadline.remaining()
    fd, temporary = tempfile.mkstemp(prefix='.'+path.name+'.', dir=path.parent)
    committed = False
    try:
        with os.fdopen(fd, 'w') as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, separators=(',', ':'))
            stream.flush()
            os.fsync(stream.fileno())
        if deadline is not None:
            deadline.remaining()
        os.replace(temporary, path)
        committed = True
    finally:
        if not committed:
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
        except FeedStop:
            raise
        except Exception:
            raise FeedStop('loop') from None
        delay = min(tick + interval, runtime_end) - monotonic()
        if delay > 0:
            sleep(delay)


class Operator:
    """CLI adapters use argv locally; remote code is fixed and arguments quoted."""
    def __init__(self, args):
        self.args = args
        self.mode = getattr(args, 'mode', 'full-replacement')
        require(self.mode in ('full-replacement', 'disabled-rehearsal'))
        self.inventory_mode = InventoryMode()
        self.previous = ()
        self.retained_targets = ()
        self.known_ids = set()
        self.backend_ids = set()
        self.required_address_ids = set()
        self.previous_desired = None
        self.allowed_departures = 0
        self.feedSession = str(uuid.uuid4())
        self.sequence = 0
        self.deadline = None
        self.stage = 'startup'
        self.stopped = False
        status_file = getattr(args, 'status_file', None)
        self.status_file = Path(status_file) if status_file is not None else None
        if self.status_file is not None:
            require(self.status_file.is_absolute() and not os.path.lexists(self.status_file))
            require(self.status_file.parent.is_dir())
            parent = self.status_file.parent.lstat()
            require(stat.S_ISDIR(parent.st_mode) and stat.S_IMODE(parent.st_mode) == 0o700
                    and parent.st_uid == os.getuid())

    def command(self, argv):
        try:
            timeout = min(10, self.deadline.remaining()) if self.deadline is not None else 10
            environment = dict(os.environ)
            # Python <3.10 on macOS has a process-relative monotonic().
            # Startup reads precede the first tick and retain their 10s timeout.
            environment['PGCAT_LANE_DEADLINE_MONOTONIC'] = str(
                time.clock_gettime(time.CLOCK_MONOTONIC) +
                (self.deadline.remaining() if self.deadline is not None else timeout))
            result = subprocess.run(argv, check=True, capture_output=True, text=True, timeout=timeout, env=environment).stdout
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
        self.stage = 'ess-group'
        body = self.aliyun('ess', 'DescribeScalingGroups', **{'ScalingGroupId.1':GROUP_ID, 'PageNumber':1, 'PageSize':1})
        require(body.get('PageNumber') == 1 and body.get('TotalCount') == 1)
        rows = body['ScalingGroups']['ScalingGroup']
        require(len(rows) == 1)
        return rows[0]

    def fetch_ess(self, page):
        self.stage = 'ess-members'
        return self.aliyun('ess', 'DescribeScalingInstances', ScalingGroupId=GROUP_ID, PageNumber=page, PageSize=50)

    def fetch_ecs(self, ids):
        self.stage = 'ecs-instances'
        body = self.aliyun('ecs', 'DescribeInstances', InstanceIds=json.dumps(ids), VpcId=VPC_ID, PageNumber=1, PageSize=100)
        rows = body['Instances']['Instance']
        require(isinstance(rows, list) and type(body.get('TotalCount')) is int)
        require(body['TotalCount'] == len(rows) <= len(ids) and body.get('PageNumber') == 1)
        return rows

    def fetch_nlb(self):
        self.stage = 'nlb-servers'
        servers = collect_backends(lambda token:self.aliyun('nlb','ListServerGroupServers',ServerGroupId=SERVER_GROUP_ID,MaxResults=100,NextToken=token), SERVER_GROUP_ID)
        self.stage = 'nlb-listener'
        listener = self.aliyun('nlb','GetListenerAttribute',ListenerId=self.args.listener_id)
        require(listener.get('ListenerId') == self.args.listener_id)
        self.stage = 'nlb-health'
        health = self.aliyun('nlb','GetListenerHealthStatus',ListenerId=self.args.listener_id)
        return {'Servers':servers,'Listener':listener,'Health':health}

    def read(self, *, deadline=None):
        self.deadline = deadline
        self.stage = 'read'
        # Timestamp at start, never after a slow provider read.
        try:
            # Capture the desired decrease before checking a possibly delayed ESS departure.
            now = datetime.now(timezone.utc)
            group = self.fetch_group()
            allowance = self.departures_for(group.get('DesiredCapacity'))
            return snapshot(self.fetch_ess,lambda:group,self.fetch_ecs,self.fetch_nlb,
                            group_id=GROUP_ID,server_group_id=SERVER_GROUP_ID,now=now,
                            known_ids=self.known_ids,allowed_departures=allowance,
                            required_address_ids=self.required_address_ids)
        except FeedStop:
            raise
        except Exception:
            raise FeedStop(self.stage) from None

    def departures_for(self, desired):
        # Count each sequential decrease once; later equal-desired ticks retain it.
        expected = ((3, 2),) if self.mode == 'disabled-rehearsal' else ((4, 3), (3, 2))
        if self.allowed_departures < len(expected) and (self.previous_desired, desired) == expected[self.allowed_departures]:
            return self.allowed_departures + 1
        return self.allowed_departures

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
        self.stage = 'inventory-write'
        # The remote worker needs only stdlib Python; no repository deployment.
        script = 'import json,os,tempfile,sys,time; from pathlib import Path\n' + _ATOMIC_SOURCE + '\natomic_write(sys.argv[1],json.loads(sys.argv[2]),deadline=globals().get(\"_remote_deadline\"))'
        self.remote(script,self.args.worker_inventory,json.dumps(value,separators=(',',':')))

    def existing_targets(self):
        self.stage = 'targets-read'
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
        self.stage = 'targets-update'
        values = [{'targets':[address+':9930'],'labels':{'env':'rdev.ali','db_proxy_code':'DP00001','target_db_code':'D00002','ecs_instance_id':node}} for node,address in targets]
        self.remote(_TARGET_SCRIPT,self.args.kubeconfig,json.dumps(values,separators=(',',':')))

    def write(self, value, *, deadline=None):
        if self.stopped:
            raise FeedStop(self.stage)
        self.deadline = deadline
        self.stage = 'publish'
        try:
            require(value.get('mode', self.mode) == self.mode)
            require(value.get('feedSession', self.feedSession) == self.feedSession)
            candidate = dict(value, feedSession=self.feedSession, sequence=self.sequence + 1)
            if self.mode == 'disabled-rehearsal':candidate['mode'] = self.mode
            mode_state = copy.deepcopy(self.inventory_mode)
            mode_state.observe(candidate)
            targets = publish(candidate,self.previous,self.atomic_worker_write,self.update_targets,
                              deadline=deadline,retain_targets=self.retained_targets)
            self.inventory_mode = mode_state
            self.previous = self.retained_targets = targets
            self.known_ids.update(value['members'])
            self.backend_ids.update(row['id'] for row in value['backendStates'])
            self.required_address_ids.update(value['privateAddresses'])
            self.required_address_ids.update(self.backend_ids)
            self.required_address_ids.update(row['id'] for row in value['memberStates']
                                             if row['lifecycle'] not in ('Pending', 'Pending:Wait'))
            self.allowed_departures = self.departures_for(value['desired'])
            self.previous_desired = value['desired']
            self.sequence += 1
            if self.status_file is not None:
                self.stage = 'status-write'
                receipt = {'feedSession':self.feedSession, 'sequence':self.sequence,
                           'mode':self.mode, 'desired':candidate['desired'],
                           'observedAt':candidate['observedAt']}
                write_status_receipt(self.status_file, receipt, deadline=deadline)
        except Exception:
            # Neither uncertain worker writes nor a later local failure may
            # replay publication through this operator.
            self.stopped = True
            raise FeedStop(self.stage) from None


# Keep the remote write identical to the locally tested implementation.
import inspect
_ATOMIC_SOURCE = inspect.getsource(atomic_write)
_CLOCK_PROBE = '''import json,time
boot=open('/proc/sys/kernel/random/boot_id').read().strip()
print(json.dumps({'monotonic':time.monotonic(),'boot':boot}))
'''
_TARGET_SCRIPT = '''import json,os,stat,subprocess,sys,time
path=sys.argv[1]
if not (stat.S_ISREG(os.stat(path).st_mode) and stat.S_IMODE(os.stat(path).st_mode)==0o600): raise ValueError('Invalid kubeconfig mode')
args=['kubectl','--kubeconfig',path,'-n','raptor-test']
def run(extra):
 remaining=_remote_deadline-time.monotonic()
 if remaining<=0: raise ValueError('Remote deadline exceeded')
 return subprocess.run(args+extra,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=min(8,remaining)).stdout
body=json.loads(run(['get','configmap','pgcat-provider-inventory','-o','json']))
if not (body['metadata']['name']=='pgcat-provider-inventory' and body['metadata']['namespace']=='raptor-test'): raise ValueError('Unexpected ConfigMap identity')
if not ('targets.json' in body['data']): raise ValueError('Missing targets key')
if len(sys.argv)==2:
 print(body['data']['targets.json']); sys.exit(0)
patch={'metadata':{'resourceVersion':body['metadata']['resourceVersion']},'data':{'targets.json':sys.argv[2]}}
run(['patch','configmap','pgcat-provider-inventory','--type=merge','-p',json.dumps(patch)])
verified=json.loads(run(['get','configmap','pgcat-provider-inventory','-o','json']))
if not (verified['metadata']['name']=='pgcat-provider-inventory' and verified['metadata']['namespace']=='raptor-test'): raise ValueError('Unexpected readback identity')
if not (json.loads(verified['data']['targets.json'])==json.loads(sys.argv[2])): raise ValueError('Target readback mismatch')
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode',choices=('full-replacement','disabled-rehearsal'),default='full-replacement',
                        help='bind this feed to the full replacement or disabled 2-to-3-to-2 rehearsal')
    parser.add_argument('--interval',type=int,default=15)
    parser.add_argument('--max-seconds',type=int,default=5400)
    parser.add_argument('--listener-id',default=LISTENER_ID)
    parser.add_argument('--workbench',required=True)
    parser.add_argument('--worker-inventory',required=True)
    parser.add_argument('--status-file',metavar='ABSOLUTE_PATH',
                        help='optional initially absent local receipt in an existing mode-0700 directory')
    parser.add_argument('--kubeconfig',required=True,help='already authorized private worker path; never transferred by this tool')
    args = parser.parse_args()
    try:
        require(args.worker_inventory.startswith('/') and args.kubeconfig.startswith('/') and args.worker_inventory != args.kubeconfig)
        operator = Operator(args)
        operator.previous = operator.existing_targets()
        operator.stage = 'loop'
        run_loop(operator.read,operator.write,interval=args.interval,max_seconds=args.max_seconds)
    except FeedStop as error:
        parser.exit(1,str(error)+'\n')
    except Exception:
        parser.exit(1,str(FeedStop(operator.stage if 'operator' in locals() else 'startup'))+'\n')


if __name__ == '__main__':
    main()
