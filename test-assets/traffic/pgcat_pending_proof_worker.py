#!/usr/bin/env python3
"""Validate actual worker evidence and seal one Pending-before-NLB proof.

The only stdout on success is a fixed address-free summary. All detailed
readbacks and source paths remain in owner-only worker files. This helper is
read-only except for its one-use private proof bundle and seal.
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


def require(condition):
    if not condition:
        raise Refused()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      allow_nan=False).encode('ascii')


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
    return decode(raw), digest


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
    frames, segments, total = [], {}, 0
    for index, path in enumerate(files, 1):
        require(path.name == 'segment-%04d.jsonl' % index)
        raw = read_private(path, MAX_TOTAL)
        total += len(raw)
        require(total <= MAX_TOTAL and raw.endswith(b'\n'))
        # The collector may append after this bounded read. Retain the exact
        # prefix length so the captured digest stays verifiable later.
        segments[path.name] = dict(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
        for line in raw.splitlines(True):
            require(line.endswith(b'\n') and len(line) <= MAX_FRAME)
            frame = decode(line)
            require(capture.clean(frame) == frame)
            frames.append(frame)
            require(len(frames) <= 5406)
    require(len(frames) >= 3)
    return frames, segments


def exact_targets(cm, baseline, new_id, address, resource_version):
    meta = cm['metadata']
    old_meta = baseline['configMap']
    require(all(meta.get(key) == old_meta[key] for key in
                ('name', 'namespace', 'uid')))
    require(meta['name'] == 'pgcat-provider-inventory' and
            meta['namespace'] == 'raptor-test' and
            not meta.get('deletionTimestamp') and
            meta.get('resourceVersion') == resource_version and
            type(resource_version) is str and resource_version.isascii() and
            resource_version.isdigit() and
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
    return hashlib.sha256(canonical(bound)).hexdigest(), bound


def sealed_write(path, value):
    raw = canonical(value) + b'\n'
    require(len(raw) <= MAX_FRAME)
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL |
                 getattr(os, 'O_NOFOLLOW', 0), 0o600)
    try:
        os.fchmod(fd, 0o600)
        while raw:
            count = os.write(fd, raw)
            require(count > 0)
            raw = raw[count:]
        os.fsync(fd)
    finally:
        os.close(fd)
    parent = os.open(str(path.parent), os.O_RDONLY)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    return hashlib.sha256(read_private(path, MAX_FRAME)).hexdigest()


def produce(contract_dir, run_id, kubectl=None, live_roster=None, now=None):
    """Revalidate the private sources; return only a fixed public-safe summary."""
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
        baseline, base_digest = read_sealed(folder, 'baseline')
        pre, pre_digest = read_sealed(folder, 'pre-scale')
        pending, pending_digest = read_sealed(folder, 'pending-ready')
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
        frames, segment_prefixes = verified_frames(capture_dir)
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
        live_inv = decode(read_private(inventory_path, MAX_FRAME))
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
        roster_digest, bound_roster = exact_clients(baseline, frames, roster,
                                                   deployment, pods)
        # The provider feed can publish while Kubernetes reads are in flight.
        # A changed publication needs a fresh collector correlation and a new
        # proof attempt; it must not inherit this earlier Pending snapshot.
        require(decode(read_private(inventory_path, MAX_FRAME)) == live_inv)
        fresh(live_inv['observedAt'], clock(), 30)
        fresh(latest['at'], clock(), 3)
        bundle = dict(version=1, runId=run_id, baseline=baseline, preScale=pre,
            pendingReady=pending, provider=live_inv, configMap=cm,
            collectorFrame=latest, collectorSegmentPrefixes=segment_prefixes,
            liveDeployment=deployment, livePods=pods, liveRoster=bound_roster,
            sourceDigests=dict(baseline=base_digest, preScale=pre_digest,
                               pendingReady=pending_digest))
        bundle_path = folder / 'pending-proof.bundle.json'
        evidence_sha = sealed_write(bundle_path, bundle)
        sealed_write(Path(str(bundle_path) + '.sha256'), {'sha256': evidence_sha})
        source_sha = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        return dict(runId=run_id, feedSession=session, newId=new_id,
                    originalInstanceIds=ids, sequence=pending['sequence'],
                    observedAt=pending['observedAt'],
                    resourceVersion=pending['configMapResourceVersion'],
                    clientRosterSha256=roster_digest,
                    evidenceSha256=evidence_sha, sourceSha256=source_sha)
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
        sys.stdout.write(canonical(summary).decode('ascii') + '\n')
        return 0
    except Exception:
        # A refusal has no raw diagnostics or address-bearing output.
        return 1
    finally:
        os.umask(previous_umask)


if __name__ == '__main__':
    sys.exit(main())
