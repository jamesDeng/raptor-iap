"""Offline 2→3→2 feed, capture and private archive regressions."""
import copy
import json
import pathlib
import tempfile
import unittest
from types import SimpleNamespace

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
            root = pathlib.Path(d); inv_path = root / 'inventory.json'; target_state = []
            op = self.operator()
            op.atomic_worker_write = lambda value: feed.atomic_write(inv_path, value)
            op.update_targets = lambda targets: target_state.__setitem__(slice(None), targets)
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
                    # Explicit reviewed cleanup uses the real publication routine;
                    # target sink/readback stands in for the external ConfigMap.
                    feed.publish(dict(value, **json.loads(inv_path.read_text())), op.previous,
                                 op.atomic_worker_write, op.update_targets)
                    self.assertEqual(target_state, [('i-0','10.0.0.1'), ('i-1','10.0.0.2')])
                metrics = ('i-0','i-1','i-2') if t in (1,2,3) else ('i-0','i-1')
                frame = transition_frame(t, ('i-0','i-1'), metrics=metrics)
                frame['inventory'] = json.loads(inv_path.read_text())
                if t == 5:
                    self.assertEqual(target_state, [('i-0','10.0.0.1'), ('i-1','10.0.0.2')])
                    frame['phase'] = 'settling'; frame['pods'][0]['snapshot'].update(final=True, connected=0)
                capture.append(frame)
            self.assertEqual(capture.finish(completed=True, cancelled=False, errors=[])['status'], 'PASS')
            self.assertEqual(assess_manifest(root / 'capture')['status'], 'PASS')
            self.assertEqual(op.sequence, 6)
