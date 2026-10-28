#!/usr/bin/env python3
"""Validate actual worker evidence for a Pending-before-NLB proof.

The provider sidecar can bind a run and independently observed ECS addresses,
but the retired detached producer has not been reviewed for this source graph.
The CLI therefore refuses before publishing proof. Any future staging must pin
this file and its transitive imports:
capture_window.py, finish_clients.py, and worker_capture.py.
"""
import hashlib
import ipaddress
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
import time
import uuid

# Use the source-pinned phase dispatcher; legacy direct scripts are not runtime entries.
if __name__ == '__main__':
    raise SystemExit(1)

import capture_window as capture
import finish_clients
import worker_capture


MAX_FRAME = 2 * 1024 * 1024
MAX_TOTAL = 128 * 1024 * 1024
TOKEN = re.compile(r'[A-Za-z0-9_-]{1,128}\Z')
KIND = ('active', 'idle', 'waiting')
ALLOWED_ASSESSMENT_REASONS = frozenset((
    'capture cancelled or missing completion marker',
    'operations in flight at boundary',
    'incomplete rehearsal transition',
))


class Refused(Exception):
    """Never carry raw evidence or private paths across the caller boundary."""
    def __init__(self, category='invalid'):
        self.category = category
        super().__init__(category)


def require(condition, category='invalid'):
    if not condition:
        raise Refused(category)


def frame_digest(frame):
    # Historical collector contract hashes JSON without compact separators.
    return hashlib.sha256(json.dumps(frame, sort_keys=True,
                                    allow_nan=False).encode('utf-8')).hexdigest()


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def decode(raw):
    def invalid_constant(_):
        raise Refused()
    return json.loads(raw.decode('utf-8'), object_pairs_hook=unique,
                      parse_constant=invalid_constant)


def private(path, directory=False):
    path = Path(path)
    require(path.is_absolute() and not path.is_symlink() and
            path.resolve() == path)
    info = path.lstat()
    require(info.st_uid == os.geteuid())
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
    require(stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600))
    if not directory:
        require(info.st_nlink == 1)
    return path


def read_private(path, limit=65536):
    path = private(path)
    flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_NONBLOCK', 0)
    fd = os.open(str(path), flags)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1
                and info.st_size <= limit)
        raw = os.read(fd, limit + 1)
        require(len(raw) == info.st_size and len(raw) <= limit)
        return raw
    finally:
        os.close(fd)


def read_sealed(folder, name):
    path = folder / (name + '.json')
    raw = read_private(path)
    seal = decode(read_private(Path(str(path) + '.sha256'), 256))
    digest = hashlib.sha256(raw).hexdigest()
    require(seal == {'sha256': digest})
    return decode(raw)


def verify_provider_source(contract_dir, run_id, baseline, pre, pending,
                           selected_inventory, live_inventory_raw, live_inventory):
    """Verify worker-private provider provenance without publishing proof."""
    def check(value):
        require(value, 'source-binding')

    def directory(path):
        path = Path(path)
        check(path.is_absolute() and '..' not in path.parts)
        fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            for part in path.parts[1:]:
                child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                dir_fd=fd)
                os.close(fd)
                fd = child
            info = os.fstat(fd)
            check(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
                  and info.st_uid == os.geteuid())
            return fd
        except Exception:
            os.close(fd)
            raise

    def read_at(fd, name, limit):
        check(type(name) is str and '/' not in name and name not in ('.', '..'))
        item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
        try:
            before = os.fstat(item)
            check(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid()
                  and stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1
                  and before.st_size <= limit)
            raw = os.read(item, limit + 1)
            after = os.fstat(item)
            current = os.stat(name, dir_fd=fd, follow_symlinks=False)
            fields = lambda value: (value.st_dev, value.st_ino, value.st_size,
                                    value.st_mtime_ns, value.st_ctime_ns)
            check(len(raw) == before.st_size and len(raw) <= limit and
                  fields(before) == fields(after) == fields(current))
            return raw
        finally:
            os.close(item)

    def uuid_text(value):
        check(type(value) is str)
        check(str(uuid.UUID(value)) == value)

    def sha(value):
        check(type(value) is str and re.fullmatch(r'[0-9a-f]{64}', value) is not None)

    try:
        contract = Path(contract_dir)
        check(contract.name == 'contract' and contract.is_absolute())
        expected_inventory = contract.parent / 'run' / 'provider-inventory.json'
        check(Path(baseline['inventoryPath']) == expected_inventory)
        check(type(live_inventory_raw) is bytes and
              decode(live_inventory_raw) == live_inventory)
        parent = directory(contract)
        try:
            source = os.open('provider-source', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                             dir_fd=parent)
            try:
                info = os.fstat(source)
                check(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
                      and stat.S_IMODE(info.st_mode) == 0o700)
                context_raw = read_at(source, 'run-context.json', 2048)
                record = decode(context_raw)
                check(type(record) is dict and set(record) ==
                      {'schemaVersion', 'runContext', 'feedSession'} and
                      type(record['schemaVersion']) is int and record['schemaVersion'] == 1)
                context = record['runContext']
                check(type(context) is dict and set(context) ==
                      {'schemaVersion', 'runId', 'supervisorId', 'targetSha256',
                       'bundleSha256', 'nonce'} and
                      type(context['schemaVersion']) is int and context['schemaVersion'] == 1)
                for value in (context['runId'], context['supervisorId'], context['nonce'],
                              record['feedSession']):
                    uuid_text(value)
                for value in (context['targetSha256'], context['bundleSha256']):
                    sha(value)
                check(context['runId'] == run_id and
                      record['feedSession'] == baseline['receipt']['feedSession'] ==
                      pre['feedSession'])
                context_sha = hashlib.sha256(context_raw).hexdigest()
                check(all(artifact.get('providerRunContextSha256') == context_sha
                          for artifact in (baseline, pre, pending)))
                selected_sequence = pending['sequence']
                live_sequence = live_inventory['sequence']
                check(type(selected_sequence) is int and type(live_sequence) is int and
                      1 <= selected_sequence <= live_sequence <= 999999)
                expected_addresses = {row['id']:row['address'] for row in baseline['originals']}
                expected_addresses[pending['newId']] = pending['address']
                check(len(expected_addresses) == 3 and
                      len(set(expected_addresses.values())) == 3)
                for sequence, observed, inventory, raw_digest in (
                        (selected_sequence, pending['observedAt'],
                         selected_inventory, None),
                        (live_sequence, live_inventory['observedAt'], live_inventory,
                         hashlib.sha256(live_inventory_raw).hexdigest())):
                    check(capture.clean_inventory(inventory) == inventory)
                    semantic_digest = hashlib.sha256(json.dumps(inventory,
                        sort_keys=True, separators=(',', ':'),
                        allow_nan=False).encode('ascii')).hexdigest()
                    sidecar = decode(read_at(source, '%06d.json' % sequence, 4096))
                    check(type(sidecar) is dict and set(sidecar) ==
                          {'schemaVersion', 'runId', 'supervisorId', 'contextNonce',
                           'targetSha256', 'bundleSha256', 'feedSession', 'sequence',
                           'observedAt', 'inventorySha256', 'inventorySemanticSha256',
                           'privateAddresses'} and
                          type(sidecar['schemaVersion']) is int and sidecar['schemaVersion'] == 1)
                    sha(sidecar['inventorySha256'])
                    check(sidecar['runId'] == context['runId'] and
                          sidecar['supervisorId'] == context['supervisorId'] and
                          sidecar['contextNonce'] == context['nonce'] and
                          sidecar['targetSha256'] == context['targetSha256'] and
                          sidecar['bundleSha256'] == context['bundleSha256'] and
                          sidecar['feedSession'] == record['feedSession'] and
                          type(sidecar['sequence']) is int and sidecar['sequence'] == sequence and
                          type(sidecar['observedAt']) in (int, float) and
                          math.isfinite(sidecar['observedAt']) and sidecar['observedAt'] == observed and
                          sidecar['inventorySemanticSha256'] == semantic_digest and
                          (raw_digest is None or sidecar['inventorySha256'] == raw_digest) and
                          sidecar['privateAddresses'] == expected_addresses)
                return {'targetSha256': context['targetSha256'],
                        'providerRunContextSha256': context_sha}
            finally:
                os.close(source)
        finally:
            os.close(parent)
    except Refused:
        raise Refused('source-binding') from None
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError):
        raise Refused('source-binding') from None


def write_proof_bundle(folder, value):
    """Seal one address-free result before allowing the CLI to print it."""
    raw = (json.dumps(value, sort_keys=True, separators=(',', ':'),
                      allow_nan=False) + '\n').encode('ascii')
    require(len(raw) <= 16384, 'source-binding')
    fd = os.open(folder, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and
                stat.S_IMODE(info.st_mode) == 0o700, 'source-binding')
        for name, contents in (('pending-proof.bundle.json', raw),
                ('pending-proof.bundle.json.sha256',
                 (json.dumps({'sha256': hashlib.sha256(raw).hexdigest()},
                             sort_keys=True, separators=(',', ':')) + '\n').encode('ascii'))):
            item = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                           0o600, dir_fd=fd)
            try:
                os.fchmod(item, 0o600)
                while contents:
                    contents = contents[os.write(item, contents):]
                os.fsync(item)
            finally:
                os.close(item)
            os.fsync(fd)
    finally:
        os.close(fd)


def text(value):
    require(type(value) is str and TOKEN.fullmatch(value) is not None)
    return value


def timestamp(value):
    stamp = capture.seconds(value)
    require(math.isfinite(stamp))
    return stamp


def fresh(value, now, limit):
    age = now - timestamp(value)
    require(-1 <= age <= limit)


def private_address(value):
    parsed = ipaddress.IPv4Address(value)
    require(str(parsed) == value and parsed.is_private and
            not parsed.is_loopback and not parsed.is_link_local and
            not parsed.is_unspecified)
    return value


def verified_frames(capture_dir):
    private(capture_dir, True)
    files = sorted(capture_dir.glob('segment-*.jsonl'))
    require(1 <= len(files) <= 4)
    frames, total = [], 0
    for index, path in enumerate(files, 1):
        require(path.name == 'segment-%04d.jsonl' % index)
        raw = read_private(path, MAX_TOTAL)
        total += len(raw)
        require(total <= MAX_TOTAL and raw.endswith(b'\n'))
        for line in raw.splitlines(True):
            require(line.endswith(b'\n') and len(line) <= MAX_FRAME)
            frame = decode(line)
            require(capture.clean(frame) == frame)
            frames.append(frame)
            require(len(frames) <= 5406)
    require(len(frames) >= 3)
    return frames


def exact_targets(cm, baseline, new_id, address, resource_version):
    meta = cm['metadata']
    old_meta = baseline['configMap']
    require(all(meta.get(key) == old_meta[key] for key in
                ('name', 'namespace', 'uid')))
    require(meta['name'] == 'pgcat-provider-inventory' and
            meta['namespace'] == 'raptor-test' and
            not meta.get('deletionTimestamp') and
            meta.get('resourceVersion') == resource_version and
            type(resource_version) is str and
            re.fullmatch(r'[0-9]{1,32}', resource_version) is not None and
            type(old_meta['resourceVersion']) is str and
            re.fullmatch(r'[0-9]{1,32}', old_meta['resourceVersion']) is not None and
            int(resource_version) > int(old_meta['resourceVersion']) > 0)
    require(type(cm['data']) is dict and set(cm['data']) == {'targets.json'})
    targets = json.loads(cm['data']['targets.json'], object_pairs_hook=unique)
    expected = []
    for row in baseline['originals'] + [dict(id=new_id, address=address)]:
        expected.append(dict(targets=[row['address'] + ':9930'], labels=dict(
            env='rdev.ali', db_proxy_code='DP00001', target_db_code='D00002',
            ecs_instance_id=row['id'])))
    require(type(targets) is list and len(targets) == 3)
    require(sorted(targets, key=lambda row: row['labels']['ecs_instance_id']) ==
            sorted(expected, key=lambda row: row['labels']['ecs_instance_id']))


def exact_provider(inv, session, originals, new_id, minimum, now):
    require(capture.clean_inventory(inv) == inv)
    require(inv.get('schemaVersion') == 2 and inv.get('mode') == 'disabled-rehearsal'
            and inv['feedSession'] == session and type(inv['sequence']) is int
            and inv['sequence'] >= minimum and inv['desired'] == 3)
    ids = [row['id'] for row in originals]
    require(inv['members'] == sorted(ids + [new_id]) and
            inv['healthyRegistered'] == ids)
    states = {row['id']: row for row in inv['memberStates']}
    require(len(states) == 3 and set(states) == set(ids + [new_id]))
    for node in ids:
        require(states[node] == dict(id=node, lifecycle='Protected',
                                     health='Healthy', addressKnown=True))
    require(states[new_id]['lifecycle'] in ('Pending', 'Pending:Wait') and
            states[new_id]['health'] in ('Healthy', 'Unhealthy') and
            states[new_id]['addressKnown'] is True)
    require(inv['backendStates'] == [dict(id=node, status='Available', weight=100,
        listenerHealth='Normal') for node in ids])
    fresh(inv['observedAt'], now, 30)


def complete_metrics(frame, ids, now):
    metric = frame['metrics']
    require(metric['available'] is True and sorted(metric['nodes']) == sorted(ids))
    at = timestamp(frame['at'])
    fresh(metric['observedAt'], at, 3)
    fresh(metric['sourceAt'], at, 45)
    fresh(metric['observedAt'], now, 3)
    fresh(metric['sourceAt'], now, 45)
    coverage = {}
    for row in metric['raw']:
        node = row['node']; pool = row['pool']; user = row['user']; kind = row['kind']
        require(node in ids and type(pool) is str and pool and
                type(user) is str and user and kind in KIND)
        value = row['value']
        require(type(value) in (int, float) and math.isfinite(value) and
                value >= 0 and value == int(value) and value <= 2**53)
        fresh(row['sourceAt'], at, 45)
        fresh(row['sourceAt'], now, 45)
        key = (node, pool, user)
        require(kind not in coverage.setdefault(key, set()))
        coverage[key].add(kind)
    require(all(kinds == set(KIND) for kinds in coverage.values()))
    require(all((node, 'test', 'poc_app') in coverage for node in ids))


def exact_clients(baseline, frames, current_roster, deployment, pods):
    bound = finish_clients.validate_roster(baseline['roster'])
    require(finish_clients.validate_roster(current_roster) == bound)
    require(type(pods) is list and len(pods) == 2)
    live_pods = {}
    for pod in pods:
        meta = pod['metadata']; status = pod['status']
        require(meta.get('namespace') == 'raptor-test' and
                not meta.get('deletionTimestamp') and
                status['phase'] == 'Running' and
                any(row.get('type') == 'Ready' and row.get('status') == 'True'
                    for row in status['conditions']))
        containers = status['containerStatuses']
        require(type(containers) is list and len(containers) == 1 and
                containers[0]['ready'] is True and
                'running' in containers[0]['state'])
        uid = meta['uid']
        require(uid not in live_pods)
        live_pods[uid] = (meta['name'], containers[0]['containerID'])
    require(live_pods == {row['uid']: (row['name'], row['containerId'])
                          for row in bound})
    original = [(row['uid'], row['processId']) for row in bound]
    for frame in frames:
        require(frame['phase'] == 'measurement' and
                type(frame['expectedReplicas']) is int and frame['expectedReplicas'] == 2)
        pods = frame['pods']
        require(type(pods) is list and len(pods) == 2 and all(
            pod['snapshot']['final'] is False for pod in pods))
        require(sorted((pod['uid'], pod['snapshot']['sample']['processId'])
                       for pod in pods) == sorted(original))
        for pod in pods:
            sample = pod['snapshot']['sample']
            require(all(type(sample[key]) is int and sample[key] == 0 for key in
                        ('Failure', 'Timeout', 'Ambiguous', 'Skipped')))
    previous = {pod['uid']: pod['snapshot']['sample']['Success']
                for pod in frames[-2]['pods']}
    for pod in frames[-1]['pods']:
        require(pod['snapshot']['sample']['Success'] > previous[pod['uid']])
    expected = baseline['deployment']
    meta = deployment['metadata']
    require(all(meta.get(key) == expected[key] for key in
                ('name', 'namespace', 'uid', 'generation')) and
            not meta.get('deletionTimestamp'))
    require(deployment['spec']['replicas'] == 2 and
            deployment['status']['observedGeneration'] == expected['generation'] and
            all(deployment['status'][key] == 2 for key in
                ('replicas', 'updatedReplicas', 'readyReplicas', 'availableReplicas')))


def produce(contract_dir, run_id, kubectl=None, live_roster=None, now=None):
    """Revalidate private sources; refuse until independent bindings exist."""
    try:
        folder = private(Path(contract_dir), True)
        require(type(run_id) is str and str(uuid.UUID(run_id)) == run_id)
        clock = time.time if now is None else now
        current = clock()
        require(type(current) in (int, float) and math.isfinite(current))
        reader = worker_capture.kubectl if kubectl is None else kubectl
        roster_reader = finish_clients.live_roster if live_roster is None else live_roster
        require(not (folder / 'pending-proof.bundle.json').exists() and
                not (folder / 'pending-proof.bundle.json.sha256').exists())
        baseline = read_sealed(folder, 'baseline')
        pre = read_sealed(folder, 'pre-scale')
        pending = read_sealed(folder, 'pending-ready')
        require(baseline['purpose'] == 'disabled-rehearsal-post-stop' and
                baseline['mode'] == 'disabled-rehearsal')
        originals = baseline['originals']
        require(type(originals) is list and len(originals) == 2 and
                [row['id'] for row in originals] == sorted(set(row['id'] for row in originals)))
        ids = [text(row['id']) for row in originals]
        addresses = [private_address(row['address']) for row in originals]
        require(len(set(addresses)) == 2)
        receipt = baseline['receipt']
        session = receipt['feedSession']
        require(str(uuid.UUID(session)) == session and receipt['mode'] ==
                'disabled-rehearsal' and receipt['desired'] == 2 and
                type(receipt['sequence']) is int and receipt['sequence'] > 0)
        require(pre['feedSession'] == session and type(pre['sequence']) is int and
                pre['sequence'] > receipt['sequence'])
        new_id = text(pending['newId'])
        require(new_id not in ids and pending['address'] not in addresses)
        private_address(pending['address'])
        require(type(pending['sequence']) is int and
                pending['sequence'] > pre['sequence'] and
                pending['postReadbackSequence'] >= pending['sequence'] and
                timestamp(pre['latestAt']) < timestamp(pending['at']))
        require(timestamp(pending['configMapReadbackAt']) <
                timestamp(pending['postReadbackObservedAt']) <=
                timestamp(pending['observedAt']) <= current)
        fresh(pending['observedAt'], current, 30)
        capture_dir = private(Path(baseline['paths']['captureDir']), True)
        private(Path(baseline['paths']['kubeconfig']))
        private(Path(baseline['paths']['boundaryDir']), True)
        inventory_path = private(Path(baseline['inventoryPath']))
        require(not (capture_dir / 'completion.json').exists())
        frames = verified_frames(capture_dir)
        require(frames[0]['at'] == pre['firstAt'] and
                frame_digest(frames[0]) == pre['firstFrameSha256'] and
                frame_digest(next(f for f in frames if f['at'] == pre['latestAt'])) ==
                pre['latestFrameSha256'])
        require(timestamp(baseline['baselineAt']) < timestamp(frames[0]['at']) and
                frames[0]['inventory']['sequence'] >= receipt['sequence'] and
                next(f for f in frames if f['at'] == pre['latestAt'])['inventory']['sequence']
                <= pre['sequence'])
        index = pending['frameIndex']
        require(type(index) is int and 0 <= index < len(frames) and
                frame_digest(frames[index]) == pending['frameSha256'] and
                frames[index]['at'] == pending['at'])
        selected = frames[index]
        require(selected['inventory']['sequence'] == pending['sequence'] and
                selected['inventory']['observedAt'] == pending['observedAt'] and
                selected['metrics']['sourceAt'] == pending['sourceAt'] and
                selected['inventory']['sequence'] >= pending['postReadbackSequence'] and
                timestamp(selected['inventory']['observedAt']) >=
                timestamp(pending['postReadbackObservedAt']))
        candidates = set()
        for frame in frames:
            inv = frame['inventory']
            require(inv.get('mode') == 'disabled-rehearsal' and
                    inv['feedSession'] == session and
                    new_id not in {row['id'] for row in inv['backendStates']})
            added = set(inv['members']) - set(ids)
            require(not added or timestamp(frame['at']) > timestamp(pre['latestAt']))
            candidates.update(added)
            states = {row['id']: row for row in inv['memberStates']}
            require(set(ids).issubset(states) and all(
                states[node] == dict(id=node, lifecycle='Protected',
                                     health='Healthy', addressKnown=True)
                for node in ids))
            require(inv['healthyRegistered'] == ids and
                    inv['backendStates'] == [dict(id=node, status='Available',
                        weight=100, listenerHealth='Normal') for node in ids])
        require(candidates == {new_id})
        latest = frames[-1]
        live_inventory_raw = read_private(inventory_path, MAX_FRAME)
        live_inv = decode(live_inventory_raw)
        exact_provider(live_inv, session, originals, new_id,
                       pending['postReadbackSequence'], current)
        require(latest['inventory'] == live_inv and
                latest['inventory']['sequence'] >= selected['inventory']['sequence'] and
                timestamp(latest['inventory']['observedAt']) >=
                timestamp(selected['inventory']['observedAt']))
        fresh(latest['at'], current, 3)
        require(timestamp(latest['inventory']['observedAt']) >
                timestamp(pending['configMapReadbackAt']))
        complete_metrics(selected, ids + [new_id], timestamp(selected['at']))
        complete_metrics(latest, ids + [new_id], current)
        readback_at = timestamp(pending['configMapReadbackAt'])
        require(timestamp(selected['metrics']['sourceAt']) > readback_at and
                all(timestamp(row['sourceAt']) > readback_at
                    for row in selected['metrics']['raw'] if row['node'] == new_id),
                'chronology')
        assessment = capture.assess(frames, completed=False)
        require(assessment['status'] != 'FAIL' and
                set(assessment['reasons']).issubset(ALLOWED_ASSESSMENT_REASONS) and
                assessment['success'] > 0)
        old_kube = os.environ.get('KUBECONFIG')
        os.environ['KUBECONFIG'] = str(baseline['paths']['kubeconfig'])
        try:
            cm = reader('get', 'configmap', 'pgcat-provider-inventory', '-o', 'json')
            exact_targets(cm, baseline, new_id, pending['address'],
                          pending['configMapResourceVersion'])
            deployment = reader('get', 'deployment', 'rdev-test-client-client', '-o', 'json')
            pods = reader('get', 'pods', '-l',
                          'app.kubernetes.io/name=rdev-test-client-client', '-o', 'json')['items']
            roster = roster_reader()
        finally:
            if old_kube is None:
                os.environ.pop('KUBECONFIG', None)
            else:
                os.environ['KUBECONFIG'] = old_kube
        exact_clients(baseline, frames, roster, deployment, pods)
        # The provider feed can publish while Kubernetes reads are in flight.
        # A changed publication needs a fresh collector correlation and a new
        # proof attempt; it must not inherit this earlier Pending snapshot.
        require(read_private(inventory_path, MAX_FRAME) == live_inventory_raw)
        fresh(live_inv['observedAt'], clock(), 30)
        fresh(latest['at'], clock(), 3)
        source = verify_provider_source(folder, run_id, baseline, pre, pending,
                                        selected['inventory'], live_inventory_raw, live_inv)
        require(type(baseline.get('version')) is int and baseline['version'] == 1 and
                baseline.get('targetSha256') == source['targetSha256'] and
                pre.get('targetSha256') == source['targetSha256'] and
                pending.get('targetSha256') == source['targetSha256'], 'source-binding')
        for name in ('baseline', 'pre-scale', 'pending-ready'):
            require(decode(read_private(folder / (name + '.intent'), 256)) ==
                    {'version': 1, 'runId': run_id}, 'source-binding')
        inputs = {}
        for name, value in (('baseline', baseline), ('pre-scale', pre),
                            ('pending-ready', pending)):
            raw = read_private(folder / (name + '.json'))
            require(decode(raw) == value and
                    decode(read_private(folder / (name + '.json.sha256'), 256)) ==
                    {'sha256': hashlib.sha256(raw).hexdigest()}, 'source-binding')
            inputs[name] = hashlib.sha256(raw).hexdigest()
        bound_roster = finish_clients.validate_roster(baseline['roster'])
        require(bound_roster == baseline['roster'], 'source-binding')
        roster_raw = json.dumps(bound_roster, sort_keys=True, separators=(',', ':'),
                                allow_nan=False).encode('ascii')
        source_sha = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        summary = {'runId': run_id, 'feedSession': session, 'newId': new_id,
            'originalInstanceIds': ids, 'sequence': pending['sequence'],
            'observedAt': pending['observedAt'],
            'resourceVersion': pending['configMapResourceVersion'],
            'clientRosterSha256': hashlib.sha256(roster_raw).hexdigest(),
            'evidenceSha256': inputs['pending-ready'], 'sourceSha256': source_sha}
        bundle = {'schemaVersion': 1, 'runId': run_id, 'feedSession': session,
                  'summary': summary, 'sealedInputsSha256': inputs,
                  'providerRunContextSha256': source['providerRunContextSha256']}
        require(read_private(inventory_path, MAX_FRAME) == live_inventory_raw and
                verified_frames(capture_dir)[-1] == latest, 'source-binding')
        fresh(live_inv['observedAt'], clock(), 30)
        fresh(latest['at'], clock(), 3)
        write_proof_bundle(folder, bundle)
        return summary
    except Refused:
        raise
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, StopIteration, UnicodeError, AttributeError):
        raise Refused() from None


def main():
    previous_umask = os.umask(0o077)
    try:
        if len(sys.argv) != 3:
            return 2
        summary = produce(Path(sys.argv[1]), sys.argv[2])
        print(json.dumps(summary, sort_keys=True, separators=(',', ':'), allow_nan=False))
        return 0
    except Exception:
        # A refusal has no raw diagnostics or address-bearing output.
        return 1
    finally:
        os.umask(previous_umask)


if __name__ == '__main__':
    sys.exit(main())
