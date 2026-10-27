"""Offline provider -> collector -> immutable manifest proof; not live acceptance.

Only provider, Kubernetes and HTTP boundaries are substituted. Publication,
collection, Prometheus parsing, retention and reassessment use production code.
"""
import json
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
from urllib.parse import parse_qs, urlparse

import provider_feed
import worker_capture
from segmented_capture import SegmentedCapture, assess_manifest


# One provider publication every two client frames, including identical repeats.
# Old nodes remain measurable through zero, then a later publication proves absence.
STAGES = (
    (2, 'ab', 'ab', {}, {}),
    (4, 'abcd', 'ab', {'c': 'Pending', 'd': 'Pending:Wait'}, {}),
    (4, 'abcd', 'abcd', {'c': 'Pending', 'd': 'Pending:Wait'},
     {'c': ('Adding', 100, 'Initial'), 'd': ('Adding', 100, 'Initial')}),
    (4, 'abcd', 'abcd', {'c': 'InService', 'd': 'InService'},
     {'c': ('Configuring', 100, 'Initial'), 'd': ('Configuring', 100, 'Initial')}),
    (4, 'abcd', 'abcd', {}, {}),
    (3, 'abcd', 'abcd', {'a': 'Removing'}, {'a': ('Removing', 0, 'Unavailable')}),
    (3, 'bcd', 'abcd', {}, {'a': ('Removing', 0, 'Unavailable')}),
    (3, 'bcd', 'bcd', {}, {}),
    (3, 'bcd', 'bcd', {}, {}),
    (2, 'bcd', 'bcd', {'b': 'Removing:Wait'}, {'b': ('Removing', 0, 'Unavailable')}),
    (2, 'cd', 'bcd', {}, {'b': ('Removing', 0, 'Unavailable')}),
    (2, 'cd', 'cd', {}, {}),
    (2, 'cd', 'cd', {}, {}),
)


def provider_responses(stage):
    desired, members, backends, lifecycles, states = stage
    pages = [{'PageNumber': i + 1, 'TotalCount': len(members),
              'ScalingInstances': {'ScalingInstance': [{
                  'InstanceId': node, 'ScalingGroupId': provider_feed.GROUP_ID,
                  'LifecycleState': lifecycles.get(node, 'Protected'), 'HealthStatus': 'Healthy'}]}}
             for i, node in enumerate(members)]
    group = {'ScalingGroupId': provider_feed.GROUP_ID, 'VpcId': provider_feed.VPC_ID,
             'DesiredCapacity': desired}
    ecs = [{'InstanceId': node, 'VpcAttributes': {'VpcId': provider_feed.VPC_ID,
            'PrivateIpAddress': {'IpAddress': ['10.0.0.' + str('abcd'.index(node) + 1)]}}}
           for node in sorted(set(members + backends))
           if node in backends or lifecycles.get(node) not in ('Pending', 'Pending:Wait')]
    nlb = {
        'Servers': [{'ServerId': node, 'ServerGroupId': provider_feed.SERVER_GROUP_ID,
                     'ServerType': 'Ecs', 'Port': 6432,
                     'Status': states.get(node, ('Available', 100, 'Normal'))[0],
                     'Weight': states.get(node, ('Available', 100, 'Normal'))[1]}
                    for node in backends],
        'Listener': {'ListenerId': 'fixture-listener', 'ServerGroupId': provider_feed.SERVER_GROUP_ID,
                     'ListenerPort': 6432, 'ListenerStatus': 'Running'},
        'Health': {'ListenerHealthStatus': [{'ListenerId': 'fixture-listener', 'ListenerPort': 6432,
                   'ServerGroupInfos': [{'ServerGroupId': provider_feed.SERVER_GROUP_ID,
                                        'HeathCheckEnabled': True,
                                        'NonNormalServers': [{'ServerId': node, 'Port': 6432, 'Status': state[2]}
                                                             for node, state in states.items() if state[2] != 'Normal']}]}]},
    }
    return pages, group, ecs, nlb


def client_snapshot(t, name):
    events = []
    for n in range(1, t + 1):
        events.extend([
            {'processId': name, 'sequence': n * 2 - 1, 'at': n, 'kind': 'scheduled', 'operationId': str(n)},
            {'processId': name, 'sequence': n * 2, 'at': n, 'kind': 'operation', 'operationId': str(n), 'outcome': 'success'},
        ])
    return {'sample': {'processId': name, 'at': t, 'sequence': t * 2, 'Scheduled': t,
                       'Attempts': t, 'Success': t, 'Failure': 0, 'Timeout': 0, 'Ambiguous': 0, 'Skipped': 0},
            'events': events, 'connected': 0 if t == 24 else 4, 'truncated': False, 'final': t == 24}


def capture_fixture(directory, corrupt=None):
    """Return real collected frames and target history with no external calls."""
    inventory = directory / 'provider.json'
    operator = provider_feed.Operator(SimpleNamespace())
    targets = []
    target_history = []

    def update_targets(value):
        targets[:] = value

    def worker_write(value):
        # The simulated remote boundary writes the real sanitized publication.
        provider_feed.atomic_write(inventory, value)

    operator.update_targets = update_targets
    operator.atomic_worker_write = worker_write
    frames = []
    for t in range(25):
        if t % 2 == 0:
            pages, group, ecs, nlb = provider_responses(STAGES[t // 2])
            operator.fetch_ess = lambda page: pages[page - 1]
            operator.fetch_group = lambda: group
            operator.fetch_ecs = lambda ids: [row for row in ecs if row['InstanceId'] in ids]
            operator.fetch_nlb = lambda: nlb
            with patch.object(provider_feed, 'datetime') as clock:
                clock.now.return_value = datetime.fromtimestamp(t, timezone.utc)
                operator.write(operator.read())
        target_history.append([node for node, _ in targets])
        metric_nodes = 'ab' if t < 4 else 'abcd' if t < 14 else 'bcd' if t < 22 else 'cd'
        pods = [{'metadata': {'uid': 'pod-' + name, 'name': 'client-' + name},
                 'status': {'podIP': 'client-' + name, 'phase': 'Running',
                            'conditions': [{'type': 'Ready', 'status': 'True'}],
                            'containerStatuses': [{'containerID': 'container-' + name}]}}
                for name in ('one', 'two')]

        def kubectl(*args):
            if args[1] == 'pods':
                return {'items': pods}
            if args[1] == 'deployment':
                return {'metadata': {'uid': 'client-deployment', 'generation': 1},
                        'spec': {'replicas': 2},
                        'status': {'observedGeneration': 1, 'readyReplicas': 2}}
            if args[1] == 'service':
                return {'spec': {'clusterIP': 'fixture-prometheus'}}
            raise AssertionError('unexpected Kubernetes read')

        def http_get(url):
            if url.endswith('/evidence'):
                name = 'one' if 'client-one:' in url else 'two'
                return client_snapshot(t, name)
            query = parse_qs(urlparse(url).query)['query'][0]
            if 'up{' in query:
                rows = [{'metric': {'ecs_instance_id': node}, 'value': [t, str(t)]} for node in metric_nodes]
            else:
                kind = next(k for k in ('active', 'idle', 'waiting') if 'cl_' + k in query)
                rows = []
                for node in metric_nodes:
                    for pool, user in (('test', 'poc_app'), ('other', 'other_user')):
                        busy = (node == 'a' and t < 12) or (node == 'b' and t < 20) or node in 'cd'
                        value = t if query.startswith('timestamp(') else int(busy and kind == 'active')
                        rows.append({'metric': {'ecs_instance_id': node, 'pool': pool, 'user': user}, 'value': [t, str(value)]})
            return {'status': 'success', 'data': {'resultType': 'vector', 'result': rows}}

        with patch.object(worker_capture, 'kubectl', kubectl), patch.object(worker_capture, 'get', http_get), patch.object(worker_capture, 'utc', lambda: t):
            f = worker_capture.capture_frame(inventory)
        if t == 24:
            f['phase'] = 'settling'
        if corrupt:
            corrupt(t, f)
        frames.append(f)
    return frames, target_history


def retain_fixture(directory, frames):
    path = directory / 'capture'
    writer = SegmentedCapture(path, max_segment_seconds=4, max_total_seconds=30)
    for f in frames:
        writer.append(f)
    return path, writer.finish(completed=True, cancelled=False, errors=[])


def omit_metrics(frame, node):
    frame['metrics']['nodes'] = [n for n in frame['metrics']['nodes'] if n != node]
    frame['metrics']['raw'] = [row for row in frame['metrics']['raw'] if row['node'] != node]


class TransitionIntegrationTests(unittest.TestCase):
    def test_complete_provider_collector_manifest_transition(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            frames, targets = capture_fixture(directory)
            path, result = retain_fixture(directory, frames)
            self.assertEqual(result, {'status': 'PASS', 'success': 48, 'reasons': []})
            self.assertEqual(assess_manifest(path), result)
            self.assertEqual([frames[t]['inventory']['desired'] for t in (0, 2, 10, 18, 24)], [2, 4, 3, 2, 2])
            self.assertEqual(frames[2]['inventory']['memberStates'][2],
                             {'id': 'c', 'lifecycle': 'Pending', 'health': 'Healthy', 'addressKnown': False})
            self.assertEqual(frames[0]['inventory'], frames[1]['inventory'])
            self.assertEqual(targets[0], ['a', 'b'])
            self.assertTrue(all(nodes == ['a', 'b', 'c', 'd'] for nodes in targets[4:]))
            self.assertEqual(frames[-1]['inventory']['members'], ['c', 'd'])
            manifest = json.loads((path / 'completion.json').read_text())
            self.assertEqual(len(manifest['segments']), 7)
            self.assertEqual(manifest['clientFinalMarkers'], [
                {'uid': 'pod-one', 'processId': 'one', 'final': True},
                {'uid': 'pod-two', 'processId': 'two', 'final': True}])
            for segment in manifest['segments']:
                retained = (path / segment['basename']).read_text()
                self.assertNotIn('10.0.0.', retained)
                self.assertNotIn('privateAddresses', retained)
                self.assertTrue(all(json.loads(line)['inventory']['schemaVersion'] == 2 for line in retained.splitlines()))

    def test_corrupted_provider_metrics_client_or_final_evidence_is_inconclusive(self):
        # Each mutation removes one acceptance condition from the same full run.
        cases = {
            'new backend no scrape': 'missing metrics coverage',
            'draining backend no scrape': 'missing metrics coverage',
            'old node before zero': 'missing metrics coverage',
            'other pool busy': 'missing metrics coverage',
            'incomplete triplet': 'missing expected raw triplets',
            'stale provider': 'stale provider inventory',
            'stale scrape': 'stale metrics',
            'stale metrics observation': 'stale metrics',
            'stale raw zero': 'stale raw observation',
            'feed restart': 'provider feed discontinuity',
            'sequence skip': 'provider feed discontinuity',
            'unknown backend': 'unexpected provider membership',
            'missing member state': 'incomplete provider inventory',
            'unhealthy backends': 'no healthy registered backends',
            'missing client': 'incomplete client roster',
            'missing event': 'event ring lost or duplicate events',
            'missing final': 'missing client final marker',
            'first skipped': 'scheduled work skipped',
        }
        for case, reason in cases.items():
            def corrupt(t, f):
                if case == 'new backend no scrape' and t == 4: omit_metrics(f, 'c')
                if case == 'draining backend no scrape' and t == 10: omit_metrics(f, 'a')
                if case == 'old node before zero' and t == 12: omit_metrics(f, 'a')
                if case == 'other pool busy' and t in (12, 13):
                    for row in f['metrics']['raw']:
                        if row['node'] == 'a' and row['pool'] == 'other': row['value'] = 1
                if case == 'incomplete triplet' and t == 4: f['metrics']['raw'].pop()
                if case == 'stale provider' and t in (0, 1): f['inventory']['observedAt'] = -31
                if case == 'stale scrape' and t == 4: f['metrics']['sourceAt'] = -100
                if case == 'stale metrics observation' and t == 4: f['metrics']['observedAt'] = -100
                if case == 'stale raw zero' and t in (12, 13):
                    for row in f['metrics']['raw']:
                        if row['node'] == 'a': row['sourceAt'] = -100
                if case == 'feed restart' and t >= 4: f['inventory']['feedSession'] = '00000000-0000-4000-8000-000000000002'
                if case == 'sequence skip' and t >= 4: f['inventory']['sequence'] += 1
                if case == 'unknown backend' and t in (4, 5):
                    f['inventory']['backendStates'][0]['id'] = 'foreign'
                    f['inventory']['backendStates'].sort(key=lambda row: row['id'])
                    f['inventory']['healthyRegistered'].remove('a')
                if case == 'missing member state' and t == 24: f['inventory']['memberStates'].pop()
                if case == 'unhealthy backends' and t in (4, 5):
                    f['inventory']['healthyRegistered'] = []
                    for row in f['inventory']['backendStates']: row['listenerHealth'] = 'Unhealthy'
                if case == 'missing client' and t == 4: f['pods'].pop()
                if case == 'missing event' and t == 4: f['pods'][0]['snapshot']['events'].pop()
                if case == 'missing final' and t == 24: f['pods'][0]['snapshot']['final'] = False
                if case == 'first skipped':
                    sample = f['pods'][0]['snapshot']['sample']
                    sample['Skipped'] = 1
                    sample['Scheduled'] += 1
            with self.subTest(case=case), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                frames, _ = capture_fixture(directory, corrupt)
                # Fault injection must preserve repeated publication identity;
                # otherwise continuity rejection could mask the intended gate.
                for t in range(0, 24, 2):
                    self.assertEqual(frames[t]['inventory'], frames[t + 1]['inventory'])
                _, result = retain_fixture(directory, frames)
                self.assertEqual(result['status'], 'INCONCLUSIVE', result)
                self.assertIn(reason, result['reasons'])
                if case not in ('feed restart', 'sequence skip'):
                    self.assertNotIn('provider feed discontinuity', result['reasons'])
                if case != 'missing member state':
                    self.assertNotIn('incomplete provider inventory', result['reasons'])

    def test_zero_requires_later_fresh_backend_absence(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            frames, _ = capture_fixture(directory)
            # Provider already absent at 14, but the last nonzero metric at 13
            # means zero at 14 cannot retire it using the same snapshot at 15.
            for f in frames[:14]:
                for row in f['metrics']['raw']:
                    if row['node'] == 'a': row['value'] = 1
            frames[14]['metrics']['nodes'].append('a')
            frames[14]['metrics']['raw'].extend([
                {'node': 'a', 'pool': pool, 'user': user, 'kind': kind, 'value': 0, 'sourceAt': 14}
                for pool, user in (('test', 'poc_app'), ('other', 'other_user'))
                for kind in ('active', 'idle', 'waiting')])
            _, result = retain_fixture(directory, frames)
            self.assertEqual(result['status'], 'INCONCLUSIVE', result)

    def test_missing_or_tampered_segment_never_upgrades_full_window(self):
        for corrupt in ('missing', 'tampered'):
            with self.subTest(corrupt=corrupt), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                frames, _ = capture_fixture(directory)
                path, _ = retain_fixture(directory, frames)
                segment = path / 'segment-0002.jsonl'
                if corrupt == 'missing': segment.unlink()
                else: segment.write_text('{}\n')
                self.assertEqual(assess_manifest(path)['status'], 'INCONCLUSIVE')

    def test_observed_sql_failure_overrides_missing_provider_and_segment(self):
        def corrupt(t, f):
            if t in (4, 5): f['inventory'].pop('backendStates')
            if t >= 18:
                snapshot = f['pods'][0]['snapshot']
                snapshot['sample']['Success'] -= 1
                snapshot['sample']['Failure'] = 1
                snapshot['events'][35]['outcome'] = 'failure'
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            frames, _ = capture_fixture(directory, corrupt)
            path, result = retain_fixture(directory, frames)
            self.assertEqual(result['status'], 'FAIL')
            (path / 'segment-0002.jsonl').unlink()
            self.assertEqual(assess_manifest(path)['status'], 'FAIL')


if __name__ == '__main__':
    unittest.main()
