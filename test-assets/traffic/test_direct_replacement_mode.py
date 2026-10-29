"""Offline direct 2→4→2 topology, publication and retained evidence fixtures."""
import copy
import contextlib
import io
import sys
import json
import pathlib
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import provider_feed as feed
from capture_window import TransitionEvidence, assess, clean_inventory
from segmented_capture import SegmentedCapture, assess_manifest
from test_capture_window import transition_frame
import test_provider_feed

MODE = 'full-replacement-direct-v2'


def direct_frames():
    frames = [transition_frame(0),
              transition_frame(1, desired=4),
              transition_frame(2, ('a','b','c'), ('a','b'), ('c',), desired=4),
              transition_frame(3, ('a','b','c','d'), ('a','b'), ('c','d'), desired=4),
              transition_frame(4, ('a','b','c','d')),
              transition_frame(5, ('a','b','c','d')),
              transition_frame(6, ('a','b','c','d'), ('b','c','d'), desired=2, metrics=('a','b','c','d')),
              transition_frame(7, ('b','c','d'), ('c','d'), desired=2, metrics=('c','d')),
              transition_frame(8, ('c','d')),
              transition_frame(9, ('c','d'))]
    for f in frames:
        f['inventory']['mode'] = MODE
        # Both old nodes serve traffic at ready-four. A reaches complete zero
        # at 5 and later backend absence at 6; B reaches zero at 6, absence at 7.
        for row in f['metrics']['raw']:
            if row['node'] in ('a','b') and (f['at'] == 4 or row['node'] == 'b' and f['at'] == 5):
                row['value'] = 1
        if f['at'] in (4,5,6):
            for node in ('a','b'):
                f['metrics']['raw'] += [dict(node=node, pool='other', user='other', kind=k,
                    value=1 if f['at'] == 4 or node == 'b' and f['at'] == 5 else 0, sourceAt=f['at'])
                    for k in ('active','idle','waiting')]
        if f['at'] == 5:
            for row in f['inventory']['backendStates'][:2]: row['weight'] = 0
            f['inventory']['healthyRegistered'] = ['c','d']
    frames[-1]['phase'] = 'settling'
    frames[-1]['pods'][0]['snapshot'].update(final=True, connected=0)
    return frames


def replace_inventory(frames, index, members, desired, backends=None):
    inv = transition_frame(index, members, backends, desired=desired)['inventory']
    inv['mode'] = MODE
    frames[index]['inventory'] = inv


def rollout_frames():
    """Two old clients, one surge slot, then the exact successor pair."""
    frames = direct_frames()
    def pod(uid, at, start, end):
        count = min(at, end) - start
        final = at >= end
        events = [dict(processId=uid, sequence=i+1, at=start, kind='connected') for i in range(4)]
        for n in range(1, count+1):
            events += [dict(processId=uid, sequence=len(events)+1, at=start+n,
                            kind='scheduled', operationId=f'{uid}-{n}'),
                       dict(processId=uid, sequence=len(events)+2, at=start+n,
                            kind='operation', operationId=f'{uid}-{n}', outcome='success')]
        if final:
            events += [dict(processId=uid, sequence=len(events)+i+1, at=end, kind='disconnected') for i in range(4)]
        sample = dict(processId=uid, at=at, sequence=len(events), Scheduled=count,
                      Attempts=count, Success=count, Failure=0, Timeout=0, Ambiguous=0, Skipped=0)
        return dict(uid=uid, snapshot=dict(sample=sample, events=events, final=final,
                                         connected=0 if final else 4, truncated=False))
    for f in frames:
        at = f['at']; f['expectedReplicas'] = 2
        roster = []
        if at <= 4: roster.append(pod('old-a', at, 0, 4))
        if at <= 7: roster.append(pod('old-b', at, 0, 7))
        if at >= 3: roster.append(pod('new-c', at, 3, 9))
        if at >= 6: roster.append(pod('new-d', at, 6, 9))
        f['pods'] = roster
    return frames


class DirectReplacementModeTests(unittest.TestCase):
    def test_rollout_retains_predecessor_finals_and_successor_history_across_surge(self):
        frames = rollout_frames()
        self.assertEqual([len(f['pods']) for f in frames], [2,2,2,3,3,2,3,3,2,2])
        self.assertEqual(assess(frames, completed=True)['status'], 'PASS')
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory)/'capture'
            capture = SegmentedCapture(path, max_segment_seconds=2)
            for f in frames: capture.append(f)
            self.assertEqual(capture.finish(completed=True, cancelled=False, errors=[])['status'], 'PASS')
            self.assertEqual(assess_manifest(path)['status'], 'PASS')
            rows = [json.loads(line) for p in sorted(path.glob('segment-*.jsonl')) for line in p.read_text().splitlines()]
            finals = {p['uid'] for f in rows for p in f['pods'] if p['snapshot']['final']}
            self.assertEqual(finals, {'old-a','old-b','new-c','new-d'})
            for uid in ('new-c','new-d'):
                events = [e for f in rows for p in f['pods'] if p['uid']==uid for e in p['snapshot']['events']]
                self.assertEqual([e['sequence'] for e in events], list(range(1, len(events)+1)))
                self.assertEqual([e['kind'] for e in events[:4]], ['connected']*4)

    def test_rollout_cannot_hide_missing_finals_history_or_sql_failure(self):
        for case in ('old final', 'new startup', 'new final', 'old failure', 'new failure'):
            frames = rollout_frames()
            if case == 'old final': frames[4]['pods'][0]['snapshot'].update(final=False, connected=4)
            if case == 'new startup':
                for f in frames[3:]:
                    for p in f['pods']:
                        if p['uid']=='new-c': p['snapshot']['events'] = p['snapshot']['events'][1:]
            if case == 'new final': frames[-1]['pods'][0]['snapshot'].update(final=False, connected=4)
            if case in ('old failure','new failure'):
                uid = 'old-a' if case=='old failure' else 'new-c'
                for f in frames:
                    for p in f['pods']:
                        if p['uid']==uid and p['snapshot']['sample']['Attempts']:
                            s=p['snapshot']['sample']; s['Success']-=1; s['Failure']=1
                            next(e for e in p['snapshot']['events'] if e['kind']=='operation')['outcome']='failure'
                # Corrupt coverage as well: observed SQL failure must win.
                frames[-1]['metrics']['available'] = False
            with self.subTest(case=case):
                with tempfile.TemporaryDirectory() as directory:
                    path = pathlib.Path(directory)/'capture'
                    capture = SegmentedCapture(path, max_segment_seconds=2)
                    for f in frames: capture.append(f)
                    result = capture.finish(completed=True, cancelled=False, errors=[])
                    expected = 'FAIL' if 'failure' in case else 'INCONCLUSIVE'
                    self.assertEqual(result['status'], expected, result)
                    self.assertEqual(assess_manifest(path)['status'], expected)

    def operator(self, **kwargs):
        op = feed.Operator(SimpleNamespace(mode=MODE, **kwargs))
        op.atomic_worker_write = lambda value: None
        op.update_targets = lambda targets: None
        return op

    def value(self, frame):
        value = copy.deepcopy(frame['inventory'])
        value.pop('feedSession'); value.pop('sequence')
        ids = set(value['members']) | {r['id'] for r in value['backendStates']}
        value['privateAddresses'] = {n:'10.0.0.'+str(ord(n)-ord('a')+1) for n in ids}
        return value

    def test_direct_sequence_passes_with_independent_old_retirement_and_segment_history(self):
        frames = direct_frames()
        result = assess(frames, completed=True)
        self.assertEqual(result['status'], 'PASS', result)
        history = TransitionEvidence(); issues = []
        for f in frames:
            history.assess(clean_inventory(f['inventory']), f['metrics'], f['at'], issues.append)
            if f['at'] == 5: self.assertEqual(history.retired, set())
            if f['at'] == 6: self.assertEqual(history.retired, {'a'})
            if f['at'] == 7: self.assertEqual(history.retired, {'a','b'})
        self.assertEqual(issues, [])
        self.assertEqual(history.departures, 2)
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory)/'capture'
            capture = SegmentedCapture(path, max_segment_seconds=2)
            for f in frames: capture.append(f)
            self.assertEqual(capture.finish(completed=True, cancelled=False, errors=[])['status'], 'PASS')
            self.assertEqual(assess_manifest(path)['status'], 'PASS')
            retained = [json.loads(line) for p in path.glob('segment-*.jsonl') for line in p.read_text().splitlines()]
            self.assertEqual({f['inventory']['mode'] for f in retained}, {MODE})
            self.assertNotIn('10.0.0.', json.dumps(retained))

    def test_direct_drop_allows_simultaneous_original_departures_once(self):
        frames = direct_frames()
        replace_inventory(frames, 6, ('c','d'), 2)
        replace_inventory(frames, 7, ('c','d'), 2)
        result = assess(frames, completed=True)
        self.assertEqual(result['status'], 'PASS', result)
        op = self.operator()
        for f in frames: op.write(self.value(f))
        self.assertEqual(op.allowed_departures, 2)
        self.assertEqual(op.departures_for(2), 2)
        self.assertEqual(op.sequence, 10)

    def test_capture_rejects_topology_identity_and_stream_drift(self):
        for case in ('desired three', 'no baseline', 'skipped expansion', 'incomplete expansion',
                     'extra new ID', 'changed new ID', 'lost replacement', 'early original departure',
                     'early new ID', 'original reappearance', 'second expansion', 'retained original',
                     'final old backend', 'final unhealthy replacement', 'mode change', 'mode removed',
                     'session change', 'sequence change', 'uncovered backend identity'):
            fs = direct_frames()
            if case == 'desired three': fs[6]['inventory']['desired'] = 3
            if case == 'no baseline': fs = fs[1:]
            if case == 'skipped expansion':
                for f in fs[1:6]: f['inventory']['desired'] = 2
            if case == 'incomplete expansion':
                for i in (3,4,5): replace_inventory(fs, i, ('a','b','c'), 4)
            if case == 'extra new ID': replace_inventory(fs, 3, ('a','b','d','e'), 4)
            if case == 'changed new ID': replace_inventory(fs, 5, ('a','b','c','e'), 4)
            if case == 'lost replacement': replace_inventory(fs, 7, ('a','b','c'), 2)
            if case == 'early original departure': replace_inventory(fs, 5, ('b','c','d'), 4)
            if case == 'early new ID': replace_inventory(fs, 0, ('a','b','c'), 2)
            if case == 'original reappearance': replace_inventory(fs, 8, ('a','c','d'), 2)
            if case == 'second expansion': fs[8]['inventory']['desired'] = 4
            if case == 'retained original':
                for i in (8,9): replace_inventory(fs, i, ('b','c','d'), 2, ('c','d'))
            if case == 'final old backend': replace_inventory(fs, 9, ('c','d'), 2, ('a','c','d'))
            if case == 'final unhealthy replacement':
                fs[-1]['inventory']['backendStates'][0]['listenerHealth'] = 'Unhealthy'
                fs[-1]['inventory']['healthyRegistered'] = ['d']
            if case == 'mode change': fs[5]['inventory']['mode'] = 'full-replacement'
            if case == 'mode removed': del fs[5]['inventory']['mode']
            if case == 'session change': fs[5]['inventory']['feedSession'] = '00000000-0000-4000-8000-000000000002'
            if case == 'sequence change': fs[5]['inventory']['sequence'] += 1
            if case == 'uncovered backend identity':
                fs[5]['inventory']['backendStates'][0]['id'] = 'e'
                fs[5]['inventory']['backendStates'].sort(key=lambda row:row['id'])
            with self.subTest(case=case):
                self.assertEqual(assess(fs, completed=True)['status'], 'INCONCLUSIVE')

    def test_old_nodes_keep_metrics_obligations_until_each_complete_zero_and_later_absence(self):
        for case in ('missing old metrics', 'missing replacement metrics', 'stale scrape', 'stale raw',
                     'missing other pool idle', 'nonzero other pool', 'no old zero', 'same publication',
                     'later nonzero', 'stale provider', 'unfinished', 'sql failure'):
            fs = direct_frames()
            if case in ('missing old metrics','missing replacement metrics'):
                node = 'b' if case == 'missing old metrics' else 'c'
                fs[4]['metrics']['nodes'].remove(node)
                fs[4]['metrics']['raw'] = [r for r in fs[4]['metrics']['raw'] if r['node'] != node]
            if case == 'stale scrape': fs[6]['metrics']['sourceAt'] = -100
            if case == 'stale raw':
                for r in fs[6]['metrics']['raw']:
                    if r['node'] == 'b': r['sourceAt'] = -100
            if case == 'missing other pool idle':
                fs[6]['metrics']['raw'] = [r for r in fs[6]['metrics']['raw'] if (r['node'],r['pool'],r['kind']) != ('b','other','idle')]
            if case == 'nonzero other pool':
                fs[6]['metrics']['raw'][-1]['value'] = 1
            if case == 'no old zero':
                for f in fs[5:7]:
                    for r in f['metrics']['raw']:
                        if r['node'] == 'b': r['value'] = 1
            if case == 'same publication':
                fs[6]['inventory']['backendStates'] = copy.deepcopy(fs[7]['inventory']['backendStates'])
                fs[6]['inventory']['healthyRegistered'] = ['c','d']
                fs[7]['inventory'] = copy.deepcopy(fs[6]['inventory'])
                for r in fs[7]['metrics']['raw']: r['sourceAt'] = 7
                fs[8]['inventory']['sequence'] = 8
                fs[9]['inventory']['sequence'] = 9
            if case == 'later nonzero':
                fs[7]['metrics']['nodes'].append('b')
                fs[7]['metrics']['raw'] += [dict(node='b', pool='test', user='poc_app', kind=k, value=1, sourceAt=7) for k in ('active','idle','waiting')]
            if case == 'stale provider': fs[6]['inventory']['observedAt'] = -100
            if case == 'sql failure': fs[6]['pods'][0]['snapshot']['sample']['Failure'] = 1
            with self.subTest(case=case):
                self.assertEqual(assess(fs, completed=case != 'unfinished')['status'],
                                 'FAIL' if case == 'sql failure' else 'INCONCLUSIVE')

    def test_direct_completion_requires_both_old_retirement_proofs_even_with_full_metrics(self):
        for case in ('neither retired','only one retired','later nonzero'):
            fs = direct_frames()
            start = 9 if case == 'later nonzero' else 5
            nodes = ('a','b') if case == 'neither retired' else ('b',)
            for f in fs[start:]:
                for node in nodes:
                    if node not in f['metrics']['nodes']:
                        f['metrics']['nodes'].append(node)
                        f['metrics']['raw'] += [dict(node=node, pool='test', user='poc_app',
                            kind=k, value=1, sourceAt=f['at']) for k in ('active','idle','waiting')]
                for row in f['metrics']['raw']:
                    if row['node'] in nodes: row['value'] = 1
            with self.subTest(case=case):
                result = assess(fs, completed=True)
                self.assertEqual(result['status'], 'INCONCLUSIVE', result)
                self.assertNotIn('missing metrics coverage', result['reasons'])

    def test_producer_emits_sanitizable_mode_and_retains_all_targets_after_departures(self):
        op = self.operator(); publications = []; updates = []
        op.atomic_worker_write = publications.append; op.update_targets = updates.append
        for f in direct_frames(): op.write(self.value(f))
        self.assertEqual(op.allowed_departures, 2)
        self.assertEqual(op.retained_targets, (('a','10.0.0.1'),('b','10.0.0.2'),('c','10.0.0.3'),('d','10.0.0.4')))
        self.assertEqual(updates[-1], op.retained_targets)
        self.assertEqual(publications[-1]['members'], ['c','d'])
        self.assertEqual({p.get('mode') for p in publications}, {MODE})
        for i, p in enumerate(publications):
            self.assertEqual(p['sequence'], i+1)
            self.assertEqual(p['feedSession'], op.feedSession)
            self.assertEqual(clean_inventory(p), p)
        self.assertNotIn('10.0.0.', json.dumps(publications))

    def test_producer_refuses_bad_topology_before_targets_or_inventory_change(self):
        for case in ('desired three', 'lost replacement', 'original reappearance', 'extra new ID',
                     'early original departure', 'changed mode', 'changed session', 'changed sequence'):
            fs = direct_frames()
            if case == 'desired three': fs[6]['inventory']['desired'] = 3
            if case == 'lost replacement': replace_inventory(fs, 7, ('a','b','c'), 2)
            if case == 'original reappearance': replace_inventory(fs, 8, ('a','c','d'), 2)
            if case == 'extra new ID': replace_inventory(fs, 5, ('a','b','c','e'), 4)
            if case == 'early original departure': replace_inventory(fs, 5, ('b','c','d'), 4)
            if case == 'changed mode': fs[5]['inventory']['mode'] = 'full-replacement'
            op = self.operator(); effects = []
            op.atomic_worker_write = lambda value: effects.append('inventory')
            op.update_targets = lambda targets: effects.append('targets')
            refused = False
            for i, f in enumerate(fs):
                value = self.value(f)
                if case == 'extra new ID' and i == 5: value['privateAddresses'] = {}
                if case == 'changed session' and i == 5: value['feedSession'] = '00000000-0000-4000-8000-000000000002'
                if case == 'changed sequence' and i == 5: value['sequence'] = 999
                before = list(effects)
                try: op.write(value)
                except feed.FeedStop:
                    self.assertEqual(effects, before); refused = True; break
            with self.subTest(case=case): self.assertTrue(refused, case)

    def attach_authoritative_fixture(self, op, frame):
        source = frame['inventory']; mapping = {n:'i-'+str(i) for i,n in enumerate('abcd')}
        fixture = test_provider_feed.FeedTests().fixture(4)
        pages, group, ecs, nlb = fixture
        members = {mapping[n] for n in source['members']}
        pages[:] = [page for page in pages if page['ScalingInstances']['ScalingInstance'][0]['InstanceId'] in members]
        group.update(ScalingGroupId=feed.GROUP_ID, DesiredCapacity=source['desired'])
        states = {mapping[row['id']]:row for row in source['memberStates']}
        for i,page in enumerate(pages):
            page.update(TotalCount=len(pages), PageNumber=i+1)
            row = page['ScalingInstances']['ScalingInstance'][0]
            row.update(ScalingGroupId=feed.GROUP_ID, LifecycleState=states[row['InstanceId']]['lifecycle'],
                       HealthStatus=states[row['InstanceId']]['health'])
        backends = {mapping[r['id']]:r for r in source['backendStates']}
        nlb['Servers'][:] = [row for row in nlb['Servers'] if row['ServerId'] in backends]
        for row in nlb['Servers']:
            row.update(ServerGroupId=feed.SERVER_GROUP_ID,
                       Status=backends[row['ServerId']]['status'], Weight=backends[row['ServerId']]['weight'])
        nlb['Listener']['ServerGroupId'] = feed.SERVER_GROUP_ID
        nlb['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['ServerGroupId'] = feed.SERVER_GROUP_ID
        op.fetch_group = lambda: group
        op.fetch_ess = lambda page: pages[page-1]
        op.fetch_ecs = lambda ids: [ecs[n] for n in ids if n in ecs]
        op.fetch_nlb = lambda: nlb

    def test_authoritative_read_write_and_capture_agree_through_membership_lag(self):
        op = self.operator(); publications = []; frames = direct_frames()
        op.atomic_worker_write = publications.append
        for f in frames:
            self.attach_authoritative_fixture(op, f)
            value = op.read(); value['observedAt'] = f['at']; op.write(value)
            f['inventory'] = publications[-1]
            f['metrics']['nodes'] = ['i-'+str('abcd'.index(n)) for n in f['metrics']['nodes']]
            for row in f['metrics']['raw']: row['node'] = 'i-'+str('abcd'.index(row['node']))
        self.assertEqual([p['desired'] for p in publications], [2,4,4,4,4,4,2,2,2,2])
        self.assertEqual([len(p['members']) for p in publications], [2,2,3,4,4,4,4,3,2,2])
        self.assertEqual(publications[-1]['members'], ['i-2','i-3'])
        self.assertEqual(op.allowed_departures, 2)
        self.assertEqual(len(op.retained_targets), 4)
        result = assess(frames, completed=True)
        self.assertEqual(result['status'], 'PASS', result)

    def test_authoritative_wrong_replacement_departure_cannot_publish(self):
        op = self.operator(); publications = []; updates = []
        op.atomic_worker_write = publications.append; op.update_targets = updates.append
        frames = direct_frames()
        for f in frames[:7]:
            self.attach_authoritative_fixture(op, f)
            value = op.read(); value['observedAt'] = f['at']; op.write(value)
        replace_inventory(frames, 7, ('a','b','c'), 2)
        self.attach_authoritative_fixture(op, frames[7])
        before = (copy.deepcopy(publications), copy.deepcopy(updates), op.sequence)
        value = op.read()
        with self.assertRaises(feed.FeedStop): op.write(value)
        self.assertEqual((publications,updates,op.sequence), before)

    def test_legacy_modes_do_not_acquire_direct_departure_allowance(self):
        for mode in (None,'full-replacement'):
            fs = direct_frames()
            for f in fs:
                if mode is None: f['inventory'].pop('mode')
                else: f['inventory']['mode'] = mode
            with self.subTest(mode=mode):
                result = assess(fs, completed=True)
                self.assertEqual(result['status'], 'INCONCLUSIVE', result)
                self.assertIn('unexpected provider membership', result['reasons'])
        # Legacy full replacement still accepts its historical staged removal.
        fs = direct_frames()
        for f in fs: f['inventory'].pop('mode')
        fs[6]['inventory']['desired'] = 3
        fs[7]['inventory']['desired'] = 3
        result = assess(fs, completed=True)
        self.assertEqual(result['status'], 'PASS', result)

    def test_cli_recognizes_direct_mode_but_refuses_runtime_context_before_provider_access(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory); root.chmod(0o700)
            args = ['provider_feed.py','--mode',MODE,'--workbench','/offline/workbench',
                    '--worker-inventory','/offline/run/provider-inventory.json',
                    '--run-context',str(root/'run-context.json'),'--contract-dir','/offline/contract',
                    '--status-file',str(root/'status.json'),'--kubeconfig','/offline/kubeconfig']
            effects = []
            with patch.object(sys,'argv',args), patch.object(feed,'_require_verified'), \
                    patch.object(feed.Operator,'command',side_effect=lambda *a: effects.append(a)), \
                    contextlib.redirect_stderr(io.StringIO()) as errors:
                with self.assertRaises(SystemExit) as error: feed.main()
            self.assertEqual(error.exception.code, 1, errors.getvalue())
            self.assertIn('startup', errors.getvalue())
            self.assertNotIn('invalid choice', errors.getvalue())
            self.assertEqual(effects, [])
            self.assertFalse((root/'status.json').exists())

    def test_direct_mode_does_not_open_native_run_context_or_no_scale_admission(self):
        with self.assertRaises(ValueError): self.operator(run_context='/private/context')
        with self.assertRaises(feed.FeedStop): self.operator(no_scale_target='{}')


if __name__ == '__main__': unittest.main()
