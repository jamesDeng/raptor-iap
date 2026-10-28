"""Local safety tests for fresh Pending evidence production."""
import copy
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
import uuid
from unittest.mock import patch

from capture_window import clean
from test_capture_window import transition_frame


class RehearsalContractTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(os.path.realpath(self.tmp.name))
        self.root.chmod(0o700)
        self.contract = self.root / 'contract'
        self.contract.mkdir(mode=0o700)
        self.run = self.root / 'run'
        self.run.mkdir(mode=0o700)
        self.source = self.contract / 'provider-source'
        self.source.mkdir(mode=0o700)
        self.run_id = str(uuid.uuid4())
        self.session = str(uuid.uuid4())
        self.target = {'accountId': '1360282071200743', 'regionId': 'ap-southeast-1',
                       'workerId': 'i-t4nj1bitmcz7cuoch82h',
                       'scalingGroupId': 'asg-t4necb4dfmu6o3z2rn0g',
                       'nlbServerGroupId': 'sgp-xs5s42tihtl6ja2a67',
                       'originalInstanceIds': ['i-a', 'i-b']}
        self.target_sha = hashlib.sha256(json.dumps(self.target, sort_keys=True,
            separators=(',', ':')).encode('ascii')).hexdigest()
        self.record = {
            'schemaVersion': 1,
            'runContext': {
                'schemaVersion': 1,
                'runId': self.run_id,
                'supervisorId': str(uuid.uuid4()),
                'targetSha256': self.target_sha,
                'bundleSha256': 'b' * 64,
                'nonce': str(uuid.uuid4()),
            },
            'feedSession': self.session,
        }
        self.raw = json.dumps(self.record, sort_keys=True, separators=(',', ':')).encode('ascii')
        self.context = self.source / 'run-context.json'
        self.context.write_bytes(self.raw)
        self.context.chmod(0o600)
        self.inventory_data = {
            'schemaVersion': 2, 'feedSession': self.session, 'sequence': 1,
            'observedAt': 123.0, 'desired': 2,
            'members': ['i-a', 'i-b'], 'healthyRegistered': ['i-a', 'i-b'],
            'memberStates': [
                {'id': 'i-a', 'lifecycle': 'Protected', 'health': 'Healthy', 'addressKnown': True},
                {'id': 'i-b', 'lifecycle': 'Protected', 'health': 'Healthy', 'addressKnown': True},
            ],
            'backendStates': [
                {'id': 'i-a', 'status': 'Available', 'weight': 100, 'listenerHealth': 'Normal'},
                {'id': 'i-b', 'status': 'Available', 'weight': 100, 'listenerHealth': 'Normal'},
            ],
            'mode': 'disabled-rehearsal',
        }
        self.inventory = self.run / 'provider-inventory.json'
        self.inventory_raw = json.dumps(self.inventory_data, separators=(',', ':')).encode('ascii')
        self.inventory.write_bytes(self.inventory_raw)
        self.inventory.chmod(0o600)
        sidecar = {
            'schemaVersion': 1, 'runId': self.run_id,
            'supervisorId': self.record['runContext']['supervisorId'],
            'contextNonce': self.record['runContext']['nonce'],
            'targetSha256': self.target_sha, 'bundleSha256': 'b' * 64,
            'feedSession': self.session, 'sequence': 1, 'observedAt': 123.0,
            'inventorySha256': hashlib.sha256(self.inventory_raw).hexdigest(),
            'inventorySemanticSha256': hashlib.sha256(json.dumps(
                self.inventory_data, sort_keys=True, separators=(',', ':')).encode('ascii')).hexdigest(),
            'privateAddresses': {'i-a': '10.0.0.1', 'i-b': '10.0.0.2'},
        }
        self.sidecar = self.source / '000001.json'
        self.sidecar.write_text(json.dumps(sidecar, separators=(',', ':')))
        self.sidecar.chmod(0o600)
        self.boundary = self.run / 'disabled-boundary'
        self.boundary.mkdir(mode=0o700)
        self.kubeconfig = self.root / 'kubeconfig'
        self.kubeconfig.write_text('private-test-credential')
        self.kubeconfig.chmod(0o600)
        self.originals = [('i-a', '10.0.0.1'), ('i-b', '10.0.0.2')]
        self.cm = {'metadata': {'name': 'pgcat-provider-inventory', 'namespace': 'raptor-test',
                                'uid': 'cm-uid', 'resourceVersion': '10'},
                   'data': {'targets.json': json.dumps([
                       {'targets': [address + ':9930'], 'labels': {
                           'env': 'rdev.ali', 'db_proxy_code': 'DP00001',
                           'target_db_code': 'D00002', 'ecs_instance_id': node}}
                       for node, address in self.originals])}}
        self.deployment = {'metadata': {'name': 'rdev-test-client-client',
            'namespace': 'raptor-test', 'uid': 'deployment', 'generation': 1},
            'spec': {'replicas': 2}, 'status': {'observedGeneration': 1,
            'replicas': 2, 'updatedReplicas': 2, 'readyReplicas': 2, 'availableReplicas': 2}}
        self.pods = []
        self.readings = {}
        for i in range(2):
            name = 'client-' + str(i)
            pod = {'metadata': {'name': name, 'namespace': 'raptor-test',
                                'uid': 'pod-' + str(i)},
                   'status': {'phase': 'Running', 'conditions': [{'type': 'Ready', 'status': 'True'}],
                              'containerStatuses': [{'ready': True, 'state': {'running': {}},
                                                      'containerID': 'container-' + str(i)}]}}
            self.pods.append(pod)
            self.readings[name] = {'name': name, 'uid': 'pod-' + str(i),
                'containerId': 'container-' + str(i),
                'snapshot': {'final': False, 'sample': {'processId': 'process-' + str(i)}}}
        self.metrics = {'available': True, 'observedAt': 123.0, 'sourceAt': 123.0,
                        'nodes': ['i-a', 'i-b'], 'raw': [
            {'node': node, 'pool': 'test', 'user': 'poc_app', 'kind': kind,
             'value': 1, 'sourceAt': 123.0}
            for node, _ in self.originals for kind in ('active', 'idle', 'waiting')]}

    def test_fresh_contract_accepts_only_feed_source_and_binds_exact_run(self):
        from pgcat_rehearsal_contract import fresh_provider_source
        source = fresh_provider_source(self.contract, self.run_id, self.inventory)
        self.assertEqual(source, {
            'providerRunContextSha256': hashlib.sha256(self.raw).hexdigest(),
            'targetSha256': self.target_sha,
            'feedSession': self.session,
            'sequence': 1,
            'observedAt': 123.0,
            'privateAddresses': {'i-a': '10.0.0.1', 'i-b': '10.0.0.2'},
            'inventoryRawSha256': hashlib.sha256(self.inventory_raw).hexdigest(),
        })
        (self.contract / 'old-baseline.json').write_text('{}')
        with self.assertRaises(ValueError):
            fresh_provider_source(self.contract, self.run_id, self.inventory)

    def test_cross_run_and_symlinked_context_refuse(self):
        from pgcat_rehearsal_contract import fresh_provider_source
        with self.assertRaises(ValueError):
            fresh_provider_source(self.contract, str(uuid.uuid4()), self.inventory)
        self.context.unlink()
        alternate = self.root / 'alternate.json'
        alternate.write_bytes(self.raw)
        alternate.chmod(0o600)
        self.context.symlink_to(alternate)
        with self.assertRaises(ValueError):
            fresh_provider_source(self.contract, self.run_id, self.inventory)

    def test_unacknowledged_or_changed_inventory_refuses_source_link(self):
        from pgcat_rehearsal_contract import fresh_provider_source
        self.inventory.write_bytes(b'{}')
        with self.assertRaises(ValueError):
            fresh_provider_source(self.contract, self.run_id, self.inventory)

    def _kubectl(self, *args):
        kind = args[1]
        if kind == 'configmap':
            return self.cm
        if kind == 'deployment':
            return self.deployment
        if kind == 'pods':
            return {'items': self.pods}
        if kind == 'service':
            return {'spec': {'clusterIP': '10.0.0.10'}}
        self.fail('unexpected Kubernetes read')

    def _baseline(self, read_pod=None):
        from pgcat_rehearsal_contract import baseline
        return baseline(self.contract, self.run_id, self.kubeconfig, self.target,
                        kubectl=self._kubectl,
                        read_pod=read_pod or (lambda pod: self.readings[pod['metadata']['name']]),
                        query_metrics=lambda base, stamp: self.metrics,
                        clock=lambda: 123.5)

    def test_baseline_seals_context_and_exact_provider_addresses(self):
        self._baseline()
        path = self.contract / 'baseline.json'
        self.assertTrue(path.is_file())
        value = json.loads(path.read_text())
        self.assertEqual(value['providerRunContextSha256'], hashlib.sha256(self.raw).hexdigest())
        self.assertEqual(value['targetSha256'], self.target_sha)
        self.assertEqual(value['originals'], [{'id': 'i-a', 'address': '10.0.0.1'},
                                               {'id': 'i-b', 'address': '10.0.0.2'}])
        self.assertEqual(value['receipt']['feedSession'], self.session)
        self.assertEqual(json.loads(Path(str(path) + '.sha256').read_text()),
                         {'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})

    def test_baseline_refuses_wrong_bound_target_fingerprint(self):
        self.target['regionId'] = 'other-region'
        with self.assertRaises(ValueError):
            self._baseline()
        self.assertFalse((self.contract / 'baseline.json').exists())

    def test_baseline_refuses_wrong_originals_even_with_matching_target_fingerprint(self):
        self.target['originalInstanceIds'] = ['i-a', 'i-c']
        changed = hashlib.sha256(json.dumps(self.target, sort_keys=True,
            separators=(',', ':')).encode('ascii')).hexdigest()
        self.record['runContext']['targetSha256'] = changed
        self.raw = json.dumps(self.record, sort_keys=True,
            separators=(',', ':')).encode('ascii')
        self.context.write_bytes(self.raw)
        sidecar = json.loads(self.sidecar.read_text())
        sidecar['targetSha256'] = changed
        self.sidecar.write_text(json.dumps(sidecar, separators=(',', ':')))
        with self.assertRaises(ValueError):
            self._baseline()
        self.assertFalse((self.contract / 'baseline.json').exists())

    def test_baseline_refuses_configmap_address_that_disagrees_with_ecs_sidecar(self):
        rows = json.loads(self.cm['data']['targets.json'])
        rows[0]['targets'] = ['10.0.0.99:9930']
        self.cm['data']['targets.json'] = json.dumps(rows)
        with self.assertRaises(ValueError):
            self._baseline()
        self.assertFalse((self.contract / 'baseline.json').exists())

    def test_baseline_refuses_provider_advance_before_seal(self):
        def read_pod(pod):
            self.inventory.write_bytes(b'{}')
            return self.readings[pod['metadata']['name']]
        with self.assertRaises(ValueError):
            self._baseline(read_pod)
        self.assertFalse((self.contract / 'baseline.json').exists())

    def test_baseline_uses_only_bound_worker_kubeconfig_during_reads(self):
        original = self._kubectl
        def checked(*args):
            self.assertEqual(os.environ.get('KUBECONFIG'), str(self.kubeconfig))
            return original(*args)
        self._kubectl = checked
        self._baseline()
        self.assertNotEqual(os.environ.get('KUBECONFIG'), str(self.kubeconfig))

    def _advance_provider(self, sequence, observed_at, members=None, pending=False):
        members = ['i-a', 'i-b'] if members is None else members
        value = copy.deepcopy(self.inventory_data)
        value.update(sequence=sequence, observedAt=observed_at, desired=len(members),
                     members=members, healthyRegistered=['i-a', 'i-b'])
        if pending:
            value['memberStates'].append({'id': 'i-c', 'lifecycle': 'Pending',
                                          'health': 'Healthy', 'addressKnown': True})
        self.inventory.write_bytes(json.dumps(value, separators=(',', ':')).encode('ascii'))
        addresses = {'i-a': '10.0.0.1', 'i-b': '10.0.0.2'}
        if pending:
            addresses['i-c'] = '10.0.0.3'
        raw = self.inventory.read_bytes()
        sidecar = json.loads(self.sidecar.read_text())
        sidecar.update(sequence=sequence, observedAt=observed_at,
            inventorySha256=hashlib.sha256(raw).hexdigest(),
            inventorySemanticSha256=hashlib.sha256(json.dumps(value, sort_keys=True,
                separators=(',', ':')).encode('ascii')).hexdigest(),
            privateAddresses=addresses)
        path = self.source / ('%06d.json' % sequence)
        path.write_text(json.dumps(sidecar, separators=(',', ':')))
        path.chmod(0o600)

    def _install_capture(self):
        capture = self.run / 'rehearsal-capture'
        capture.mkdir(mode=0o700)
        frames = []
        for index, stamp in enumerate((124.0, 125.0, 126.0)):
            frame = transition_frame(index, members=('i-a', 'i-b'), desired=2)
            frame['at'] = stamp
            frame['phase'] = 'measurement'
            frame['expectedReplicas'] = 2
            frame['inventory'].update(feedSession=self.session, mode='disabled-rehearsal',
                                      sequence=index + 2, observedAt=stamp - .1)
            frame['metrics'].update(observedAt=stamp, sourceAt=stamp,
                                    nodes=['i-a', 'i-b'])
            for row in frame['metrics']['raw']:
                row['sourceAt'] = stamp
            first = frame['pods'][0]
            first['uid'] = 'pod-0'
            first['snapshot']['sample']['processId'] = 'process-0'
            for event in first['snapshot']['events']:
                event['processId'] = 'process-0'
                event['at'] = stamp
            first['snapshot']['sample']['at'] = stamp
            other = copy.deepcopy(first)
            other['uid'] = 'pod-1'
            other['snapshot']['sample']['processId'] = 'process-1'
            for event in other['snapshot']['events']:
                event['processId'] = 'process-1'
            frame['pods'].append(other)
            frames.append(clean(frame))
        segment = capture / 'segment-0001.jsonl'
        segment.write_text(''.join(json.dumps(frame, sort_keys=True) + '\n' for frame in frames))
        segment.chmod(0o600)
        self._advance_provider(5, 126.2)
        return frames

    def test_pre_scale_seals_same_context_and_two_clean_frames(self):
        self._baseline()
        frames = self._install_capture()
        from pgcat_rehearsal_contract import pre_scale
        pre_scale(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                  read_pod=lambda pod: self.readings[pod['metadata']['name']],
                  clock=lambda: 126.5)
        value = json.loads((self.contract / 'pre-scale.json').read_text())
        self.assertEqual(value['providerRunContextSha256'], hashlib.sha256(self.raw).hexdigest())
        self.assertEqual(value['feedSession'], self.session)
        self.assertEqual(value['sequence'], 5)
        self.assertEqual(value['firstAt'], frames[0]['at'])
        self.assertEqual(value['latestAt'], frames[-1]['at'])

    def test_pre_scale_refuses_context_replacement_before_sealing(self):
        self._baseline()
        self._install_capture()
        original = self._kubectl
        def replace(*args):
            if args[1] == 'deployment':
                self.context.write_bytes(b'{}')
            return original(*args)
        from pgcat_rehearsal_contract import pre_scale
        with self.assertRaises(ValueError):
            pre_scale(self.contract, self.run_id, self.target, kubectl=replace,
                      read_pod=lambda pod: self.readings[pod['metadata']['name']],
                      clock=lambda: 126.5)
        self.assertFalse((self.contract / 'pre-scale.json').exists())

    def test_pre_scale_refuses_target_swap_after_baseline(self):
        self._baseline()
        self._install_capture()
        self.target['regionId'] = 'other-region'
        from pgcat_rehearsal_contract import pre_scale
        with self.assertRaises(ValueError):
            pre_scale(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                      read_pod=lambda pod: self.readings[pod['metadata']['name']],
                      clock=lambda: 126.5)
        self.assertFalse((self.contract / 'pre-scale.json').exists())

    def test_pre_scale_refuses_malformed_capture_without_sealing(self):
        self._baseline()
        self._install_capture()
        (self.run / 'rehearsal-capture' / 'segment-0001.jsonl').write_text('{}\n')
        from pgcat_rehearsal_contract import pre_scale
        with self.assertRaises(ValueError):
            pre_scale(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                      read_pod=lambda pod: self.readings[pod['metadata']['name']],
                      clock=lambda: 126.5)
        self.assertFalse((self.contract / 'pre-scale.json').exists())

    def _prepare_pending(self):
        from pgcat_rehearsal_contract import pre_scale
        self._baseline()
        frames = self._install_capture()
        pre_scale(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                  read_pod=lambda pod: self.readings[pod['metadata']['name']],
                  clock=lambda: 126.5)
        bridge = transition_frame(3, members=('i-a', 'i-b'), desired=2)
        bridge['at'] = 126.7
        bridge['phase'] = 'measurement'
        bridge['expectedReplicas'] = 2
        bridge['inventory'].update(feedSession=self.session, mode='disabled-rehearsal',
                                   sequence=5, observedAt=126.2)
        bridge['metrics'].update(observedAt=126.7, sourceAt=126.7, nodes=['i-a', 'i-b'])
        for row in bridge['metrics']['raw']:
            row['sourceAt'] = 126.7
        first = bridge['pods'][0]
        first['uid'] = 'pod-0'
        first['snapshot']['sample'].update(processId='process-0', at=126.7)
        for event in first['snapshot']['events']:
            event.update(processId='process-0', at=126.7)
        other = copy.deepcopy(first)
        other['uid'] = 'pod-1'
        other['snapshot']['sample']['processId'] = 'process-1'
        for event in other['snapshot']['events']:
            event['processId'] = 'process-1'
        bridge['pods'].append(other)
        frames.append(clean(bridge))
        for index, stamp in enumerate((127.0, 128.4), 4):
            frame = transition_frame(index, members=('i-a', 'i-b', 'i-c'),
                                     backends=('i-a', 'i-b'), pending=('i-c',),
                                     metrics=('i-a', 'i-b', 'i-c'), desired=3)
            frame['at'] = stamp
            frame['phase'] = 'measurement'
            frame['expectedReplicas'] = 2
            frame['inventory'].update(feedSession=self.session, mode='disabled-rehearsal',
                sequence=index + 2, observedAt=stamp - .1)
            frame['inventory']['memberStates'][-1]['addressKnown'] = True
            frame['metrics'].update(observedAt=stamp, sourceAt=stamp,
                                    nodes=['i-a', 'i-b', 'i-c'])
            for row in frame['metrics']['raw']:
                row['sourceAt'] = stamp
            first = frame['pods'][0]
            first['uid'] = 'pod-0'
            first['snapshot']['sample']['processId'] = 'process-0'
            first['snapshot']['sample']['at'] = stamp
            for event in first['snapshot']['events']:
                event.update(processId='process-0', at=stamp)
            other = copy.deepcopy(first)
            other['uid'] = 'pod-1'
            other['snapshot']['sample']['processId'] = 'process-1'
            for event in other['snapshot']['events']:
                event['processId'] = 'process-1'
            frame['pods'].append(other)
            frames.append(clean(frame))
        segment = self.run / 'rehearsal-capture' / 'segment-0001.jsonl'
        segment.write_text(''.join(json.dumps(frame, sort_keys=True) + '\n' for frame in frames))
        segment.chmod(0o600)
        self._advance_provider(7, 128.3, ['i-a', 'i-b', 'i-c'], pending=True)
        self.cm['metadata']['resourceVersion'] = '11'
        rows = json.loads(self.cm['data']['targets.json'])
        rows.append({'targets': ['10.0.0.3:9930'], 'labels': {
            'env': 'rdev.ali', 'db_proxy_code': 'DP00001', 'target_db_code': 'D00002',
            'ecs_instance_id': 'i-c'}})
        self.cm['data']['targets.json'] = json.dumps(rows)
        return frames

    def test_pending_seals_post_readback_candidate_and_context(self):
        frames = self._prepare_pending()
        from pgcat_rehearsal_contract import pending_ready
        pending_ready(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                      read_pod=lambda pod: self.readings[pod['metadata']['name']],
                      clock=lambda: 128.5, readback_clock=lambda: 127.5)
        value = json.loads((self.contract / 'pending-ready.json').read_text())
        self.assertEqual(value['providerRunContextSha256'], hashlib.sha256(self.raw).hexdigest())
        self.assertEqual(value['newId'], 'i-c')
        self.assertEqual(value['address'], '10.0.0.3')
        self.assertEqual(value['sequence'], frames[-1]['inventory']['sequence'])
        self.assertEqual(value['configMapResourceVersion'], '11')
        self.assertGreater(value['postReadbackObservedAt'], value['configMapReadbackAt'])

    def _finished_pending_source(self):
        self._prepare_pending()
        from pgcat_rehearsal_contract import pending_ready
        pending_ready(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                      read_pod=lambda pod: self.readings[pod['metadata']['name']],
                      clock=lambda: 128.5, readback_clock=lambda: 127.5)
        return [dict(name=name, uid=row['uid'], containerId=row['containerId'],
                     processId=row['snapshot']['sample']['processId'])
                for name, row in self.readings.items()]

    def test_final_pending_proof_seals_address_free_summary(self):
        roster = self._finished_pending_source()
        import pgcat_pending_proof_worker as proof
        summary = proof.produce(self.contract, self.run_id, kubectl=self._kubectl,
                                live_roster=lambda: roster, now=lambda: 128.5)
        baseline = json.loads((self.contract / 'baseline.json').read_text())
        pending = json.loads((self.contract / 'pending-ready.json').read_text())
        expected = {'runId': self.run_id, 'feedSession': self.session,
            'newId': 'i-c', 'originalInstanceIds': ['i-a', 'i-b'],
            'sequence': pending['sequence'], 'observedAt': pending['observedAt'],
            'resourceVersion': '11',
            'clientRosterSha256': hashlib.sha256(json.dumps(baseline['roster'],
                sort_keys=True, separators=(',', ':'), allow_nan=False).encode('ascii')).hexdigest(),
            'evidenceSha256': hashlib.sha256((self.contract / 'pending-ready.json').read_bytes()).hexdigest(),
            'sourceSha256': hashlib.sha256(Path(proof.__file__).read_bytes()).hexdigest()}
        self.assertEqual(summary, expected)
        raw = (self.contract / 'pending-proof.bundle.json').read_bytes()
        self.assertEqual(json.loads(raw)['summary'], expected)
        self.assertEqual(json.loads((self.contract / 'pending-proof.bundle.json.sha256').read_text()),
                         {'sha256': hashlib.sha256(raw).hexdigest()})
        self.assertNotIn(b'10.0.0.', raw)
        with self.assertRaises(proof.Refused):
            proof.produce(self.contract, self.run_id, kubectl=self._kubectl,
                          live_roster=lambda: roster, now=lambda: 128.5)

    def test_final_pending_proof_rejects_wrong_target_link_without_bundle(self):
        roster = self._finished_pending_source()
        pending_path = self.contract / 'pending-ready.json'
        pending = json.loads(pending_path.read_text())
        pending['targetSha256'] = '0' * 64
        raw = (json.dumps(pending, sort_keys=True, allow_nan=False) + '\n').encode('ascii')
        pending_path.write_bytes(raw)
        (self.contract / 'pending-ready.json.sha256').write_text(
            json.dumps({'sha256': hashlib.sha256(raw).hexdigest()}) + '\n')
        import pgcat_pending_proof_worker as proof
        with self.assertRaises(proof.Refused) as caught:
            proof.produce(self.contract, self.run_id, kubectl=self._kubectl,
                          live_roster=lambda: roster, now=lambda: 128.5)
        self.assertEqual(caught.exception.category, 'source-binding')
        self.assertFalse((self.contract / 'pending-proof.bundle.json').exists())

    def test_final_pending_cli_prints_only_address_free_summary(self):
        roster = self._finished_pending_source()
        import pgcat_pending_proof_worker as proof
        output = io.StringIO()
        with patch.object(proof.sys, 'argv', ['pending-proof', str(self.contract), self.run_id]), \
             patch.object(proof.worker_capture, 'kubectl', side_effect=self._kubectl), \
             patch.object(proof.finish_clients, 'live_roster', return_value=roster), \
             patch.object(proof.time, 'time', return_value=128.5), \
             contextlib.redirect_stdout(output):
            self.assertEqual(proof.main(), 0)
        rows = output.getvalue().splitlines()
        self.assertEqual(len(rows), 1)
        self.assertEqual(json.loads(rows[0])['newId'], 'i-c')
        self.assertNotIn('10.0.0.', rows[0])

    def test_final_pending_cli_is_silent_on_changed_context_seal(self):
        roster = self._finished_pending_source()
        baseline_path = self.contract / 'baseline.json'
        baseline = json.loads(baseline_path.read_text())
        baseline['providerRunContextSha256'] = '0' * 64
        raw = (json.dumps(baseline, sort_keys=True, allow_nan=False) + '\n').encode('ascii')
        baseline_path.write_bytes(raw)
        (self.contract / 'baseline.json.sha256').write_text(
            json.dumps({'sha256': hashlib.sha256(raw).hexdigest()}) + '\n')
        import pgcat_pending_proof_worker as proof
        output = io.StringIO()
        with patch.object(proof.sys, 'argv', ['pending-proof', str(self.contract), self.run_id]), \
             patch.object(proof.worker_capture, 'kubectl', side_effect=self._kubectl), \
             patch.object(proof.finish_clients, 'live_roster', return_value=roster), \
             patch.object(proof.time, 'time', return_value=128.5), \
             contextlib.redirect_stdout(output):
            self.assertEqual(proof.main(), 1)
        self.assertEqual(output.getvalue(), '')
        self.assertFalse((self.contract / 'pending-proof.bundle.json').exists())

    def test_final_pending_proof_refuses_inventory_change_before_bundle(self):
        roster = self._finished_pending_source()
        import pgcat_pending_proof_worker as proof
        real_verify = proof.verify_provider_source
        def change_after_source_check(*args):
            source = real_verify(*args)
            with self.inventory.open('ab') as stream:
                stream.write(b'\n')
            return source
        with patch.object(proof, 'verify_provider_source',
                          side_effect=change_after_source_check):
            with self.assertRaises(proof.Refused):
                proof.produce(self.contract, self.run_id, kubectl=self._kubectl,
                              live_roster=lambda: roster, now=lambda: 128.5)
        self.assertFalse((self.contract / 'pending-proof.bundle.json').exists())

    def test_pending_rejects_cm_candidate_ip_not_in_ecs_sidecar(self):
        self._prepare_pending()
        rows = json.loads(self.cm['data']['targets.json'])
        rows[-1]['targets'] = ['10.0.0.99:9930']
        self.cm['data']['targets.json'] = json.dumps(rows)
        from pgcat_rehearsal_contract import pending_ready
        with self.assertRaises(ValueError):
            pending_ready(self.contract, self.run_id, self.target, kubectl=self._kubectl,
                          read_pod=lambda pod: self.readings[pod['metadata']['name']],
                          clock=lambda: 128.5, readback_clock=lambda: 127.5)
        self.assertFalse((self.contract / 'pending-ready.json').exists())

    def test_pending_fleet_accepts_candidate_id_before_originals(self):
        from pgcat_rehearsal_contract import _pending_fleet
        frame = transition_frame(1, members=('i-0', 'i-a', 'i-b'),
            backends=('i-a', 'i-b'), pending=('i-0',),
            metrics=('i-0', 'i-a', 'i-b'), desired=3)
        frame['phase'] = 'measurement'
        frame['expectedReplicas'] = 2
        frame['inventory']['mode'] = 'disabled-rehearsal'
        frame['inventory']['feedSession'] = self.session
        frame['inventory']['memberStates'][0]['addressKnown'] = True
        _pending_fleet(frame, {'originals': [{'id': 'i-a'}, {'id': 'i-b'}],
                               'receipt': {'feedSession': self.session}}, 'i-0')


    def _reconcile_fixture(self):
        self.__dict__.pop('_reconcile_kubectl', None)
        import pgcat_pending_proof_worker as pending_proof
        roster = self._finished_pending_source()
        self.pending_binding = pending_proof.produce(self.contract, self.run_id,
            kubectl=self._kubectl, live_roster=lambda: roster, now=lambda: 128.5)
        self._advance_provider(20, 220.0)
        self.close_context = dict(feedSession=self.session, sequence=20, observedAt=220.0,
            scaleinRequestId=str(uuid.uuid4()), scaleinAcknowledgedAt=219.0,
            pendingBindingSha256=hashlib.sha256(json.dumps(self.pending_binding,
                sort_keys=True, separators=(',', ':'), allow_nan=False).encode('ascii')).hexdigest())
        self.reconcile_calls = []
        self.patch_failure = False
        self.readback_failure = False

    def _reconcile_kubectl(self, *args):
        self.assertEqual(os.environ.get('KUBECONFIG'), str(self.kubeconfig))
        self.reconcile_calls.append(args)
        if args[0] == 'patch':
            self.assertEqual(args[:5], ('patch', 'configmap', 'pgcat-provider-inventory', '--type=merge', '-p'))
            self.assertEqual(args[6:], ('-o', 'json'))
            self.assertEqual(len(args), 8)
            body = json.loads(args[5])
            self.assertEqual(set(body), {'metadata', 'data'})
            self.assertEqual(body['metadata'], {'uid': 'cm-uid', 'resourceVersion': self.cm['metadata']['resourceVersion']})
            self.assertEqual(set(body['data']), {'targets.json'})
            self.assertTrue((self.contract / 'reconcile-close.intent').is_file())
            if self.patch_failure:
                raise RuntimeError('private patch failure 10.0.0.3')
            self.cm['data'] = body['data']
            self.cm['metadata']['resourceVersion'] = str(int(self.cm['metadata']['resourceVersion']) + 1)
        else:
            self.assertEqual(args, ('get', 'configmap', 'pgcat-provider-inventory', '-o', 'json'))
            if self.readback_failure and len(self.reconcile_calls) > 1:
                raise RuntimeError('private readback failure 10.0.0.3')
        return copy.deepcopy(self.cm)

    def _reconcile(self, **kwargs):
        from pgcat_rehearsal_contract import reconcile_close
        return reconcile_close(self.contract, self.run_id, self.target,
            self.pending_binding, self.close_context, kubectl=self._reconcile_kubectl,
            clock=kwargs.get('clock', lambda: 220.5))

    def _reseal(self, name, value):
        raw = (json.dumps(value, sort_keys=True, allow_nan=False) + '\n').encode('ascii')
        (self.contract / name).write_bytes(raw)
        (self.contract / (name + '.sha256')).write_text(json.dumps({'sha256': hashlib.sha256(raw).hexdigest()}))

    def _rewrite_current_inventory(self, change):
        value = json.loads(self.inventory.read_text())
        change(value)
        raw = json.dumps(value, separators=(',', ':')).encode('ascii')
        self.inventory.write_bytes(raw)
        sidecar = json.loads((self.source / '000020.json').read_text())
        sidecar.update(inventorySha256=hashlib.sha256(raw).hexdigest(),
            inventorySemanticSha256=hashlib.sha256(json.dumps(value, sort_keys=True,
                separators=(',', ':')).encode('ascii')).hexdigest())
        (self.source / '000020.json').write_text(json.dumps(sidecar))

    def test_reconcile_exact_patch_readback_seal_and_old_pending_equal_live_rv(self):
        self._reconcile_fixture()
        self.assertGreater(220.5 - self.pending_binding['observedAt'], 30)
        self.assertEqual(self.cm['metadata']['resourceVersion'], self.pending_binding['resourceVersion'])
        with patch('pgcat_rehearsal_contract.proof.verified_frames', side_effect=AssertionError('capture must not be read')):
            self.assertEqual(self._reconcile(), dict(feedSession=self.session, sequence=20, resourceVersion='12'))
        self.assertEqual([call[0] for call in self.reconcile_calls], ['get', 'patch', 'get'])
        expected = [{'targets': [address + ':9930'], 'labels': dict(env='rdev.ali',
            db_proxy_code='DP00001', target_db_code='D00002', ecs_instance_id=node)}
            for node, address in self.originals]
        self.assertEqual(json.loads(self.cm['data']['targets.json']), expected)
        from pgcat_rehearsal_contract import _directory, _sealed_read
        fd = _directory(self.contract)
        try:
            sealed = _sealed_read(fd, 'reconcile-close.json')
        finally:
            os.close(fd)
        self.assertEqual(sealed['runId'], self.run_id)
        self.assertEqual(sealed['closeContext'], self.close_context)
        self.assertEqual(sealed['oldResourceVersion'], '11')
        self.assertEqual(sealed['resourceVersion'], '12')
        self.assertEqual(sealed['newTargetsSha256'], hashlib.sha256(json.dumps(expected,
            sort_keys=True, separators=(',', ':')).encode('ascii')).hexdigest())
        with self.assertRaises(ValueError):
            self._reconcile()
        self.assertEqual(sum(call[0] == 'patch' for call in self.reconcile_calls), 1)

    def test_reconcile_default_helper_uses_fixed_namespace_bounded_json_subprocess(self):
        self._reconcile_fixture()
        import pgcat_rehearsal_contract as contract
        def fake_run(argv, **kwargs):
            self.assertEqual(argv[:3], ['kubectl', '-n', 'raptor-test'])
            self.assertEqual(kwargs['timeout'], 3)
            self.assertEqual(set(kwargs), {'stdout', 'stderr', 'timeout'})
            value = self._reconcile_kubectl(*argv[3:])
            return type('Result', (), dict(returncode=0,
                stdout=json.dumps(value).encode('ascii'), stderr=b'private diagnostic'))()
        with patch.object(contract.worker_capture.subprocess, 'run', side_effect=fake_run):
            result = contract.reconcile_close(self.contract, self.run_id, self.target,
                self.pending_binding, self.close_context, clock=lambda: 220.5)
        self.assertEqual(result, dict(feedSession=self.session, sequence=20, resourceVersion='12'))
        self.assertEqual([call[0] for call in self.reconcile_calls], ['get', 'patch', 'get'])

    def test_reconcile_accepts_live_version_newer_than_pending_and_uses_live_precondition(self):
        self._reconcile_fixture()
        self.cm['metadata']['resourceVersion'] = '15'
        self.assertEqual(self._reconcile()['resourceVersion'], '16')
        body = json.loads(self.reconcile_calls[1][5])
        self.assertEqual(body['metadata']['resourceVersion'], '15')
        sealed = json.loads((self.contract / 'reconcile-close.json').read_text())
        self.assertEqual(sealed['oldResourceVersion'], '15')

    def test_reconcile_refuses_publication_advance_during_live_get_before_patch(self):
        self._reconcile_fixture()
        real = self._reconcile_kubectl
        def advance(*args):
            result = real(*args)
            self._advance_provider(21, 220.2)
            return result
        self._reconcile_kubectl = advance
        with self.assertRaises(ValueError): self._reconcile()
        self.assertEqual([call[0] for call in self.reconcile_calls], ['get'])
        self.assertFalse((self.contract / 'reconcile-close.intent').exists())

    def test_reconcile_refuses_summary_bundle_input_or_source_hash_mismatch(self):
        for case in ('summary', 'bundle-run', 'input', 'source'):
            with self.subTest(case=case):
                self.setUp(); self._reconcile_fixture()
                if case == 'summary':
                    self.pending_binding['newId'] = 'wrong'
                elif case == 'input':
                    value = json.loads((self.contract / 'pending-ready.json').read_text())
                    value['address'] = '10.0.0.99'
                    self._reseal('pending-ready.json', value)
                else:
                    bundle = json.loads((self.contract / 'pending-proof.bundle.json').read_text())
                    if case == 'bundle-run':
                        bundle['runId'] = str(uuid.uuid4())
                    else:
                        bundle['summary']['sourceSha256'] = '0' * 64
                        self.pending_binding['sourceSha256'] = '0' * 64
                    self._reseal('pending-proof.bundle.json', bundle)
                with self.assertRaises(ValueError): self._reconcile()
                self.assertEqual(self.reconcile_calls, [])

    def test_reconcile_refuses_context_types_sequence_time_digest_and_ack_order(self):
        changes = [dict(sequence=19), dict(sequence=21), dict(sequence=True),
            dict(observedAt=219.9), dict(observedAt=float('nan')),
            dict(scaleinAcknowledgedAt=220.0), dict(scaleinAcknowledgedAt=True),
            dict(scaleinRequestId='bad'), dict(feedSession=str(uuid.uuid4())),
            dict(pendingBindingSha256='0' * 64), dict(extra='forbidden')]
        for change in changes:
            with self.subTest(change=change):
                self.setUp(); self._reconcile_fixture(); self.close_context.update(change)
                with self.assertRaises(ValueError): self._reconcile()
                self.assertEqual(self.reconcile_calls, [])

    def test_reconcile_refuses_stale_future_or_nonfinite_current_clock(self):
        for stamp in (250.0, 218.9, float('nan'), True):
            with self.subTest(stamp=stamp):
                self.setUp(); self._reconcile_fixture()
                with self.assertRaises(ValueError): self._reconcile(clock=lambda: stamp)
                self.assertEqual(self.reconcile_calls, [])

    def test_reconcile_refuses_wrong_current_fleet_protection_or_backend(self):
        changes = [lambda v: v.update(desired=3),
            lambda v: v['memberStates'][0].update(lifecycle='InService'),
            lambda v: v['memberStates'][0].update(health='Unhealthy'),
            lambda v: v['backendStates'][0].update(weight=0),
            lambda v: v['backendStates'][0].update(status='Removing'),
            lambda v: v.update(healthyRegistered=['i-a'])]
        for change in changes:
            with self.subTest(change=change):
                self.setUp(); self._reconcile_fixture(); self._rewrite_current_inventory(change)
                with self.assertRaises(ValueError): self._reconcile()
                self.assertEqual(self.reconcile_calls, [])

    def test_reconcile_refuses_changed_configmap_identity_version_or_data(self):
        for case in ('uid', 'name', 'namespace', 'deletion', 'rv', 'rv-type',
                     'extra-key', 'address', 'labels', 'port', 'row-key', 'already-two', 'duplicate'):
            with self.subTest(case=case):
                self.setUp(); self._reconcile_fixture()
                meta = self.cm['metadata']; rows = json.loads(self.cm['data']['targets.json'])
                if case in ('uid', 'name', 'namespace'): meta[case] = 'changed'
                elif case == 'deletion': meta['deletionTimestamp'] = 'now'
                elif case == 'rv': meta['resourceVersion'] = '10'
                elif case == 'rv-type': meta['resourceVersion'] = 11
                elif case == 'extra-key': self.cm['data']['unexpected'] = 'x'
                elif case == 'address': rows[-1]['targets'] = ['10.0.0.99:9930']
                elif case == 'labels': rows[-1]['labels']['env'] = 'other'
                elif case == 'port': rows[-1]['targets'] = ['10.0.0.3:9931']
                elif case == 'row-key': rows[-1]['extra'] = 'x'
                elif case == 'already-two': rows = rows[:2]
                elif case == 'duplicate': rows[-1] = rows[0]
                self.cm['data']['targets.json'] = json.dumps(rows)
                with self.assertRaises(ValueError): self._reconcile()
                self.assertEqual([call[0] for call in self.reconcile_calls], ['get'])

    def test_reconcile_duplicate_intent_and_patch_or_readback_failure_never_retry(self):
        for case in ('intent', 'patch', 'readback'):
            with self.subTest(case=case):
                self.setUp(); self._reconcile_fixture()
                if case == 'intent':
                    path = self.contract / 'reconcile-close.intent'
                    path.write_text('{}'); path.chmod(0o600)
                self.patch_failure = case == 'patch'; self.readback_failure = case == 'readback'
                with self.assertRaises(ValueError): self._reconcile()
                self.assertFalse((self.contract / 'reconcile-close.json').exists())
                before = sum(call[0] == 'patch' for call in self.reconcile_calls)
                with self.assertRaises(ValueError): self._reconcile()
                self.assertEqual(sum(call[0] == 'patch' for call in self.reconcile_calls), before)

    def test_reconcile_readback_requires_newer_version_and_exact_complete_targets(self):
        for case in ('unchanged-rv', 'uid', 'data', 'extra-key'):
            with self.subTest(case=case):
                self.setUp(); self._reconcile_fixture()
                real = self._reconcile_kubectl
                def bad_readback(*args):
                    result = real(*args)
                    if args[0] == 'get' and len(self.reconcile_calls) == 3:
                        if case == 'unchanged-rv': result['metadata']['resourceVersion'] = '11'
                        elif case == 'uid': result['metadata']['uid'] = 'changed'
                        elif case == 'extra-key': result['data']['extra'] = 'forbidden'
                        else: result['data']['targets.json'] = '[]'
                    return result
                self._reconcile_kubectl = bad_readback
                with self.assertRaises(ValueError): self._reconcile()
                self.assertEqual(sum(call[0] == 'patch' for call in self.reconcile_calls), 1)
                self.assertFalse((self.contract / 'reconcile-close.json').exists())

    def _post_fixture(self):
        import shutil,pgcat_collector_control as collector,pgcat_rehearsal_contract as contract
        from segmented_capture import SegmentedCapture,canonical
        self._reconcile_fixture();self._reconcile()
        self._advance_provider(21,222.0)
        self.post_ticks=[10.0]
        self.post_hashes={}
        for key,name in collector.SOURCES.items():
            raw=(Path(__file__).parent/name).read_bytes()
            destination=self.run/name;destination.write_bytes(raw);destination.chmod(0o600)
            self.post_hashes[key]=hashlib.sha256(raw).hexdigest()
        self.post_identity=dict(pid=123,bootId='boot',startTicks=456,commandSha256='e'*64)
        job=self.run/'collector-job';job.mkdir(mode=0o700)
        def private(path,value):
            path.write_bytes(canonical(value));path.chmod(0o600)
        private(job/'intent.json',dict(runId=self.run_id,startedAt=123,profile='disabled-rehearsal'))
        private(job/'claim.json',dict(runId=self.run_id));private(job/'identity.json',self.post_identity)
        self.post_baseline=json.loads((self.contract/'baseline.json').read_text())
        context=dict(schemaVersion=1,runId=self.run_id,feedSession=self.session,
            providerRunContextSha256=hashlib.sha256(self.raw).hexdigest(),bundleSha256='b'*64,
            sourceHashes=self.post_hashes,baselineSha256=hashlib.sha256((self.contract/'baseline.json').read_bytes()).hexdigest(),
            originalRoster=self.post_baseline['roster'],deployment=self.post_baseline['deployment'],collectorIdentity=self.post_identity)
        shutil.rmtree(self.run/'rehearsal-capture')
        self.post_writer=SegmentedCapture(self.run/'rehearsal-capture',progress_context=context)
        def close_writer():
            if self.post_writer.progress_fd is not None:
                os.close(self.post_writer.progress_fd);self.post_writer.progress_fd=None
        self.addCleanup(close_writer)
        self.post_frames=[self._post_frame(222),self._post_frame(223)]
        for frame in self.post_frames:self.post_writer.append(frame)
        reconcile=json.loads((self.contract/'reconcile-close.json').read_text())
        binding=dict(feedSession=self.session,sequence=20,resourceVersion=reconcile['resourceVersion'],requestId=str(uuid.uuid4()),
            runId=self.run_id,scaleinRequestId=self.close_context['scaleinRequestId'],pendingBindingSha256=self.close_context['pendingBindingSha256'],
            sourceSha256=self.post_hashes['rehearsalContract'],sealedAt=221.0)
        inv=json.loads(self.inventory.read_text())
        receipt={key:inv[key] for key in ('feedSession','sequence','mode','desired','observedAt')}
        receipt['rosterSha256']=hashlib.sha256(canonical({key:inv[key] for key in ('members','healthyRegistered','memberStates','backendStates')})).hexdigest()
        self.post_context=dict(schemaVersion=1,requestId=str(uuid.uuid4()),stopProofSha256='a'*64,receipt=receipt,reconcileBinding=binding,collectorIdentity=self.post_identity)
        self.post_actions=[];self.post_boundary_change=None;self.post_final_change=None

    def _post_frame(self,t,final=False):
        from test_segmented_capture import witness_frame
        frame=witness_frame(t,n=int(t),finals=(0,1) if final else ())
        frame['inventory']=json.loads(self.inventory.read_text())
        frame['metrics']['nodes']=['i-a','i-b']
        for row in frame['metrics']['raw']:row['node']='i-'+row['node']
        for pod,row in zip(frame['pods'],self.post_baseline['roster']):
            pod.update({key:row[key] for key in ('name','uid','containerId')})
            pod['snapshot']['sample']['processId']=row['processId']
            for event in pod['snapshot']['events']:event['processId']=row['processId']
        return frame

    def _post_finish_io(self,path,expected_replicas=2,expected_roster=None,**kw):
        from segmented_capture import canonical,sha,publish_private,open_directory
        self.post_actions.append((expected_roster,kw))
        self.assertTrue((self.contract/'post-stop-finish.intent').exists())
        self.assertEqual(expected_roster,self.post_baseline['roster'])
        self.assertEqual(kw,dict(deadline=14.0,kubeconfig=str(self.kubeconfig)))
        fd=open_directory(path)
        try:
            request=dict(requestId='f'*32,roster=expected_roster)
            request_raw=publish_private(fd,'request.json',request);publish_private(fd,'ack.json',request)
            start=dict(schemaVersion=1,bindingSha256=self.post_writer.progress['bindingSha256'],requestSha256=sha(request_raw),requestId=request['requestId'],
                originalRosterSha256=sha(canonical(expected_roster)),startedMonotonic=10.1,deadlineMonotonic=15.1,firstSettlingFrame=None)
            final=self._post_frame(224,True)
            if self.post_final_change:self.post_final_change(final)
            self.post_writer.append(final);start['firstSettlingFrame']=self.post_writer.progress['latest']
            start_raw=publish_private(fd,'boundary-start.json',start)
            receipt=dict(requestId=request['requestId'],status='ACKNOWLEDGED',clients={r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in expected_roster})
            receipt_raw=publish_private(fd,'receipt.json',receipt)
            completed=dict(schemaVersion=1,bindingSha256=start['bindingSha256'],requestSha256=sha(request_raw),requestId=request['requestId'],
                startSha256=sha(start_raw),receiptSha256=sha(receipt_raw),finalDurableMonotonic=10.3,
                finals=[dict(uid=r['uid'],processId=r['processId'],frame=self.post_writer.final_refs[(r['uid'],r['processId'])]) for r in expected_roster],lastFrame=self.post_writer.progress['latest'])
            if self.post_boundary_change:self.post_boundary_change(start,completed,receipt)
            publish_private(fd,'boundary-completed.json',completed)
            self.post_ticks[0]=10.4
            return receipt
        finally:os.close(fd)

    def _post(self,**kw):
        import pgcat_rehearsal_contract as contract
        def read(*args,**options):
            self.assertEqual(options,dict(deadline=14.0,kubeconfig=str(self.kubeconfig)))
            return self._kubectl(*args)
        with patch('pgcat_collector_control.process_identity',return_value=self.post_identity),patch.object(contract.finish_clients,'finish',side_effect=kw.pop('finish',self._post_finish_io)),patch.object(contract.finish_clients,'live_roster',return_value=self.post_baseline['roster']),patch.object(contract.time,'monotonic',side_effect=lambda:self.post_ticks[0]):
            return contract.post_stop_finish(self.contract,self.run_id,self.target,self.post_context,source_hashes=self.post_hashes,
                kubectl=kw.pop('kubectl',read),clock=kw.pop('clock',lambda:224.5),monotonic=lambda:self.post_ticks[0],**kw)

    def test_post_exact_original_boundary_sealed_once_without_whole_archive(self):
        self._post_fixture()
        with patch('pgcat_rehearsal_contract.proof.verified_frames',side_effect=AssertionError('whole scan forbidden')):
            result=self._post()
        self.assertEqual(set(result),{'feedSession','sequence','boundaryRequestId','boundarySha256','evidenceSha256','sourceSha256'})
        self.assertEqual(result['sequence'],21);self.assertEqual(result['boundaryRequestId'],'f'*32)
        self.assertEqual(result['boundarySha256'],hashlib.sha256((self.boundary/'boundary-completed.json').read_bytes()).hexdigest())
        self.assertEqual(result['evidenceSha256'],hashlib.sha256((self.contract/'post-stop-finish.json').read_bytes()).hexdigest())
        with self.assertRaises(ValueError):self._post()
        self.assertEqual(len(self.post_actions),1)

    def test_post_context_source_provider_child_and_cm_refuse_before_intent(self):
        for case in ('extra','bool','request','proof','receipt-sequence','receipt-hash','receipt-mode','receipt-time','binding-source','binding-run','binding-session','binding-sequence','binding-rv','binding-pending','binding-scalein','binding-time','source-map','source-bytes','child','dead-child','exit','cm-uid','cm-rv','cm-data','provider'):
            with self.subTest(case=case):
                self.setUp();self._post_fixture()
                context=self.post_context
                if case=='extra':context['extra']=True
                elif case=='bool':context['schemaVersion']=True
                elif case=='request':context['requestId']='bad'
                elif case=='proof':context['stopProofSha256']='bad'
                elif case=='receipt-sequence':context['receipt']['sequence']=20
                elif case=='receipt-hash':context['receipt']['rosterSha256']='0'*64
                elif case=='receipt-mode':context['receipt']['mode']='live'
                elif case=='receipt-time':context['receipt']['observedAt']=221
                elif case.startswith('binding-'):
                    key={'source':'sourceSha256','run':'runId','session':'feedSession','sequence':'sequence','rv':'resourceVersion','pending':'pendingBindingSha256','scalein':'scaleinRequestId','time':'sealedAt'}[case[8:]]
                    context['reconcileBinding'][key]=222 if key=='sealedAt' else (21 if key=='sequence' else 'wrong')
                elif case=='source-map':self.post_hashes.pop('collectorControl')
                elif case=='source-bytes':(self.run/'finish_clients.py').write_text('changed')
                elif case=='child':context['collectorIdentity']=dict(self.post_identity,startTicks=457)
                elif case=='dead-child':self.post_identity=None
                elif case=='exit':(self.run/'collector-job'/'exit.json').write_text('{}')
                elif case=='cm-uid':self.cm['metadata']['uid']='changed'
                elif case=='cm-rv':self.cm['metadata']['resourceVersion']='13'
                elif case=='cm-data':self.cm['data']['targets.json']='[]'
                else:self.inventory.write_bytes(b'{}')
                with self.assertRaises(ValueError):self._post()
                self.assertFalse((self.contract/'post-stop-finish.intent').exists());self.assertEqual(self.post_actions,[])

    def test_post_sticky_flags_tail_sql_metrics_and_roster_refuse(self):
        from segmented_capture import publish_private
        for case in ('pollError','identityChanged','sqlFailure','skipped','invalidEvidence','gap','partial','tamper','nonadvance','adverse','metrics','stale-metrics','roster'):
            with self.subTest(case=case):
                self.setUp();self._post_fixture()
                progress=self.post_writer.progress
                if case in progress['flags']:
                    progress['flags'][case]=True;publish_private(self.post_writer.progress_fd,'progress.json',progress,replace=True)
                elif case=='gap':
                    progress['maxGapSeconds']=4;publish_private(self.post_writer.progress_fd,'progress.json',progress,replace=True)
                elif case in ('partial','tamper'):
                    path=self.run/'rehearsal-capture'/progress['latest']['basename']
                    if case=='partial':
                        with path.open('ab') as stream:stream.write(b'{')
                    else:
                        raw=path.read_bytes();path.write_bytes(raw.replace(b'"Success":',b'"Succexx":',1))
                else:
                    f=self._post_frame(224)
                    if case=='nonadvance':
                        for pod in f['pods']:
                            previous=self.post_frames[-1]['pods'][f['pods'].index(pod)]['snapshot']['sample']
                            pod['snapshot']['sample'].update({key:previous[key] for key in ('Success','Attempts','Scheduled')})
                    elif case=='adverse':f['pods'][0]['snapshot']['sample']['Failure']=1
                    elif case=='metrics':f['metrics']['raw']=[]
                    elif case=='stale-metrics':f['metrics']['observedAt']=200
                    else:f['pods'][0]['containerId']='changed'
                    self.post_writer.append(f)
                with self.assertRaises(ValueError):self._post()
                self.assertEqual(self.post_actions,[])

    def test_post_exact_boundary_hashes_finals_and_deadlines_refuse_consumed_intent(self):
        for case in ('request','start','receipt','binding','late','final','missing','timeout','uncertain'):
            with self.subTest(case=case):
                self.setUp();self._post_fixture()
                def alter(start,completed,receipt):
                    if case in ('request','start','receipt','binding'):completed[{'request':'requestSha256','start':'startSha256','receipt':'receiptSha256','binding':'bindingSha256'}[case]]='0'*64
                    elif case=='late':completed['finalDurableMonotonic']=15.2
                    elif case=='final':completed['finals'][0]['uid']='foreign'
                self.post_boundary_change=alter
                def helper(*a,**kw):
                    result=self._post_finish_io(*a,**kw)
                    if case=='missing':(self.boundary/'boundary-completed.json').unlink();self.post_ticks[0]=14
                    elif case=='timeout':self.post_ticks[0]=14
                    elif case=='uncertain':result['status']='INCONCLUSIVE'
                    return result
                with self.assertRaises(ValueError):self._post(finish=helper)
                self.assertTrue((self.contract/'post-stop-finish.intent').exists())
                self.assertFalse((self.contract/'post-stop-finish.json').exists())
                with self.assertRaises(ValueError):self._post()
                self.assertEqual(len(self.post_actions),1)

    def test_post_shared_budget_rejects_after_blocking_read_before_finish(self):
        self._post_fixture();calls=[]
        def slow(*a,**kw):
            calls.append(a);self.post_ticks[0]+=3
            return self._kubectl(*a)
        with self.assertRaises(ValueError):self._post(kubectl=slow)
        self.assertEqual(self.post_actions,[]);self.assertLessEqual(len(calls),2)

    def test_post_clean_progress_flag_cannot_hide_actual_adverse_sql_events(self):
        from segmented_capture import publish_private
        self._post_fixture()
        frame=self._post_frame(224)
        event=next(e for e in reversed(frame['pods'][0]['snapshot']['events']) if e['kind']=='operation')
        event['outcome']='failure'
        self.post_writer.append(frame)
        self.post_writer.progress['flags']={key:False for key in self.post_writer.progress['flags']}
        publish_private(self.post_writer.progress_fd,'progress.json',self.post_writer.progress,replace=True)
        with self.assertRaises(ValueError):self._post()
        self.assertEqual(self.post_actions,[])

    def test_post_final_counter_reset_or_wrong_final_provider_never_completes(self):
        for case in ('counter-reset','provider','deployment','sample-type','events','connected','scheduled'):
            with self.subTest(case=case):
                self.setUp();self._post_fixture()
                def mutate(frame):
                    sample=frame['pods'][0]['snapshot']['sample']
                    if case=='counter-reset':sample.update(Success=0,Attempts=0,Scheduled=0)
                    elif case=='provider':frame['inventory']['sequence']=20
                    elif case=='deployment':frame['deployment']['uid']='foreign'
                    elif case=='sample-type':sample['Skipped']=False
                    elif case=='events':next(e for e in reversed(frame['pods'][0]['snapshot']['events']) if e['kind']=='operation')['outcome']='failure'
                    elif case=='connected':frame['pods'][0]['snapshot']['connected']=1
                    else:sample['Scheduled']+=1
                self.post_final_change=mutate
                with self.assertRaises(ValueError):self._post()
                self.assertTrue((self.contract/'post-stop-finish.intent').exists());self.assertFalse((self.contract/'post-stop-finish.json').exists())

    def test_post_changed_sealed_input_after_finish_refuses_durable_success(self):
        self._post_fixture()
        def change(*a,**kw):
            result=self._post_finish_io(*a,**kw)
            value=json.loads((self.contract/'baseline.json').read_text());value['roster'][0]['processId']='changed'
            self._reseal('baseline.json',value)
            return result
        with self.assertRaises(ValueError):self._post(finish=change)
        self.assertTrue((self.contract/'post-stop-finish.intent').exists());self.assertFalse((self.contract/'post-stop-finish.json').exists())

    def test_post_reconcile_independent_session_and_intent_must_match(self):
        for case in ('close-session','intent','provider-sequence','pending-run'):
            with self.subTest(case=case):
                self.setUp();self._post_fixture()
                value=json.loads((self.contract/'reconcile-close.json').read_text())
                if case=='close-session':value['closeContext']['feedSession']=str(uuid.uuid4())
                elif case=='provider-sequence':value['providerSource']['sequence']=19
                elif case=='pending-run':value['pendingBinding']['runId']=str(uuid.uuid4())
                else:(self.contract/'reconcile-close.intent').write_text('{}')
                self._reseal('reconcile-close.json',value)
                with self.assertRaises(ValueError):self._post()
                self.assertEqual(self.post_actions,[])

    def test_post_original_child_context_types_are_strict(self):
        self._post_fixture()
        self.post_identity['pid']=1
        path=self.run/'collector-job'/'identity.json';path.write_text(json.dumps(self.post_identity))
        context=json.loads((self.run/'rehearsal-capture'/'progress-binding.json').read_text())
        context['collectorIdentity']['pid']=1
        # Rebind a legitimate retained PID1, then submit a bool-valued assertion.
        from segmented_capture import publish_private,canonical,sha
        raw=publish_private(self.post_writer.progress_fd,'progress-binding.json',context,replace=True)
        self.post_writer.progress['bindingSha256']=sha(raw)
        publish_private(self.post_writer.progress_fd,'progress.json',self.post_writer.progress,replace=True)
        self.post_context['collectorIdentity']=dict(self.post_identity,pid=True)
        with self.assertRaises(ValueError):self._post()
        self.assertEqual(self.post_actions,[])

    def test_post_latest_boundary_frame_must_still_show_both_original_finals(self):
        self._post_fixture()
        def mutate(start,completed,receipt):
            from segmented_capture import read_frame
            frame=read_frame(self.post_writer.progress_fd,completed['lastFrame'])
            frame['at']=225
            frame['pods'][0]['snapshot']['final']=False
            self.post_writer.append(frame)
            completed['lastFrame']=self.post_writer.progress['latest']
        self.post_boundary_change=mutate
        with self.assertRaises(ValueError):self._post()
        self.assertTrue((self.contract/'post-stop-finish.intent').exists());self.assertFalse((self.contract/'post-stop-finish.json').exists())

    def test_post_freshness_is_checked_after_durable_result_write(self):
        self._post_fixture()
        import pgcat_rehearsal_contract as contract
        real=contract._sealed_write;clock=[224.5]
        def delayed(fd,name,value):
            real(fd,name,value)
            if name=='post-stop-finish.json':clock[0]=252.0
        with patch.object(contract,'_sealed_write',side_effect=delayed):
            with self.assertRaises(ValueError):self._post(clock=lambda:clock[0])
        self.assertTrue((self.contract/'post-stop-finish.intent').exists())

    def test_post_clean_progress_cannot_hide_adverse_tail_after_final_markers(self):
        self._post_fixture()
        def mutate(start,completed,receipt):
            from segmented_capture import read_frame,publish_private
            frame=read_frame(self.post_writer.progress_fd,completed['lastFrame']);frame['at']=225
            event=next(e for e in reversed(frame['pods'][0]['snapshot']['events']) if e['kind']=='operation')
            event.update(outcome='failure',sequence=event['sequence']+1,at=225)
            self.post_writer.append(frame)
            self.assertTrue(any(e.get('outcome')=='failure' for e in read_frame(self.post_writer.progress_fd,self.post_writer.progress['latest'])['pods'][0]['snapshot']['events']))
            self.post_writer.progress['flags']={key:False for key in self.post_writer.progress['flags']}
            publish_private(self.post_writer.progress_fd,'progress.json',self.post_writer.progress,replace=True)
            completed['lastFrame']=self.post_writer.progress['latest']
        self.post_boundary_change=mutate
        with self.assertRaises(ValueError):self._post()
        self.assertTrue((self.contract/'post-stop-finish.intent').exists());self.assertFalse((self.contract/'post-stop-finish.json').exists())

    def test_post_original_collector_cap_requires_five_seconds_at_finish_dispatch(self):
        import pgcat_rehearsal_contract as contract
        for case in ('enough','exact','short','spent-before-dispatch'):
            with self.subTest(case=case):
                self.setUp();self._post_fixture()
                path=self.run/'collector-job'/'intent.json';intent=json.loads(path.read_text())
                intent['startedAt']=-5171 if case=='short' else -5170
                path.write_text(json.dumps(intent))
                current=[225.0 if case=='exact' else 224.5];actual=contract._write_once
                def write(fd,name,value):
                    actual(fd,name,value)
                    if case=='spent-before-dispatch' and name=='post-stop-finish.intent':current[0]=225.2
                with patch.object(contract,'_write_once',side_effect=write):
                    if case in ('enough','exact'):self.assertEqual(self._post(clock=lambda:current[0])['sequence'],21)
                    else:
                        with self.assertRaises(ValueError):self._post(clock=lambda:current[0])
                        self.assertEqual(self.post_actions,[])

    def test_post_boundary_directory_replacement_during_readback_refuses(self):
        self._post_fixture()
        import segmented_capture as witness
        actual=witness.read_private;changed=[]
        def read(fd,name,*args):
            raw=actual(fd,name,*args)
            if name=='boundary-completed.json' and not changed:
                changed.append(True);self.boundary.rename(self.run/'moved-boundary');self.boundary.mkdir(mode=0o700)
            return raw
        with patch.object(witness,'read_private',side_effect=read):
            with self.assertRaises(ValueError):self._post()
        self.assertTrue((self.contract/'post-stop-finish.intent').exists());self.assertFalse((self.contract/'post-stop-finish.json').exists())

    def test_post_each_private_and_external_phase_refuses_exhausted_budget(self):
        import pgcat_rehearsal_contract as contract,segmented_capture as witness
        for phase in ('source','baseline','pending','reconcile','provider','child','progress','configmap','deployment','pods','finish','boundary','frame','final-frame','result'):
            with self.subTest(phase=phase):
                self.setUp();self._post_fixture()
                actual_read=contract._read_at;actual_progress=witness.read_progress;actual_frame=witness.read_frame;actual_boundary=witness.read_private;actual_seal=contract._sealed_write
                names={'source':'pgcat_rehearsal_worker.py','baseline':'baseline.json','pending':'pending-proof.bundle.json','reconcile':'reconcile-close.json','provider':'run-context.json','child':'identity.json'}
                def read(fd,name,limit):
                    value=actual_read(fd,name,limit)
                    if names.get(phase)==name:self.post_ticks[0]=14
                    return value
                def progress(*a):
                    value=actual_progress(*a)
                    if phase=='progress':self.post_ticks[0]=14
                    return value
                def frame(*a):
                    value=actual_frame(*a)
                    if phase=='frame' or (phase=='final-frame' and self.post_actions):self.post_ticks[0]=14
                    return value
                def boundary(fd,name,*a):
                    value=actual_boundary(fd,name,*a)
                    if phase=='boundary' and name=='boundary-completed.json':self.post_ticks[0]=14
                    return value
                def seal(fd,name,value):
                    actual_seal(fd,name,value)
                    if phase=='result' and name=='post-stop-finish.json':self.post_ticks[0]=14
                def kube(*a,**kw):
                    value=self._kubectl(*a)
                    if a[1]==phase:self.post_ticks[0]=14
                    return value
                def finish(*a,**kw):
                    value=self._post_finish_io(*a,**kw)
                    if phase=='finish':self.post_ticks[0]=14
                    return value
                with patch.object(contract,'_read_at',side_effect=read),patch.object(witness,'read_progress',side_effect=progress),patch.object(witness,'read_frame',side_effect=frame),patch.object(witness,'read_private',side_effect=boundary),patch.object(contract,'_sealed_write',side_effect=seal):
                    with self.assertRaises(ValueError):self._post(kubectl=kube,finish=finish)
                if phase in ('finish','boundary','final-frame','result'):self.assertTrue((self.contract/'post-stop-finish.intent').exists())
                else:self.assertEqual(self.post_actions,[])

    def test_post_source_authority_uses_private_snapshots_without_direct_self_path_read(self):
        self._post_fixture()
        import pgcat_rehearsal_contract as contract
        original=Path.read_bytes
        def read(path):
            if path == Path(contract.__file__):raise AssertionError('unguarded direct module path read')
            return original(path)
        with patch.object(Path,'read_bytes',autospec=True,side_effect=read):
            self.assertEqual(self._post()['sourceSha256'],self.post_hashes['rehearsalContract'])

    def test_post_each_retained_frame_binds_full_original_identity_even_with_cleared_flags(self):
        from segmented_capture import publish_private,read_progress
        for position in ('previous','latest'):
            for field in ('name','containerId'):
                with self.subTest(position=position,field=field):
                    self.setUp();self._post_fixture()
                    frames=[self._post_frame(224),self._post_frame(225)]
                    frames[0 if position=='previous' else 1]['pods'][0][field]='valid-replacement'
                    for frame in frames:self.post_writer.append(frame)
                    self.assertTrue(self.post_writer.progress['flags']['identityChanged'])
                    self.post_writer.progress['flags']={key:False for key in self.post_writer.progress['flags']}
                    publish_private(self.post_writer.progress_fd,'progress.json',self.post_writer.progress,replace=True)
                    progress,previous,latest=read_progress(self.run/'rehearsal-capture',self.post_writer.progress_context)
                    self.assertFalse(any(progress['flags'].values()))
                    retained=previous if position=='previous' else latest
                    self.assertEqual(retained['pods'][0][field],'valid-replacement')
                    self.assertEqual(retained['pods'][0]['uid'],self.post_baseline['roster'][0]['uid'])
                    self.assertEqual(retained['pods'][0]['snapshot']['sample']['processId'],self.post_baseline['roster'][0]['processId'])
                    with self.assertRaises(ValueError):self._post()
                    self.assertFalse((self.contract/'post-stop-finish.intent').exists())
                    self.assertEqual(self.post_actions,[])


if __name__ == '__main__':
    unittest.main()
