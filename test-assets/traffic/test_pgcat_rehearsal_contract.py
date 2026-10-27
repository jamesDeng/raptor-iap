"""Local safety tests for fresh Pending evidence production."""
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
import uuid

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


if __name__ == '__main__':
    unittest.main()
