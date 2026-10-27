"""Offline contract tests for the worker's Pending proof boundary."""
import copy
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
import uuid
from unittest.mock import patch

from capture_window import clean
from test_capture_window import transition_frame

import pgcat_pending_proof_worker as proof


SESSION = '00000000-0000-4000-8000-000000000001'


def write_private(path, value):
    path.write_text(json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n')
    path.chmod(0o600)


def seal(path, value):
    write_private(path, value)
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    write_private(Path(str(path) + '.sha256'), {'sha256': digest})


class PendingProofWorkerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.contract = self.root / 'contract'
        self.capture = self.root / 'capture'
        self.contract.mkdir(mode=0o700)
        self.capture.mkdir(mode=0o700)
        self.kubeconfig = self.root / 'kubeconfig'
        write_private(self.kubeconfig, {'test': True})
        self.inventory_path = self.root / 'inventory.json'
        self.now = int(time.time())
        self.run = str(uuid.uuid4())
        self.originals = [dict(id='a', address='10.0.0.1'),
                          dict(id='b', address='10.0.0.2')]
        self.roster = [dict(name='client-a', uid='pod', containerId='container-a', processId='process'),
                       dict(name='client-b', uid='pod-b', containerId='container-b', processId='process-b')]
        self.frames = []
        for t, members, pending in [(0, ('a', 'b'), ()),
                                    (1, ('a', 'b'), ()),
                                    (2, ('a', 'b', 'c'), ('c',)),
                                    (3, ('a', 'b', 'c'), ('c',)),
                                    (4, ('a', 'b', 'c'), ('c',))]:
            frame = transition_frame(t, members, ('a', 'b'), pending,
                                     metrics=('a', 'b', 'c') if t >= 2 else ('a', 'b'),
                                     desired=3 if t >= 1 else 2)
            frame['inventory']['mode'] = 'disabled-rehearsal'
            frame['inventory']['memberStates'][-1]['addressKnown'] = True
            other = copy.deepcopy(frame['pods'][0])
            other['uid'] = 'pod-b'
            other['snapshot']['sample']['processId'] = 'process-b'
            for event in other['snapshot']['events']:
                event['processId'] = 'process-b'
            frame['pods'].append(other)
            frame['expectedReplicas'] = 2
            frame['phase'] = 'measurement'
            offset = self.now - 5
            for section, key in [(frame, 'at'), (frame['inventory'], 'observedAt'),
                                 (frame['metrics'], 'observedAt'), (frame['metrics'], 'sourceAt')]:
                section[key] += offset
            for row in frame['metrics']['raw']:
                row['sourceAt'] += offset
            for pod in frame['pods']:
                pod['snapshot']['sample']['at'] += offset
                for event in pod['snapshot']['events']:
                    event['at'] += offset
            self.frames.append(clean(frame))
        with (self.capture / 'segment-0001.jsonl').open('w') as stream:
            for frame in self.frames:
                stream.write(json.dumps(frame, sort_keys=True) + '\n')
        (self.capture / 'segment-0001.jsonl').chmod(0o600)
        write_private(self.inventory_path, self.frames[-1]['inventory'])
        self.baseline = dict(version=1, purpose='disabled-rehearsal-post-stop',
            mode='disabled-rehearsal', originals=self.originals,
            configMap=dict(name='pgcat-provider-inventory', namespace='raptor-test',
                           uid='cm-uid', resourceVersion='10'),
            paths=dict(captureDir=str(self.capture), boundaryDir=str(self.root / 'boundary'),
                       kubeconfig=str(self.kubeconfig)),
            roster=self.roster, metricGroups=[dict(node=n, pool='test', user='poc_app')
                                             for n in ('a', 'b')],
            receipt=dict(feedSession=SESSION, sequence=1, mode='disabled-rehearsal',
                         desired=2, observedAt=self.frames[0]['inventory']['observedAt']),
            inventoryPath=str(self.inventory_path), baselineAt=self.frames[0]['at'] - 1,
            deployment=dict(name='rdev-test-client-client', namespace='raptor-test',
                            uid='dep-uid', generation=1))
        digest = lambda value: hashlib.sha256(json.dumps(value, sort_keys=True,
            allow_nan=False).encode('utf-8')).hexdigest()
        self.pre = dict(firstAt=self.frames[0]['at'], latestAt=self.frames[1]['at'],
            sequence=2, feedSession=SESSION,
            firstFrameSha256=digest(self.frames[0]),
            latestFrameSha256=digest(self.frames[1]))
        self.pending = dict(newId='c', frameIndex=3,
            frameSha256=digest(self.frames[3]),
            at=self.frames[3]['at'], sequence=4,
            observedAt=self.frames[3]['inventory']['observedAt'],
            sourceAt=self.frames[3]['metrics']['sourceAt'], address='10.0.0.3',
            configMapResourceVersion='11',
            configMapReadbackAt=self.frames[2]['at'] + .1,
            postReadbackSequence=4,
            postReadbackObservedAt=self.frames[3]['inventory']['observedAt'])
        (self.root / 'boundary').mkdir(mode=0o700)
        self.cm = dict(metadata=dict(name='pgcat-provider-inventory', namespace='raptor-test',
                                    uid='cm-uid', resourceVersion='11'),
            data={'targets.json':json.dumps([dict(targets=[row['address'] + ':9930'],
                labels=dict(env='rdev.ali', db_proxy_code='DP00001',
                            target_db_code='D00002', ecs_instance_id=row['id']))
                for row in self.originals + [dict(id='c', address='10.0.0.3')]])})
        self.deployment = dict(metadata=dict(name='rdev-test-client-client',
            namespace='raptor-test', uid='dep-uid', generation=1),
            spec=dict(replicas=2), status=dict(observedGeneration=1, replicas=2,
            updatedReplicas=2, readyReplicas=2, availableReplicas=2))
        self.pods = [dict(metadata=dict(name=row['name'], namespace='raptor-test',
                                         uid=row['uid']),
            status=dict(phase='Running', conditions=[dict(type='Ready', status='True')],
                        containerStatuses=[dict(ready=True, state={'running': {}},
                                                containerID=row['containerId'])]))
            for row in self.roster]
        self.save()

    def save(self):
        for name, value in [('baseline', self.baseline), ('pre-scale', self.pre),
                            ('pending-ready', self.pending)]:
            seal(self.contract / (name + '.json'), value)

    def kubectl(self, *args):
        if args[1] == 'configmap':
            return copy.deepcopy(self.cm)
        if args[1] == 'deployment':
            return copy.deepcopy(self.deployment)
        if args[1] == 'pods':
            return {'items': copy.deepcopy(self.pods)}
        self.fail('unexpected kubectl read')

    def produce(self):
        return proof.produce(self.contract, self.run, kubectl=self.kubectl,
                             live_roster=lambda: copy.deepcopy(self.roster), now=lambda: self.now)

    def test_produces_private_bundle_and_strict_address_free_summary(self):
        summary = self.produce()
        self.assertEqual(set(summary), {'runId', 'feedSession', 'newId',
            'originalInstanceIds', 'sequence', 'observedAt', 'resourceVersion',
            'clientRosterSha256', 'evidenceSha256', 'sourceSha256'})
        self.assertEqual(summary['newId'], 'c')
        self.assertEqual(summary['sequence'], 4)
        self.assertEqual(summary['observedAt'], self.frames[3]['inventory']['observedAt'])
        self.assertNotIn('10.0.0.', json.dumps(summary))
        bundle = self.contract / 'pending-proof.bundle.json'
        self.assertEqual(bundle.stat().st_mode & 0o777, 0o600)
        self.assertIn('10.0.0.3', bundle.read_text())
        self.assertTrue((self.contract / 'pending-proof.bundle.json.sha256').exists())
        saved = json.loads(bundle.read_text())
        prefix = saved['collectorSegmentPrefixes']['segment-0001.jsonl']
        captured = (self.capture / 'segment-0001.jsonl').read_bytes()
        self.assertEqual(prefix['size'], len(captured))
        self.assertEqual(prefix['sha256'], hashlib.sha256(captured).hexdigest())

    def test_rejects_new_backend_seen_before_pending_binding(self):
        self.frames[2]['inventory']['backendStates'].append(dict(id='c', status='Adding',
            weight=100, listenerHealth='Initial'))
        self._rewrite_frames()
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_rejects_client_restart_and_incomplete_new_metric(self):
        self.roster[0]['processId'] = 'restarted'
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_rejects_live_client_not_ready(self):
        self.pods[0]['status']['containerStatuses'][0]['ready'] = False
        with self.assertRaises(proof.Refused):
            self.produce()
        self.roster[0]['processId'] = 'process'
        self.frames[-1]['metrics']['raw'].pop()
        self._rewrite_frames()
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_rejects_changed_configmap_and_unsealed_source(self):
        rows = json.loads(self.cm['data']['targets.json'])
        rows[-1]['targets'] = ['10.0.0.9:9930']
        self.cm['data']['targets.json'] = json.dumps(rows)
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_rejects_original_losing_protection_in_collector_history(self):
        self.frames[2]['inventory']['memberStates'][0]['lifecycle'] = 'InService'
        self._rewrite_frames()
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_rejects_new_member_before_pre_scale_frame(self):
        self.frames[1]['inventory']['members'].append('c')
        self.frames[1]['inventory']['memberStates'].append(dict(
            id='c', lifecycle='Pending', health='Healthy', addressKnown=True))
        self._rewrite_frames()
        with self.assertRaises(proof.Refused):
            self.produce()

    def test_rejects_provider_change_during_live_readback(self):
        def racing_kubectl(*args):
            result = self.kubectl(*args)
            if args[1] == 'pods':
                changed = copy.deepcopy(self.frames[-1]['inventory'])
                changed['sequence'] += 1
                changed['observedAt'] += 1
                changed['backendStates'].append(dict(id='c', status='Adding',
                    weight=100, listenerHealth='Initial'))
                write_private(self.inventory_path, changed)
            return result
        with self.assertRaises(proof.Refused):
            proof.produce(self.contract, self.run, kubectl=racing_kubectl,
                          live_roster=lambda: copy.deepcopy(self.roster),
                          now=lambda: self.now)

    def test_cli_stdout_contains_only_summary_and_refusal_is_silent(self):
        output = io.StringIO()
        with patch.object(proof.sys, 'argv', ['pending-proof', str(self.contract), self.run]), \
                patch.object(proof.worker_capture, 'kubectl', side_effect=self.kubectl), \
                patch.object(proof.finish_clients, 'live_roster',
                             side_effect=lambda: copy.deepcopy(self.roster)), \
                patch.object(proof.time, 'time', return_value=self.now), \
                contextlib.redirect_stdout(output):
            self.assertEqual(proof.main(), 0)
        parsed = json.loads(output.getvalue())
        self.assertEqual(set(parsed), {'runId', 'feedSession', 'newId',
            'originalInstanceIds', 'sequence', 'observedAt', 'resourceVersion',
            'clientRosterSha256', 'evidenceSha256', 'sourceSha256'})
        self.assertNotIn('10.0.0.', output.getvalue())
        output = io.StringIO()
        with patch.object(proof.sys, 'argv', ['pending-proof', str(self.contract), self.run]), \
                contextlib.redirect_stdout(output):
            self.assertEqual(proof.main(), 1)
        self.assertEqual(output.getvalue(), '')
        self.cm['data']['targets.json'] = json.dumps([dict(targets=[row['address'] + ':9930'],
            labels=dict(env='rdev.ali', db_proxy_code='DP00001', target_db_code='D00002',
                        ecs_instance_id=row['id'])) for row in self.originals +
                        [dict(id='c', address='10.0.0.3')]])
        (self.contract / 'pending-ready.json.sha256').unlink()
        with self.assertRaises(proof.Refused):
            self.produce()

    def _rewrite_frames(self):
        with (self.capture / 'segment-0001.jsonl').open('w') as stream:
            for frame in self.frames:
                stream.write(json.dumps(frame, sort_keys=True) + '\n')
        independent_digest = lambda value: hashlib.sha256(json.dumps(value,
            sort_keys=True, allow_nan=False).encode('utf-8')).hexdigest()
        self.pre['firstFrameSha256'] = independent_digest(self.frames[0])
        self.pre['latestFrameSha256'] = independent_digest(self.frames[1])
        self.pending['frameSha256'] = independent_digest(self.frames[3])
        self.save()


if __name__ == '__main__':
    unittest.main()
