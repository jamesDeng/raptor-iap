"""Fresh worker-local preparation for the disabled PgCat rehearsal."""
import hashlib
import ipaddress
import json
import math
import os
from pathlib import Path
import re
import stat
import time
import uuid

import capture_window
import finish_clients
import pgcat_pending_proof_worker as proof
import worker_capture

TARGET_KEYS = {'accountId', 'regionId', 'workerId', 'scalingGroupId',
               'nlbServerGroupId', 'originalInstanceIds'}


def _require(value):
    if not value:
        raise ValueError('source')


def _unique(pairs):
    result = {}
    for key, value in pairs:
        _require(key not in result)
        result[key] = value
    return result


def _decode(raw):
    def invalid(_):
        raise ValueError('source')
    return json.loads(raw.decode('ascii'), object_pairs_hook=_unique,
                      parse_constant=invalid)


def _directory(path):
    parts = Path(path).parts
    _require(parts and parts[0] == '/' and '..' not in parts)
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=fd)
            os.close(fd)
            fd = child
        info = os.fstat(fd)
        _require(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
                 and info.st_uid == os.geteuid())
        return fd
    except Exception:
        os.close(fd)
        raise


def _read_at(fd, name, limit):
    _require(type(name) is str and '/' not in name and name not in ('.', '..'))
    item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
    try:
        before = os.fstat(item)
        _require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid()
                 and stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1
                 and before.st_size <= limit)
        raw = os.read(item, limit + 1)
        after = os.fstat(item)
        current = os.stat(name, dir_fd=fd, follow_symlinks=False)
        def identity(value):
            return (value.st_dev, value.st_ino, value.st_size,
                    value.st_mtime_ns, value.st_ctime_ns)
        _require(len(raw) == before.st_size and len(raw) <= limit and
                 identity(before) == identity(after) == identity(current))
        return raw
    finally:
        os.close(item)


def _uuid(value):
    _require(type(value) is str and str(uuid.UUID(value)) == value)


def _sha(value):
    _require(type(value) is str and re.fullmatch(r'[0-9a-f]{64}', value) is not None)


def _bound_target(value):
    _require(type(value) is dict and set(value) == TARGET_KEYS)
    _require(all(type(value[key]) is str and
                 re.fullmatch(r'[A-Za-z0-9_-]{1,256}', value[key]) is not None
                 for key in TARGET_KEYS - {'originalInstanceIds'}))
    ids = value['originalInstanceIds']
    _require(type(ids) is list and len(ids) == 2 and
             all(type(node) is str and
                 re.fullmatch(r'[A-Za-z0-9_-]{1,256}', node) is not None
                 for node in ids) and ids == sorted(set(ids)))
    return hashlib.sha256(json.dumps(value, sort_keys=True,
        separators=(',', ':'), allow_nan=False).encode('ascii')).hexdigest()


def fresh_provider_source(contract_dir, run_id, inventory_path, require_fresh=True):
    """Bind an initial worker inventory to the feed-written private context."""
    try:
        contract = Path(contract_dir)
        inventory = Path(inventory_path)
        _require(contract.is_absolute() and contract.name == 'contract' and
                 inventory == contract.parent / 'run' / 'provider-inventory.json')
        _uuid(run_id)
        contract_fd = _directory(contract)
        try:
            if require_fresh:
                _require(set(os.listdir(contract_fd)) == {'provider-source'})
            source_fd = os.open('provider-source', os.O_RDONLY | os.O_DIRECTORY |
                                os.O_NOFOLLOW, dir_fd=contract_fd)
            try:
                info = os.fstat(source_fd)
                _require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
                         and stat.S_IMODE(info.st_mode) == 0o700)
                context_raw = _read_at(source_fd, 'run-context.json', 2048)
                record = _decode(context_raw)
                _require(type(record) is dict and set(record) ==
                         {'schemaVersion', 'runContext', 'feedSession'} and
                         type(record['schemaVersion']) is int and record['schemaVersion'] == 1)
                context = record['runContext']
                _require(type(context) is dict and set(context) ==
                         {'schemaVersion', 'runId', 'supervisorId', 'targetSha256',
                          'bundleSha256', 'nonce'} and
                         type(context['schemaVersion']) is int and context['schemaVersion'] == 1)
                for value in (context['runId'], context['supervisorId'], context['nonce'],
                              record['feedSession']):
                    _uuid(value)
                for value in (context['targetSha256'], context['bundleSha256']):
                    _sha(value)
                _require(context['runId'] == run_id)
                inventory_fd = _directory(inventory.parent)
                try:
                    inventory_raw = _read_at(inventory_fd, inventory.name, 16384)
                finally:
                    os.close(inventory_fd)
                value = _decode(inventory_raw)
                _require(type(value) is dict and capture_window.clean_inventory(value) == value
                         and value.get('mode') == 'disabled-rehearsal' and
                         value.get('feedSession') == record['feedSession'] and
                         type(value.get('sequence')) is int and 1 <= value['sequence'] <= 999999)
                sidecar = _decode(_read_at(source_fd, '%06d.json' % value['sequence'], 4096))
                _require(type(sidecar) is dict and set(sidecar) == {
                    'schemaVersion', 'runId', 'supervisorId', 'contextNonce',
                    'targetSha256', 'bundleSha256', 'feedSession', 'sequence',
                    'observedAt', 'inventorySha256', 'inventorySemanticSha256',
                    'privateAddresses'} and
                    type(sidecar['schemaVersion']) is int and sidecar['schemaVersion'] == 1)
                for key, source_key in (('runId','runId'),('supervisorId','supervisorId'),
                                        ('contextNonce','nonce'),('targetSha256','targetSha256'),
                                        ('bundleSha256','bundleSha256')):
                    _require(sidecar[key] == context[source_key])
                _require(sidecar['feedSession'] == record['feedSession'] and
                         type(sidecar['sequence']) is int and sidecar['sequence'] == value['sequence'] and
                         type(sidecar['observedAt']) in (int,float) and
                         math.isfinite(sidecar['observedAt']) and
                         sidecar['observedAt'] == value['observedAt'] and
                         sidecar['inventorySha256'] == hashlib.sha256(inventory_raw).hexdigest() and
                         sidecar['inventorySemanticSha256'] == hashlib.sha256(json.dumps(
                             value, sort_keys=True, separators=(',', ':'),
                             allow_nan=False).encode('ascii')).hexdigest())
                addresses = sidecar['privateAddresses']
                _require(type(addresses) is dict and set(addresses) == set(value['members']) and
                         len(addresses) == len(set(addresses.values())))
                for address in addresses.values():
                    parsed = ipaddress.IPv4Address(address)
                    _require(str(parsed) == address and parsed.is_private and
                             not parsed.is_loopback and not parsed.is_link_local)
                return {'providerRunContextSha256': hashlib.sha256(context_raw).hexdigest(),
                        'targetSha256': context['targetSha256'],
                        'feedSession': record['feedSession'],
                        'sequence': value['sequence'], 'observedAt': value['observedAt'],
                        'privateAddresses': dict(addresses),
                        'inventoryRawSha256': hashlib.sha256(inventory_raw).hexdigest()}
            finally:
                os.close(source_fd)
        finally:
            os.close(contract_fd)
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError):
        raise ValueError('source') from None


def _write_once(fd, name, value):
    raw = (json.dumps(value, sort_keys=True, allow_nan=False) + '\n').encode('ascii')
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


def _sealed_write(fd, name, value):
    raw = (json.dumps(value, sort_keys=True, allow_nan=False) + '\n').encode('ascii')
    _write_once(fd, name, value)
    _write_once(fd, name + '.sha256', {'sha256': hashlib.sha256(raw).hexdigest()})


def _sealed_read(fd, name):
    raw = _read_at(fd, name, 65536)
    seal = _decode(_read_at(fd, name + '.sha256', 256))
    _require(seal == {'sha256': hashlib.sha256(raw).hexdigest()})
    return _decode(raw)


def _private_file(path):
    path = Path(path)
    _require(path.is_absolute() and '..' not in path.parts)
    parent = _directory(path.parent)
    try:
        _read_at(parent, path.name, 65536)
    finally:
        os.close(parent)


def _current_inventory(path, source, now):
    path = Path(path)
    directory = _directory(path.parent)
    try:
        raw = _read_at(directory, path.name, 16384)
    finally:
        os.close(directory)
    _require(hashlib.sha256(raw).hexdigest() == source['inventoryRawSha256'])
    value = _decode(raw)
    ids = sorted(source['privateAddresses'])
    _require(value['schemaVersion'] == 2 and value['mode'] == 'disabled-rehearsal' and
             value['feedSession'] == source['feedSession'] and
             value['sequence'] == source['sequence'] and value['desired'] == 2 and
             value['members'] == value['healthyRegistered'] == ids and
             value['memberStates'] == [dict(id=node, lifecycle='Protected',
                 health='Healthy', addressKnown=True) for node in ids] and
             value['backendStates'] == [dict(id=node, status='Available', weight=100,
                 listenerHealth='Normal') for node in ids] and
             -1 <= now - proof.timestamp(value['observedAt']) <= 30)
    return value


def _read_configmap(kubectl, addresses):
    cm = kubectl('get', 'configmap', 'pgcat-provider-inventory', '-o', 'json')
    meta = cm['metadata']
    _require(all(meta.get(key) == value for key, value in {
        'name': 'pgcat-provider-inventory', 'namespace': 'raptor-test'}.items()) and
        not meta.get('deletionTimestamp') and
        type(meta.get('uid')) is str and meta['uid'] and
        type(meta.get('resourceVersion')) is str and
        re.fullmatch(r'[1-9][0-9]{0,31}', meta['resourceVersion']) is not None)
    _require(type(cm['data']) is dict and set(cm['data']) == {'targets.json'})
    rows = _decode(cm['data']['targets.json'].encode('ascii'))
    expected = [dict(targets=[addresses[node] + ':9930'], labels=dict(
        env='rdev.ali', db_proxy_code='DP00001', target_db_code='D00002',
        ecs_instance_id=node)) for node in sorted(addresses)]
    _require(type(rows) is list and len(rows) == 2 and
             sorted(rows, key=lambda row: row['labels']['ecs_instance_id']) == expected)
    return {key: meta[key] for key in ('name', 'namespace', 'uid', 'resourceVersion')}


def _with_kubeconfig(path, operation, *args):
    previous = os.environ.get('KUBECONFIG')
    os.environ['KUBECONFIG'] = str(path)
    try:
        return operation(*args)
    finally:
        if previous is None:
            os.environ.pop('KUBECONFIG', None)
        else:
            os.environ['KUBECONFIG'] = previous


def baseline(contract_dir, run_id, kubeconfig_path, target, *, kubectl=None, read_pod=None,
             query_metrics=None, clock=None):
    """Seal one exact disabled-mode baseline after fresh provider/Kubernetes reads."""
    try:
        contract = Path(contract_dir)
        run = contract.parent / 'run'
        inventory = run / 'provider-inventory.json'
        capture = run / 'rehearsal-capture'
        boundary = run / 'disabled-boundary'
        source = fresh_provider_source(contract, run_id, inventory)
        target_sha = _bound_target(target)
        _require(source['targetSha256'] == target_sha and
                 sorted(source['privateAddresses']) == target['originalInstanceIds'])
        _require(len(source['privateAddresses']) == 2)
        _require(not capture.exists() and not capture.is_symlink())
        boundary_fd = _directory(boundary)
        try:
            _require(not os.listdir(boundary_fd))
        finally:
            os.close(boundary_fd)
        _private_file(kubeconfig_path)
        read = worker_capture.kubectl if kubectl is None else kubectl
        pod_reader = (lambda pod: worker_capture.read_pod(pod, {})) if read_pod is None else read_pod
        metrics_reader = worker_capture.query_metrics if query_metrics is None else query_metrics
        now = time.time if clock is None else clock
        contract_fd = _directory(contract)
        try:
            _write_once(contract_fd, 'baseline.intent', {'version': 1, 'runId': run_id})
            config = _with_kubeconfig(kubeconfig_path, _read_configmap,
                                      read, source['privateAddresses'])
            deployment = _with_kubeconfig(kubeconfig_path, read,
                'get', 'deployment', 'rdev-test-client-client', '-o', 'json')
            meta = deployment['metadata']
            _require(meta.get('name') == 'rdev-test-client-client' and
                     meta.get('namespace') == 'raptor-test' and
                     type(meta.get('uid')) is str and meta['uid'] and
                     type(meta.get('generation')) is int and meta['generation'] > 0 and
                     not meta.get('deletionTimestamp') and
                     deployment['spec']['replicas'] == 2 and
                     deployment['status']['observedGeneration'] == meta['generation'] and
                     all(deployment['status'][key] == 2 for key in
                         ('replicas', 'updatedReplicas', 'readyReplicas', 'availableReplicas')))
            pods = _with_kubeconfig(kubeconfig_path, read, 'get', 'pods', '-l',
                'app.kubernetes.io/name=rdev-test-client-client', '-o', 'json')['items']
            _require(type(pods) is list and len(pods) == 2)
            readings = []
            for pod in pods:
                pmeta, status = pod['metadata'], pod['status']
                _require(pmeta.get('namespace') == 'raptor-test' and
                         not pmeta.get('deletionTimestamp') and
                         status.get('phase') == 'Running' and
                         any(row.get('type') == 'Ready' and row.get('status') == 'True'
                             for row in status['conditions']) and
                         len(status['containerStatuses']) == 1 and
                         status['containerStatuses'][0]['ready'] is True and
                         'running' in status['containerStatuses'][0]['state'])
                reading = _with_kubeconfig(kubeconfig_path, pod_reader, pod)
                _require(reading['name'] == pmeta['name'] and
                         reading['uid'] == pmeta['uid'] and
                         reading['containerId'] == status['containerStatuses'][0]['containerID'] and
                         reading['snapshot']['final'] is False)
                readings.append(reading)
            roster = finish_clients.validate_roster([dict(name=row['name'], uid=row['uid'],
                containerId=row['containerId'], processId=row['snapshot']['sample']['processId'])
                for row in readings])
            service = _with_kubeconfig(kubeconfig_path, read,
                'get', 'service', 'rdev-test-metrics-metrics', '-o', 'json')
            metrics = metrics_reader('http://' + service['spec']['clusterIP'] + ':9090',
                                     worker_capture.utc())
            ids = sorted(source['privateAddresses'])
            proof.complete_metrics({'metrics': metrics, 'at': now()}, ids, now())
            groups = sorted({(row['node'], row['pool'], row['user']) for row in metrics['raw']})
            _require(len(groups) <= 256)
            final_source = fresh_provider_source(contract, run_id, inventory,
                                                 require_fresh=False)
            _require(final_source['providerRunContextSha256'] == source['providerRunContextSha256'] and
                     final_source['targetSha256'] == target_sha and
                     final_source['feedSession'] == source['feedSession'] and
                     final_source['privateAddresses'] == source['privateAddresses'] and
                     final_source['sequence'] >= source['sequence'])
            _current_inventory(inventory, final_source, now())
            baseline_value = {'version': 1, 'purpose': 'disabled-rehearsal-post-stop',
                'mode': 'disabled-rehearsal',
                'providerRunContextSha256': final_source['providerRunContextSha256'],
                'targetSha256': target_sha,
                'originals': [dict(id=node, address=final_source['privateAddresses'][node])
                              for node in ids], 'configMap': config,
                'paths': {'captureDir': str(capture), 'boundaryDir': str(boundary),
                          'kubeconfig': str(kubeconfig_path)}, 'roster': roster,
                'metricGroups': [dict(node=node, pool=pool, user=user)
                                 for node, pool, user in groups],
                'receipt': {'feedSession': final_source['feedSession'],
                            'sequence': final_source['sequence'], 'mode': 'disabled-rehearsal',
                            'desired': 2, 'observedAt': final_source['observedAt']},
                'inventoryPath': str(inventory), 'baselineAt': now(),
                'deployment': {key: meta[key] for key in
                               ('name', 'namespace', 'uid', 'generation')}}
            _sealed_write(contract_fd, 'baseline.json', baseline_value)
            return baseline_value
        finally:
            os.close(contract_fd)
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError):
        raise ValueError('baseline') from None


def _frame_sha(frame):
    return hashlib.sha256(json.dumps(frame, sort_keys=True,
        allow_nan=False).encode('utf-8')).hexdigest()


def _exact_original_frame(frame, baseline_value):
    ids = [row['id'] for row in baseline_value['originals']]
    inv = frame['inventory']
    _require(frame['phase'] == 'measurement' and frame['expectedReplicas'] == 2 and
             inv['schemaVersion'] == 2 and inv['mode'] == 'disabled-rehearsal' and
             inv['feedSession'] == baseline_value['receipt']['feedSession'] and
             inv['desired'] == 2 and inv['members'] == inv['healthyRegistered'] == ids and
             inv['memberStates'] == [dict(id=node, lifecycle='Protected',
                 health='Healthy', addressKnown=True) for node in ids] and
             inv['backendStates'] == [dict(id=node, status='Available', weight=100,
                 listenerHealth='Normal') for node in ids])


def pre_scale(contract_dir, run_id, target, *, kubectl=None, read_pod=None, clock=None):
    """Seal a clean advancing two-node capture before ESS 2→3 admission."""
    try:
        contract = Path(contract_dir)
        fd = _directory(contract)
        try:
            baseline_value = _sealed_read(fd, 'baseline.json')
            target_sha = _bound_target(target)
            _require(baseline_value['purpose'] == 'disabled-rehearsal-post-stop' and
                     baseline_value['mode'] == 'disabled-rehearsal' and
                     baseline_value['targetSha256'] == target_sha and
                     [row['id'] for row in baseline_value['originals']] ==
                     target['originalInstanceIds'])
            _write_once(fd, 'pre-scale.intent', {'version': 1, 'runId': run_id})
            capture = Path(baseline_value['paths']['captureDir'])
            _require(not (capture / 'completion.json').exists())
            frames = proof.verified_frames(capture)
            _require(len(frames) >= 2)
            for frame in frames:
                _exact_original_frame(frame, baseline_value)
            read = worker_capture.kubectl if kubectl is None else kubectl
            pod_reader = (lambda pod: worker_capture.read_pod(pod, {})) if read_pod is None else read_pod
            now = time.time if clock is None else clock
            kube = baseline_value['paths']['kubeconfig']
            _private_file(kube)
            deployment = _with_kubeconfig(kube, read,
                'get', 'deployment', 'rdev-test-client-client', '-o', 'json')
            pods = _with_kubeconfig(kube, read, 'get', 'pods', '-l',
                'app.kubernetes.io/name=rdev-test-client-client', '-o', 'json')['items']
            readings = [_with_kubeconfig(kube, pod_reader, pod) for pod in pods]
            roster = finish_clients.validate_roster([dict(name=row['name'], uid=row['uid'],
                containerId=row['containerId'], processId=row['snapshot']['sample']['processId'])
                for row in readings])
            proof.exact_clients(baseline_value, frames, roster, deployment, pods)
            newer = proof.verified_frames(capture)
            _require(newer[:len(frames)] == frames)
            for frame in newer:
                _exact_original_frame(frame, baseline_value)
            assessment = capture_window.assess(newer, completed=False)
            _require(assessment['status'] != 'FAIL' and set(assessment['reasons']).issubset({
                'capture cancelled or missing completion marker',
                'operations in flight at boundary', 'incomplete rehearsal transition',
                'no covered successful traffic'}))
            latest = newer[-1]
            proof.complete_metrics(latest, [row['id'] for row in baseline_value['originals']], now())
            _require(-1 <= now() - proof.timestamp(latest['at']) <= 3 and
                     proof.timestamp(baseline_value['baselineAt']) <
                     proof.timestamp(newer[0]['at']) < proof.timestamp(latest['at']))
            inventory = Path(baseline_value['inventoryPath'])
            source = fresh_provider_source(contract, run_id, inventory, require_fresh=False)
            _require(source['providerRunContextSha256'] ==
                     baseline_value['providerRunContextSha256'] and
                     source['targetSha256'] == target_sha and
                     source['feedSession'] == baseline_value['receipt']['feedSession'] and
                     source['privateAddresses'] == {row['id']:row['address']
                         for row in baseline_value['originals']} and
                     source['sequence'] >= latest['inventory']['sequence'] >
                     newer[0]['inventory']['sequence'] >= baseline_value['receipt']['sequence'])
            _current_inventory(inventory, source, now())
            value = {'providerRunContextSha256': source['providerRunContextSha256'],
                'targetSha256': target_sha,
                'firstAt': newer[0]['at'], 'latestAt': latest['at'],
                'sequence': source['sequence'], 'feedSession': source['feedSession'],
                'firstFrameSha256': _frame_sha(newer[0]),
                'latestFrameSha256': _frame_sha(latest)}
            _sealed_write(fd, 'pre-scale.json', value)
            return value
        finally:
            os.close(fd)
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError):
        raise ValueError('pre-scale') from None


def _pending_fleet(frame, baseline_value, new_id):
    ids = [row['id'] for row in baseline_value['originals']]
    inv = frame['inventory']
    states = inv['memberStates']
    _require(type(states) is list and len(states) == 3 and
             [row['id'] for row in states] == sorted(ids + [new_id]))
    by_id = {row['id']: row for row in states}
    _require(frame['phase'] == 'measurement' and frame['expectedReplicas'] == 2 and
             inv['schemaVersion'] == 2 and inv['mode'] == 'disabled-rehearsal' and
             inv['feedSession'] == baseline_value['receipt']['feedSession'] and
             inv['desired'] == 3 and inv['members'] == sorted(ids + [new_id]) and
             inv['healthyRegistered'] == ids and
             all(by_id[node] == dict(id=node, lifecycle='Protected',
                 health='Healthy', addressKnown=True) for node in ids) and
             by_id[new_id]['lifecycle'] in ('Pending', 'Pending:Wait') and
             by_id[new_id]['health'] in ('Healthy', 'Unhealthy') and
             by_id[new_id]['addressKnown'] is True and
             inv['backendStates'] == [dict(id=node, status='Available', weight=100,
                 listenerHealth='Normal') for node in ids])


def _pending_configmap(kubectl, baseline_value, addresses, new_id):
    cm = kubectl('get', 'configmap', 'pgcat-provider-inventory', '-o', 'json')
    meta = cm['metadata']
    _require(all(meta.get(key) == baseline_value['configMap'][key] for key in
                 ('name', 'namespace', 'uid')) and not meta.get('deletionTimestamp') and
             type(meta.get('resourceVersion')) is str and
             re.fullmatch(r'[1-9][0-9]{0,31}', meta['resourceVersion']) is not None and
             int(meta['resourceVersion']) > int(baseline_value['configMap']['resourceVersion']))
    _require(type(cm['data']) is dict and set(cm['data']) == {'targets.json'})
    rows = _decode(cm['data']['targets.json'].encode('ascii'))
    _require(type(rows) is list and len(rows) == 3 and set(addresses) ==
             {row['id'] for row in baseline_value['originals']} | {new_id})
    expected = [dict(targets=[addresses[node] + ':9930'], labels=dict(
        env='rdev.ali', db_proxy_code='DP00001', target_db_code='D00002',
        ecs_instance_id=node)) for node in sorted(addresses)]
    _require(sorted(rows, key=lambda row: row['labels']['ecs_instance_id']) == expected)
    return meta['resourceVersion']


def pending_ready(contract_dir, run_id, target, *, kubectl=None, read_pod=None,
                  clock=None, readback_clock=None, sleep=None):
    """Seal exactly one Pending candidate before any NLB entry."""
    try:
        contract = Path(contract_dir)
        fd = _directory(contract)
        try:
            baseline_value = _sealed_read(fd, 'baseline.json')
            pre = _sealed_read(fd, 'pre-scale.json')
            target_sha = _bound_target(target)
            _require(baseline_value['mode'] == 'disabled-rehearsal' and
                     baseline_value['targetSha256'] == target_sha and
                     [row['id'] for row in baseline_value['originals']] ==
                     target['originalInstanceIds'] and
                     pre['targetSha256'] == target_sha and
                     pre['providerRunContextSha256'] ==
                     baseline_value['providerRunContextSha256'] and
                     pre['feedSession'] == baseline_value['receipt']['feedSession'])
            _write_once(fd, 'pending-ready.intent', {'version': 1, 'runId': run_id})
            now = time.time if clock is None else clock
            readback = now if readback_clock is None else readback_clock
            pause = time.sleep if sleep is None else sleep
            read = worker_capture.kubectl if kubectl is None else kubectl
            pod_reader = (lambda pod: worker_capture.read_pod(pod, {})) if read_pod is None else read_pod
            capture = Path(baseline_value['paths']['captureDir'])
            _require(not (capture / 'completion.json').exists())
            frames = proof.verified_frames(capture)
            _require(frames[0]['at'] == pre['firstAt'] and
                     _frame_sha(frames[0]) == pre['firstFrameSha256'])
            older = next(frame for frame in frames if frame['at'] == pre['latestAt'])
            _require(_frame_sha(older) == pre['latestFrameSha256'])
            ids = [row['id'] for row in baseline_value['originals']]
            added = set().union(*(set(frame['inventory']['members']) - set(ids)
                                  for frame in frames))
            _require(len(added) == 1)
            new_id = next(iter(added))
            _require(new_id not in ids and
                     all(new_id not in {row['id'] for row in frame['inventory']['backendStates']}
                         for frame in frames) and
                     all(new_id not in frame['inventory']['members'] or
                         proof.timestamp(frame['at']) > proof.timestamp(pre['latestAt'])
                         for frame in frames))
            for frame in frames:
                if new_id in frame['inventory']['members']:
                    _pending_fleet(frame, baseline_value, new_id)
                else:
                    _exact_original_frame(frame, baseline_value)
            kube = baseline_value['paths']['kubeconfig']
            _private_file(kube)
            deployment = _with_kubeconfig(kube, read,
                'get', 'deployment', 'rdev-test-client-client', '-o', 'json')
            pods = _with_kubeconfig(kube, read, 'get', 'pods', '-l',
                'app.kubernetes.io/name=rdev-test-client-client', '-o', 'json')['items']
            readings = [_with_kubeconfig(kube, pod_reader, pod) for pod in pods]
            roster = finish_clients.validate_roster([dict(name=row['name'], uid=row['uid'],
                containerId=row['containerId'], processId=row['snapshot']['sample']['processId'])
                for row in readings])
            proof.exact_clients(baseline_value, frames, roster, deployment, pods)
            inventory = Path(baseline_value['inventoryPath'])
            source = fresh_provider_source(contract, run_id, inventory, require_fresh=False)
            _require(source['providerRunContextSha256'] ==
                     baseline_value['providerRunContextSha256'] and
                     source['targetSha256'] == target_sha and
                     source['feedSession'] == pre['feedSession'] and
                     source['sequence'] > pre['sequence'] and
                     len(source['privateAddresses']) == 3 and
                     set(source['privateAddresses']) == set(ids + [new_id]) and
                     all(source['privateAddresses'][row['id']] == row['address']
                         for row in baseline_value['originals']))
            rv = _with_kubeconfig(kube, _pending_configmap, read,
                                   baseline_value, source['privateAddresses'], new_id)
            readback_at = readback()
            deadline = time.monotonic() + 24
            while True:
                source = fresh_provider_source(contract, run_id, inventory,
                                               require_fresh=False)
                frames = proof.verified_frames(capture)
                _require(all(new_id not in {row['id'] for row in frame['inventory']['backendStates']}
                             for frame in frames))
                last = frames[-1]
                observed = proof.timestamp(source['observedAt'])
                if (observed > readback_at and
                        last['inventory']['sequence'] == source['sequence'] and
                        proof.timestamp(last['inventory']['observedAt']) == observed and
                        proof.timestamp(last['at']) >= observed):
                    break
                _require(time.monotonic() < deadline)
                pause(.25)
            _pending_fleet(last, baseline_value, new_id)
            _require(source['providerRunContextSha256'] ==
                     baseline_value['providerRunContextSha256'] and
                     source['targetSha256'] == target_sha and
                     source['feedSession'] == pre['feedSession'] and
                     source['privateAddresses'] == {**{row['id']: row['address']
                         for row in baseline_value['originals']},
                         new_id: source['privateAddresses'][new_id]} and
                     last['inventory']['sequence'] > pre['sequence'] and
                     proof.timestamp(last['at']) > proof.timestamp(pre['latestAt']) and
                     -1 <= now() - proof.timestamp(last['at']) <= 3)
            raw_fd = _directory(inventory.parent)
            try:
                inventory_raw = _read_at(raw_fd, inventory.name, 16384)
            finally:
                os.close(raw_fd)
            _require(hashlib.sha256(inventory_raw).hexdigest() == source['inventoryRawSha256'] and
                     _decode(inventory_raw) == last['inventory'])
            proof.exact_provider(last['inventory'], source['feedSession'],
                baseline_value['originals'], new_id, pre['sequence'] + 1, now())
            proof.complete_metrics(last, ids + [new_id], now())
            assessment = capture_window.assess(frames, completed=False)
            _require(assessment['status'] != 'FAIL' and set(assessment['reasons']).issubset({
                'capture cancelled or missing completion marker',
                'operations in flight at boundary', 'incomplete rehearsal transition',
                'no covered successful traffic'}))
            final_source = fresh_provider_source(contract, run_id, inventory,
                                                 require_fresh=False)
            _require(final_source == source and
                     proof.verified_frames(capture)[-1] == last)
            value = {'providerRunContextSha256': source['providerRunContextSha256'],
                'targetSha256': target_sha,
                'newId': new_id, 'frameIndex': len(frames) - 1,
                'frameSha256': _frame_sha(last), 'at': last['at'],
                'sequence': last['inventory']['sequence'],
                'observedAt': last['inventory']['observedAt'],
                'sourceAt': last['metrics']['sourceAt'],
                'address': source['privateAddresses'][new_id],
                'configMapResourceVersion': rv, 'configMapReadbackAt': readback_at,
                'postReadbackSequence': source['sequence'],
                'postReadbackObservedAt': source['observedAt']}
            _sealed_write(fd, 'pending-ready.json', value)
            return value
        finally:
            os.close(fd)
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError, StopIteration):
        raise ValueError('pending') from None


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      allow_nan=False).encode('ascii')


def _target_rows(addresses):
    _require(len(set(addresses.values())) == len(addresses))
    for address in addresses.values():
        proof.private_address(address)
    return [dict(targets=[addresses[node] + ':9930'], labels=dict(
        env='rdev.ali', db_proxy_code='DP00001', target_db_code='D00002',
        ecs_instance_id=node)) for node in sorted(addresses)]


def _reconcile_configmap(cm, baseline_value, expected, minimum, newer=False):
    meta = cm['metadata']
    _require(all(meta.get(key) == baseline_value['configMap'][key] for key in
                 ('name', 'namespace', 'uid')) and
             meta.get('name') == 'pgcat-provider-inventory' and
             meta.get('namespace') == 'raptor-test' and
             type(meta.get('uid')) is str and meta['uid'] and
             not meta.get('deletionTimestamp'))
    rv = meta.get('resourceVersion')
    _require(type(rv) is str and re.fullmatch(r'[1-9][0-9]{0,31}', rv) is not None and
             (int(rv) > int(minimum) if newer else int(rv) >= int(minimum)))
    _require(type(cm['data']) is dict and set(cm['data']) == {'targets.json'})
    rows = _decode(cm['data']['targets.json'].encode('ascii'))
    _require(type(rows) is list and
             sorted(rows, key=lambda row: row['labels']['ecs_instance_id']) == expected)
    return rv


def reconcile_close(contract_dir, run_id, target, pending_binding, close_context, *,
                    kubectl=None, clock=None):
    """Reconcile one asserted post-ACK receipt; local journal ACK proof is separate.

    Any attempted patch consumes the exclusive intent, including an unknown
    timeout outcome. Never retry, reverse, or inspect the whole traffic capture.
    """
    try:
        _uuid(run_id)
        target_sha = _bound_target(target)
        contract = Path(contract_dir)
        _require(contract.is_absolute() and contract.name == 'contract')
        fd = _directory(contract)
        try:
            _require(not any(name.startswith('reconcile-close.') for name in os.listdir(fd)))
            inputs = {}
            values = {}
            for name in ('baseline', 'pre-scale', 'pending-ready'):
                values[name] = _sealed_read(fd, name + '.json')
                raw = _read_at(fd, name + '.json', 65536)
                _require(_decode(raw) == values[name])
                inputs[name] = hashlib.sha256(raw).hexdigest()
                _require(_decode(_read_at(fd, name + '.intent', 256)) ==
                         {'version': 1, 'runId': run_id})
            baseline_value, pre, pending = (values[name] for name in
                                           ('baseline', 'pre-scale', 'pending-ready'))
            bundle = _sealed_read(fd, 'pending-proof.bundle.json')
            bundle_sha = hashlib.sha256(_read_at(fd, 'pending-proof.bundle.json', 65536)).hexdigest()
            _require(type(bundle) is dict and set(bundle) == {'schemaVersion', 'runId',
                'feedSession', 'summary', 'sealedInputsSha256', 'providerRunContextSha256'} and
                type(bundle['schemaVersion']) is int and bundle['schemaVersion'] == 1 and
                bundle['runId'] == run_id and bundle['sealedInputsSha256'] == inputs)
            _require(baseline_value['purpose'] == 'disabled-rehearsal-post-stop' and
                     baseline_value['mode'] == 'disabled-rehearsal' and
                     type(baseline_value['version']) is int and baseline_value['version'] == 1)
            originals = baseline_value['originals']
            ids = target['originalInstanceIds']
            _require(type(originals) is list and len(originals) == 2 and
                     all(type(row) is dict and set(row) == {'id', 'address'} for row in originals) and
                     [row['id'] for row in originals] == ids)
            addresses = {row['id']: row['address'] for row in originals}
            new_rows = _target_rows(addresses)
            receipt = baseline_value['receipt']
            session = receipt['feedSession']
            _uuid(session)
            _require(receipt['mode'] == 'disabled-rehearsal' and receipt['desired'] == 2 and
                     type(receipt['sequence']) is int and receipt['sequence'] > 0 and
                     type(pre['sequence']) is int and pre['sequence'] > receipt['sequence'] and
                     pre['feedSession'] == bundle['feedSession'] == session and
                     type(pending['sequence']) is int and pending['sequence'] > pre['sequence'] and
                     type(pending['postReadbackSequence']) is int and
                     pending['postReadbackSequence'] >= pending['sequence'])
            for value in (baseline_value, pre, pending, bundle):
                _require(value['providerRunContextSha256'] == baseline_value['providerRunContextSha256'])
            _sha(baseline_value['providerRunContextSha256'])
            for value in (baseline_value, pre, pending):
                _require(value['targetSha256'] == target_sha)
            new_id = proof.text(pending['newId'])
            _require(new_id not in ids)
            old_addresses = dict(addresses)
            old_addresses[new_id] = pending['address']
            old_rows = _target_rows(old_addresses)
            pending_rv = pending['configMapResourceVersion']
            _require(type(pending_rv) is str and
                     re.fullmatch(r'[1-9][0-9]{0,31}', pending_rv) is not None and
                     type(baseline_value['configMap']['resourceVersion']) is str and
                     re.fullmatch(r'[1-9][0-9]{0,31}', baseline_value['configMap']['resourceVersion']) is not None and
                     int(pending_rv) > int(baseline_value['configMap']['resourceVersion']))
            source_sha = hashlib.sha256(Path(proof.__file__).read_bytes()).hexdigest()
            expected_summary = dict(runId=run_id, feedSession=session, newId=new_id,
                originalInstanceIds=ids, sequence=pending['sequence'],
                observedAt=pending['observedAt'], resourceVersion=pending_rv,
                clientRosterSha256=hashlib.sha256(_canonical(baseline_value['roster'])).hexdigest(),
                evidenceSha256=inputs['pending-ready'], sourceSha256=source_sha)
            _require(type(pending_binding) is dict and pending_binding == expected_summary and
                     bundle['summary'] == expected_summary)
            _require(type(close_context) is dict and set(close_context) == {
                'feedSession', 'sequence', 'observedAt', 'scaleinRequestId',
                'scaleinAcknowledgedAt', 'pendingBindingSha256'})
            _uuid(close_context['feedSession'])
            _uuid(close_context['scaleinRequestId'])
            _sha(close_context['pendingBindingSha256'])
            _require(close_context['feedSession'] == session and
                     type(close_context['sequence']) is int and
                     pending['postReadbackSequence'] < close_context['sequence'] <= 999999 and
                     close_context['pendingBindingSha256'] ==
                     hashlib.sha256(_canonical(pending_binding)).hexdigest())
            for value in (pending['observedAt'], pending['postReadbackObservedAt'],
                          close_context['observedAt'], close_context['scaleinAcknowledgedAt']):
                _require(type(value) in (int, float) and math.isfinite(value))
            _require(close_context['observedAt'] > max(pending['observedAt'],
                     pending['postReadbackObservedAt'], close_context['scaleinAcknowledgedAt']))
            inventory = contract.parent / 'run' / 'provider-inventory.json'
            _require(baseline_value['inventoryPath'] == str(inventory))
            source = fresh_provider_source(contract, run_id, inventory, require_fresh=False)
            _require(source['providerRunContextSha256'] == baseline_value['providerRunContextSha256'] and
                     source['targetSha256'] == target_sha and source['feedSession'] == session and
                     source['sequence'] == close_context['sequence'] and
                     source['observedAt'] == close_context['observedAt'] and
                     source['privateAddresses'] == addresses)
            now = time.time if clock is None else clock
            current = now()
            _require(type(current) in (int, float) and math.isfinite(current) and
                     -1 <= current - source['observedAt'] < 30)
            _current_inventory(inventory, source, current)
            kube = baseline_value['paths']['kubeconfig']
            _private_file(kube)
            read = worker_capture.kubectl if kubectl is None else kubectl
            live = _with_kubeconfig(kube, read, 'get', 'configmap',
                                    'pgcat-provider-inventory', '-o', 'json')
            old_rv = _reconcile_configmap(live, baseline_value, old_rows, pending_rv)
            # Refuse a concurrent publication or changed sealed input before claiming mutation.
            _require(fresh_provider_source(contract, run_id, inventory, require_fresh=False) == source)
            for name in inputs:
                _require(hashlib.sha256(_read_at(fd, name + '.json', 65536)).hexdigest() == inputs[name])
                _sealed_read(fd, name + '.json')
            _require(_sealed_read(fd, 'pending-proof.bundle.json') == bundle)
            current = now()
            _require(type(current) in (int, float) and math.isfinite(current) and
                     -1 <= current - source['observedAt'] < 30)
            provenance = dict(version=1, runId=run_id, closeContext=close_context,
                pendingBinding=pending_binding, pendingBundleSha256=bundle_sha,
                sealedInputsSha256=inputs, providerSource=source,
                oldResourceVersion=old_rv, configMapUid=live['metadata']['uid'],
                oldTargetsSha256=hashlib.sha256(_canonical(old_rows)).hexdigest(),
                newTargetsSha256=hashlib.sha256(_canonical(new_rows)).hexdigest())
            _write_once(fd, 'reconcile-close.intent', provenance)
            patch = dict(metadata=dict(uid=live['metadata']['uid'], resourceVersion=old_rv),
                         data={'targets.json': _canonical(new_rows).decode('ascii')})
            _with_kubeconfig(kube, read, 'patch', 'configmap', 'pgcat-provider-inventory',
                             '--type=merge', '-p', _canonical(patch).decode('ascii'), '-o', 'json')
            readback = _with_kubeconfig(kube, read, 'get', 'configmap',
                                        'pgcat-provider-inventory', '-o', 'json')
            new_rv = _reconcile_configmap(readback, baseline_value, new_rows, old_rv, newer=True)
            sealed = dict(provenance, resourceVersion=new_rv)
            _sealed_write(fd, 'reconcile-close.json', sealed)
            return dict(feedSession=session, sequence=source['sequence'], resourceVersion=new_rv)
        finally:
            os.close(fd)
    except Exception:
        # Patch errors/timeouts can contain private diagnostics. Preserve the intent
        # and evidence, and never infer that a failed local call cancelled the patch.
        raise ValueError('reconcile') from None


def post_stop_finish(contract_dir, run_id, target, stop_context, *, source_hashes,
                     kubectl=None, clock=None, monotonic=None):
    """Complete one exact original-client boundary; local stop proof is asserted.

    This is never a whole-window traffic verdict. Once intent exists an unknown
    finish consumes it permanently, even if its transport outlives this call.
    """
    mono = time.monotonic if monotonic is None else monotonic
    started = mono()
    deadline = started + 4
    fd = None
    try:
        import segmented_capture as witness
        import pgcat_collector_control as collector
        def budget():
            value = mono()
            _require(type(value) in (int, float) and math.isfinite(value) and
                     started <= value < deadline)
            return deadline - value
        def bounded(operation, *args, **kwargs):
            budget()
            result = operation(*args, **kwargs)
            budget()
            return result
        def open_dir(path, operation=_directory):
            budget()
            opened = operation(path)
            try:
                budget();return opened
            except BaseException:
                os.close(opened);raise
        def read_at(directory, name, limit=65536):
            return bounded(_read_at, directory, name, limit)
        def sealed(name):
            value = bounded(_sealed_read, fd, name)
            raw = read_at(fd, name)
            _require(_decode(raw) == value)
            return value, hashlib.sha256(raw).hexdigest()
        budget()
        _uuid(run_id)
        target_sha = _bound_target(target)
        contract = Path(contract_dir)
        run = contract.parent / 'run'
        _require(contract.is_absolute() and contract.name == 'contract')
        fd = open_dir(contract)
        _require(not any(name.startswith('post-stop-finish.') for name in os.listdir(fd)))
        _require(type(source_hashes) is dict and set(source_hashes) == witness.SOURCE_KEYS)
        sources = dict(source_hashes)
        def verify_sources():
            source_fd = open_dir(run)
            try:
                for key, name in collector.SOURCES.items():
                    _sha(sources[key])
                    _require(hashlib.sha256(read_at(source_fd, name, 262144)).hexdigest() == sources[key])
            finally:os.close(source_fd)
        verify_sources()
        context = _decode(_canonical(stop_context))
        _require(type(context) is dict and set(context) == {
            'schemaVersion', 'requestId', 'stopProofSha256', 'receipt',
            'reconcileBinding', 'collectorIdentity'} and
            type(context['schemaVersion']) is int and context['schemaVersion'] == 1)
        _uuid(context['requestId']);_sha(context['stopProofSha256'])
        baseline_value, baseline_sha = sealed('baseline.json')
        _require(baseline_value['version'] == 1 and type(baseline_value['version']) is int and
                 baseline_value['purpose'] == 'disabled-rehearsal-post-stop' and
                 baseline_value['mode'] == 'disabled-rehearsal' and baseline_value['targetSha256'] == target_sha)
        paths = dict(captureDir=str(run/'rehearsal-capture'), boundaryDir=str(run/'disabled-boundary'),
                     kubeconfig=str(contract.parent/'kubeconfig'))
        _require(baseline_value['paths'] == paths and baseline_value['inventoryPath'] == str(run/'provider-inventory.json'))
        bounded(_private_file, paths['kubeconfig'])
        roster = finish_clients.validate_roster(baseline_value['roster'])
        _require(roster == baseline_value['roster'])
        ids = target['originalInstanceIds']
        _require(type(baseline_value['originals']) is list and
                 [r['id'] for r in baseline_value['originals']] == ids and
                 all(type(r) is dict and set(r) == {'id', 'address'} for r in baseline_value['originals']))
        addresses = {r['id']:r['address'] for r in baseline_value['originals']}
        targets = _target_rows(addresses)
        targets_sha = hashlib.sha256(_canonical(targets)).hexdigest()
        inputs = {'baseline':baseline_sha}
        for name in ('pre-scale', 'pending-ready'):
            value, digest = sealed(name+'.json');inputs[name] = digest
            _require(value['providerRunContextSha256'] == baseline_value['providerRunContextSha256'] and value['targetSha256'] == target_sha)
        for name in inputs:
            _require(_decode(read_at(fd, name+'.intent', 256)) == {'version':1,'runId':run_id})
        pending, pending_sha = sealed('pending-proof.bundle.json')
        reconcile, reconcile_sha = sealed('reconcile-close.json')
        _require(type(reconcile) is dict and set(reconcile) == {'version','runId','closeContext','pendingBinding','pendingBundleSha256',
            'sealedInputsSha256','providerSource','oldResourceVersion','configMapUid','oldTargetsSha256','newTargetsSha256','resourceVersion'} and
            type(reconcile['version']) is int and reconcile['version'] == 1 and reconcile['runId'] == run_id and
            reconcile['sealedInputsSha256'] == inputs and reconcile['pendingBundleSha256'] == pending_sha and
            reconcile['newTargetsSha256'] == targets_sha and reconcile['configMapUid'] == baseline_value['configMap']['uid'])
        _require(_decode(read_at(fd,'reconcile-close.intent')) == {key:reconcile[key] for key in reconcile if key != 'resourceVersion'})
        session = baseline_value['receipt']['feedSession'];_uuid(session)
        _require(type(pending) is dict and set(pending) == {'schemaVersion','runId','feedSession','summary','sealedInputsSha256','providerRunContextSha256'} and
            type(pending['schemaVersion']) is int and pending['schemaVersion'] == 1 and pending['runId'] == run_id and pending['feedSession'] == session and
            pending['sealedInputsSha256'] == inputs and pending['summary'] == reconcile['pendingBinding'] and
            pending['providerRunContextSha256'] == baseline_value['providerRunContextSha256'] and
            pending['summary']['sourceSha256'] == sources['pendingProofWorker'])
        close = reconcile['closeContext']
        _require(type(close) is dict and set(close) == {'feedSession','sequence','observedAt','scaleinRequestId','scaleinAcknowledgedAt','pendingBindingSha256'} and
                 close['feedSession'] == session and type(close['sequence']) is int and
                 type(close['observedAt']) in (int,float) and math.isfinite(close['observedAt']) and
                 type(close['scaleinAcknowledgedAt']) in (int,float) and math.isfinite(close['scaleinAcknowledgedAt']) and close['observedAt'] > close['scaleinAcknowledgedAt'] and
                 pending['summary']['runId'] == run_id and pending['summary']['feedSession'] == session and pending['summary']['originalInstanceIds'] == ids)
        binding = context['reconcileBinding']
        _require(type(binding) is dict and set(binding) == {'feedSession','sequence','resourceVersion','requestId','runId','scaleinRequestId','pendingBindingSha256','sourceSha256','sealedAt'})
        _uuid(binding['requestId']);_uuid(binding['scaleinRequestId'])
        _require(binding['feedSession'] == session and binding['runId'] == run_id and
                 type(binding['sequence']) is int and binding['sequence'] == reconcile['closeContext']['sequence'] and
                 binding['resourceVersion'] == reconcile['resourceVersion'] and binding['sourceSha256'] == sources['rehearsalContract'] and
                 binding['scaleinRequestId'] == reconcile['closeContext']['scaleinRequestId'] and
                 binding['pendingBindingSha256'] == reconcile['closeContext']['pendingBindingSha256'] == hashlib.sha256(_canonical(pending['summary'])).hexdigest())
        # requestId, stopProof and this later journal timestamp are assertions at
        # the worker boundary. The downstream supervisor derives them itself.
        sealed_at = binding['sealedAt']
        _require(type(sealed_at) in (int,float) and math.isfinite(sealed_at) and sealed_at >= reconcile['closeContext']['observedAt'])
        receipt = context['receipt']
        _require(type(receipt) is dict and set(receipt) == {'feedSession','sequence','mode','desired','observedAt','rosterSha256'} and
                 receipt['feedSession'] == session and receipt['mode'] == 'disabled-rehearsal' and
                 type(receipt['desired']) is int and receipt['desired'] == 2 and
                 type(receipt['sequence']) is int and binding['sequence'] <= receipt['sequence'] <= 999999 and
                 type(receipt['observedAt']) in (int,float) and math.isfinite(receipt['observedAt']) and receipt['observedAt'] > sealed_at)
        _sha(receipt['rosterSha256'])
        now = time.time if clock is None else clock
        def current_time():
            current = bounded(now)
            _require(type(current) in (int,float) and math.isfinite(current) and -1 <= current-receipt['observedAt'] < 30)
            return current
        source = bounded(fresh_provider_source, contract, run_id, run/'provider-inventory.json', require_fresh=False)
        _require(source['providerRunContextSha256'] == baseline_value['providerRunContextSha256'] and source['targetSha256'] == target_sha and
                 source['feedSession'] == session and source['sequence'] == receipt['sequence'] and source['observedAt'] == receipt['observedAt'] and source['privateAddresses'] == addresses)
        inventory = bounded(_current_inventory, run/'provider-inventory.json', source, current_time())
        _require(receipt['rosterSha256'] == hashlib.sha256(_canonical({key:inventory[key] for key in ('members','healthyRegistered','memberStates','backendStates')})).hexdigest())
        _require(reconcile['providerSource']['providerRunContextSha256'] == source['providerRunContextSha256'] and
                 reconcile['providerSource']['targetSha256'] == target_sha and reconcile['providerSource']['feedSession'] == session and
                 reconcile['providerSource']['sequence'] == binding['sequence'] and reconcile['providerSource']['observedAt'] == reconcile['closeContext']['observedAt'] and
                 reconcile['providerSource']['privateAddresses'] == addresses)
        provider_fd = open_dir(contract/'provider-source')
        try:provider_raw = read_at(provider_fd,'run-context.json',2048)
        finally:os.close(provider_fd)
        _require(hashlib.sha256(provider_raw).hexdigest() == source['providerRunContextSha256'])
        provider_record = _decode(provider_raw)
        job = open_dir(run/'collector-job')
        try:
            intent = _decode(read_at(job,'intent.json'))
            _require(type(intent) is dict and set(intent) == {'runId','startedAt','profile'} and intent['runId'] == run_id and
                     intent['profile'] == 'disabled-rehearsal' and type(intent['startedAt']) is int and
                     0 <= current_time()-intent['startedAt'] < 5400 and 'exit.json' not in os.listdir(job))
            _require(_decode(read_at(job,'claim.json')) == dict(runId=run_id))
            child = _decode(read_at(job,'identity.json'));collector.valid_identity(child)
            collector.valid_identity(context['collectorIdentity'])
            _require(child == context['collectorIdentity'] and bounded(collector.process_identity,child['pid']) == child)
        finally:os.close(job)
        expected = witness.validate_context(dict(schemaVersion=1,runId=run_id,feedSession=session,
            providerRunContextSha256=source['providerRunContextSha256'],bundleSha256=provider_record['runContext']['bundleSha256'],
            sourceHashes=sources,baselineSha256=baseline_sha,originalRoster=roster,deployment=baseline_value['deployment'],collectorIdentity=child))
        progress, previous, latest = bounded(witness.read_progress,paths['captureDir'],expected)
        _require(previous is not None and latest is not None and not any(progress['flags'].values()) and progress['maxGapSeconds'] <= 3)
        _require(sealed_at < proof.timestamp(previous['at']) < proof.timestamp(latest['at']) and proof.timestamp(latest['at'])-proof.timestamp(previous['at']) <= 3)
        for frame in (previous,latest):
            _require(capture_window.clean(frame) == frame and not capture_window.observed_sql_failure(frame,proof.timestamp(progress['firstAt'])) and
                     not any(e['kind'] == 'skipped' for p in frame['pods'] for e in p['snapshot']['events']))
            frame_roster = finish_clients.validate_roster([dict(name=pod['name'],uid=pod['uid'],
                containerId=pod['containerId'],processId=pod['snapshot']['sample']['processId'])
                for pod in frame['pods']])
            _require(frame_roster == roster)
            _exact_original_frame(frame,baseline_value)
            _require(type(frame['inventory']['sequence']) is int and frame['inventory']['sequence'] >= binding['sequence'])
            proof.complete_metrics(frame,ids,current_time())
            deployment = frame['deployment'];original = baseline_value['deployment']
            _require(deployment == dict(uid=original['uid'],generation=original['generation'],observedGeneration=original['generation'],desired=2,ready=2))
        previous_samples = {p['uid']:p['snapshot']['sample'] for p in previous['pods']}
        latest_samples = {p['uid']:p['snapshot']['sample'] for p in latest['pods']}
        for uid,sample in latest_samples.items():
            before = previous_samples[uid]
            _require(sample['sequence'] > before['sequence'] and all(type(sample[k]) is int and sample[k] >= before[k] for k in capture_window.COUNTERS) and
                     sample['Attempts'] == sum(sample[k] for k in ('Success','Failure','Timeout','Ambiguous')) and sample['Scheduled'] >= sample['Attempts']+sample['Skipped'])
        _require(latest['inventory'] == inventory)
        reader = worker_capture.kubectl if kubectl is None else kubectl
        options = dict(deadline=deadline,kubeconfig=paths['kubeconfig'])
        cm = bounded(reader,'get','configmap','pgcat-provider-inventory','-o','json',**options)
        _require(_reconcile_configmap(cm,baseline_value,targets,binding['resourceVersion']) == binding['resourceVersion'])
        deployment = bounded(reader,'get','deployment','rdev-test-client-client','-o','json',**options)
        pods = bounded(reader,'get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json',**options)['items']
        live = bounded(finish_clients.live_roster,**options)
        proof.exact_clients(baseline_value,[previous,latest],live,deployment,pods)
        # Recheck immutable inputs and the stopped source before consuming intent.
        verify_sources()
        _require(bounded(fresh_provider_source,contract,run_id,run/'provider-inventory.json',require_fresh=False) == source)
        for name, digest in inputs.items():_require(sealed(name+'.json')[1] == digest)
        _require(sealed('pending-proof.bundle.json')[1] == pending_sha and sealed('reconcile-close.json')[1] == reconcile_sha)
        _require(bounded(collector.process_identity,child['pid']) == child)
        _require(current_time()+5 <= intent['startedAt']+5400)
        budget()
        bounded(_write_once,fd,'post-stop-finish.intent',dict(version=1,runId=run_id,context=context))
        _require(current_time()+5 <= intent['startedAt']+5400)
        result = bounded(finish_clients.finish,Path(paths['boundaryDir']),expected_roster=roster,**options)
        _require(type(result) is dict and result.get('status') == 'ACKNOWLEDGED')
        boundary_fd = open_dir(paths['boundaryDir'],witness.open_directory)
        capture_fd = None
        try:
            capture_fd = open_dir(paths['captureDir'],witness.open_directory)
            def boundary(name):
                bounded(witness.check_directory,boundary_fd,paths['boundaryDir'])
                raw = bounded(witness.read_private,boundary_fd,name)
                bounded(witness.check_directory,boundary_fd,paths['boundaryDir'])
                return raw
            while True:
                budget()
                try:completed_raw = boundary('boundary-completed.json');break
                except FileNotFoundError:time.sleep(min(.01,budget()))
            raws = {name:boundary(name+'.json') for name in ('request','ack','receipt','boundary-start')}
            request, ack, actual_receipt, start = (witness.decode_private(raws[name]) for name in ('request','ack','receipt','boundary-start'))
            completed = witness.decode_private(completed_raw)
            _require(type(request) is dict and set(request) == {'requestId','roster'} and type(request['requestId']) is str and re.fullmatch('[0-9a-f]{32}',request['requestId']) is not None and request['roster'] == roster and ack == request)
            clients = {r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in roster}
            _require(actual_receipt == result == dict(requestId=request['requestId'],status='ACKNOWLEDGED',clients=clients))
            _require(type(start) is dict and set(start) == {'schemaVersion','bindingSha256','requestSha256','requestId','originalRosterSha256','startedMonotonic','deadlineMonotonic','firstSettlingFrame'} and type(start['schemaVersion']) is int and start['schemaVersion'] == 1 and
                start['bindingSha256'] == progress['bindingSha256'] and start['requestSha256'] == witness.sha(raws['request']) and start['requestId'] == request['requestId'] and start['originalRosterSha256'] == witness.sha(witness.canonical(roster)))
            _require(all(type(start[k]) in (int,float) and math.isfinite(start[k]) for k in ('startedMonotonic','deadlineMonotonic')) and
                     started <= start['startedMonotonic'] and start['deadlineMonotonic'] == start['startedMonotonic']+5)
            _require(type(completed) is dict and set(completed) == {'schemaVersion','bindingSha256','requestSha256','requestId','startSha256','receiptSha256','finalDurableMonotonic','finals','lastFrame'} and type(completed['schemaVersion']) is int and completed['schemaVersion'] == 1 and
                completed['bindingSha256'] == progress['bindingSha256'] and completed['requestSha256'] == witness.sha(raws['request']) and completed['requestId'] == request['requestId'] and completed['startSha256'] == witness.sha(raws['boundary-start']) and completed['receiptSha256'] == witness.sha(raws['receipt']))
            durable = completed['finalDurableMonotonic']
            _require(type(durable) in (int,float) and math.isfinite(durable) and start['startedMonotonic'] <= durable <= start['deadlineMonotonic'] and durable <= mono())
            first = bounded(witness.read_frame,capture_fd,start['firstSettlingFrame'])
            _require(first['phase'] == 'settling' and proof.timestamp(first['at']) >= proof.timestamp(latest['at']))
            _require(type(completed['finals']) is list and len(completed['finals']) == 2 and all(type(r) is dict and set(r) == {'uid','processId','frame'} for r in completed['finals']))
            _require(sorted((r['uid'],r['processId']) for r in completed['finals']) == sorted((r['uid'],r['processId']) for r in roster))
            final_samples = {}
            for ref in completed['finals']:
                frame = bounded(witness.read_frame,capture_fd,ref['frame'])
                _require(frame['phase'] == 'settling' and capture_window.clean(frame) == frame and proof.timestamp(frame['at']) >= proof.timestamp(first['at']) and
                         frame['inventory'] == inventory and frame['expectedReplicas'] == 2 and
                         not capture_window.observed_sql_failure(frame,proof.timestamp(progress['firstAt'])) and
                         not any(e['kind'] == 'skipped' for p in frame['pods'] for e in p['snapshot']['events']))
                candidates = [p for p in frame['pods'] if p['uid'] == ref['uid'] and p['snapshot']['sample']['processId'] == ref['processId']]
                _require(len(candidates) == 1)
                pod = candidates[0];snap = pod['snapshot'];sample = snap['sample']
                _require(dict(name=pod['name'],uid=pod['uid'],containerId=pod['containerId'],processId=sample['processId']) in roster and
                         snap['final'] is True and type(snap['connected']) is int and snap['connected'] == 0 and
                         sample['Scheduled'] == sample['Attempts']+sample['Skipped'] and sample['Skipped'] == 0 and
                         all(type(sample[k]) is int and sample[k] >= latest_samples[pod['uid']][k] for k in capture_window.COUNTERS+('sequence',)) and
                         all(type(sample[k]) is int and sample[k] == 0 for k in ('Failure','Timeout','Ambiguous')))
                final_samples[pod['uid']] = sample
            after, _, tail = bounded(witness.read_progress,paths['captureDir'],expected)
            _require(not any(after['flags'].values()) and after['maxGapSeconds'] <= 3 and after['latest'] == completed['lastFrame'])
            _require(tail['phase'] == 'settling' and capture_window.clean(tail) == tail and tail['inventory'] == inventory and tail['expectedReplicas'] == 2 and
                     not capture_window.observed_sql_failure(tail,proof.timestamp(progress['firstAt'])) and
                     not any(e['kind'] == 'skipped' for p in tail['pods'] for e in p['snapshot']['events']) and
                     len(tail['pods']) == 2 and sorted(p['uid'] for p in tail['pods']) == sorted(final_samples))
            for pod in tail['pods']:
                snap = pod['snapshot'];sample = snap['sample']
                _require(dict(name=pod['name'],uid=pod['uid'],containerId=pod['containerId'],processId=sample['processId']) in roster and
                         snap['final'] is True and type(snap['connected']) is int and snap['connected'] == 0 and sample == final_samples[pod['uid']])
            _require(all(proof.timestamp(ref['frame']['at']) <= proof.timestamp(tail['at']) for ref in completed['finals']))
            bounded(witness.read_frame,capture_fd,completed['lastFrame'],True)
            for name,raw in raws.items():_require(boundary(name+'.json') == raw)
            _require(boundary('boundary-completed.json') == completed_raw)
        finally:
            os.close(boundary_fd)
            if capture_fd is not None:os.close(capture_fd)
        verify_sources();current_time()
        for name,digest in inputs.items():_require(sealed(name+'.json')[1] == digest)
        _require(sealed('pending-proof.bundle.json')[1] == pending_sha and sealed('reconcile-close.json')[1] == reconcile_sha)
        _require(bounded(fresh_provider_source,contract,run_id,run/'provider-inventory.json',require_fresh=False) == source)
        evidence = dict(version=1,runId=run_id,context=context,progressBindingSha256=progress['bindingSha256'],
            progress=progress,frames=dict(previous=progress['previous'],latest=progress['latest']),configMap=dict(uid=cm['metadata']['uid'],resourceVersion=binding['resourceVersion'],targetsSha256=targets_sha),
            sourceHashes=sources,sealedInputsSha256=inputs,pendingBundleSha256=pending_sha,reconcileSha256=reconcile_sha,providerSource=source,
            boundaryHashes={name:witness.sha(raw) for name,raw in raws.items()},boundaryCompletedSha256=witness.sha(completed_raw),
            elapsedWorkerSeconds=mono()-started)
        bounded(_sealed_write,fd,'post-stop-finish.json',evidence)
        _require(sealed('post-stop-finish.json')[0] == evidence)
        evidence_raw = read_at(fd,'post-stop-finish.json')
        current_time();budget()
        return dict(feedSession=session,sequence=receipt['sequence'],boundaryRequestId=request['requestId'],
            boundarySha256=witness.sha(completed_raw),evidenceSha256=witness.sha(evidence_raw),sourceSha256=sources['rehearsalContract'])
    except Exception:
        raise ValueError('post-stop-finish') from None
    finally:
        if fd is not None:os.close(fd)
