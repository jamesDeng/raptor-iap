"""Offline native rollout evidence; real committed segments, no authority fixtures."""
import copy
import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from test_segmented_capture import witness_context, witness_frame
from test_capture_window import frame
import worker_capture

if importlib.util.find_spec('pgcat_native_evidence'):
    import pgcat_native_evidence as native
else:
    native = None

DIGEST = 'sha256:' + 'a' * 64


def context(kind='native-replacement'):
    value = witness_context()
    value.update(schemaVersion=2, mode='full-replacement-direct-v2', executionKind=kind, planSha256='f'*64)
    value['initialRoster'] = value.pop('originalRoster')
    value['sourceHashes']['nativeEvidence'] = 'e'*64
    value['deployment'].update(initialReplicaSetUid='rs-old', initialTemplateHash='hash-old', imageDigest=DIGEST)
    return value


def ownership(successor=False, terminating=False):
    return dict(deploymentUid='deployment', replicaSetUid='rs-new' if successor else 'rs-old',
                templateHash='hash-new' if successor else 'hash-old', imageDigest=DIGEST, terminating=terminating)


def rolling(t):
    value = witness_frame(t, n=min(t, 2), finals=(0,1) if t >= 2 else ())
    value['phase'] = 'measurement'
    value['inventory']['mode'] = 'full-replacement-direct-v2'
    for pod in value['pods']:
        pod['ownership'] = ownership(terminating=t >= 2)
        if t >= 2:pod['snapshot']['sample']['at'] = 2
    if t >= 1:
        for i in range(2):
            pod = copy.deepcopy(frame(t, t-1)['pods'][0])
            pod.update(name='new-'+str(i), uid='new-uid-'+str(i), containerId='new-container-'+str(i), ready=t >= 2,
                       ownership=ownership(True))
            pod['snapshot']['sample']['processId'] = 'new-process-'+str(i)
            for event in pod['snapshot']['events']:
                event['processId'] = 'new-process-'+str(i);event['at'] += 1
            value['pods'].append(pod)
        value['deployment'].update(generation=2, observedGeneration=1 if t == 1 else 2, ready=2)
    if t >= 3:value['pods'] = value['pods'][2:]
    return value


def kubernetes(t=4):
    value = rolling(t)
    dep = dict(metadata=dict(name='rdev-test-client-client', namespace='raptor-test', uid='deployment', generation=2),
               spec=dict(replicas=2, selector=dict(matchLabels={'app':'client'}), template=dict(spec=dict(containers=[dict(name='client', image='repo@'+DIGEST)]))),
               status=dict(observedGeneration=2, readyReplicas=2))
    rss = []
    for uid, template in [('rs-old','hash-old'),('rs-new','hash-new')]:
        rss.append(dict(metadata=dict(uid=uid, namespace='raptor-test', labels={'pod-template-hash':template},
                                     ownerReferences=[dict(kind='Deployment', name=dep['metadata']['name'], uid='deployment', controller=True)]),
                        spec=dict(template=dict(metadata=dict(labels={'pod-template-hash':template}), spec=dict(containers=[dict(name='client', image='repo@'+DIGEST)])))))
    pods = []
    for pod in value['pods']:
        own = pod['ownership']
        pods.append(dict(metadata=dict(name=pod['name'], namespace='raptor-test', uid=pod['uid'], labels={'app':'client','pod-template-hash':own['templateHash']},
                                       ownerReferences=[dict(kind='ReplicaSet',uid=own['replicaSetUid'],controller=True)]),
                         spec=dict(containers=[dict(name='client',image='repo@'+DIGEST)]),
                         status=dict(phase='Running',podIP='127.0.0.1',conditions=[dict(type='Ready',status='True')],
                                     containerStatuses=[dict(name='client',containerID=pod['containerId'],imageID='repo@'+DIGEST)])))
    roster = [dict(name=p['name'],uid=p['uid'],containerId=p['containerId'],processId=p['snapshot']['sample']['processId']) for p in value['pods']]
    return dict(deployment=dep, replicaSets=rss, pods=pods, roster=roster)


class NativeContextCohort(unittest.TestCase):
    def setUp(self):self.assertIsNotNone(native, 'native rolling evidence interface is missing')

    def test_strict_context_detached_and_exact(self):
        source = context();value = native.validate_context(source)
        source['initialRoster'][0]['name'] = 'mutated'
        self.assertEqual(value['initialRoster'][0]['name'], 'client-0')
        for key, bad in [('schemaVersion',True),('schemaVersion',1),('mode','direct-v2'),('executionKind','other'),('planSha256','A'*64)]:
            with self.subTest(key=key,bad=bad),self.assertRaises(ValueError):native.validate_context(dict(context(), **{key:bad}))
        for key in context():
            value = context();del value[key]
            with self.subTest(missing=key),self.assertRaises(ValueError):native.validate_context(value)
        for generation in (True,0,2**53):
            value = context();value['deployment']['generation'] = generation
            with self.assertRaises(ValueError):native.validate_context(value)
        value=context();value['sourceHashes'].pop('nativeEvidence')
        with self.assertRaises(ValueError):native.validate_context(value)
        with self.assertRaises(ValueError):native.validate_context(dict(context(), extra=True))

    def test_native_owner_chain_and_image_required(self):
        self.assertTrue(callable(getattr(worker_capture, 'native_owner_evidence', None)), 'authoritative owner collector missing')
        live = kubernetes()
        result = worker_capture.native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context())
        self.assertEqual(result, {'new-uid-0':ownership(True), 'new-uid-1':ownership(True)})
        for case in ('foreign','multi','no-owner','image','template','unknown','initial-template','rs-owner','container','deployment'):
            live = kubernetes();pod=live['pods'][0];rs=live['replicaSets'][1]
            if case=='foreign':rs['metadata']['ownerReferences'][0]['uid']='foreign'
            if case=='multi':pod['metadata']['ownerReferences'].append(copy.deepcopy(pod['metadata']['ownerReferences'][0]))
            if case=='no-owner':pod['metadata'].pop('ownerReferences')
            if case=='image':pod['status']['containerStatuses'][0]['imageID']='repo@sha256:'+'b'*64
            if case=='template':pod['metadata']['labels']['pod-template-hash']='wrong'
            if case=='unknown':pod['metadata']['ownerReferences'][0]['uid']='unknown'
            if case=='initial-template':live['replicaSets'][0]['metadata']['labels']['pod-template-hash']='wrong'
            if case=='rs-owner':rs['metadata']['ownerReferences'][0]['controller']=False
            if case=='container':pod['spec']['containers'].append(dict(name='sidecar',image='other'))
            if case=='deployment':live['deployment']['metadata']['name']='other'
            with self.subTest(case=case),self.assertRaises((ValueError,KeyError)):worker_capture.native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context())

    def test_dormant_old_image_history_does_not_change_current_owners(self):
        for stage in (0,4):
            with self.subTest(stage=stage):
                live=kubernetes(stage)
                history=copy.deepcopy(live['replicaSets'][0])
                history['metadata'].update(uid='rs-history',labels={'pod-template-hash':'hash-history'})
                history['spec']['template']['metadata']['labels']['pod-template-hash']='hash-history'
                history['spec']['template']['spec']['containers'][0]['image']='repo@sha256:'+'b'*64
                history['spec']['replicas']=0;history['status']={'replicas':0}
                live['replicaSets'].append(history)
                result=worker_capture.native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context())
                expected={('uid-' if stage==0 else 'new-uid-')+str(i):ownership(stage!=0) for i in range(2)}
                self.assertEqual(result,expected)

    def test_wrong_image_relevant_replicasets_and_invalid_history_still_reject(self):
        for case in ('initial','owning','desired','observed','unknown-count','namespace','duplicate','owner','template'):
            live=kubernetes();history=copy.deepcopy(live['replicaSets'][1])
            history['metadata'].update(uid='rs-history',labels={'pod-template-hash':'hash-history'})
            history['spec']['template']['metadata']['labels']['pod-template-hash']='hash-history'
            history['spec']['replicas']=0;history['status']={'replicas':0}
            live['replicaSets'].append(history)
            target=live['replicaSets'][0 if case=='initial' else 1] if case in ('initial','owning') else history
            target['spec']['template']['spec']['containers'][0]['image']='repo@sha256:'+'b'*64
            target['spec']['replicas']=0;target['status']={'replicas':0}
            if case=='desired':history['spec']['replicas']=1
            if case=='observed':history['status']['replicas']=1
            if case=='unknown-count':history['status'].pop('replicas')
            if case=='namespace':history['metadata']['namespace']='other'
            if case=='duplicate':history['metadata']['uid']='rs-old'
            if case=='owner':history['metadata']['ownerReferences'][0]['uid']='other'
            if case=='template':history['spec']['template']['metadata']['labels']['pod-template-hash']='other'
            with self.subTest(case=case),self.assertRaises(ValueError):
                worker_capture.native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context())

    def test_native_rejects_reincarnation_extra_cohort_and_generation(self):
        for case in ('process','container','fifth','second-rs','generation','regression','disappearance','events'):
            cohort = native.NativeCohort(context());cohort.observe(rolling(0));cohort.observe(rolling(1))
            value = rolling(2)
            if case=='process':value['pods'][0]['snapshot']['sample']['processId']='replacement'
            if case=='container':value['pods'][0]['containerId']='replacement'
            if case=='fifth':
                pod=copy.deepcopy(value['pods'][-1]);pod.update(name='fifth',uid='fifth',containerId='fifth');pod['snapshot']['sample']['processId']='fifth';value['pods'].append(pod)
            if case=='second-rs':value['pods'][-1]['ownership']['replicaSetUid']='third'
            if case=='generation':value['deployment']['generation']=3
            if case=='regression':value['deployment']['generation']=1
            if case=='disappearance':value['pods']=value['pods'][2:]
            if case=='events':value['pods'][-1]['snapshot']['events']=[]
            cohort.observe(value)
            with self.subTest(case=case):self.assertTrue(any(cohort.snapshot()['flags'].values()))

    def test_normal_overlap_status_lag_and_termination_are_not_identity_changes(self):
        cohort=native.NativeCohort(context())
        for t in range(3):cohort.observe(rolling(t))
        snapshot=cohort.snapshot()
        self.assertFalse(any(snapshot['flags'].values()), snapshot['flags'])
        self.assertEqual(len(snapshot['cohort']),4)
        self.assertEqual(snapshot['observedGeneration'],None)
        self.assertTrue(all(row['firstFrame'] is None and row['finalFrame'] is None for row in snapshot['cohort']))


def operation(kind='native-replacement'):
    value=dict(schemaVersion=1, kind=kind, planSha256='f'*64, restartGeneration=2, ingressSha256='a'*64)
    if kind=='native-replacement':
        value.update(requestId='33333333-3333-4333-8333-333333333333',definitionSha256='b'*64,
                     skillsCommit='c'*40,model='fixture-model',resultSha256='d'*64,eventsSha256='e'*64)
    else:value['restartReceiptSha256']='b'*64
    return value


class NativeTerminal(unittest.TestCase):
    def setUp(self):
        self.assertTrue(callable(getattr(native,'seal_terminal_clients',None)), 'terminal sealing interface is missing')
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(os.path.realpath(self.temp.name));self.directory=self.root/'capture'

    def writer(self, kind='native-replacement', mutate=None):
        from segmented_capture import SegmentedCapture, publish_private
        self.context=context(kind);self.operation=operation(kind)
        writer=SegmentedCapture(self.directory,progress_context=self.context,max_segment_seconds=2)
        for t in range(5):
            value=rolling(t)
            if mutate:mutate(t,value)
            writer.append(value)
        publish_private(writer.progress_fd,'native-operation-binding.json',self.operation)
        return writer

    def seal(self, now=4, live=None):
        return native.seal_terminal_clients(self.directory,self.context,self.operation,lambda: live or kubernetes(),now=lambda:now)

    def test_terminal_pair_is_late_bound_to_committed_cohort_and_predecessor_finals(self):
        from segmented_capture import read_progress, read_frame
        writer=self.writer();result=self.seal();progress,previous,latest=read_progress(self.directory,self.context)
        self.assertEqual([row['uid'] for row in result['roster']],['new-uid-0','new-uid-1'])
        self.assertEqual(progress['schemaVersion'],2)
        self.assertEqual(progress['nativeCohort']['observedGeneration'],2)
        self.assertFalse(any(progress['flags'].values()), progress['flags'])
        rows=progress['nativeCohort']['cohort'];self.assertEqual(len(rows),4)
        self.assertEqual(len(writer.final_refs),2)
        for row in rows:
            first=read_frame(writer.progress_fd,row['firstFrame'])
            self.assertIn(row['identity']['uid'],[p['uid'] for p in first['pods']])
        sealed=__import__('json').loads((self.directory/'terminal-clients.json').read_text())
        self.assertEqual([row['frame']['at'] for row in sealed['predecessorFinals']],[2,2])
        self.assertTrue(all(row['finalFrame'] is None for row in rows if row['identity']['uid'].startswith('new-')))
        self.assertEqual(native.read_terminal_clients(self.directory,self.context,self.operation),result)
        with self.assertRaises((ValueError,OSError)):self.seal()

    def test_disjoint_operation_binding_kinds(self):
        for kind,other in [('native-replacement','disabled-proof'),('disabled-proof','native-replacement')]:
            self.assertEqual(native.validate_operation_binding(operation(kind),context(kind)),operation(kind))
            with self.assertRaises(ValueError):native.validate_operation_binding(operation(other),context(kind))
        for key,value in [('schemaVersion',True),('restartGeneration',True),('restartGeneration',3),('planSha256','e'*64),('requestId','wrong'),('skillsCommit','C'*40),('model','')]:
            with self.subTest(key=key),self.assertRaises(ValueError):native.validate_operation_binding(dict(operation(),**{key:value}),context())
        with self.assertRaises(ValueError):native.validate_operation_binding(dict(operation('disabled-proof'),resultSha256='e'*64),context('disabled-proof'))
        writer=self.writer('disabled-proof');self.seal()
        with self.assertRaises(ValueError):native.read_terminal_clients(self.directory,context(),operation())

    def test_disabled_restart_receipt_finishes_successor_pair_without_native_result(self):
        from segmented_capture import publish_private,sha,read_private
        writer=self.writer('disabled-proof')
        receipt=dict(schemaVersion=1,deploymentUid='deployment',generation=2,operation='operator-restart')
        raw=publish_private(writer.progress_fd,'operator-restart-receipt.json',receipt)
        self.operation['restartReceiptSha256']=sha(raw)
        (self.directory/'native-operation-binding.json').unlink()
        publish_private(writer.progress_fd,'native-operation-binding.json',self.operation)
        result=self.seal()
        self.assertEqual(self.operation['restartReceiptSha256'],sha(read_private(writer.progress_fd,'operator-restart-receipt.json')))
        self.assertEqual([r['uid'] for r in result['roster']],['new-uid-0','new-uid-1'])
        self.assertNotIn('requestId',self.operation)
        self.assertNotIn('resultSha256',self.operation)
        self.assertEqual(native.read_terminal_clients(self.directory,self.context,self.operation),result)

    def test_historical_terminal_getter_after_tail_advance_and_hold(self):
        from segmented_capture import publish_private
        writer=self.writer();result=self.seal()
        for t in range(5,130):writer.append(rolling(t))
        self.assertEqual(native.read_terminal_clients(self.directory,self.context,self.operation),result)
        path=self.directory/'segment-0001.jsonl';saved=path.read_bytes();path.write_bytes(saved.replace(b'"at":0',b'"at":9',1))
        with self.assertRaises(ValueError):native.read_terminal_clients(self.directory,self.context,self.operation)
        path.write_bytes(saved)
        altered=dict(self.operation,eventsSha256='0'*64)
        (self.directory/'native-operation-binding.json').unlink();publish_private(writer.progress_fd,'native-operation-binding.json',altered)
        with self.assertRaises(ValueError):native.read_terminal_clients(self.directory,self.context,altered)

    def test_terminal_rejects_lost_history_stale_live_mismatch_and_file_substitution(self):
        # Every case reaches real read_frame/read_private; no validator stubs.
        cases=('missing-final','startup-history','generation','stale','live-roster','live-owner','not-ready','inode','truncated','forged-ref','binding','nonconsecutive','poll-error','skipped')
        for case in cases:
            with self.subTest(case=case),tempfile.TemporaryDirectory(dir=str(self.root)) as folder:
                self.directory=Path(folder)/'capture'
                def mutate(t,value):
                    if case=='missing-final' and t==2:
                        for p in value['pods'][:2]:p['snapshot'].update(final=False,connected=4)
                    if case=='startup-history' and t==2:value['pods'][-1]['snapshot']['events']=[]
                    if case=='generation' and t==4:value['deployment']['generation']=1
                    if case=='not-ready' and t==4:value['pods'][0]['ready']=False
                    if case=='skipped' and t==0:value['pods'][0]['snapshot']['sample']['Skipped']=1
                writer=self.writer(mutate=mutate);live=kubernetes()
                if case=='poll-error':writer.note_poll_error(4)
                if case=='live-roster':live['roster'][0]['processId']='wrong'
                if case=='live-owner':live['pods'][0]['metadata']['ownerReferences'][0]['uid']='unknown'
                if case in ('inode','truncated'):
                    path=self.directory/'segment-0001.jsonl';raw=path.read_bytes()
                    if case=='inode':path.rename(self.directory/'old');path.write_bytes(raw);path.chmod(0o600)
                    else:path.write_bytes(raw[:-1])
                if case=='binding':(self.directory/'progress-binding.json').write_text('{}')
                if case in ('forged-ref','nonconsecutive'):
                    import json
                    path=self.directory/'progress.json';progress=json.loads(path.read_text())
                    if case=='forged-ref':progress['nativeCohort']['cohort'][0]['firstFrame']['sha256']='0'*64
                    else:progress['previous']=progress['nativeCohort']['cohort'][0]['firstFrame']
                    path.write_text(json.dumps(progress))
                with self.assertRaises((ValueError,OSError,KeyError)):self.seal(now=8 if case=='stale' else 4,live=live)


def starting_row(index=0,container=None):
    return dict(name='new-'+str(index),uid='new-uid-'+str(index),deploymentUid='deployment',
                replicaSetUid='rs-new',templateHash='hash-new',containerId=container)


class NativeStartup(unittest.TestCase):
    def test_pending_runtime_refusal_then_full_history_is_retained(self):
        from segmented_capture import SegmentedCapture,read_progress
        with tempfile.TemporaryDirectory() as directory:
            path=Path(os.path.realpath(directory))/'capture';writer=SegmentedCapture(path,progress_context=context())
            writer.append(rolling(0))
            pending=rolling(1);pending['pods']=pending['pods'][:2];pending['startingPods']=[starting_row(i) for i in range(2)]
            writer.append(pending)
            progress,_,latest=read_progress(path,context())
            self.assertEqual(progress['nativeCohort'].get('startingPods'),pending['startingPods'])
            self.assertEqual(latest.get('startingPods'),pending['startingPods'])
            runtime=copy.deepcopy(pending);runtime['at']=1.5
            runtime['startingPods']=[starting_row(i,'new-container-'+str(i)) for i in range(2)]
            writer.append(runtime)
            writer.append(rolling(2));writer.append(rolling(3));writer.append(rolling(4))
            progress,_,_=read_progress(path,context())
            self.assertFalse(any(progress['flags'].values()),progress['flags'])
            self.assertEqual(progress['nativeCohort']['startingPods'],[])
            self.assertEqual(len(progress['nativeCohort']['cohort']),4)

    def test_startup_loss_extra_container_change_or_covered_process_loss_is_sticky(self):
        for case in ('lost','extra','container','observed','initial','traffic','foreign'):
            with self.subTest(case=case):
                cohort=native.NativeCohort(context());cohort.observe(rolling(0))
                pending=rolling(1);pending['pods']=pending['pods'][:2];pending['startingPods']=[starting_row(0,'new-container-0')]
                if case=='initial':pending['startingPods'][0]['uid']=context()['initialRoster'][0]['uid']
                if case=='traffic':pending['pods']=pending['pods'][:1]
                if case=='foreign':pending['startingPods'][0]['deploymentUid']='foreign'
                cohort.observe(pending)
                next_frame=rolling(2)
                if case=='lost':next_frame['pods']=next_frame['pods'][:2]
                if case=='extra':next_frame['startingPods']=[dict(starting_row(),uid='fifth')]
                if case=='container':next_frame['pods'][2]['containerId']='changed'
                if case=='observed':
                    next_frame=rolling(1);cohort.observe(next_frame);next_frame=copy.deepcopy(pending)
                cohort.observe(next_frame)
                self.assertTrue(any(cohort.snapshot()['flags'].values()),case)

class NativeFullGraph(unittest.TestCase):
    def test_complete_direct_graph_retains_all_finals_and_ordinary_assessment(self):
        from test_direct_replacement_mode import direct_frames
        from segmented_capture import SegmentedCapture,publish_private
        with tempfile.TemporaryDirectory() as directory:
            path=Path(os.path.realpath(directory))/'capture';writer=SegmentedCapture(path,progress_context=context(),max_segment_seconds=2)
            for t,provider in enumerate(direct_frames()):
                value=rolling(t);value['inventory']=provider['inventory'];value['metrics']=provider['metrics']
                value['inventory']['feedSession']=context()['feedSession']
                if t==9:
                    value['phase']='settling'
                    for pod in value['pods']:pod['snapshot'].update(final=True,connected=0)
                writer.append(value)
                if t==8:
                    publish_private(writer.progress_fd,'native-operation-binding.json',operation())
                    sealed=native.seal_terminal_clients(path,context(),operation(),lambda:kubernetes(),now=lambda:8)
            self.assertEqual(len(writer.final_refs),4)
            result=writer.finish(completed=True,cancelled=False,errors=[])
            self.assertEqual(result['status'],'PASS',result)
            self.assertEqual(native.read_terminal_clients(path,context(),operation()),sealed)

class NativeMeasurementCoverage(unittest.TestCase):
    def test_one_active_successor_without_startup_rows_is_sticky_and_cannot_seal(self):
        from segmented_capture import SegmentedCapture,publish_private,read_progress
        with tempfile.TemporaryDirectory() as directory:
            path=Path(os.path.realpath(directory))/'capture'
            writer=SegmentedCapture(path,progress_context=context())
            for t in range(6):
                value=rolling(t)
                if t in (1,2):
                    value['pods']=[pod for pod in value['pods'] if pod['uid']!='new-uid-1']
                writer.append(value)
            publish_private(writer.progress_fd,'native-operation-binding.json',operation())
            progress,_,_=read_progress(path,context())
            # The later full sequence-1 history must not cure t=2's coverage gap.
            with self.subTest(invariant='sticky measurement coverage'):
                self.assertTrue(progress['flags']['invalidEvidence'])
                self.assertTrue(progress['nativeCohort']['flags']['invalidEvidence'])
            with self.subTest(invariant='terminal refuses historical coverage gap'):
                with self.assertRaises(ValueError):
                    native.seal_terminal_clients(path,context(),operation(),lambda:kubernetes(),now=lambda:5)
            self.assertFalse((path/'terminal-clients.json').exists())
