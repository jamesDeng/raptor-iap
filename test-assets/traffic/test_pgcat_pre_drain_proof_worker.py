"""Offline tests for the three-node proof before any NLB removal."""
import copy
import hashlib
import json
from pathlib import Path
import time
import unittest

from capture_window import clean
import pgcat_pending_proof_worker as proof
import test_pgcat_pending_proof_worker as pending_fixture

write_private = pending_fixture.write_private


class PreDrainProofWorkerTests(unittest.TestCase):
    def setUp(self):
        fixture = pending_fixture.PendingProofWorkerTests(
            'test_provider_source_rejects_correlated_wrong_ecs_address')
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        fixture.install_provider_source()
        start = fixture.now
        base_frame = copy.deepcopy(fixture.frames[-1])
        # Two collector frames from different provider publications after NLB entry.
        for sequence, at in ((6, start), (7, start + 1)):
            frame = copy.deepcopy(base_frame)
            frame['at'] = at
            frame['inventory']['sequence'] = sequence
            frame['inventory']['observedAt'] = at - .5
            frame['inventory']['healthyRegistered'] = ['a', 'b', 'c']
            frame['inventory']['memberStates'][-1] = dict(
                id='c', lifecycle='InService', health='Healthy', addressKnown=True)
            if not any(row['id'] == 'c' for row in frame['inventory']['backendStates']):
                frame['inventory']['backendStates'].append(dict(
                    id='c', status='Available', weight=100, listenerHealth='Normal'))
            frame['metrics']['observedAt'] = at - .1
            frame['metrics']['sourceAt'] = at - .2
            for row in frame['metrics']['raw']:
                row['sourceAt'] = at - .2
            for pod in frame['pods']:
                sample = pod['snapshot']['sample']
                sample['at'] = at
                sample['Success'] += 1
                sample['Scheduled'] += 1
                sample['Attempts'] += 1
                next_sequence = sample['sequence'] + 1
                sample['sequence'] += 2
                operation_id = str(sample['Success'])
                pod['snapshot']['events'].extend([
                    dict(processId=sample['processId'], sequence=next_sequence,
                         at=at, kind='scheduled', operationId=operation_id),
                    dict(processId=sample['processId'], sequence=next_sequence + 1,
                         at=at, kind='operation', operationId=operation_id,
                         outcome='success')])
            fixture.frames.append(clean(frame))
            base_frame = frame
        fixture.now = start + 1
        with (fixture.capture / 'segment-0001.jsonl').open('w') as stream:
            for frame in fixture.frames:
                stream.write(json.dumps(frame, sort_keys=True) + '\n')
        write_private(fixture.inventory_path, fixture.frames[-1]['inventory'])
        source = fixture.contract / 'provider-source'
        prior = json.loads((source / '000005.json').read_text())
        for frame in fixture.frames[-2:]:
            inventory = frame['inventory']
            raw = (fixture.inventory_path.read_bytes() if inventory['sequence'] == 7
                   else json.dumps(inventory, sort_keys=True).encode('ascii'))
            sidecar = dict(prior, sequence=inventory['sequence'],
                observedAt=inventory['observedAt'],
                inventorySha256=hashlib.sha256(raw).hexdigest(),
                inventorySemanticSha256=hashlib.sha256(json.dumps(inventory,
                    sort_keys=True, separators=(',', ':'),
                    allow_nan=False).encode('ascii')).hexdigest())
            write_private(source / ('%06d.json' % inventory['sequence']), sidecar)
        fixture.baseline['providerRunContextSha256'] = hashlib.sha256(
            (source / 'run-context.json').read_bytes()).hexdigest()
        fixture.pre['providerRunContextSha256'] = fixture.baseline['providerRunContextSha256']
        fixture.pending['providerRunContextSha256'] = fixture.baseline['providerRunContextSha256']
        fixture.save()
        roster_sha = hashlib.sha256(json.dumps(fixture.baseline['roster'],
            sort_keys=True, separators=(',', ':'), allow_nan=False).encode('ascii')).hexdigest()
        self.binding = dict(runId=fixture.run, feedSession=fixture.baseline['receipt']['feedSession'],
            newId='c', originalInstanceIds=['a', 'b'], sequence=fixture.pending['sequence'],
            observedAt=fixture.pending['observedAt'], resourceVersion='11',
            clientRosterSha256=roster_sha,
            evidenceSha256=hashlib.sha256((fixture.contract / 'pending-ready.json').read_bytes()).hexdigest(),
            sourceSha256='a' * 64)

    def produce(self):
        import pgcat_pre_drain_proof_worker as pre_drain
        f = self.fixture
        return pre_drain.produce(f.contract, f.run, self.binding,
            kubectl=f.kubectl, live_roster=lambda: copy.deepcopy(f.roster),
            now=lambda: f.now)

    def persist_frames(self):
        f = self.fixture
        with (f.capture / 'segment-0001.jsonl').open('w') as stream:
            for frame in f.frames:
                stream.write(json.dumps(frame, sort_keys=True) + '\n')
        write_private(f.inventory_path, f.frames[-1]['inventory'])

    def append_provider_frame(self, sequence, candidate_registered):
        f = self.fixture
        frame = copy.deepcopy(f.frames[-1])
        frame['at'] += 1
        inv = frame['inventory']
        inv['sequence'] = sequence
        inv['observedAt'] = frame['at'] - .5
        inv['healthyRegistered'] = ['a', 'b', 'c'] if candidate_registered else ['a', 'b']
        inv['backendStates'] = [row for row in inv['backendStates'] if row['id'] != 'c']
        if candidate_registered:
            inv['backendStates'].append(dict(id='c', status='Available', weight=100,
                                            listenerHealth='Normal'))
        frame['metrics']['observedAt'] = frame['at'] - .1
        frame['metrics']['sourceAt'] = frame['at'] - .2
        for row in frame['metrics']['raw']:
            row['sourceAt'] = frame['at'] - .2
        for pod in frame['pods']:
            sample = pod['snapshot']['sample']
            sample['at'] = frame['at']
            sample['Success'] += 1
            sample['Scheduled'] += 1
            sample['Attempts'] += 1
            next_sequence = sample['sequence'] + 1
            sample['sequence'] += 2
            operation_id = str(sample['Success'])
            pod['snapshot']['events'].extend([
                dict(processId=sample['processId'], sequence=next_sequence,
                     at=frame['at'], kind='scheduled', operationId=operation_id),
                dict(processId=sample['processId'], sequence=next_sequence + 1,
                     at=frame['at'], kind='operation', operationId=operation_id,
                     outcome='success')])
        frame = clean(frame)
        f.frames.append(frame)
        f.now = frame['at']
        self.persist_frames()
        source = f.contract / 'provider-source'
        previous = json.loads((source / '000007.json').read_text())
        raw = f.inventory_path.read_bytes()
        sidecar = dict(previous, sequence=sequence,
            observedAt=inv['observedAt'], inventorySha256=hashlib.sha256(raw).hexdigest(),
            inventorySemanticSha256=hashlib.sha256(json.dumps(inv, sort_keys=True,
                separators=(',', ':'), allow_nan=False).encode('ascii')).hexdigest())
        write_private(source / ('%06d.json' % sequence), sidecar)

    def test_two_distinct_healthy_post_entry_observations_seal_address_free_proof(self):
        f = self.fixture
        rows = json.loads(f.cm['data']['targets.json'])
        f.cm['metadata']['resourceVersion'] = '12'
        f.cm['data']['targets.json'] = json.dumps(rows)
        result = self.produce()
        self.assertEqual(result['runId'], f.run)
        self.assertEqual(result['newId'], 'c')
        self.assertEqual(result['sequence'], 7)
        self.assertNotIn('10.0.0.', json.dumps(result))
        self.assertTrue((f.contract / 'pre-drain.json').exists())

    def test_one_post_entry_publication_cannot_qualify(self):
        f = self.fixture
        f.frames.pop()
        self.persist_frames()
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_pending_summary_must_match_sealed_candidate_and_digest(self):
        for changed in (dict(newId='a'), dict(evidenceSha256='0' * 64),
                        dict(clientRosterSha256='0' * 64)):
            with self.subTest(changed=changed):
                prior = dict(self.binding)
                self.binding.update(changed)
                with self.assertRaises(proof.Refused):
                    self.produce()
                self.assertFalse((self.fixture.contract / 'pre-drain.json').exists())
                self.binding = prior

    def test_candidate_must_be_available_and_sole_unprotected(self):
        f = self.fixture
        f.frames[-1]['inventory']['backendStates'][-1]['status'] = 'Adding'
        self.persist_frames()
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_changed_client_or_sql_failure_refuses(self):
        for key, value in (('processId', 'new-process'), ('Failure', 1)):
            with self.subTest(key=key):
                f = self.fixture
                original = copy.deepcopy(f.frames[-1])
                f.frames[-1]['pods'][0]['snapshot']['sample'][key] = value
                self.persist_frames()
                with self.assertRaises(proof.Refused):
                    self.produce()
                self.assertFalse((f.contract / 'pre-drain.json').exists())
                f.frames[-1] = original
                self.persist_frames()

    def test_configmap_wrong_ecs_address_refuses(self):
        f = self.fixture
        rows = json.loads(f.cm['data']['targets.json'])
        rows[-1]['targets'] = ['10.0.0.99:9930']
        f.cm['data']['targets.json'] = json.dumps(rows)
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_correlated_sidecar_and_configmap_wrong_ecs_address_refuses(self):
        f = self.fixture
        source = f.contract / 'provider-source'
        sidecar = json.loads((source / '000007.json').read_text())
        sidecar['privateAddresses']['c'] = '10.0.0.99'
        write_private(source / '000007.json', sidecar)
        rows = json.loads(f.cm['data']['targets.json'])
        rows[-1]['targets'] = ['10.0.0.99:9930']
        f.cm['data']['targets.json'] = json.dumps(rows)
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_incomplete_new_backend_metrics_refuses(self):
        f = self.fixture
        f.frames[-1]['metrics']['raw'] = [row for row in f.frames[-1]['metrics']['raw']
            if not (row['node'] == 'c' and row['kind'] == 'waiting')]
        self.persist_frames()
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_proof_is_one_use(self):
        self.produce()
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_later_collector_frame_may_reuse_latest_provider_publication(self):
        f = self.fixture
        frame = copy.deepcopy(f.frames[-1])
        frame['at'] += 1
        frame['metrics']['observedAt'] = frame['at'] - .1
        frame['metrics']['sourceAt'] = frame['at'] - .2
        for row in frame['metrics']['raw']:
            row['sourceAt'] = frame['at'] - .2
        for pod in frame['pods']:
            sample = pod['snapshot']['sample']
            sample['at'] = frame['at']
            sample['Success'] += 1
            sample['Scheduled'] += 1
            sample['Attempts'] += 1
            next_sequence = sample['sequence'] + 1
            sample['sequence'] += 2
            operation_id = str(sample['Success'])
            pod['snapshot']['events'].extend([
                dict(processId=sample['processId'], sequence=next_sequence,
                     at=frame['at'], kind='scheduled', operationId=operation_id),
                dict(processId=sample['processId'], sequence=next_sequence + 1,
                     at=frame['at'], kind='operation', operationId=operation_id,
                     outcome='success')])
        f.frames.append(clean(frame))
        f.now = frame['at']
        self.persist_frames()
        self.assertEqual(self.produce()['sequence'], 7)

    def test_candidate_disappearance_then_reentry_after_nlb_entry_refuses(self):
        self.append_provider_frame(8, False)
        self.append_provider_frame(9, True)
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((self.fixture.contract / 'pre-drain.json').exists())

    def test_initial_adding_then_two_healthy_publications_qualify(self):
        f = self.fixture
        first = f.frames[-2]
        first['inventory']['backendStates'][-1]['status'] = 'Adding'
        first['inventory']['backendStates'][-1]['listenerHealth'] = 'Initial'
        first['inventory']['healthyRegistered'] = ['a', 'b']
        self.append_provider_frame(8, True)
        self.assertEqual(self.produce()['sequence'], 8)

    def test_initial_configuring_with_incomplete_metrics_refuses(self):
        f = self.fixture
        first = f.frames[-2]
        first['inventory']['backendStates'][-1]['status'] = 'Configuring'
        first['inventory']['backendStates'][-1]['listenerHealth'] = 'Initial'
        first['inventory']['healthyRegistered'] = ['a', 'b']
        first['metrics']['raw'] = [row for row in first['metrics']['raw']
            if not (row['node'] == 'c' and row['kind'] == 'waiting')]
        self.append_provider_frame(8, True)
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_initial_configuring_with_complete_metrics_qualifies(self):
        f = self.fixture
        first = f.frames[-2]
        first['inventory']['backendStates'][-1]['status'] = 'Configuring'
        first['inventory']['backendStates'][-1]['listenerHealth'] = 'Initial'
        first['inventory']['healthyRegistered'] = ['a', 'b']
        self.append_provider_frame(8, True)
        self.assertEqual(self.produce()['sequence'], 8)

    def test_healthy_then_adding_then_healthy_refuses(self):
        self.append_provider_frame(8, True)
        f = self.fixture
        middle = f.frames[-2]
        middle['inventory']['backendStates'][-1]['status'] = 'Adding'
        middle['inventory']['backendStates'][-1]['listenerHealth'] = 'Initial'
        middle['inventory']['healthyRegistered'] = ['a', 'b']
        self.persist_frames()
        with self.assertRaises(proof.Refused):
            self.produce()
        self.assertFalse((f.contract / 'pre-drain.json').exists())

    def test_candidate_order_between_originals_is_valid(self):
        import pgcat_pre_drain_proof_worker as pre_drain
        frame = copy.deepcopy(self.fixture.frames[-1])
        frame['inventory']['memberStates'] = [
            dict(id='a', lifecycle='Protected', health='Healthy', addressKnown=True),
            dict(id='b', lifecycle='InService', health='Healthy', addressKnown=True),
            dict(id='c', lifecycle='Protected', health='Healthy', addressKnown=True)]
        pre_drain.exact_three(frame, ['a', 'c'], 'b')


if __name__ == '__main__':
    unittest.main()
