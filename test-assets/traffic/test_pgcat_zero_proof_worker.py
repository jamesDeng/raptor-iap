"""Offline, worker-only no-restart zero-gate regressions."""
import copy
import json
import unittest

import pgcat_pending_proof_worker as pending
import test_pgcat_pre_drain_proof_worker as pre_fixture


class ZeroProofWorkerTests(unittest.TestCase):
    def setUp(self):
        fixture = pre_fixture.PreDrainProofWorkerTests('test_proof_is_one_use')
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        self.f = fixture.fixture
        self.f.cm['metadata']['resourceVersion'] = '12'
        self.binding = fixture.produce()
        # The NLB removal has been acknowledged. The existing provider feed
        # publishes Removing, then absence, then two separate zero scrapes,
        # then a later absence observation.
        fixture.append_provider_frame(8, True)
        frame = self.f.frames[-1]
        frame['inventory']['backendStates'][-1] = dict(
            id='c', status='Removing', weight=0, listenerHealth='Unavailable')
        frame['inventory']['healthyRegistered'] = ['a', 'b']
        fixture.persist_frames()
        for sequence in (9, 10, 11, 12):
            fixture.append_provider_frame(sequence, False)
            frame = self.f.frames[-1]
            at = frame['at']
            frame['metrics']['upSourceAt'] = {node: at - .2 for node in ('a', 'b', 'c')}
            for row in frame['metrics']['raw']:
                if row['node'] == 'c':
                    row['value'] = (1 if sequence == 9 and
                                    row['kind'] == 'active' else 0)
            fixture.persist_frames()

    def produce(self):
        import pgcat_zero_proof_worker as zero
        f = self.f
        return zero.produce(f.contract, f.run, self.binding,
            kubectl=f.kubectl, live_roster=lambda: copy.deepcopy(f.roster),
            now=lambda: f.now)

    def assert_refused(self):
        with self.assertRaises(pending.Refused):
            self.produce()
        self.assertFalse((self.f.contract / 'zero-gate.json').exists())

    def append_zero_tail(self, last_sequence=62):
        for sequence in range(13, last_sequence + 1):
            self.fixture.append_provider_frame(sequence, False)
            frame = self.f.frames[-1]
            frame['metrics']['upSourceAt'] = {
                node: frame['at'] - .2 for node in ('a', 'b', 'c')}
            self.fixture.persist_frames()

    def test_distinct_zero_scrapes_and_later_absence_seal_address_free_proof(self):
        result = self.produce()
        self.assertEqual(result['newId'], 'c')
        self.assertEqual(result['sequence'], 12)
        self.assertNotIn('10.0.0.', json.dumps(result))
        self.assertTrue((self.f.contract / 'zero-gate.json').exists())

    def test_long_capture_selects_fresh_distinct_pair(self):
        self.append_zero_tail()
        result = self.produce()
        self.assertEqual(result['sequence'], 62)

    def test_old_post_zero_nonzero_refuses_even_with_later_fresh_zeros(self):
        self.f.frames[-1]['metrics']['raw'][-1]['value'] = 1
        self.fixture.persist_frames()
        self.append_zero_tail()
        self.assert_refused()

    def test_bounded_removing_weight_100_normal_is_allowed(self):
        frame = next(frame for frame in self.f.frames if
                     frame['inventory']['sequence'] == 8)
        row = next(row for row in frame['inventory']['backendStates'] if
                   row['id'] == 'c')
        row.update(weight=100, listenerHealth='Normal')
        self.fixture.persist_frames()
        self.produce()

    def test_same_scrape_replayed_under_new_collector_frame_refuses(self):
        first = self.f.frames[-3]['metrics']
        second = self.f.frames[-2]['metrics']
        second['sourceAt'] = first['sourceAt']
        second['upSourceAt'] = copy.deepcopy(first['upSourceAt'])
        second['raw'] = copy.deepcopy(first['raw'])
        self.fixture.persist_frames()
        self.assert_refused()

    def test_aggregate_min_advances_but_candidate_up_does_not_refuses(self):
        first = self.f.frames[-3]['metrics']
        second = self.f.frames[-2]['metrics']
        first['upSourceAt']['a'] = first['upSourceAt']['c'] - 1
        second['upSourceAt']['c'] = first['upSourceAt']['c']
        second['sourceAt'] = first['sourceAt'] + .5
        self.fixture.persist_frames()
        self.assert_refused()

    def test_up_advances_but_one_candidate_raw_series_replays(self):
        first = self.f.frames[-3]['metrics']
        second = self.f.frames[-2]['metrics']
        candidate_key = next(row for row in second['raw'] if row['node'] == 'c')
        first_key = next(row for row in first['raw'] if row['node'] == 'c' and
                         (row['pool'], row['user'], row['kind']) ==
                         (candidate_key['pool'], candidate_key['user'], candidate_key['kind']))
        candidate_key['sourceAt'] = first_key['sourceAt']
        self.fixture.persist_frames()
        self.assert_refused()

    def test_missing_pool_user_kind_refuses(self):
        frame = self.f.frames[-2]
        frame['metrics']['raw'] = [row for row in frame['metrics']['raw'] if not
            (row['node'] == 'c' and row['pool'] == 'test' and
             row['user'] == 'poc_app' and row['kind'] == 'waiting')]
        self.fixture.persist_frames()
        self.assert_refused()

    def test_missing_or_stale_up_and_raw_refuses(self):
        for edit in (
                lambda m: m.pop('upSourceAt'),
                lambda m: m['upSourceAt'].__setitem__('c', m['observedAt'] - 46),
                lambda m: m['raw'].__getitem__(-1).__setitem__('sourceAt',
                    m['observedAt'] - 46)):
            with self.subTest(edit=edit):
                frame = self.f.frames[-2]
                before = copy.deepcopy(frame['metrics'])
                edit(frame['metrics'])
                self.fixture.persist_frames()
                self.assert_refused()
                frame['metrics'] = before
                self.fixture.persist_frames()

    def test_later_nonzero_or_nlb_reappearance_refuses(self):
        for edit in (
                lambda f: f['metrics']['raw'][-1].__setitem__('value', 1),
                lambda f: f['inventory']['backendStates'].append(dict(
                    id='c', status='Available', weight=100, listenerHealth='Normal'))):
            with self.subTest(edit=edit):
                frame = self.f.frames[-1]
                before = copy.deepcopy(frame)
                edit(frame)
                self.fixture.persist_frames()
                self.assert_refused()
                self.f.frames[-1] = before
                self.fixture.persist_frames()

    def test_changed_client_or_protection_refuses(self):
        for edit in (
                lambda f: f['pods'][0]['snapshot']['sample'].__setitem__('processId', 'changed'),
                lambda f: f['inventory']['memberStates'][0].__setitem__('lifecycle', 'InService')):
            with self.subTest(edit=edit):
                frame = self.f.frames[-1]
                before = copy.deepcopy(frame)
                edit(frame)
                self.fixture.persist_frames()
                self.assert_refused()
                self.f.frames[-1] = before
                self.fixture.persist_frames()

    def test_changed_deployment_or_live_roster_refuses(self):
        before = copy.deepcopy(self.f.deployment)
        self.f.deployment['metadata']['uid'] = 'new-deployment'
        self.assert_refused()
        self.f.deployment = before
        self.f.roster[0]['processId'] = 'new-process'
        self.assert_refused()

    def test_stale_provider_or_collector_gap_refuses(self):
        frame = self.f.frames[-1]
        frame['inventory']['observedAt'] = frame['at'] - 31
        self.fixture.persist_frames()
        self.assert_refused()
        frame['inventory']['observedAt'] = frame['at'] - .5
        frame['at'] += 4
        self.f.now = frame['at']
        self.fixture.persist_frames()
        self.assert_refused()

    def test_no_provider_observation_after_second_zero_refuses(self):
        frame = self.f.frames[-1]
        frame['inventory']['observedAt'] = self.f.frames[-2]['at']
        self.fixture.persist_frames()
        self.assert_refused()

    def test_binding_replay_and_one_use_refuse(self):
        saved = dict(self.binding)
        self.binding['evidenceSha256'] = '0' * 64
        self.assert_refused()
        self.binding = saved
        self.produce()
        with self.assertRaises(pending.Refused):
            self.produce()


if __name__ == '__main__':
    unittest.main()
