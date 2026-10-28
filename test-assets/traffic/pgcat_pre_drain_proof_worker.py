#!/usr/bin/env python3
"""Worker-local, address-free proof of one healthy three-node pre-drain state.

This module performs reads and seals evidence only. The Mac action that would
admit an NLB removal is deliberately separate and is not implemented here.
"""
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys
import time
import uuid

# Use the source-pinned phase dispatcher; legacy direct scripts are not runtime entries.
if __name__ == '__main__':
    raise SystemExit(1)

import capture_window as capture
import finish_clients
import pgcat_pending_proof_worker as pending_proof
import pgcat_rehearsal_contract as contract_source
import worker_capture


Refused = pending_proof.Refused


def require(value):
    if not value:
        raise Refused('pre-drain')


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      allow_nan=False).encode('ascii')


def digest(value):
    return hashlib.sha256(value).hexdigest()


def sealed(folder, name):
    value = pending_proof.read_sealed(folder, name)
    raw = pending_proof.read_private(folder / (name + '.json'))
    return value, raw


def exact_pending_binding(binding, baseline, pending, raw, run_id):
    require(type(binding) is dict and set(binding) == {
        'runId', 'feedSession', 'newId', 'originalInstanceIds', 'sequence',
        'observedAt', 'resourceVersion', 'clientRosterSha256',
        'evidenceSha256', 'sourceSha256'})
    require(type(run_id) is str and str(uuid.UUID(run_id)) == run_id)
    require(binding['runId'] == run_id and
            binding['feedSession'] == baseline['receipt']['feedSession'] and
            binding['newId'] == pending['newId'] and
            binding['originalInstanceIds'] == [row['id'] for row in baseline['originals']] and
            type(binding['sequence']) is int and binding['sequence'] == pending['sequence'] and
            type(binding['observedAt']) in (int, float) and
            math.isfinite(binding['observedAt']) and
            binding['observedAt'] == pending['observedAt'] and
            binding['resourceVersion'] == pending['configMapResourceVersion'] and
            binding['evidenceSha256'] == digest(raw) and
            binding['clientRosterSha256'] == digest(canonical(baseline['roster'])) and
            type(binding['sourceSha256']) is str and
            re.fullmatch(r'[0-9a-f]{64}', binding['sourceSha256']) is not None)
    return digest(canonical(binding))


def exact_three(frame, ids, candidate):
    inv = frame['inventory']
    members = sorted(ids + [candidate])
    require(frame['phase'] == 'measurement' and frame['expectedReplicas'] == 2 and
            inv['schemaVersion'] == 2 and inv['mode'] == 'disabled-rehearsal' and
            inv['desired'] == 3 and inv['members'] == members and
            inv['healthyRegistered'] == members and
            inv['memberStates'] == [dict(id=node,
                lifecycle='InService' if node == candidate else 'Protected',
                health='Healthy', addressKnown=True) for node in members] and
            inv['backendStates'] == [dict(id=node, status='Available', weight=100,
                listenerHealth='Normal') for node in members])


def exact_pre_convergence(frame, ids, candidate):
    """Allow only a clean Adding/Configuring observation before first health."""
    inv = frame['inventory']
    members = sorted(ids + [candidate])
    backends = inv['backendStates']
    require(frame['phase'] == 'measurement' and frame['expectedReplicas'] == 2 and
            inv['schemaVersion'] == 2 and inv['mode'] == 'disabled-rehearsal' and
            inv['desired'] == 3 and inv['members'] == members and
            inv['healthyRegistered'] == ids and
            inv['memberStates'] == [dict(id=node,
                lifecycle='InService' if node == candidate else 'Protected',
                health='Healthy', addressKnown=True) for node in members] and
            type(backends) is list and len(backends) == 3 and
            [row['id'] for row in backends] == members)
    for row in backends:
        if row['id'] == candidate:
            require(row['status'] in ('Adding', 'Configuring') and
                    row['weight'] == 100 and
                    row['listenerHealth'] in ('Initial', 'Normal'))
        else:
            require(row == dict(id=row['id'], status='Available', weight=100,
                                listenerHealth='Normal'))


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


def produce(contract_dir, run_id, pending_binding, *, kubectl=None,
            live_roster=None, now=None):
    """Seal one private proof and return only a fixed, address-free summary."""
    try:
        folder = pending_proof.private(Path(contract_dir), True)
        require(folder.name == 'contract')
        clock = time.time if now is None else now
        current = clock()
        require(type(current) in (int, float) and math.isfinite(current))
        baseline, _ = sealed(folder, 'baseline')
        pre, _ = sealed(folder, 'pre-scale')
        pending, pending_raw = sealed(folder, 'pending-ready')
        binding_sha = exact_pending_binding(pending_binding, baseline, pending,
                                            pending_raw, run_id)
        require(baseline['mode'] == 'disabled-rehearsal' and
                baseline['purpose'] == 'disabled-rehearsal-post-stop' and
                baseline['providerRunContextSha256'] ==
                pre['providerRunContextSha256'] ==
                pending['providerRunContextSha256'] and
                pre['feedSession'] == pending_binding['feedSession'])
        ids = [row['id'] for row in baseline['originals']]
        candidate = pending_binding['newId']
        require(len(ids) == 2 and ids == sorted(set(ids)) and candidate not in ids)
        capture_dir = pending_proof.private(Path(baseline['paths']['captureDir']), True)
        require(not (capture_dir / 'completion.json').exists())
        frames = pending_proof.verified_frames(capture_dir)
        selected_index = pending['frameIndex']
        require(type(selected_index) is int and 0 <= selected_index < len(frames) and
                pending_proof.frame_digest(frames[selected_index]) ==
                pending['frameSha256'])
        require(all(frame['inventory'].get('feedSession') ==
                    pending_binding['feedSession'] for frame in frames))
        require(all(candidate not in {row['id'] for row in frame['inventory']['backendStates']}
                    for frame in frames[:selected_index + 1]))
        post = []
        last_sequence = -1
        entered = False
        converged = False
        for frame in frames[selected_index + 1:]:
            backends = {row['id']: row for row in frame['inventory']['backendStates']}
            if candidate in backends:
                entered = True
            if entered:
                if converged or backends[candidate]['status'] == 'Available':
                    exact_three(frame, ids, candidate)
                    converged = True
                    seq = frame['inventory']['sequence']
                    if seq != last_sequence:
                        post.append(frame)
                        last_sequence = seq
                    else:
                        post[-1] = frame
                else:
                    exact_pre_convergence(frame, ids, candidate)
                    pending_proof.complete_metrics(frame, ids + [candidate],
                        pending_proof.timestamp(frame['at']))
        require(len(post) >= 2 and frames[-1] == post[-1] and
                post[-2]['inventory']['sequence'] < post[-1]['inventory']['sequence'] and
                pending_proof.timestamp(post[-2]['inventory']['observedAt']) <
                pending_proof.timestamp(post[-1]['inventory']['observedAt']) and
                pending_proof.timestamp(post[-2]['at']) <
                pending_proof.timestamp(post[-1]['at']))
        require(all(0 < pending_proof.timestamp(right['at']) -
                    pending_proof.timestamp(left['at']) <= 3
                    for left, right in zip(frames, frames[1:])))
        for frame in post[-2:]:
            require(0 <= pending_proof.timestamp(frame['at']) -
                    pending_proof.timestamp(frame['inventory']['observedAt']) <= 30)
            pending_proof.complete_metrics(frame, ids + [candidate],
                                           pending_proof.timestamp(frame['at']))
        latest = frames[-1]
        pending_proof.fresh(latest['at'], current, 3)
        pending_proof.fresh(latest['inventory']['observedAt'], current, 30)
        pending_proof.complete_metrics(latest, ids + [candidate], current)
        require(all(next(p['snapshot']['sample']['Success'] for p in later['pods']
                         if p['uid'] == uid) >
                    next(p['snapshot']['sample']['Success'] for p in earlier['pods']
                         if p['uid'] == uid)
                    for uid in [row['uid'] for row in baseline['roster']]
                    for earlier, later in ((post[-2], post[-1]),)))
        inventory_path = pending_proof.private(Path(baseline['inventoryPath']))
        live_raw = pending_proof.read_private(inventory_path,
                                              pending_proof.MAX_FRAME)
        live = pending_proof.decode(live_raw)
        require(live == latest['inventory'])
        pending_proof.verify_provider_source(folder, run_id, baseline, pre, pending,
            frames[selected_index]['inventory'], live_raw, live)
        assessment = capture.assess(frames, completed=False)
        require(assessment['status'] != 'FAIL' and assessment['success'] > 0 and
                set(assessment['reasons']).issubset(
                    pending_proof.ALLOWED_ASSESSMENT_REASONS))
        kube = pending_proof.private(Path(baseline['paths']['kubeconfig']))
        reader = worker_capture.kubectl if kubectl is None else kubectl
        roster_reader = finish_clients.live_roster if live_roster is None else live_roster
        cm = _with_kubeconfig(kube, reader, 'get', 'configmap',
                              'pgcat-provider-inventory', '-o', 'json')
        rv = cm['metadata']['resourceVersion']
        require(type(rv) is str and rv.isdigit() and
                int(rv) >= int(pending['configMapResourceVersion']))
        pending_proof.exact_targets(cm, baseline, candidate, pending['address'], rv)
        deployment = _with_kubeconfig(kube, reader, 'get', 'deployment',
            'rdev-test-client-client', '-o', 'json')
        pods = _with_kubeconfig(kube, reader, 'get', 'pods', '-l',
            'app.kubernetes.io/name=rdev-test-client-client', '-o', 'json')['items']
        roster = _with_kubeconfig(kube, roster_reader)
        pending_proof.exact_clients(baseline, frames, roster, deployment, pods)
        require(pending_proof.read_private(inventory_path,
                pending_proof.MAX_FRAME) == live_raw and
                pending_proof.verified_frames(capture_dir)[-1] == latest)
        pending_proof.fresh(latest['at'], clock(), 3)
        detail = dict(runId=run_id, feedSession=pending_binding['feedSession'],
            newId=candidate, pendingBindingSha256=binding_sha,
            firstPostEntrySequence=post[-2]['inventory']['sequence'],
            sequence=latest['inventory']['sequence'],
            observedAt=latest['inventory']['observedAt'],
            resourceVersion=rv, clientRosterSha256=pending_binding['clientRosterSha256'],
            latestFrameSha256=pending_proof.frame_digest(latest),
            providerRunContextSha256=baseline['providerRunContextSha256'])
        fd = contract_source._directory(folder)
        try:
            contract_source._sealed_write(fd, 'pre-drain.json', detail)
        finally:
            os.close(fd)
        detail_raw = pending_proof.read_private(folder / 'pre-drain.json')
        return dict(runId=run_id, feedSession=pending_binding['feedSession'],
            newId=candidate, originalInstanceIds=ids,
            pendingBindingSha256=binding_sha,
            sequence=latest['inventory']['sequence'],
            observedAt=latest['inventory']['observedAt'],
            resourceVersion=rv,
            clientRosterSha256=pending_binding['clientRosterSha256'],
            evidenceSha256=digest(detail_raw),
            sourceSha256=digest(Path(__file__).read_bytes()))
    except Refused:
        raise
    except (OSError, ValueError, TypeError, KeyError, IndexError, StopIteration,
            OverflowError, UnicodeError, AttributeError):
        raise Refused('pre-drain') from None


def main():
    previous_umask = os.umask(0o077)
    try:
        if len(sys.argv) != 4:
            return 2
        binding = pending_proof.decode(sys.argv[3].encode('ascii'))
        result = produce(Path(sys.argv[1]), sys.argv[2], binding)
        print(json.dumps(result, sort_keys=True, separators=(',', ':')))
        return 0
    except Exception:
        return 1
    finally:
        os.umask(previous_umask)


if __name__ == '__main__':
    sys.exit(main())
