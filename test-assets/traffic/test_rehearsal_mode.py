"""Offline 2→3→2 feed, capture and private archive regressions."""
import copy
import json
import pathlib
import subprocess
import sys
import tempfile
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import provider_feed as feed
from capture_window import assess
from segmented_capture import SegmentedCapture, assess_manifest
from test_capture_window import transition_frame
import test_provider_feed

MODE = 'disabled-rehearsal'


def rehearsal_frames():
    frames = [transition_frame(0),
              transition_frame(1, ('a', 'b'), desired=3),
              transition_frame(2, ('a', 'b', 'c'), ('a', 'b'), ('c',)),
              transition_frame(3, ('a', 'b', 'c')),
              transition_frame(4, ('a', 'b', 'c'), ('a', 'b'), metrics=('a','b','c')),
              transition_frame(5, ('a', 'b'), metrics=('a', 'b', 'c')),
              transition_frame(6), transition_frame(7)]
    for f in frames:
        f['inventory']['mode'] = MODE
    frames[-1]['phase'] = 'settling'
    frames[-1]['pods'][0]['snapshot'].update(final=True, connected=0)
    return frames


def lingering_drained_member_frames():
    """Exporter disappears after zero/absence proof, before ESS departure."""
    frames = [transition_frame(0),
              transition_frame(1, desired=3),
              transition_frame(2, ('a','b','c'), ('a','b'), ('c',)),
              transition_frame(3, ('a','b','c')),
              transition_frame(4, ('a','b','c'), desired=2),
              transition_frame(5, ('a','b','c'), ('a','b'), metrics=('a','b','c'), desired=2),
              transition_frame(6, ('a','b','c'), ('a','b'), metrics=('a','b'), desired=2),
              transition_frame(7), transition_frame(8)]
    for f in frames:
        f['inventory']['mode'] = MODE
        for row in f['metrics']['raw']:
            if row['node'] == 'c' and f['at'] == 3:
                row['value'] = 1
        if f['at'] in (4, 5):
            f['metrics']['raw'] += [dict(node='c', pool='other', user='other', kind=k,
                                         value=0, sourceAt=f['at'])
                                    for k in ('active','idle','waiting')]
        if f['at'] in (5, 6):
            f['inventory']['memberStates'][2]['lifecycle'] = 'Removing'
    frames[-1]['phase'] = 'settling'
    frames[-1]['pods'][0]['snapshot'].update(final=True, connected=0)
    return frames


def no_restart_rehearsal_frames():
    """Two unchanged clients, two distinct zero scrapes, then one 3→2 return."""
    frames = lingering_drained_member_frames()
    for f in frames:
        if f['at'] in (4, 5):
            f['inventory']['desired'] = 3
        if f['at'] == 4:
            f['inventory']['backendStates'].pop()
            f['inventory']['healthyRegistered'].remove('c')
        other = copy.deepcopy(f['pods'][0])
        other['uid'] = 'second-pod'
        other['snapshot']['sample']['processId'] = 'second-process'
        for event in other['snapshot']['events']:
            event['processId'] = 'second-process'
        f['pods'].append(other)
        f['expectedReplicas'] = 2
    return frames


def configmap_remote(body, calls, corrupt_readback=False):
    """Execute the real target script against an in-memory kubectl boundary."""
    def remote(script, *arguments):
        def kubectl(argv, **kwargs):
            calls.append(argv)
            if argv[5] == 'patch':
                value = json.loads(argv[-1])
                assert value['metadata']['resourceVersion'] == body['metadata']['resourceVersion']
                body['data'].update(value['data'])
                body['metadata']['resourceVersion'] = str(int(body['metadata']['resourceVersion']) + 1)
                return SimpleNamespace(stdout='')
            value = copy.deepcopy(body)
            if corrupt_readback and len(calls) > 1 and calls[-2][5] == 'patch':
                rows = json.loads(value['data']['targets.json'])
                value['data']['targets.json'] = json.dumps(rows[:1])
            return SimpleNamespace(stdout=json.dumps(value))
        with patch.object(sys, 'argv', ['target-script'] + list(arguments)), \
                patch.object(subprocess, 'run', side_effect=kubectl):
            exec(script, {'_remote_deadline': time.monotonic() + 15})
    return remote


class RehearsalModeTests(unittest.TestCase):
    def test_complete_rehearsal_survives_segmentation_and_archive_reassessment(self):
        with tempfile.TemporaryDirectory() as d:
            path = pathlib.Path(d) / 'capture'
            capture = SegmentedCapture(path, max_segment_seconds=2)
            for f in rehearsal_frames():
                capture.append(f)
            self.assertEqual(capture.finish(completed=True, cancelled=False, errors=[])['status'], 'PASS')
            self.assertEqual(assess_manifest(path)['status'], 'PASS')
            retained = [json.loads(line) for p in path.glob('segment-*.jsonl') for line in p.read_text().splitlines()]
            self.assertEqual({f['inventory'].get('mode') for f in retained}, {MODE})

    def test_two_client_no_restart_window_preserves_continuous_success(self):
        fs = no_restart_rehearsal_frames()
        identities = {(p['uid'], p['snapshot']['sample']['processId']) for p in fs[0]['pods']}
        self.assertEqual(len(identities), 2)
        for f in fs:
            self.assertEqual({(p['uid'], p['snapshot']['sample']['processId']) for p in f['pods']}, identities)
            self.assertEqual(f['expectedReplicas'], 2)
        self.assertEqual({r['sourceAt'] for f in fs[4:6] for r in f['metrics']['raw'] if r['node'] == 'c'}, {4, 5})
        result = assess(fs, completed=True)
        self.assertEqual(result['status'], 'PASS', result)
        self.assertEqual(result['success'], 16)

    def test_no_restart_window_rejects_lost_client_and_unproved_drain(self):
        # These are real assessor failures, not a new operator-gate API. The
        # operator must additionally reject even a fully finalized client swap.
        for case in ('changed client', 'nonzero drain', 'stale drain'):
            fs = no_restart_rehearsal_frames()
            if case == 'changed client':
                for f in fs[6:]:
                    snap = f['pods'][1]['snapshot']
                    snap['sample']['processId'] = 'changed-process'
                    for event in snap['events']: event['processId'] = 'changed-process'
            else:
                for f in fs[4:6]:
                    for row in f['metrics']['raw']:
                        if row['node'] == 'c':
                            if case == 'nonzero drain': row['value'] = 1
                            else: row['sourceAt'] = -100
            with self.subTest(case=case):
                result = assess(fs, completed=True)
                self.assertEqual(result['status'], 'INCONCLUSIVE', result)
                self.assertIn('old process lost without final counters' if case == 'changed client'
                              else 'missing metrics coverage', result['reasons'])

    def test_zero_then_later_absence_retires_lingering_ess_member_metrics(self):
        result = assess(lingering_drained_member_frames(), completed=True)
        self.assertEqual(result['status'], 'PASS', result)

    def test_lingering_member_requires_complete_fresh_zero_then_later_absence(self):
        for case in ('incomplete zero', 'stale zero', 'stale scrape', 'no zero',
                     'missing zero', 'same provider', 'still registered',
                     'later nonzero', 're-registration'):
            fs = lingering_drained_member_frames()
            if case in ('incomplete zero', 'stale zero', 'stale scrape', 'no zero', 'missing zero'):
                for f in fs[4:6]:
                    if case == 'incomplete zero':
                        f['metrics']['raw'] = [r for r in f['metrics']['raw']
                                               if not (r['node'] == 'c' and r['pool'] == 'other' and r['kind'] == 'idle')]
                    if case == 'stale zero':
                        for row in f['metrics']['raw']:
                            if row['node'] == 'c': row['sourceAt'] = -100
                    if case == 'stale scrape': f['metrics']['sourceAt'] = -100
                    if case == 'no zero':
                        for row in f['metrics']['raw']:
                            if row['node'] == 'c': row['value'] = 1
                    if case == 'missing zero':
                        f['metrics']['nodes'].remove('c')
                        f['metrics']['raw'] = [r for r in f['metrics']['raw'] if r['node'] != 'c']
            if case == 'same provider':
                fs[4]['inventory']['backendStates'].pop()
                fs[4]['inventory']['healthyRegistered'].remove('c')
                for f in fs[5:7]: f['inventory'] = copy.deepcopy(fs[4]['inventory'])
                fs[7]['inventory']['sequence'] = 6
                fs[8]['inventory']['sequence'] = 7
            if case in ('still registered', 're-registration'):
                for f in fs[5:7] if case == 'still registered' else fs[6:7]:
                    f['inventory']['backendStates'].append(dict(id='c', status='Removing', weight=0, listenerHealth='Unhealthy'))
            if case == 'later nonzero':
                for row in fs[5]['metrics']['raw']:
                    if row['node'] == 'c': row['value'] = 1
            with self.subTest(case=case):
                result = assess(fs, completed=True)
                self.assertEqual(result['status'], 'INCONCLUSIVE', result)
                self.assertIn('missing metrics coverage', result['reasons'])

    def test_clock_skew_cannot_make_same_publication_retire_member(self):
        fs = lingering_drained_member_frames()
        inv = fs[4]['inventory']
        inv['backendStates'].pop()
        inv['healthyRegistered'].remove('c')
        # One-second provider clock lead is permitted freshness skew. This is
        # still the publication already present when the zero was observed.
        inv['observedAt'] = 5
        for f in fs[5:7]: f['inventory'] = copy.deepcopy(inv)
        fs[7]['inventory']['sequence'] = 6
        fs[8]['inventory']['sequence'] = 7
        result = assess(fs, completed=True)
        self.assertEqual(result['status'], 'INCONCLUSIVE', result)
        self.assertEqual(result['reasons'], ['missing metrics coverage'])

    def test_later_publication_can_retire_member_despite_provider_clock_lead(self):
        fs = lingering_drained_member_frames()
        for f in fs[4:]: f['inventory']['observedAt'] = f['at'] + 1
        result = assess(fs, completed=True)
        self.assertEqual(result['status'], 'PASS', result)

    def test_retirement_does_not_hide_sql_failure_or_missing_completion(self):
        for case in ('SQL failure', 'missing completion'):
            fs = lingering_drained_member_frames()
            if case == 'SQL failure': fs[6]['pods'][0]['snapshot']['sample']['Failure'] = 1
            with self.subTest(case=case):
                self.assertEqual(assess(fs, completed=case != 'missing completion')['status'],
                                 'FAIL' if case == 'SQL failure' else 'INCONCLUSIVE')

    def test_no_scale_capture_cannot_be_rehearsal_pass(self):
        fs = rehearsal_frames()[:1] + [transition_frame(1)]
        for f in fs:
            f['inventory']['mode'] = MODE
        self.assertEqual(assess(fs, completed=True)['status'], 'INCONCLUSIVE')

    def test_rehearsal_rejects_wrong_mode_identity_order_and_incomplete_return(self):
        for case in ('mode change', 'mode removed', 'unknown mode', 'session change', 'skip sequence',
                     'four desired', 'four members', 'original departure', 'changed new id',
                     'missing expansion', 'new gone before decrease', 'reexpand', 'unfinished'):
            fs = rehearsal_frames()
            if case == 'mode change': fs[3]['inventory']['mode'] = 'full-replacement'
            if case == 'mode removed': del fs[3]['inventory']['mode']
            if case == 'unknown mode': fs[0]['inventory']['mode'] = 'other'
            if case == 'session change': fs[3]['inventory']['feedSession'] = '00000000-0000-4000-8000-000000000002'
            if case == 'skip sequence': fs[3]['inventory']['sequence'] += 1
            if case == 'four desired': fs[1]['inventory']['desired'] = 4
            if case == 'four members': fs[2] = transition_frame(2, ('a','b','c','d'), desired=3); fs[2]['inventory']['mode'] = MODE
            if case == 'original departure':
                for f in fs[5:]:
                    f['inventory'] = transition_frame(f['at'], ('b','c'))['inventory']; f['inventory']['mode'] = MODE
            if case == 'changed new id':
                fs[3]['inventory'] = transition_frame(3, ('a','b','d'))['inventory']; fs[3]['inventory']['mode'] = MODE
            if case == 'missing expansion': fs[1]['inventory']['desired'] = 2; fs[2]['inventory']['desired'] = 2
            if case == 'new gone before decrease': fs[5]['inventory']['desired'] = 3
            if case == 'reexpand': fs[-1]['inventory']['desired'] = 3
            if case == 'unfinished': fs = fs[:5]
            with self.subTest(case=case):
                self.assertEqual(assess(fs, completed=True)['status'], 'INCONCLUSIVE')

    def test_strict_metrics_sql_and_completion_are_not_relaxed(self):
        for case in ('registered gap', 'stale source', 'raw gap', 'sql failure', 'no completion'):
            fs = rehearsal_frames()
            if case == 'registered gap': fs[3]['metrics']['nodes'].remove('c')
            if case == 'stale source': fs[3]['metrics']['sourceAt'] = -100
            if case == 'raw gap': fs[3]['metrics']['raw'].pop()
            if case == 'sql failure': fs[3]['pods'][0]['snapshot']['sample']['Failure'] = 1
            with self.subTest(case=case):
                self.assertEqual(assess(fs, completed=case != 'no completion')['status'], 'FAIL' if case == 'sql failure' else 'INCONCLUSIVE')

    def operator(self, mode=MODE):
        op = feed.Operator(SimpleNamespace(mode=mode))
        op.atomic_worker_write = lambda value: None
        op.update_targets = lambda value: None
        return op

    def value(self, count, desired=None):
        fixture = test_provider_feed.FeedTests().fixture(count)
        if desired is not None: fixture[1]['DesiredCapacity'] = desired
        return test_provider_feed.FeedTests().take(fixture)

    def test_provider_rejects_bad_rehearsal_before_publication(self):
        for case in ('start three', 'skip four', 'wrong original', 'changed new', 'early departure', 'repeat expansion', 'mode change'):
            op = self.operator(); writes = []; op.atomic_worker_write = writes.append
            if case == 'start three':
                with self.assertRaises(feed.FeedStop): op.write(self.value(3))
                self.assertEqual(writes, []); continue
            op.write(self.value(2))
            if case == 'skip four': bad = self.value(4)
            elif case == 'mode change': bad = dict(self.value(3), mode='full-replacement')
            else:
                op.write(self.value(3))
                bad = self.value(2)
                if case == 'wrong original':
                    bad['members'] = ['i-1','i-2']
                if case == 'changed new': bad = self.value(3); bad['members'][-1] = 'i-other'
                if case == 'early departure': bad['desired'] = 3
                if case == 'repeat expansion': op.write(self.value(2)); bad = self.value(3)
            count = len(writes)
            with self.subTest(case=case), self.assertRaises(feed.FeedStop): op.write(bad)
            self.assertEqual(len(writes), count)

    def test_provider_feed_and_capture_continue_through_final_target_readback(self):
        with tempfile.TemporaryDirectory() as d:
            root = pathlib.Path(d); inv_path = root / 'inventory.json'
            kube = root / 'kubeconfig'; kube.write_text('synthetic'); kube.chmod(0o600)
            status = root / 'status.json'
            op = feed.Operator(SimpleNamespace(mode=MODE, kubeconfig=str(kube), status_file=str(status)))
            op.atomic_worker_write = lambda value: feed.atomic_write(inv_path, value)
            body = {'metadata': {'name': 'pgcat-provider-inventory', 'namespace': 'raptor-test', 'resourceVersion': '17'},
                    'data': {'targets.json': '[]'}}
            calls = []
            op.remote = configmap_remote(body, calls)
            def target_ids():
                return [r['labels']['ecs_instance_id'] for r in json.loads(body['data']['targets.json'])]
            capture = SegmentedCapture(root / 'capture', max_segment_seconds=2)
            for t, count in enumerate((2, 3, 3, 2, 2, 2)):
                fixture = test_provider_feed.FeedTests().fixture(count)
                fixture[1]['ScalingGroupId'] = feed.GROUP_ID
                for p in fixture[0]: p['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId'] = feed.GROUP_ID
                for r in fixture[3]['Servers']: r['ServerGroupId'] = feed.SERVER_GROUP_ID
                fixture[3]['Listener']['ServerGroupId'] = feed.SERVER_GROUP_ID
                fixture[3]['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['ServerGroupId'] = feed.SERVER_GROUP_ID
                op.fetch_group = lambda: fixture[1]
                op.fetch_ess = lambda page: fixture[0][page-1]
                op.fetch_ecs = lambda ids: [fixture[2][n] for n in ids if n in fixture[2]]
                op.fetch_nlb = lambda: fixture[3]
                try:
                    value = op.read(); value['observedAt'] = t; op.write(value)
                except feed.FeedStop as error:
                    self.fail('Valid 2→3→2 feed stopped at tick {}: {}'.format(t, error))
                if t == 4:
                    # Execute the real resourceVersion patch/readback before the
                    # explicit final two-target worker publication.
                    feed.publish(dict(value, **json.loads(inv_path.read_text())), op.previous,
                                 op.atomic_worker_write, op.update_targets)
                    self.assertEqual(target_ids(), ['i-0', 'i-1'])
                metrics = ('i-0','i-1','i-2') if t in (1,2,3) else ('i-0','i-1')
                frame = transition_frame(t, ('i-0','i-1'), metrics=metrics)
                frame['inventory'] = json.loads(inv_path.read_text())
                if t == 5:
                    self.assertEqual(target_ids(), ['i-0', 'i-1'])
                    receipt = json.loads(status.read_text())
                    self.assertEqual(receipt['feedSession'], frame['inventory']['feedSession'])
                    self.assertEqual(receipt['sequence'], frame['inventory']['sequence'])
                    self.assertGreater(receipt['sequence'], 5)
                    self.assertEqual(receipt['observedAt'], t)
                    frame['phase'] = 'settling'; frame['pods'][0]['snapshot'].update(final=True, connected=0)
                capture.append(frame)
            self.assertEqual(capture.finish(completed=True, cancelled=False, errors=[])['status'], 'PASS')
            self.assertEqual(assess_manifest(root / 'capture')['status'], 'PASS')
            self.assertEqual(op.sequence, 6)

    def test_incomplete_final_target_readback_cannot_publish_closeout(self):
        with tempfile.TemporaryDirectory() as d:
            root = pathlib.Path(d); status = root / 'status.json'
            kube = root / 'kubeconfig'; kube.write_text('synthetic'); kube.chmod(0o600)
            op = feed.Operator(SimpleNamespace(mode=MODE, kubeconfig=str(kube), status_file=str(status)))
            writes = []; op.atomic_worker_write = writes.append
            body = {'metadata': {'name': 'pgcat-provider-inventory', 'namespace': 'raptor-test', 'resourceVersion': '17'},
                    'data': {'targets.json': '[]'}}
            calls = []; op.remote = configmap_remote(body, calls)
            for count in (2, 3, 2): op.write(self.value(count))
            # Feed target history deliberately retains the third node. Explicit
            # final reconciliation must verify exactly two, independently of
            # the local success receipt and desired-two worker inventory.
            self.assertEqual(len(json.loads(body['data']['targets.json'])), 3)
            before = status.read_bytes(); published = len(writes)
            op.remote = configmap_remote(body, calls, corrupt_readback=True)
            with self.assertRaises(AssertionError):
                feed.publish(dict(writes[-1], privateAddresses=self.value(2)['privateAddresses']),
                             op.previous, op.atomic_worker_write, op.update_targets)
            self.assertEqual(len(writes), published)
            self.assertEqual(status.read_bytes(), before)
