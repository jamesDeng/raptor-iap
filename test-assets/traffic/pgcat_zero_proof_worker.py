#!/usr/bin/env python3
"""Worker-local, read-only no-restart zero proof for one bound PgCat candidate.

This only seals evidence. Scale-in admission and the action-time scaling-activity
check belong to the separately reviewed supervisor integration.
"""
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys
import time

# Use the source-pinned phase dispatcher; legacy direct scripts are not runtime entries.
if __name__ == '__main__':
    raise SystemExit(1)

import capture_window as capture
import finish_clients
import pgcat_pending_proof_worker as pending
import pgcat_pre_drain_proof_worker as pre_drain
import pgcat_rehearsal_contract as contract_source
import worker_capture


Refused = pending.Refused


def require(value):
    if not value:
        raise Refused('zero-gate')


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def stamp(value):
    return pending.timestamp(value)


def fresh(value, now, limit):
    age = now - stamp(value)
    require(0 <= age <= limit)


def exact_binding(binding, folder, baseline, pre, pending_artifact, run_id):
    require(type(binding) is dict and set(binding) == {
        'runId', 'feedSession', 'newId', 'originalInstanceIds',
        'pendingBindingSha256', 'sequence', 'observedAt', 'resourceVersion',
        'clientRosterSha256', 'evidenceSha256', 'sourceSha256'})
    detail = pending.read_sealed(folder, 'pre-drain')
    raw = pending.read_private(folder / 'pre-drain.json')
    ids = [row['id'] for row in baseline['originals']]
    candidate = pending_artifact['newId']
    require(binding['runId'] == run_id and
            binding['feedSession'] == baseline['receipt']['feedSession'] and
            binding['newId'] == candidate and
            binding['originalInstanceIds'] == ids and
            binding['evidenceSha256'] == sha(raw) and
            type(binding['sourceSha256']) is str and
            binding['sourceSha256'] == sha(Path(pre_drain.__file__).read_bytes()) and
            binding['pendingBindingSha256'] == detail['pendingBindingSha256'] and
            binding['sequence'] == detail['sequence'] and
            binding['observedAt'] == detail['observedAt'] and
            binding['resourceVersion'] == detail['resourceVersion'] and
            binding['clientRosterSha256'] == detail['clientRosterSha256'] and
            detail['runId'] == run_id and
            detail['feedSession'] == binding['feedSession'] and
            detail['newId'] == candidate and
            detail['providerRunContextSha256'] ==
            baseline['providerRunContextSha256'] ==
            pre['providerRunContextSha256'] ==
            pending_artifact['providerRunContextSha256'])
    require(len(ids) == 2 and ids == sorted(set(ids)) and candidate not in ids)
    return detail, sha(pre_drain.canonical(binding))


def exact_inventory(inv, ids, candidate, session):
    members = sorted(ids + [candidate])
    require(inv['schemaVersion'] == 2 and inv['mode'] == 'disabled-rehearsal' and
            inv['feedSession'] == session and inv['desired'] == 3 and
            inv['members'] == members and
            inv['memberStates'] == [dict(id=node,
                lifecycle='InService' if node == candidate else 'Protected',
                health='Healthy', addressKnown=True) for node in members] and
            inv['healthyRegistered'] in (ids, members))
    backends = {row['id']: row for row in inv['backendStates']}
    require(set(backends) in (set(ids), set(members)))
    for node in ids:
        require(backends[node] == dict(id=node, status='Available', weight=100,
                                      listenerHealth='Normal'))
    if candidate in backends:
        state = backends[candidate]
        require(state == dict(id=candidate, status='Available', weight=100,
                              listenerHealth='Normal') or
                (state['status'] == 'Removing' and state['weight'] in (0, 100) and
                 state['listenerHealth'] in
                 ('Normal', 'Initial', 'Unhealthy', 'Unavailable')))
        require(inv['healthyRegistered'] ==
                (members if state['status'] == 'Available' else ids))
        return state['status']
    require(inv['healthyRegistered'] == ids)
    return 'absent'


def metric_series(frame, ids, candidate, now=None):
    metric = frame['metrics']
    at = stamp(frame['at'])
    require(metric['available'] is True and metric['nodes'] == sorted(ids + [candidate]))
    require(type(metric.get('upSourceAt')) is dict and
            set(metric['upSourceAt']) == set(ids + [candidate]))
    for value in (metric['observedAt'], metric['sourceAt']):
        fresh(value, at, 45)
        if now is not None:
            fresh(value, now, 45)
    up = {}
    for node in ids + [candidate]:
        up[node] = stamp(metric['upSourceAt'][node])
        fresh(up[node], at, 45)
        if now is not None:
            fresh(up[node], now, 45)
    series = {}
    groups = {}
    for row in metric['raw']:
        node = row['node']
        key = (node, row['pool'], row['user'], row['kind'])
        require(node in ids + [candidate] and
                type(row['pool']) is str and row['pool'] and
                type(row['user']) is str and row['user'] and
                row['kind'] in pending.KIND and key not in series)
        value = row['value']
        require(type(value) in (int, float) and math.isfinite(value) and
                value >= 0 and value == int(value) and value <= 2**53)
        source = stamp(row['sourceAt'])
        fresh(source, at, 45)
        if now is not None:
            fresh(source, now, 45)
        series[key] = (value, source)
        groups.setdefault((node, row['pool'], row['user']), set()).add(row['kind'])
    require(groups and all(kinds == set(pending.KIND) for kinds in groups.values()))
    require(all((node, 'test', 'poc_app') in groups for node in ids + [candidate]))
    require(all(value == 0 for key, (value, _) in series.items()
                if key[0] == candidate))
    return up, series, max([at, stamp(metric['observedAt']),
                            stamp(metric['sourceAt'])] + list(up.values()) +
                           [source for _, source in series.values()])


def produce(contract_dir, run_id, pre_drain_binding, *, kubectl=None,
            live_roster=None, now=None):
    """Seal one address-free action-time summary from private retained sources."""
    try:
        folder = pending.private(Path(contract_dir), True)
        require(folder.name == 'contract')
        clock = time.time if now is None else now
        current = clock()
        require(type(current) in (int, float) and math.isfinite(current))
        require(not (folder / 'zero-gate.json').exists() and
                not (folder / 'zero-gate.json.sha256').exists())
        baseline = pending.read_sealed(folder, 'baseline')
        pre = pending.read_sealed(folder, 'pre-scale')
        selected = pending.read_sealed(folder, 'pending-ready')
        detail, binding_sha = exact_binding(pre_drain_binding, folder, baseline,
                                            pre, selected, run_id)
        require(baseline['purpose'] == 'disabled-rehearsal-post-stop' and
                baseline['mode'] == 'disabled-rehearsal')
        ids = pre_drain_binding['originalInstanceIds']
        candidate = pre_drain_binding['newId']
        session = pre_drain_binding['feedSession']
        capture_dir = pending.private(Path(baseline['paths']['captureDir']), True)
        require(not (capture_dir / 'completion.json').exists())
        frames = pending.verified_frames(capture_dir)
        require(frames[0]['at'] == pre['firstAt'] and
                pending.frame_digest(frames[0]) == pre['firstFrameSha256'])
        require(len([frame for frame in frames if frame['at'] == pre['latestAt'] and
                pending.frame_digest(frame) == pre['latestFrameSha256']]) == 1)
        selected_index = selected['frameIndex']
        require(type(selected_index) is int and 0 <= selected_index < len(frames))
        require(pending.frame_digest(frames[selected_index]) == selected['frameSha256'])
        boundary = [i for i, frame in enumerate(frames) if
            frame['inventory']['sequence'] == detail['sequence'] and
            pending.frame_digest(frame) == detail['latestFrameSha256']]
        require(len(boundary) == 1 and boundary[0] > selected_index and
                boundary[0] < len(frames) - 2)
        prior = frames[boundary[0]]['inventory']
        require(prior['observedAt'] == detail['observedAt'] and
                prior['feedSession'] == session)
        stages = []
        previous_at = None
        previous_sequence = None
        previous_observed = None
        for frame in frames[boundary[0]:]:
            at = stamp(frame['at'])
            inv = frame['inventory']
            require(frame['phase'] == 'measurement' and
                    frame['expectedReplicas'] == 2 and
                    inv['feedSession'] == session)
            state = exact_inventory(inv, ids, candidate, session)
            if previous_at is not None:
                require(0 < at - previous_at <= 3)
                require(inv['sequence'] >= previous_sequence and
                        inv['sequence'] <= previous_sequence + 1)
                require(stamp(inv['observedAt']) >= previous_observed)
                if inv['sequence'] > previous_sequence:
                    require(stamp(inv['observedAt']) > previous_observed)
            previous_at = at
            previous_sequence = inv['sequence']
            previous_observed = stamp(inv['observedAt'])
            require(0 <= at - stamp(inv['observedAt']) < 30)
            pending.complete_metrics(frame, ids + [candidate], at)
            stages.append(state)
        require(stages[0] == 'Available')
        rank = {'Available': 0, 'Removing': 1, 'absent': 2}
        require(all(rank[right] >= rank[left] for left, right in
                    zip(stages, stages[1:])) and 'absent' in stages)
        first_absence_at = stamp(frames[boundary[0] + stages.index('absent')]
                                 ['inventory']['observedAt'])
        require(all(0 < stamp(right['at']) - stamp(left['at']) <= 3
                    for left, right in zip(frames, frames[1:])))
        roster_ids = [row['uid'] for row in baseline['roster']]
        for earlier, later_frame in zip(frames[boundary[0]:],
                                        frames[boundary[0] + 1:]):
            before = {pod['uid']: pod['snapshot']['sample']['Success']
                      for pod in earlier['pods']}
            after = {pod['uid']: pod['snapshot']['sample']['Success']
                     for pod in later_frame['pods']}
            require(set(before) == set(after) == set(roster_ids) and
                    all(after[uid] > before[uid] for uid in roster_ids))
        latest = frames[-1]
        require(stages[-1] == 'absent' and
                latest['inventory']['sequence'] > detail['sequence'])
        require(0 <= current - stamp(latest['at']) <= 3 and
                0 <= current - stamp(latest['inventory']['observedAt']) < 30)
        # A zero sample is an actual distinct Prometheus scrape. Collector
        # frames sharing raw or per-node up timestamps cannot be counted twice.
        zero_samples = []
        for frame, stage in zip(frames[boundary[0] + 1:], stages[1:]):
            if stage != 'absent':
                continue
            metric = frame['metrics']
            if any(row['node'] == candidate and row['value'] != 0
                   for row in metric['raw']):
                # Nonzero after absence invalidates even an earlier zero pair.
                require(not zero_samples)
                continue
            up, series, maximum = metric_series(frame, ids, candidate)
            sample = (frame, up, series, maximum)
            if zero_samples:
                prior = zero_samples[-1]
                require(set(prior[2]) == set(series))
                old_stamps = {key: value[1] for key, value in prior[2].items()}
                new_stamps = {key: value[1] for key, value in series.items()}
                if up == prior[1] and new_stamps == old_stamps:
                    continue  # A repeated collector frame is not another scrape.
                require(all(up[node] > prior[1][node] for node in ids + [candidate]) and
                        all(new_stamps[key] > old_stamps[key] for key in old_stamps))
            zero_samples.append(sample)
        require(len(zero_samples) >= 2)
        # Pick the newest distinct pair for which a later provider publication
        # exists. Historical frames must be valid at capture time, but the
        # selected pair must also be fresh at action time.
        pair = None
        for index in range(len(zero_samples) - 1, 0, -1):
            first, second = zero_samples[index - 1:index + 1]
            # Collector frames can carry Prometheus samples that predate the
            # provider's first observation of NLB absence. Neither selected
            # zero sample may reuse one of those earlier scrapes.
            if not all(all(value > first_absence_at for value in sample[1].values())
                       and all(source > first_absence_at for _, source in sample[2].values())
                       for sample in (first, second)):
                continue
            later = [frame for frame in frames if
                     frame['inventory']['sequence'] > second[0]['inventory']['sequence'] and
                     stamp(frame['inventory']['observedAt']) > second[3] and
                     stamp(frame['at']) > stamp(second[0]['at'])]
            if later and latest in later:
                try:
                    metric_series(first[0], ids, candidate, current)
                    metric_series(second[0], ids, candidate, current)
                except Refused:
                    continue
                pair = (first, second)
                break
        require(pair is not None)
        first, second = pair
        for frame in frames[frames.index(second[0]):]:
            require(exact_inventory(frame['inventory'], ids, candidate,
                                    session) == 'absent')
            metric_series(frame, ids, candidate, current)
        live_path = pending.private(Path(baseline['inventoryPath']))
        live_raw = pending.read_private(live_path, pending.MAX_FRAME)
        live = pending.decode(live_raw)
        require(live == latest['inventory'])
        pending.verify_provider_source(folder, run_id, baseline, pre, selected,
            frames[selected_index]['inventory'], live_raw, live)
        assessment = capture.assess(frames, completed=False)
        require(assessment['status'] != 'FAIL' and assessment['success'] > 0 and
                set(assessment['reasons']).issubset(
                    pending.ALLOWED_ASSESSMENT_REASONS))
        kube = pending.private(Path(baseline['paths']['kubeconfig']))
        reader = worker_capture.kubectl if kubectl is None else kubectl
        roster_reader = finish_clients.live_roster if live_roster is None else live_roster
        cm = pre_drain._with_kubeconfig(kube, reader, 'get', 'configmap',
            'pgcat-provider-inventory', '-o', 'json')
        rv = cm['metadata']['resourceVersion']
        require(type(rv) is str and rv.isdigit() and
                int(rv) >= int(pre_drain_binding['resourceVersion']))
        pending.exact_targets(cm, baseline, candidate, selected['address'], rv)
        deployment = pre_drain._with_kubeconfig(kube, reader, 'get', 'deployment',
            'rdev-test-client-client', '-o', 'json')
        pods = pre_drain._with_kubeconfig(kube, reader, 'get', 'pods', '-l',
            'app.kubernetes.io/name=rdev-test-client-client', '-o', 'json')['items']
        roster = pre_drain._with_kubeconfig(kube, roster_reader)
        pending.exact_clients(baseline, frames, roster, deployment, pods)
        require(pending.read_private(live_path, pending.MAX_FRAME) == live_raw and
                pending.verified_frames(capture_dir)[-1] == latest)
        require(0 <= clock() - stamp(latest['at']) <= 3)
        detail_out = dict(runId=run_id, feedSession=session, newId=candidate,
            preDrainBindingSha256=binding_sha,
            firstZeroFrameSha256=pending.frame_digest(first[0]),
            secondZeroFrameSha256=pending.frame_digest(second[0]),
            sequence=latest['inventory']['sequence'],
            observedAt=latest['inventory']['observedAt'],
            clientRosterSha256=pre_drain_binding['clientRosterSha256'],
            latestFrameSha256=pending.frame_digest(latest),
            providerRunContextSha256=baseline['providerRunContextSha256'])
        fd = contract_source._directory(folder)
        try:
            contract_source._sealed_write(fd, 'zero-gate.json', detail_out)
        finally:
            os.close(fd)
        raw = pending.read_private(folder / 'zero-gate.json')
        return dict(runId=run_id, feedSession=session, newId=candidate,
            originalInstanceIds=ids, preDrainBindingSha256=binding_sha,
            sequence=latest['inventory']['sequence'],
            observedAt=latest['inventory']['observedAt'],
            clientRosterSha256=pre_drain_binding['clientRosterSha256'],
            evidenceSha256=sha(raw), sourceSha256=sha(Path(__file__).read_bytes()))
    except Refused:
        raise
    except (OSError, ValueError, TypeError, KeyError, IndexError, StopIteration,
            OverflowError, UnicodeError, AttributeError):
        raise Refused('zero-gate') from None


def main():
    previous_umask = os.umask(0o077)
    try:
        if len(sys.argv) != 4:
            return 2
        binding = pending.decode(sys.argv[3].encode('ascii'))
        result = produce(Path(sys.argv[1]), sys.argv[2], binding)
        print(json.dumps(result, sort_keys=True, separators=(',', ':')))
        return 0
    except Exception:
        return 1
    finally:
        os.umask(previous_umask)


if __name__ == '__main__':
    sys.exit(main())
