"""Bounded, fail-closed provider evidence. No cloud provisioning operations."""
import argparse
import copy
import hashlib
from capture_window import InventoryMode
from pgcat_lane_protocol import (roster_sha, encode, target_valid, original_roster,
    validate_no_scale_stop, no_scale_stop_ack, NO_SCALE_STOP_INTENT, NO_SCALE_STOP_ACK, Refused,
    REHEARSAL_STOP_INTENT, REHEARSAL_STOP_ACK, validate_rehearsal_stop_receipt,
    rehearsal_stop_ack, Journal, private_json)
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




def _wrapper_command(path, arguments):
    from pgcat_source_bootstrap import command, current_hashes
    return command(current_hashes(), 'laneWrapper', arguments)


def _require_verified():
    from pgcat_source_bootstrap import require_verified
    try: require_verified(__name__)
    except (ValueError, KeyError): raise ValueError('source') from None

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
    stay in the private worker sidecar; the published inventory omits them.
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


def _unique_pairs(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value)
        value[key] = item
    return value


def validate_run_context(root, path, *, no_scale=False, mode='disabled-rehearsal'):
    """Bind context and requested feed guards to the active journal mode."""
    from pgcat_lane_protocol import Journal, encode, identifier, private, read_private
    import pgcat_lane_supervisor as supervisor
    root, path = Path(root), Path(path)
    require(root.is_absolute() and path == root/'run-context.json')
    private(root, True)
    folder = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        raw = read_private(folder, 'run-context.json', 2048)
        startup = json.loads(read_private(folder, 'supervisor-startup.json', 2048,
                                          publication_marker=True).decode('ascii'),
                             object_pairs_hook=_unique_pairs)
    finally:
        os.close(folder)
    context = json.loads(raw.decode('ascii'), object_pairs_hook=_unique_pairs)
    require(type(context) is dict and set(context) == {
        'schemaVersion','runId','supervisorId','targetSha256','bundleSha256','nonce'})
    require(type(context['schemaVersion']) is int and context['schemaVersion'] == 1)
    for key in ('runId','supervisorId','nonce'):
        identifier(context[key])
    require(type(startup) is dict and set(startup) == {'runId','supervisorId','contextNonce'})
    require((context['runId'],context['supervisorId'],context['nonce']) ==
            (startup['runId'],startup['supervisorId'],startup['contextNonce']))
    private(supervisor.GLOBAL_ROOT, True)
    active_folder = os.open(supervisor.GLOBAL_ROOT,
                            os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        active = json.loads(read_private(active_folder, 'active-run', 2048).decode('ascii'),
                            object_pairs_hook=_unique_pairs)
    finally:
        os.close(active_folder)
    require(active == {'runId':context['runId'],'supervisorId':context['supervisorId'],
                       'laneRoot':str(root)})
    journal = Journal(root).run_snapshot()
    from pgcat_lane_protocol import _release_admission
    _release_admission(journal['mode'])
    require(journal['runState'] == 'active' and journal['runId'] == context['runId'])
    require(type(no_scale) is bool and mode in ('disabled-rehearsal','full-replacement-direct-v2') and
            journal['mode'] in ('no-scale','disabled-rehearsal','full-replacement-direct-v2') and
            (journal['mode']=='no-scale')==no_scale and
            (journal['mode']=='full-replacement-direct-v2')==(mode=='full-replacement-direct-v2'))
    if mode=='full-replacement-direct-v2':Journal(root).native_plan()
    target = journal['target']
    require(target['scalingGroupId'] == GROUP_ID and target['nlbServerGroupId'] == SERVER_GROUP_ID
            and target['workerId'] == WORKER_ID and target['regionId'] == REGION)
    require(context['targetSha256'] == hashlib.sha256(encode(target)).hexdigest())
    hashes = dict(journal['hashes'])
    bundle = hashes.pop('bundleSha256')
    require(context['bundleSha256'] == bundle == hashlib.sha256(encode(hashes)).hexdigest())
    return context


def write_private_publication(contract_dir, context, session, sequence, observed_at,
                              addresses, inventory_path, inventory_raw, deadline=None):
    """Worker-side, single-call private sidecar then address-free inventory."""
    def check(value):
        if not value:
            raise ValueError('Worker publication rejected')
    def unique(pairs):
        value = {}
        for key, item in pairs:
            check(key not in value)
            value[key] = item
        return value
    def directory(fd):
        info = os.fstat(fd)
        check(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
              and info.st_uid == os.getuid())
    def open_private_directory(path):
        parts = Path(path).parts
        check(parts and parts[0] == '/' and '..' not in parts)
        fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            for part in parts[1:]:
                next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                  dir_fd=fd)
                os.close(fd)
                fd = next_fd
            directory(fd)
            return fd
        except Exception:
            os.close(fd)
            raise ValueError('Worker publication rejected') from None
    def write_once(fd, name, raw):
        item = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                       0o600, dir_fd=fd)
        try:
            os.fchmod(item, 0o600)
            while raw:
                raw = raw[os.write(item, raw):]
            os.fsync(item)
        finally:
            os.close(item)
        os.fsync(fd)
    def read_once(fd, name, maximum):
        try:
            item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
        except OSError:
            raise ValueError('Worker publication rejected') from None
        try:
            before = os.fstat(item)
            check(stat.S_ISREG(before.st_mode) and stat.S_IMODE(before.st_mode) == 0o600
                  and before.st_uid == os.getuid() and before.st_nlink == 1
                  and before.st_size <= maximum)
            raw = os.read(item, maximum + 1)
            after = os.fstat(item)
            current = os.stat(name, dir_fd=fd, follow_symlinks=False)
            check(len(raw) <= maximum and
                  (before.st_dev,before.st_ino,before.st_size,before.st_mtime_ns,before.st_ctime_ns) ==
                  (after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns,after.st_ctime_ns) ==
                  (current.st_dev,current.st_ino,current.st_size,current.st_mtime_ns,current.st_ctime_ns))
            return raw
        finally:
            os.close(item)
    check(type(context) is dict and set(context) == {'schemaVersion','runId','supervisorId',
          'targetSha256','bundleSha256','nonce'} and context['schemaVersion'] == 1)
    check(type(session) is str and type(sequence) is int and 1 <= sequence <= 999999)
    check(type(observed_at) in (int,float) and type(addresses) is dict and
          2 <= len(addresses) <= 4 and len(set(addresses.values())) == len(addresses))
    check(type(inventory_raw) is bytes and len(inventory_raw) <= 16384)
    contract_path = Path(contract_dir)
    inventory_file = Path(inventory_path)
    check(contract_path.is_absolute() and contract_path.name == 'contract' and
          inventory_file.is_absolute() and inventory_file.name == 'provider-inventory.json' and
          inventory_file.parent.name == 'run' and
          inventory_file.parent.parent == contract_path.parent)
    inventory = json.loads(inventory_raw.decode('ascii'), object_pairs_hook=unique)
    check(type(inventory) is dict and inventory.get('feedSession') == session and
          inventory.get('sequence') == sequence and inventory.get('observedAt') == observed_at)
    semantic_raw = json.dumps(inventory, sort_keys=True, separators=(',', ':'),
                              allow_nan=False).encode('ascii')
    record = {'schemaVersion':1,'runContext':context,'feedSession':session}
    record_raw = json.dumps(record,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')
    sidecar = {'schemaVersion':1,'runId':context['runId'],
        'supervisorId':context['supervisorId'],'contextNonce':context['nonce'],
        'targetSha256':context['targetSha256'],'bundleSha256':context['bundleSha256'],
        'feedSession':session,'sequence':sequence,'observedAt':observed_at,
        'inventorySha256':hashlib.sha256(inventory_raw).hexdigest(),
        'inventorySemanticSha256':hashlib.sha256(semantic_raw).hexdigest(),
        'privateAddresses':addresses}
    sidecar_raw = json.dumps(sidecar,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')
    check(len(record_raw) <= 2048 and len(sidecar_raw) <= 4096)
    parent = open_private_directory(contract_dir)
    try:
        directory(parent)
        if sequence == 1:
            try:
                os.mkdir('provider-source', 0o700, dir_fd=parent)
            except FileExistsError:
                raise ValueError('Worker publication rejected') from None
            os.fsync(parent)
        source = os.open('provider-source', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                         dir_fd=parent)
        try:
            directory(source)
            if sequence == 1:
                write_once(source, 'run-context.json', record_raw)
            else:
                check(read_once(source, 'run-context.json', 2048) == record_raw)
                previous = json.loads(read_once(source, '%06d.json' % (sequence-1), 4096)
                                      .decode('ascii'), object_pairs_hook=unique)
                check(type(previous) is dict and previous.get('feedSession') == session and
                      previous.get('sequence') == sequence-1 and
                      previous.get('runId') == context['runId'] and
                      previous.get('contextNonce') == context['nonce'])
                prior_path = Path(inventory_path)
                prior_folder = open_private_directory(str(prior_path.parent))
                try:
                    directory(prior_folder)
                    prior_raw = read_once(prior_folder, prior_path.name, 16384)
                finally:
                    os.close(prior_folder)
                prior_inventory = json.loads(prior_raw.decode('ascii'), object_pairs_hook=unique)
                check(type(prior_inventory) is dict and
                      prior_inventory.get('feedSession') == session and
                      prior_inventory.get('sequence') == sequence-1 and
                      hashlib.sha256(prior_raw).hexdigest() == previous.get('inventorySha256'))
            write_once(source, '%06d.json' % sequence, sidecar_raw)
        finally:
            os.close(source)
    finally:
        os.close(parent)
    check(deadline is None or time.monotonic() < deadline)
    path = Path(inventory_path)
    folder = open_private_directory(str(path.parent))
    try:
        directory(folder)
        try:
            existing = os.stat(path.name, dir_fd=folder, follow_symlinks=False)
        except FileNotFoundError:
            existing = None
        if existing is not None:
            check(stat.S_ISREG(existing.st_mode) and stat.S_IMODE(existing.st_mode) == 0o600
                  and existing.st_uid == os.getuid() and existing.st_nlink == 1)
        temporary = '.' + path.name + '.' + str(sequence) + '.staging'
        write_once(folder, temporary, inventory_raw)
        check(deadline is None or time.monotonic() < deadline)
        os.replace(temporary, path.name, src_dir_fd=folder, dst_dir_fd=folder)
        os.fsync(folder)
    finally:
        os.close(folder)


def run_loop(read, write, *, interval=15, max_seconds=5400, monotonic=time.monotonic, sleep=time.sleep, stop=None):
    require(type(interval) is int and 1 <= interval <= 15 and type(max_seconds) is int and 1 <= max_seconds <= 5400)
    started = monotonic()
    runtime_end = started + max_seconds
    while monotonic() < runtime_end:
        if stop is not None and stop():
            return
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
        if stop is not None and stop():
            return
        delay = min(tick + interval, runtime_end) - monotonic()
        if delay > 0:
            sleep(delay)
    # The final sleep can contain a stop intent. Acknowledge it without
    # extending the provider window or fabricating another observation.
    if stop is not None:
        stop()


class Operator:
    """CLI adapters use argv locally; remote code is fixed and arguments quoted."""
    def __init__(self, args):
        self.args = args
        self.mode = getattr(args, 'mode', 'full-replacement')
        require(self.mode in ('full-replacement', 'disabled-rehearsal', 'full-replacement-direct-v2'))
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
        run_context = getattr(args, 'run_context', None)
        self.run_context = None
        if run_context is not None:
            require(self.mode in ('disabled-rehearsal','full-replacement-direct-v2'))
            if self.mode=='full-replacement-direct-v2':
                from pgcat_lane_protocol import _release_admission
                try:_release_admission(self.mode)
                except Refused:raise ValueError('native startup') from None
            context_path = Path(run_context)
            contract = Path(args.contract_dir)
            inventory = Path(args.worker_inventory)
            require(context_path.is_absolute() and contract.is_absolute() and
                    '..' not in contract.parts and contract.name == 'contract' and
                    inventory.name == 'provider-inventory.json' and
                    inventory.parent.name == 'run' and
                    inventory.parent.parent == contract.parent)
            self.run_context = validate_run_context(context_path.parent, context_path,
                no_scale=getattr(args, 'no_scale_target', None) is not None,mode=self.mode)
        if self.run_context is not None and getattr(args,'no_scale_target',None) is None:
            require(type(args.interval) is int and args.interval == 15 and
                    type(args.max_seconds) is int and 75 <= args.max_seconds <= 5400)
            for name in (REHEARSAL_STOP_INTENT,REHEARSAL_STOP_ACK):
                require(self.status_file is not None and not os.path.lexists(self.status_file.parent/name))
        self.no_scale_target = None
        self.stop_ack = None
        raw_target = getattr(args, 'no_scale_target', None)
        if raw_target is not None:
            try:
                require(self.mode == 'disabled-rehearsal' and self.run_context is not None
                        and self.status_file is not None)
                require(type(args.interval) is int and args.interval == 15 and
                        type(args.max_seconds) is int and args.max_seconds == 180)
                target = json.loads(raw_target, object_pairs_hook=_unique_pairs)
                target_valid(target)
                original_roster(target['originalInstanceIds'])
                require(target['scalingGroupId'] == GROUP_ID and target['nlbServerGroupId'] == SERVER_GROUP_ID
                        and target['workerId'] == WORKER_ID and target['regionId'] == REGION)
                require(hashlib.sha256(encode(target)).hexdigest() == self.run_context['targetSha256'])
                for name in (NO_SCALE_STOP_INTENT, NO_SCALE_STOP_ACK):
                    require(not os.path.lexists(self.status_file.parent/name))
                self.no_scale_target = target
            except (Refused, ValueError, TypeError, KeyError):
                raise FeedStop('startup') from None

    def exact_no_scale(self, value):
        if self.no_scale_target is not None:
            require(type(value.get('desired')) is int and value['desired'] == 2 and
                    {key:value.get(key) for key in original_roster(self.no_scale_target['originalInstanceIds'])}
                    == original_roster(self.no_scale_target['originalInstanceIds']))

    def stop_no_scale(self):
        """Check at both tick boundaries, then acknowledge the actual last receipt."""
        if self.no_scale_target is None:
            return False
        from pgcat_lane_supervisor import read_json, save_once
        path = self.status_file.parent/NO_SCALE_STOP_INTENT
        try:
            if not os.path.lexists(path):
                require(self.stop_ack is None)
                return False
            # This local stop boundary is outside a provider publication. A
            # request arriving during sleep must not inherit the expired tick.
            self.deadline = None
            self.verify_run_context()
            intent = read_json(path, publication_marker=True)
            validate_no_scale_stop(intent)
            require(intent['runId'] == self.run_context['runId'] and
                    intent['feedSession'] == self.feedSession and
                    intent['minimumSequence'] <= self.sequence <= intent['minimumSequence']+1)
            receipt = read_json(self.status_file)
            require(receipt['sequence'] == self.sequence and receipt['desired'] == 2 and
                    receipt['mode'] == 'disabled-rehearsal' and
                    receipt['rosterSha256'] == roster_sha(original_roster(self.no_scale_target['originalInstanceIds'])))
            ack = no_scale_stop_ack(intent, receipt)
            if self.stop_ack is None:
                save_once(path.parent, NO_SCALE_STOP_ACK, ack, atomic=True)
                self.stop_ack = ack
            require(encode(self.stop_ack) == encode(ack) and
                    encode(read_json(path.parent/NO_SCALE_STOP_ACK, publication_marker=True)) == encode(ack))
            self.stopped = True
            return True
        except (Refused, OSError, ValueError, TypeError, KeyError):
            self.stopped = True
            raise FeedStop('loop') from None

    def stop_rehearsal(self):
        """Acknowledge only the actual final publication after sealed reconciliation."""
        if self.run_context is None or self.no_scale_target is not None:
            return False
        from pgcat_lane_supervisor import save_once
        path=self.status_file.parent/REHEARSAL_STOP_INTENT
        try:
            require(self.mode == 'disabled-rehearsal' and self.args.mode == self.mode and
                    getattr(self.args,'no_scale_target',None) is None)
            if not os.path.lexists(path):
                require(self.stop_ack is None and not os.path.lexists(path.parent/REHEARSAL_STOP_ACK))
                return False
            self.deadline=None
            self.verify_run_context()
            journal=Journal(Path(self.args.run_context).parent)
            run=journal.run_snapshot();proof=journal.reconcile_binding()
            intent,raw=private_json(path)
            require(raw == encode(intent) and intent['runId'] == self.run_context['runId'])
            receipt,_=private_json(self.status_file)
            require(receipt['feedSession'] == self.feedSession and receipt['sequence'] == self.sequence)
            validate_rehearsal_stop_receipt(intent,receipt,proof,run)
            ack=rehearsal_stop_ack(intent,receipt)
            if self.stop_ack is None:
                save_once(path.parent,REHEARSAL_STOP_ACK,ack,atomic=True)
                self.stop_ack=ack
            actual,raw=private_json(path.parent/REHEARSAL_STOP_ACK)
            require(raw == encode(ack) and encode(actual) == encode(self.stop_ack) == encode(ack))
            self.stopped=True
            return True
        except (Refused,OSError,ValueError,UnicodeError,TypeError,KeyError):
            self.stopped=True
            raise FeedStop('loop') from None

    def verify_run_context(self):
        if self.run_context is not None:
            require(self.mode in ('disabled-rehearsal','full-replacement-direct-v2') and self.args.mode == self.mode)
            path = Path(self.args.run_context)
            require(validate_run_context(path.parent, path,
                no_scale=getattr(self.args, 'no_scale_target', None) is not None,mode=self.mode) == self.run_context)
            if self.deadline is not None:
                self.deadline.remaining()

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
            value = snapshot(self.fetch_ess,lambda:group,self.fetch_ecs,self.fetch_nlb,
                            group_id=GROUP_ID,server_group_id=SERVER_GROUP_ID,now=now,
                            known_ids=self.known_ids,allowed_departures=allowance,
                            required_address_ids=self.required_address_ids)
            self.exact_no_scale(value)
            return value
        except FeedStop:
            raise
        except Exception:
            raise FeedStop(self.stage) from None

    def departures_for(self, desired):
        # The direct action permits the original pair to leave asynchronously.
        if self.mode == 'full-replacement-direct-v2':
            if self.allowed_departures == 0 and (self.previous_desired, desired) == (4, 2):
                return 2
            return self.allowed_departures
        # Count each sequential decrease once; later equal-desired ticks retain it.
        expected = ((3, 2),) if self.mode == 'disabled-rehearsal' else ((4, 3), (3, 2))
        if self.allowed_departures < len(expected) and (self.previous_desired, desired) == expected[self.allowed_departures]:
            return self.allowed_departures + 1
        return self.allowed_departures

    def workbench(self, script, *arguments):
        self.verify_run_context()
        command = shlex.join(['python3','-c',script,*arguments])
        return self.command(_wrapper_command(self.args.workbench,
            ['exec','-P',PROFILE,'-r',REGION,'-i',WORKER_ID,'-c',command]))

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

    def atomic_worker_write(self, value, snapshot=None):
        self.stage = 'inventory-write'
        # The remote worker needs only stdlib Python; no repository deployment.
        if self.run_context is not None:
            require(snapshot is not None)
            raw = json.dumps(value,separators=(',',':'),allow_nan=False).encode('ascii')
            script = ('import hashlib,json,os,stat,sys,time; from pathlib import Path\n' +
                      _PRIVATE_PUBLICATION_SOURCE + '\nwrite_private_publication('
                      'sys.argv[1],json.loads(sys.argv[2]),sys.argv[3],int(sys.argv[4]),'
                      'float(sys.argv[5]),json.loads(sys.argv[6]),sys.argv[7],'
                      'sys.argv[8].encode("ascii"),deadline=globals().get("_remote_deadline"))')
            self.remote(script,self.args.contract_dir,
                        json.dumps(self.run_context,sort_keys=True,separators=(',',':')),
                        self.feedSession,str(snapshot['sequence']),str(snapshot['observedAt']),
                        json.dumps(snapshot['privateAddresses'],sort_keys=True,separators=(',',':')),
                        self.args.worker_inventory,raw.decode('ascii'))
            return
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
            self.verify_run_context()
            require(value.get('mode', self.mode) == self.mode)
            require(value.get('feedSession', self.feedSession) == self.feedSession)
            self.exact_no_scale(value)
            if self.no_scale_target is not None:
                require(tuple(sorted(value['privateAddresses'].items())) == self.previous and
                        [node for node,_ in self.previous] == self.no_scale_target['originalInstanceIds'])
            if self.mode == 'full-replacement-direct-v2':
                require(value.get('sequence', self.sequence + 1) == self.sequence + 1)
            candidate = dict(value, feedSession=self.feedSession, sequence=self.sequence + 1)
            if self.mode != 'full-replacement':candidate['mode'] = self.mode
            mode_state = copy.deepcopy(self.inventory_mode)
            mode_state.observe(candidate)
            worker_write = self.atomic_worker_write if self.run_context is None else (
                lambda inventory:self.atomic_worker_write(inventory,candidate))
            targets = publish(candidate,self.previous,worker_write,self.update_targets,
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
                self.verify_run_context()
                receipt = {'feedSession':self.feedSession, 'sequence':self.sequence,
                           'mode':self.mode, 'desired':candidate['desired'],
                           'observedAt':candidate['observedAt'],
                           'rosterSha256':roster_sha(candidate)}
                write_status_receipt(self.status_file, receipt, deadline=deadline)
        except Exception:
            # Neither uncertain worker writes nor a later local failure may
            # replay publication through this operator.
            self.stopped = True
            raise FeedStop(self.stage) from None


# Keep the remote write identical to the locally tested implementation.
import inspect
_ATOMIC_SOURCE = inspect.getsource(atomic_write)
_PRIVATE_PUBLICATION_SOURCE = inspect.getsource(write_private_publication)
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
    _require_verified()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode',choices=('full-replacement','disabled-rehearsal','full-replacement-direct-v2'),default='full-replacement',
                        help='select historical staged replacement, disabled rehearsal, or direct-v2 topology')
    parser.add_argument('--no-scale-target',help='fixed address-free target, only for the finite no-scale proof')
    parser.add_argument('--interval',type=int,default=15)
    parser.add_argument('--max-seconds',type=int,default=5400)
    parser.add_argument('--listener-id',default=LISTENER_ID)
    parser.add_argument('--workbench',required=True)
    parser.add_argument('--worker-inventory',required=True)
    parser.add_argument('--run-context',required=True,
                        help='fixed supervisor context at laneRoot/run-context.json')
    parser.add_argument('--contract-dir',required=True,
                        help='reviewed private worker contract directory')
    parser.add_argument('--status-file',required=True,metavar='ABSOLUTE_PATH',
                        help='initially absent local receipt in an existing mode-0700 directory')
    parser.add_argument('--kubeconfig',required=True,help='already authorized private worker path; never transferred by this tool')
    args = parser.parse_args()
    try:
        if args.mode=='full-replacement-direct-v2':
            from pgcat_lane_protocol import _release_admission
            _release_admission(args.mode)
        require(args.worker_inventory.startswith('/') and args.kubeconfig.startswith('/') and args.worker_inventory != args.kubeconfig)
        operator = Operator(args)
        operator.previous = operator.existing_targets()
        if operator.no_scale_target is not None:
            require([node for node,_ in operator.previous] == operator.no_scale_target['originalInstanceIds'])
        operator.stage = 'loop'
        run_loop(operator.read,operator.write,interval=args.interval,max_seconds=args.max_seconds,
                 **({'stop':operator.stop_no_scale} if operator.no_scale_target is not None else
                    {'stop':operator.stop_rehearsal} if operator.run_context is not None and
                    operator.mode == 'disabled-rehearsal' else {}))
    except FeedStop as error:
        parser.exit(1,str(error)+'\n')
    except Exception:
        parser.exit(1,str(FeedStop(operator.stage if 'operator' in locals() else 'startup'))+'\n')


if __name__ == '__main__':
    main()
