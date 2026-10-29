"""Offline ingress contracts; never invoke cloud, HTTP or Kubernetes clients."""
import copy
import importlib.util
import unittest

from test_pgcat_native_evidence import context
from pgcat_lane_actions import SOURCE_NAMES

ingress = None
if importlib.util.find_spec('pgcat_native_ingress'):
    import pgcat_native_ingress as ingress


def plan(kind='native-replacement'):
    value = dict(schemaVersion=1, mode='full-replacement-direct-v2', executionKind=kind,
        runId=context()['runId'], target=dict(accountId='1360282071200743', regionId='ap-southeast-1',
        workerId='i-t4nj1bitmcz7cuoch82h', scalingGroupId='asg-t4necb4dfmu6o3z2rn0g',
        nlbServerGroupId='sgp-xs5s42tihtl6ja2a67', originalInstanceIds=['a','b']),
        application=context()['deployment'], sourceHashes={k:'c'*64 for k in SOURCE_NAMES|{'nativeController','nativeIngress','nativeEvidence'}},
        baselineSha256='d'*64, plannedOperation='db-proxy.replace-nodes' if kind=='native-replacement' else 'disabled-client-restart',
        deadline=dict(mutationCutoff='2026-10-29T13:30:00Z',closureDeadline='2026-10-29T14:30:00Z',captureMaxSeconds=5400),
        serviceBindings=dict(namespace='raptor-system'))
    for name, port, ip in [('gateway',8874,'10.0.0.10'),('openapi',8872,'10.0.0.11')]:
        value['serviceBindings'][name]=dict(deploymentName=name,deploymentUid=name+'-uid',containerName=name,
            serviceName=name,serviceUid=name+'-service',clusterIP=ip,port=port)
    if kind=='native-replacement':
        value['plannedInput']=dict(type='agent',providerId='codex',model='gpt-6-luna',object=dict(kind='db-proxy',code='DP00001'),
            envCode='rdev.ali',operations=[dict(name='db-proxy.replace-nodes',parameters=dict(mode='replacement'))],
            skills=dict(tag='skills-v0.4.0',commitSha='a'*40))
        value['controllerScope']=dict(envCode='rdev.ali',proxyCode='DP00001',groupId=value['target']['scalingGroupId'],
            serverGroupId=value['target']['nlbServerGroupId'],targetDbCode='D00002',application=dict(envCode='rdev.ali',
            appCode='client',clusterId='cluster',namespace='raptor-test',name='rdev-test-client-client',uid='deployment'))
    return value


class PlanTests(unittest.TestCase):
    def setUp(self):self.assertIsNotNone(ingress, 'native ingress interface missing')

    def test_exact_disjoint_plan_and_detached_projection(self):
        for kind in ('native-replacement','disabled-proof'):
            source=plan(kind);self.assertEqual(ingress.validate_plan(source),source)
            for key in source:
                changed=copy.deepcopy(source);del changed[key]
                with self.subTest(kind=kind,key=key),self.assertRaises(ValueError):ingress.validate_plan(changed)
            with self.assertRaises(ValueError):ingress.validate_plan(dict(source,unexpected=True))
        for field in ('connectionVersion','definitionSha256','application'):
            source=plan();source['plannedInput'][field]=1
            with self.assertRaises(ValueError):ingress.validate_plan(source)
        source=plan('disabled-proof');source['plannedInput']=plan()['plannedInput']
        with self.assertRaises(ValueError):ingress.validate_plan(source)
        source=plan();result=ingress.validate_plan(source);source['plannedInput']['model']='changed'
        self.assertEqual(result['plannedInput']['model'],'gpt-6-luna')

    def test_capture_cap_and_scope_are_exact(self):
        for cap,valid in [(74,False),(75,True),(5400,True),(5401,False),(True,False)]:
            source=plan();source['deadline']['captureMaxSeconds']=cap
            if valid:self.assertEqual(ingress.validate_plan(source),source)
            else:
                with self.assertRaises(ValueError):ingress.validate_plan(source)
        for mutate in (lambda p:p['controllerScope'].update(groupId='wrong'),
                       lambda p:p['controllerScope']['application'].update(uid='wrong'),
                       lambda p:p['plannedInput']['operations'][0]['parameters'].update(profile='direct-v2'),
                       lambda p:p['sourceHashes'].pop('nativeIngress'),
                       lambda p:p['serviceBindings']['gateway'].update(clusterIP='127.0.0.1')):
            source=plan();mutate(source)
            with self.assertRaises(ValueError):ingress.validate_plan(source)



# All transport responses and credentials below are synthetic in-memory fixtures.
import base64
import datetime
import os
import tempfile
from pathlib import Path
from segmented_capture import canonical, sha, SegmentedCapture, read_progress
from test_pgcat_native_evidence import ownership, kubernetes, rolling
from test_segmented_capture import witness_frame

REQUEST='33333333-3333-4333-8333-333333333333'
APPROVAL='44444444-4444-4444-8444-444444444444'
START=1793232000


def write(path,value):
    path.write_bytes(canonical(value));path.chmod(0o600)


def file_record(path):
    s=path.stat()
    return dict(device=s.st_dev,inode=s.st_ino,uid=s.st_uid,mode=s.st_mode&0o777,nlink=s.st_nlink,size=s.st_size,sha256=sha(path.read_bytes()))


def timestamp(value):
    return datetime.datetime.fromtimestamp(value,datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')


class Fixture:
    def __init__(self, root, kind='native-replacement', string_times=False):
        self.string_times=string_times
        self.root=root;self.run=root/'run';self.run.mkdir(mode=0o700)
        self.source=root/'contract'/'provider-source';self.source.mkdir(mode=0o700,parents=True);self.source.parent.chmod(0o700)
        self.now=START+1;self.plan=plan(kind);self.calls=[];self.patches=[]
        self.frame=witness_frame(0);self.frame['inventory']['mode']='full-replacement-direct-v2'
        self.frame['inventory']['feedSession']=context()['feedSession']
        self.frame['inventory']['memberStates']=[dict(id=n,lifecycle='Protected',health='Healthy',addressKnown=True) for n in ('a','b')]
        for p in self.frame['pods']:p['ownership']=ownership()
        self.baseline=dict(schemaVersion=1,mode=self.plan['mode'],executionKind=kind,runId=self.plan['runId'],observedAt=START-1,
            targetSha256=sha(canonical(self.plan['target'])),originals=[dict(id='a',address='10.1.0.1'),dict(id='b',address='10.1.0.2')],
            configMap=dict(name='pgcat-provider-inventory',namespace='raptor-test',uid='cm-uid',resourceVersion='1'),
            roster=context()['initialRoster'],metricGroups=[dict(node=n,pool='test',user='poc_app') for n in ('a','b')],
            deployment=self.plan['application'],fleet={k:self.frame['inventory'][k] for k in ('desired','members','healthyRegistered','memberStates','backendStates')},
            gateway=dict(deploymentUid='gateway-uid',replacementEnabled=False))
        write(self.run/'native-baseline.json',self.baseline);self.plan['baselineSha256']=sha((self.run/'native-baseline.json').read_bytes())
        write(self.run/'native-plan.json',self.plan)
        self.feed=dict(runId=self.plan['runId'],feedId='55555555-5555-4555-8555-555555555555',pid=9999,bindingSha256='9'*64,argvSha256='8'*64)
        write(self.run/'native-feed-binding.json',self.feed)
        self.provider=dict(schemaVersion=1,feedSession=context()['feedSession'],runContext=dict(schemaVersion=1,runId=self.plan['runId'],
            supervisorId='66666666-6666-4666-8666-666666666666',nonce='77777777-7777-4777-8777-777777777777',
            targetSha256=self.baseline['targetSha256'],bundleSha256=sha(canonical(self.plan['sourceHashes']))))
        write(self.source/'run-context.json',self.provider)
        self.context=context(kind);self.context.update(planSha256=sha(canonical(self.plan)),baselineSha256=self.plan['baselineSha256'],
            providerRunContextSha256=sha((self.source/'run-context.json').read_bytes()),bundleSha256=self.provider['runContext']['bundleSha256'])
        self.context['sourceHashes']={k:self.plan['sourceHashes'][k] for k in self.context['sourceHashes']}
        write(self.run/'native-context.json',self.context)
        self.writer=SegmentedCapture(self.run/'native-capture',progress_context=self.context)
        self.append(0);self.append(1)
        self.guardian=dict(pid=456,bootId='boot',startTicks=789,commandSha256='b'*64)
        self.runtime=dict(schemaVersion=1,runId=self.plan['runId'],observedAt=START+1,secretUid='config-uid',secretResourceVersion='1',
            deploymentUid='gateway-uid',podUid='gateway-pod',generation=1,imageDigest='sha256:'+'e'*64,
            provenance=dict(kind='read-only-inspect'),verification=dict(replacementEnabled=False,generation=1,ready=True,podUID='gateway-pod'))
        write(self.run/'native-gateway-runtime.json',self.runtime)
        progress,_,_=read_progress(self.run/'native-capture',self.context)
        self.paths=dict(plan=self.run/'native-plan.json',baseline=self.run/'native-baseline.json',context=self.run/'native-context.json',
            feedBinding=self.run/'native-feed-binding.json',providerRunContext=self.source/'run-context.json',
            progressBinding=self.run/'native-capture'/'progress-binding.json',gatewayRuntime=self.run/'native-gateway-runtime.json')
        self.ready=dict(schemaVersion=1,mode=self.plan['mode'],executionKind=kind,runId=self.plan['runId'],planSha256=self.context['planSha256'],
            baselineSha256=self.plan['baselineSha256'],contextSha256=sha((self.run/'native-context.json').read_bytes()),
            progressBindingSha256=sha((self.run/'native-capture'/'progress-binding.json').read_bytes()),feedBindingSha256=sha((self.run/'native-feed-binding.json').read_bytes()),
            feedSession=self.context['feedSession'],collectorIdentity=self.context['collectorIdentity'],guardianIdentity=self.guardian,
            frames=dict(previous=progress['previous'],latest=progress['latest']),readyAt=START+1,sources={k:file_record(p) for k,p in self.paths.items()})
        write(self.run/'native-ready.json',self.ready)
        self.admitted=dict(localRequestId='88888888-8888-4888-8888-888888888888',runId=self.plan['runId'],planSha256=self.context['planSha256'],
            readySha256=sha((self.run/'native-ready.json').read_bytes()),feedBindingSha256=self.ready['feedBindingSha256'],deadlineMonotonic=1.0)
        self.objects={};self.secret_reads=[]
        for name in ('gateway','openapi'):
            row=self.plan['serviceBindings'][name]
            self.objects[('Service','raptor-system',name)]=dict(metadata=dict(name=name,namespace='raptor-system',uid=row['serviceUid'],resourceVersion='1'),
                spec=dict(type='ClusterIP',clusterIP=row['clusterIP'],ports=[dict(port=row['port'],protocol='TCP')]))
            self.objects[('Deployment','raptor-system',name)]=dict(metadata=dict(name=name,namespace='raptor-system',uid=row['deploymentUid'],resourceVersion='1',generation=1),
                spec=dict(template=dict(spec=dict(containers=[dict(name=name,envFrom=[dict(secretRef=dict(name=name+'-auth'))])],
                volumes=[dict(name='live-config',secret=dict(secretName='gateway-live-config'))] if name=='gateway' else []))))
            self.objects[('Secret','raptor-system',name+'-auth')]=dict(metadata=dict(uid=name+'-auth-uid',resourceVersion='1'),
                data={k:base64.b64encode(v.encode()).decode() for k,v in [('SERVICE_USERNAME',name+'-username'),('SERVICE_PASSWORD',name+'-private-password')]})
        self.objects[('Secret','raptor-system','gateway-live-config')]=dict(metadata=dict(uid='config-uid',resourceVersion='1'),
            data={'config.json':base64.b64encode(b'{"replacementEnabled":false}').decode()})
        self.live=kubernetes(0);self.live['deployment']['metadata'].update(generation=1,resourceVersion='1')
        self.live['deployment']['status']['observedGeneration']=1
        self.live['deployment']['spec']['strategy']=dict(type='RollingUpdate',rollingUpdate=dict(maxSurge=1,maxUnavailable=0))
        self.objects[('Deployment','raptor-test','rdev-test-client-client')]=self.live['deployment']
        self.objects[('ConfigMap','raptor-test','pgcat-provider-inventory')]=dict(metadata=self.baseline['configMap'],data={'targets.json':__import__('json').dumps([
            dict(targets=[r['address']+':9930'],labels=dict(env='rdev.ali',db_proxy_code='DP00001',target_db_code='D00002',ecs_instance_id=r['id'])) for r in self.baseline['originals']])})
        definition=dict(copy.deepcopy(self.plan.get('plannedInput',{})),connectionVersion=2)
        self.api_context=dict(requestId=REQUEST,definition=definition,skills=definition.get('skills'),definitionSha256='d'*64,environment=dict(secret='must-discard'))
        self.execution=dict(requestId=REQUEST,attemptId='attempt-1',input=copy.deepcopy(self.api_context),appliedSkills=definition.get('skills'),
            status='running',runtimeMode='live',stage='running',cleanup='pending',recoveryNeeded=False)
        self.events=[dict(eventId='event-1',requestId=REQUEST,attemptId='attempt-1',sequence=1,occurredAt=timestamp(START+1),
            kind='tool_start',summary='tool started',details=dict(tool='mcp__raptor__request_get',status='started',runtimeSequence=1),evidenceMode='live')]
        self.override=None

    def append(self,n,frame=None):
        value=copy.deepcopy(frame or self.frame);value['at']=START+n;value['inventory'].update(observedAt=START+n,sequence=n+1)
        value['metrics'].update(observedAt=START+n,sourceAt=START+n)
        for row in value['metrics']['raw']:row['sourceAt']=START+n
        for pod in value['pods']:
            if pod['uid'] in {r['uid'] for r in self.context['initialRoster']}:
                source=witness_frame(n,n)['pods'][0]['snapshot']
                sample=copy.deepcopy(source['sample']);sample['processId']=pod['snapshot']['sample']['processId']
                pod['snapshot']['sample']=sample;pod['snapshot']['events']=copy.deepcopy(source['events'])
                for event in pod['snapshot']['events']:event['processId']=sample['processId']
            sample=pod['snapshot']['sample'];sample['at']=START+n
            for event in pod['snapshot']['events']:
                if event['at']<START:event['at']+=START
        if self.string_times:value['at']=timestamp(START+n)
        self.writer.append(value)
        write(self.run/'provider-inventory.json',value['inventory'])
        raw=(self.run/'provider-inventory.json').read_bytes();rc=self.provider['runContext']
        write(self.source/('%06d.json'%(n+1)),dict(schemaVersion=1,runId=rc['runId'],supervisorId=rc['supervisorId'],contextNonce=rc['nonce'],
            targetSha256=rc['targetSha256'],bundleSha256=rc['bundleSha256'],feedSession=self.context['feedSession'],sequence=n+1,
            observedAt=START+n,inventorySha256=sha(raw),inventorySemanticSha256=sha(canonical(value['inventory'])),
            privateAddresses={node:'10.1.0.'+str(i+1) for i,node in enumerate(value['inventory']['members'])}))
        self.now=START+n

    def admission(self,action):
        assert action in ('native-ingress','native-disabled-restart');return copy.deepcopy(self.admitted)
    def probe_process(self,pid):
        for row in (self.guardian,self.context['collectorIdentity']):
            if row['pid']==pid:return copy.deepcopy(row)
        raise AssertionError('Mac PID must not be probed on worker')
    def live_clients(self):return copy.deepcopy(self.live)
    def gateway_disabled(self):return dict(copy.deepcopy(self.runtime),observedAt=self.now)
    def kube(self,kind,namespace,name=None):
        self.calls.append((kind,namespace,name))
        if kind=='Secret':self.secret_reads.append(name)
        return copy.deepcopy(self.objects[(kind,namespace,name)])
    def http(self,origin,path,credentials):
        self.calls.append((origin,path))
        assert origin in ('http://10.0.0.10:8874','http://10.0.0.11:8872')
        expected='gateway' if ':8874' in origin else 'openapi'
        assert credentials==(expected+'-username',expected+'-private-password')
        if self.override:return self.override(origin,path)
        if path.endswith('/context'):value=dict(data=self.api_context)
        elif '/events?' in path:
            after=int(path.split('after=')[1]);rows=[e for e in self.events if e['sequence']>after][:100]
            value=dict(data=rows,nextAfter=rows[-1]['sequence'] if rows else after,hasMore=bool(rows and rows[-1]['sequence']<self.events[-1]['sequence']))
        elif path=='/v1/requests/'+REQUEST:value=dict(data=self.execution)
        else:raise AssertionError('unsupported route')
        return canonical(value)
    def read(self):return ingress.read_ingress(self.run,dict(requestId=REQUEST,approvalIds=[]) if self.plan['executionKind']=='native-replacement' else {},
        gate=self,kube_read=self.kube,http_get=self.http,now=lambda:self.now)


class IngressTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve();self.root.chmod(0o700)
        self.f=Fixture(self.root)
    def test_native_ingress_kind_and_identity_are_bound(self):
        record=ingress.seal_ingress(self.f.run,self.f.read())['record']
        self.assertEqual(record['context']['definition']['connectionVersion'],2)
        self.assertNotIn('environment',record['execution']['input'])
        self.assertNotIn('must-discard',str(record))
        self.assertIsNone(record['diagnostic'])
        self.assertFalse((self.f.run/'native-operation-binding.json').exists())
        for field,value in [('requestId','wrong'),('definitionSha256','e'*64)]:
            old=copy.deepcopy(self.f.execution);self.f.execution['input'][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):self.f.read()
            self.f.execution=old
        self.f.execution['input']['definition']['connectionVersion']=3
        with self.assertRaises(ValueError):self.f.read()
    def test_sealed_chain_bytes_and_ready_identity_cannot_be_replaced(self):
        for key in ('plan','baseline','context','feedBinding','progressBinding','providerRunContext','gatewayRuntime'):
            path=self.f.paths[key];original=path.read_bytes();path.write_bytes(original+b' ')
            with self.subTest(key=key),self.assertRaises(ValueError):self.f.read()
            path.write_bytes(original)
        projection=self.f.read();ready=self.f.run/'native-ready.json';raw=ready.read_bytes();ready.unlink();ready.write_bytes(raw);ready.chmod(0o600)
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,projection)
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,dict(projection))
    def test_native_ready_requires_committed_frames_and_live_processes(self):
        self.f.admitted['readySha256']='e'*64
        with self.assertRaises(ValueError):self.f.read()
        self.f.admitted['readySha256']=sha((self.f.run/'native-ready.json').read_bytes())
        self.f.probe_process=lambda pid:None
        with self.assertRaises(ValueError):self.f.read()
    def test_ingress_fixed_origins_and_secret_resolution(self):
        self.f.read()
        obj=self.f.objects[('Service','raptor-system','gateway')];obj['spec']['type']='ExternalName'
        with self.assertRaises(ValueError):self.f.read()
        obj['spec']['type']='ClusterIP'
        dep=self.f.objects[('Deployment','raptor-system','gateway')]
        dep['spec']['template']['spec']['containers'][0]['envFrom'].append(dict(secretRef=dict(name='gateway-auth')))
        with self.assertRaises(ValueError):self.f.read()
    def test_ingress_response_bound_error_redaction_and_pages(self):
        for body in (b'x'*(2*1024*1024+1),b'{"error":"gateway-private-password"}',b'{"data":1,"data":2}'):
            self.f.override=lambda o,p:body
            with self.assertRaises(ValueError) as caught:self.f.read()
            self.assertNotIn('password',str(caught.exception))
        self.f.override=None
        self.f.events=[dict(copy.deepcopy(self.f.events[0]),eventId='e-'+str(n),sequence=n,details=dict(tool='mcp__raptor__request_get',status='started',runtimeSequence=n)) for n in range(1,102)]
        record=ingress.seal_ingress(self.f.run,self.f.read())['record']
        self.assertEqual(len(record['events']),101)
        self.assertEqual(record['events'][0]['occurredAt'],timestamp(START+1))
        self.f.events[-1]['eventId']=self.f.events[0]['eventId']
        with self.assertRaises(ValueError):self.f.read()
    def test_rotation_between_reads_is_allowed_but_mid_read_changes_refuse(self):
        self.f.read();dep=self.f.objects[('Deployment','raptor-system','gateway')]
        dep['metadata']['resourceVersion']='2';self.f.objects[('Secret','raptor-system','gateway-auth')]['metadata']['resourceVersion']='2'
        self.f.read()
        original=self.f.http
        def changed(origin,path,credentials):
            response=original(origin,path,credentials);dep['metadata']['resourceVersion']=str(int(dep['metadata']['resourceVersion'])+1);return response
        self.f.http=changed
        with self.assertRaises(ValueError):self.f.read()
    def test_historical_ready_frames_are_not_required_to_be_current(self):
        for n in range(2,8):self.f.append(n)
        self.f.read()
        self.f.now+=4
        with self.assertRaises(ValueError):self.f.read()


class DiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.f=Fixture(Path(self.temp.name).resolve(),'disabled-proof')
    def patch(self,namespace,name,patch):
        self.assertEqual((namespace,name),('raptor-test','rdev-test-client-client'))
        dep=self.f.objects[('Deployment',namespace,name)]
        self.assertEqual(patch,dict(metadata=dict(uid='deployment',resourceVersion='1'),spec=dict(template=dict(metadata=dict(annotations={'kubectl.kubernetes.io/restartedAt':timestamp(self.f.now)})))))
        self.f.patches.append(copy.deepcopy(patch))
        dep['metadata'].update(generation=2,resourceVersion='2')
        dep['spec']['template'].setdefault('metadata',{}).setdefault('annotations',{})['kubectl.kubernetes.io/restartedAt']=timestamp(self.f.now)
        return copy.deepcopy(dep)
    def restart(self,patch=None):
        return ingress.restart_disabled(self.f.run,gate=self.f,kube_read=self.f.kube,kube_patch=patch or self.patch,now=lambda:self.f.now)
    def test_fixed_preconditioned_restart_is_one_use_and_has_no_native_identity(self):
        result=self.restart();self.assertEqual(result['kind'],'disabled-proof');self.assertEqual(result['restartGeneration'],2)
        self.assertEqual(len(self.f.patches),1)
        for key in ('requestId','model','result','selectedModel'):self.assertNotIn(key,result)
        with self.assertRaises(ValueError):self.restart()
        self.assertEqual(len(self.f.patches),1)
    def test_uncertain_patch_never_retries_or_issues_receipt(self):
        def uncertain(*args):self.f.patches.append(args);raise TimeoutError('secret-private-error')
        with self.assertRaises(ValueError) as caught:self.restart(uncertain)
        self.assertNotIn('private',str(caught.exception))
        self.assertTrue((self.f.run/'native-restart.intent').exists())
        self.assertTrue((self.f.run/'native-restart-uncertain.json').exists())
        self.assertFalse((self.f.run/'native-restart-receipt.json').exists())
        with self.assertRaises(ValueError):self.restart(uncertain)
        self.assertEqual(len(self.f.patches),1)
    def test_runtime_true_stale_proof_and_strategy_drift_refuse_before_patch(self):
        secret=self.f.objects[('Secret','raptor-system','gateway-live-config')]
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode()
        with self.assertRaises(ValueError):self.restart()
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":false}').decode()
        original=self.f.gateway_disabled;self.f.gateway_disabled=lambda:dict(self.f.runtime,observedAt=self.f.now-30)
        with self.assertRaises(ValueError):self.restart()
        self.f.gateway_disabled=original
        live=copy.deepcopy(self.f.live);self.f.live_clients=lambda:live
        self.f.objects[('Deployment','raptor-test','rdev-test-client-client')]['spec']['strategy']['rollingUpdate']['maxSurge']=2
        with self.assertRaises(ValueError):self.restart()
        self.assertFalse(self.f.patches)
    def test_returned_template_other_change_is_uncertain(self):
        def changed(*args):
            dep=self.patch(*args);dep['spec']['template']['spec']['containers'][0]['image']='changed';return dep
        with self.assertRaises(ValueError):self.restart(changed)
        self.assertFalse((self.f.run/'native-restart-receipt.json').exists())
        self.assertTrue((self.f.run/'native-restart-uncertain.json').exists())
    def test_native_kind_never_restarts_and_forged_selector_is_not_authority(self):
        other=self.f.root/'other';other.mkdir(mode=0o700);f=Fixture(other)
        with self.assertRaises(ValueError):ingress.restart_disabled(f.run,gate=f,kube_read=f.kube,kube_patch=self.patch,now=lambda:f.now)
        with self.assertRaises(ValueError):ingress.read_ingress(self.f.run,dict(ready=True),gate=self.f,kube_read=self.f.kube,http_get=self.f.http,now=lambda:self.f.now)
        self.assertFalse(self.f.patches)


def complete_native(f):
    for n,members in [(2,['a','b','c','d']),(3,['a','b','c','d']),(4,['a','b','c','d']),(5,['c','d'])]:
        frame=copy.deepcopy(f.frame) if n==2 else rolling(n-2);frame['inventory'].update(desired=len(members),members=members,healthyRegistered=members,
            memberStates=[dict(id=x,lifecycle='Protected',health='Healthy',addressKnown=True) for x in members],
            backendStates=[dict(id=x,status='Available',weight=100,listenerHealth='Normal') for x in members])
        f.append(n,frame)
    f.live=kubernetes(3)
    replacement=dict(version=2,converged=True,acceptance='pending',scope=copy.deepcopy(f.plan['controllerScope']),oldInstanceIds=['a','b'],newInstanceIds=['c','d'],
        desiredCapacity=2,restartGeneration=2,actions=[dict(actionId='expand',previousDesiredCapacity=2,desiredCapacity=4,outcome='submitted'),
            dict(actionId='shrink',previousDesiredCapacity=4,desiredCapacity=2,outcome='unknown')],observedAt=timestamp(f.now))
    f.execution.update(status='completed',result=dict(requestId=REQUEST,attemptId='attempt-1',selectedModel='gpt-6-luna',actualModel='gpt-6-luna',
        selectedProvider='codex',actualProvider='openai-codex',answer='Replacement converged; retained traffic and approval assessment is pending.',
        evidence=[],usage=[],generatedAt=timestamp(f.now),replacement=replacement))
    f.events=[dict(eventId='e-'+str(n),requestId=REQUEST,attemptId='attempt-1',sequence=n,occurredAt=timestamp(START+n+1),kind='tool_start',
        summary='scale started',details=dict(tool='mcp__infra__db_proxy_scale',status='started',runtimeSequence=n),evidenceMode='live') for n in (1,2)]


class BindingTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_final_native_binding_preserves_unknown_and_is_immutable(self):
        complete_native(self.f)
        result=ingress.seal_ingress(self.f.run,self.f.read())
        bound=ingress.seal_operation_binding(self.f.run,self.f.run/'native-ingress-0001.json')
        self.assertEqual(bound['kind'],'native-replacement');self.assertEqual(bound['ingressSha256'],result['ingressSha256'])
        self.assertEqual(result['record']['execution']['result']['replacement']['actions'][1]['outcome'],'unknown')
        self.assertNotIn('submission',str(result))
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,self.f.run/'native-ingress-0001.json')
    def test_binding_requires_final_and_complete_unambiguous_chronology(self):
        ingress.seal_ingress(self.f.run,self.f.read())
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,self.f.run/'native-ingress-0001.json')
        complete_native(self.f);self.f.events.insert(0,dict(copy.deepcopy(self.f.events[0]),eventId='old-attempt',attemptId='earlier',sequence=1))
        for n,event in enumerate(self.f.events,1):event['sequence']=n
        ingress.seal_ingress(self.f.run,self.f.read())
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,self.f.run/'native-ingress-0002.json')
    def test_untrusted_approval_selector_must_match_exact_scalein_binding(self):
        complete_native(self.f);original=self.f.http
        binding=dict(requestId=REQUEST,actionId='shrink',interface='ess.scale-in',envCode='rdev.ali',
            target=dict(proxyCode='DP00001',groupId=self.f.plan['target']['scalingGroupId']),parameters=dict(desiredCapacity=2))
        row=dict(approvalId=APPROVAL,requestId=REQUEST,actionId='shrink',binding=binding,state='approved',guidance=None)
        def http(origin,path,credentials):
            if path=='/v1/approvals/'+APPROVAL:return canonical(dict(data=row))
            return original(origin,path,credentials)
        self.f.http=http
        def read():return ingress.read_ingress(self.f.run,dict(requestId=REQUEST,approvalIds=[APPROVAL]),gate=self.f,kube_read=self.f.kube,http_get=http,now=lambda:self.f.now)
        self.assertEqual(read()['approvals'][0]['actionId'],'shrink')
        for field,value in [('requestId','wrong'),('interface','other'),('parameters',dict(desiredCapacity=3))]:
            saved=copy.deepcopy(binding);binding[field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):read()
            binding.clear();binding.update(saved)


class AdditionalBoundaryTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_full_source_closure_and_baseline_nested_shape(self):
        self.assertEqual(len(self.f.plan['sourceHashes']),23)
        self.assertEqual(len(self.f.context['sourceHashes']),12)
        for key,value in [('feedSession',context()['feedSession']),('planSha256','f'*64),('sequence',1)]:
            baseline=copy.deepcopy(self.f.baseline);baseline[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):ingress._baseline(baseline,self.f.plan)
        baseline=copy.deepcopy(self.f.baseline);baseline['fleet']['memberStates'][0]['addressKnown']=1
        with self.assertRaises(ValueError):ingress._baseline(baseline,self.f.plan)
    def test_missing_runtime_provenance_and_malformed_config_refuse(self):
        proof=copy.deepcopy(self.f.runtime);proof['provenance']={}
        with self.assertRaises(ValueError):ingress._runtime(proof,self.f.plan,self.f.now)
        secret=self.f.objects[('Secret','raptor-system','gateway-live-config')]
        for encoded in ('%%%!',base64.b64encode(b'{"replacementEnabled":false,"replacementEnabled":true}').decode(),base64.b64encode(b'{"replacementEnabled":0}').decode()):
            secret['data']['config.json']=encoded
            with self.assertRaises(ValueError):self.f.read()
    def test_historical_segment_tamper_and_source_symlink_refuse(self):
        ref=self.f.ready['frames']['previous'];path=self.f.run/'native-capture'/ref['basename'];raw=path.read_bytes()
        path.write_bytes(raw.replace(b'"Protected"',b'"InService"',1))
        with self.assertRaises(ValueError):self.f.read()
        path.write_bytes(raw)
        source=self.f.run/'native-baseline.json';copy_path=self.root/'copy';copy_path.write_bytes(source.read_bytes());copy_path.chmod(0o600)
        source.unlink();source.symlink_to(copy_path)
        with self.assertRaises(ValueError):self.f.read()
    def test_service_secret_identity_and_gate_denial(self):
        self.f.admission=lambda action: (_ for _ in ()).throw(RuntimeError('private gate error'))
        with self.assertRaises(ValueError) as caught:self.f.read()
        self.assertNotIn('private',str(caught.exception))
    def test_event_truncation_changed_attempt_and_missing_runtime_fields(self):
        original=self.f.http
        def truncated(origin,path,credentials):
            value=original(origin,path,credentials)
            if '/events?' in path:
                obj=__import__('json').loads(value);obj['nextAfter']+=10;return canonical(obj)
            return value
        self.f.http=truncated
        with self.assertRaises(ValueError):self.f.read()
        self.f.http=original;self.f.events[0]['details']={}
        with self.assertRaises(ValueError):self.f.read()
    def test_diagnostic_requires_actual_configmap_and_current_provider(self):
        other=self.root/'disabled';other.mkdir(mode=0o700);f=Fixture(other,'disabled-proof')
        f.objects[('ConfigMap','raptor-test','pgcat-provider-inventory')]['data']['targets.json']='[]'
        calls=[]
        with self.assertRaises(ValueError):ingress.restart_disabled(f.run,gate=f,kube_read=f.kube,kube_patch=lambda *a:calls.append(a),now=lambda:f.now)
        self.assertEqual(calls,[])
    def test_diagnostic_success_receipt_can_be_joined_after_rollout(self):
        other=self.root/'disabled';other.mkdir(mode=0o700);f=Fixture(other,'disabled-proof')
        def patch(ns,name,payload):
            dep=f.objects[('Deployment',ns,name)];dep['metadata'].update(generation=2,resourceVersion='2')
            dep['spec']['template'].setdefault('metadata',{})['annotations']=payload['spec']['template']['metadata']['annotations'];return copy.deepcopy(dep)
        receipt=ingress.restart_disabled(f.run,gate=f,kube_read=f.kube,kube_patch=patch,now=lambda:f.now)
        for n in (2,3,4):
            frame=rolling(n-1);frame['inventory']=copy.deepcopy(f.frame['inventory']);f.append(n,frame)
        f.live=kubernetes(3)
        projection=f.read();self.assertIsNone(projection['context']);self.assertEqual(projection['diagnostic']['receipt'],receipt)
        sealed=ingress.seal_ingress(f.run,projection);binding=ingress.seal_operation_binding(f.run,f.run/'native-ingress-0001.json')
        self.assertEqual(binding['ingressSha256'],sealed['ingressSha256']);self.assertEqual(set(binding),{'schemaVersion','kind','planSha256','restartGeneration','ingressSha256','restartReceiptSha256'})


class RealFormatTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_actual_worker_rfc3339_frame_times_and_go_nanoseconds(self):
        other=self.root/'real-times';other.mkdir(mode=0o700);f=Fixture(other,string_times=True)
        f.events[0]['occurredAt']='2026-10-29T00:00:01.123456789Z'
        self.assertEqual(f.read()['events'][0]['occurredAt'],'2026-10-29T00:00:01.123456789Z')
    def test_mid_prepatch_config_change_refuses_before_intent(self):
        other=self.root/'disabled';other.mkdir(mode=0o700);f=Fixture(other,'disabled-proof');original=f.kube;calls=[]
        def kube(kind,ns,name=None):
            value=original(kind,ns,name)
            if (kind,ns,name)==('Deployment','raptor-test','rdev-test-client-client'):
                f.objects[('Secret','raptor-system','gateway-live-config')]['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode()
            return value
        with self.assertRaises(ValueError):ingress.restart_disabled(f.run,gate=f,kube_read=kube,kube_patch=lambda *a:calls.append(a),now=lambda:f.now)
        self.assertEqual(calls,[])


class AdversarialTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_selected_definition_wrong_identity_or_defaults_refuse(self):
        for key,value in [('providerId','other'),('model','other'),('envCode','other'),('object',dict(kind='application',code='other')),
                          ('skills',dict(tag='skills-v0.4.0',commitSha='b'*40)),('operations',[dict(name='db-proxy.replace-nodes',parameters={})]),('connectionVersion',True)]:
            original=copy.deepcopy(self.f.api_context);self.f.api_context['definition'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.f.read()
            self.f.api_context=original
    def test_changed_attempt_and_mid_read_secret_uid_refuse(self):
        original=self.f.http;reads=[]
        def changed(origin,path,credentials):
            if path=='/v1/requests/'+REQUEST:
                reads.append(path)
                if len(reads)>1:self.f.execution['attemptId']='different'
            return original(origin,path,credentials)
        self.f.http=changed
        with self.assertRaises(ValueError):self.f.read()
        self.f.execution['attemptId']='attempt-1'
        def secret_changed(origin,path,credentials):
            raw=original(origin,path,credentials)
            self.f.objects[('Secret','raptor-system','gateway-auth')]['metadata']['uid']+='x';return raw
        self.f.http=secret_changed
        with self.assertRaises(ValueError):self.f.read()
    def test_new_credentials_are_resolved_between_reads_without_plan_change(self):
        self.f.read();plan_raw=(self.f.run/'native-plan.json').read_bytes()
        row=self.f.objects[('Secret','raptor-system','gateway-auth')];row['metadata']['resourceVersion']='2'
        row['data']['SERVICE_PASSWORD']=base64.b64encode(b'rotated-private-password').decode()
        original=self.f.http;observed=[]
        def http(origin,path,credentials):
            if ':8874' in origin:
                observed.append(credentials);self.assertEqual(credentials[1],'rotated-private-password')
                credentials=(credentials[0],'gateway-private-password')
            return original(origin,path,credentials)
        self.f.http=http;record=ingress.seal_ingress(self.f.run,self.f.read())
        self.assertTrue(observed);self.assertNotIn('rotated-private-password',str(record));self.assertEqual((self.f.run/'native-plan.json').read_bytes(),plan_raw)
    def test_runtime_sequence_is_not_global_and_absent_tool_arguments_are_valid(self):
        self.f.events=[dict(self.f.events[0],eventId='earlier',attemptId='attempt-old'),dict(self.f.events[0],eventId='current',sequence=3)]
        self.assertEqual([e['details']['runtimeSequence'] for e in self.f.read()['events']],[1,1])
        self.assertNotIn('arguments',str(self.f.read()['events']))
    def test_redirect_error_and_ambient_proxy_do_not_create_alternate_requests(self):
        from unittest.mock import patch
        with patch.dict(os.environ,dict(HTTP_PROXY='http://untrusted.invalid',HTTPS_PROXY='http://untrusted.invalid')):
            self.f.read()
        self.assertTrue(all(not isinstance(c[0],str) or 'untrusted' not in c[0] for c in self.f.calls))
        def redirect(*args):raise RuntimeError('redirect to https://untrusted.invalid/private-password')
        self.f.http=redirect
        with self.assertRaises(ValueError) as caught:self.f.read()
        self.assertEqual(str(caught.exception),'invalid native ingress')
    def test_event_page_limit_refuses_incomplete_stream(self):
        original=self.f.http
        def pages(origin,path,credentials):
            if '/events?' not in path:return original(origin,path,credentials)
            n=int(path.split('after=')[1])+1
            row=dict(self.f.events[0],eventId='e-'+str(n),sequence=n)
            return canonical(dict(data=[row],nextAfter=n,hasMore=True))
        self.f.http=pages
        with self.assertRaises(ValueError):self.f.read()
    def test_wrong_result_scope_originals_models_and_earlier_mutations_never_bind(self):
        complete_native(self.f)
        for key,value in [('selectedModel','other'),('actualProvider','codex')]:
            saved=copy.deepcopy(self.f.execution['result']);self.f.execution['result'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.f.read()
            self.f.execution['result']=saved
        saved=copy.deepcopy(self.f.execution['result'])
        self.f.execution['result']['replacement']['scope']['application']['uid']='wrong'
        with self.assertRaises(ValueError):self.f.read()
        self.f.execution['result']=saved
        self.f.execution['result']['replacement']['oldInstanceIds']=['c','d']
        with self.assertRaises(ValueError):self.f.read()
    def test_replay_gate_has_no_production_entry_or_transport_default(self):
        import inspect
        for name in ('read_ingress','restart_disabled'):
            params=inspect.signature(getattr(ingress,name)).parameters
            self.assertEqual(params['gate'].default,inspect.Parameter.empty)
            self.assertEqual(params['kube_read'].default,inspect.Parameter.empty)
        self.assertFalse(hasattr(ingress,'main'))


class ProjectionSafetyTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_untrusted_event_text_is_not_retained_or_error_chained(self):
        self.f.events[0]['summary']='unrelated-secret-configuration-value'
        self.assertNotIn('unrelated-secret-configuration-value',str(self.f.read()))
        def error(*args):raise RuntimeError('gateway-private-password')
        self.f.http=error
        with self.assertRaises(ValueError) as caught:self.f.read()
        self.assertIsNone(caught.exception.__context__)
    def test_ready_four_requires_new_pair_health_not_old_registration(self):
        complete_native(self.f)
        # Exercise a first four-member observation whose originals are already
        # deregistered while both replacements remain ready/protected.
        path=self.f.run/'native-capture'/'segment-0001.jsonl'
        rows=[__import__('json').loads(r) for r in path.read_bytes().splitlines()]
        # The capture bytes stay untouched; only the detached inventory varies.
        inv=copy.deepcopy(rows[2]['inventory']);inv['healthyRegistered']=['c','d']
        inv['backendStates']=[r for r in inv['backendStates'] if r['id'] in ('c','d')]
        self.assertEqual(ingress._replacement_pair(inv,{'a','b'}),['c','d'])
        inv['memberStates'][-1]['health']='Unhealthy'
        self.assertIsNone(ingress._replacement_pair(inv,{'a','b'}))


class CollectionSealRaceTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_nonready_committed_frame_changes_before_seal_refuse(self):
        self.f.append(2);projection=self.f.read()
        path=self.f.run/'native-capture'/'segment-0001.jsonl';rows=path.read_bytes().splitlines(keepends=True)
        rows[-1]=rows[-1].replace(b'"value":0',b'"value":1',1);path.write_bytes(b''.join(rows))
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,projection)


class ReadinessTrafficTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_two_connected_but_stalled_samples_do_not_establish_readiness(self):
        from segmented_capture import read_frame
        frames=[read_frame(self.f.writer.progress_fd,self.f.ready['frames'][k]) for k in ('previous','latest')]
        frames[1]['pods'][0]['snapshot']=copy.deepcopy(frames[0]['pods'][0]['snapshot'])
        with self.assertRaises(ValueError):ingress._ready_pair(frames,self.f.baseline,self.f.context,self.f.ready['readyAt'])




class OriginAnchorReviewTests(unittest.TestCase):
    def setUp(self):
        IngressTests.setUp(self);complete_native(self.f)
    def test_mutated_projection_and_apparent_authority_cannot_relabel_unknown(self):
        projection=self.f.read()
        projection['execution']['result']['replacement']['actions'][1]['outcome']='submitted'
        projection._record=canonical(dict(projection))
        projection._chain=ingress._Chain(self.f.run)
        progress,_,_=read_progress(self.f.run/'native-capture',self.f.context)
        projection._capture=(progress['latest'],ingress._capture_prefix(projection._chain,progress['latest']))
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,projection)
        self.assertFalse(list(self.f.run.glob('native-ingress-*.json')))
    def test_forged_or_exchanged_projection_handles_are_not_issued(self):
        first=self.f.read();second=self.f.read()
        forged=ingress._Projection(copy.deepcopy(dict(first)))
        forged.__dict__.update(first.__dict__)
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,forged)
        first['execution']['result']['replacement']['actions'][1]['outcome']='submitted'
        first.__dict__.clear();first.__dict__.update(second.__dict__)
        first._record=canonical(dict(first))
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,first)
        sealed=ingress.seal_ingress(self.f.run,second)
        self.assertEqual(sealed['record']['execution']['result']['replacement']['actions'][1]['outcome'],'unknown')
    def test_projection_is_consumed_once(self):
        projection=self.f.read();ingress.seal_ingress(self.f.run,projection)
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,projection)
        self.assertEqual(len(list(self.f.run.glob('native-ingress-*.json'))),1)
    def test_same_inode_ingress_rewrite_is_not_an_original_seal(self):
        sealed=ingress.seal_ingress(self.f.run,self.f.read());path=self.f.run/'native-ingress-0001.json';inode=path.stat().st_ino
        sealed['record']['execution']['result']['replacement']['actions'][1]['outcome']='submitted'
        write(path,sealed['record']);self.assertEqual(path.stat().st_ino,inode)
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,path)
        self.assertFalse((self.f.run/'native-operation-binding.json').exists())
    def test_replaced_identical_ingress_bytes_are_not_the_original_file(self):
        ingress.seal_ingress(self.f.run,self.f.read());path=self.f.run/'native-ingress-0001.json';old=path.stat().st_ino
        replacement=self.f.run/'replacement';replacement.write_bytes(path.read_bytes());replacement.chmod(0o600);replacement.replace(path)
        self.assertNotEqual(path.stat().st_ino,old)
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,path)
    def test_never_issued_numbered_file_refuses(self):
        path=self.f.run/'native-ingress-0001.json';write(path,dict(self.f.read()))
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,path)
    def test_returned_record_mutation_does_not_change_sealed_authority(self):
        sealed=ingress.seal_ingress(self.f.run,self.f.read());path=self.f.run/'native-ingress-0001.json'
        sealed['record']['execution']['result']['replacement']['actions'][1]['outcome']='submitted'
        binding=ingress.seal_operation_binding(self.f.run,path)
        actual=__import__('json').loads(path.read_bytes())
        self.assertEqual(actual['execution']['result']['replacement']['actions'][1]['outcome'],'unknown')
        self.assertEqual(binding['resultSha256'],sha(canonical(actual['execution']['result'])))
    def test_fresh_worker_cannot_reconstruct_an_anchor_from_current_file(self):
        import subprocess,sys
        ingress.seal_ingress(self.f.run,self.f.read());path=self.f.run/'native-ingress-0001.json'
        script=("import sys; from pathlib import Path; import pgcat_native_ingress as i\n"
                "try:i.seal_operation_binding(Path(sys.argv[1]),Path(sys.argv[2]))\n"
                "except ValueError:print('refused')\n"
                "else:print('accepted')\n")
        env=dict(os.environ,PYTHONPATH=str(Path(__file__).resolve().parent))
        result=subprocess.run([sys.executable,'-c',script,str(self.f.run),str(path)],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,
            universal_newlines=True,timeout=10,check=True)
        self.assertEqual(result.stdout.strip(),'refused');self.assertEqual(result.stderr,'')




def resumed_native(f,on_pause=None):
    """Three real attempt identities, two paused snapshots, one final result."""
    def stage(n,roll,members):
        frame=copy.deepcopy(f.frame) if roll is None else rolling(roll)
        frame['inventory'].update(desired=len(members),members=members,healthyRegistered=members,
            memberStates=[dict(id=x,lifecycle='Protected',health='Healthy',addressKnown=True) for x in members],
            backendStates=[dict(id=x,status='Available',weight=100,listenerHealth='Normal') for x in members])
        f.append(n,frame)
    f.events=[]
    def event(attempt,runtime,tool,t):
        f.events.append(dict(eventId=attempt+':'+str(runtime),requestId=REQUEST,attemptId=attempt,sequence=len(f.events)+1,
            occurredAt=timestamp(START+t),kind='tool_start',summary='tool_start: started',
            details=dict(tool=tool,status='started',runtimeSequence=runtime),evidenceMode='live'))
    stage(2,None,['a','b','c','d'])
    event('attempt-A',1,'mcp__infra__db_proxy_scale',2)
    event('attempt-A',2,'resource_wait',2)
    f.execution.update(attemptId='attempt-A',status='paused',stage='waiting')
    if on_pause:on_pause()
    stage(3,1,['a','b','c','d']);stage(4,2,['a','b','c','d']);stage(5,3,['a','b','c','d']);f.live=kubernetes(3)
    event('attempt-B',1,'mcp__infra__deployment_restart',3)
    event('attempt-B',2,'mcp__infra__db_proxy_nodes_deregister',4)
    event('attempt-B',3,'mcp__raptor__approval_request',5)
    event('attempt-B',4,'mcp__raptor__request_pause',5)
    f.execution.update(attemptId='attempt-B',status='paused',stage='waiting')
    if on_pause:on_pause()
    stage(6,4,['c','d']);f.live=kubernetes(4)
    event('attempt-C',1,'mcp__raptor__approval_get',6)
    event('attempt-C',2,'mcp__infra__db_proxy_scale',6)
    replacement=dict(version=2,converged=True,acceptance='pending',scope=copy.deepcopy(f.plan['controllerScope']),oldInstanceIds=['a','b'],newInstanceIds=['c','d'],
        desiredCapacity=2,restartGeneration=2,actions=[dict(actionId='expand',previousDesiredCapacity=2,desiredCapacity=4,outcome='submitted'),
            dict(actionId='shrink',previousDesiredCapacity=4,desiredCapacity=2,outcome='unknown')],observedAt=timestamp(f.now))
    f.execution.update(attemptId='attempt-C',status='completed',stage='completed',result=dict(requestId=REQUEST,attemptId='attempt-C',
        selectedModel='gpt-6-luna',actualModel='gpt-6-luna',selectedProvider='codex',actualProvider='openai-codex',
        answer='Replacement converged; retained traffic and approval assessment is pending.',evidence=[],usage=[],generatedAt=timestamp(f.now),replacement=replacement))


class AttemptSpanningReviewTests(unittest.TestCase):
    setUp=IngressTests.setUp
    def test_resource_wait_then_approval_pause_resume_keeps_three_attempts(self):
        paused=[]
        def pause():
            projection=self.f.read();paused.append(projection['execution']['attemptId'])
            sealed=ingress.seal_ingress(self.f.run,projection)
            with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,self.f.run/sealed['ingressFile']['basename'])
        resumed_native(self.f,pause)
        self.assertEqual(paused,['attempt-A','attempt-B'])
        sealed=ingress.seal_ingress(self.f.run,self.f.read());path=self.f.run/sealed['ingressFile']['basename']
        result=ingress.seal_operation_binding(self.f.run,path)
        self.assertEqual(result['requestId'],REQUEST)
        self.assertEqual([e['attemptId'] for e in sealed['record']['events']],['attempt-A']*2+['attempt-B']*4+['attempt-C']*2)
        self.assertEqual([e['details']['runtimeSequence'] for e in sealed['record']['events']],[1,2,1,2,3,4,1,2])
        self.assertEqual(sealed['record']['execution']['result']['replacement']['actions'][1]['outcome'],'unknown')
    def test_extra_foreign_preready_duplicate_replayed_or_mismatched_history_refuses(self):
        for case in ('extra-scale','foreign-request','pre-ready','duplicate-id','runtime-replay','mutation-time-regression','post-result-mutation','mismatched-final-attempt'):
            root=self.root/case;root.mkdir(mode=0o700);f=Fixture(root);resumed_native(f)
            if case=='extra-scale':
                extra=copy.deepcopy(f.events[0]);extra.update(eventId='attempt-A:2');extra['details']['runtimeSequence']=2
                f.events[1]['eventId']='attempt-A:3';f.events[1]['details']['runtimeSequence']=3;f.events.insert(1,extra)
                for n,e in enumerate(f.events,1):e['sequence']=n
            elif case=='foreign-request':f.events[0]['requestId']='99999999-9999-4999-8999-999999999999'
            elif case=='pre-ready':f.events[0]['occurredAt']=timestamp(START+1)
            elif case=='duplicate-id':f.events[-1]['eventId']=f.events[0]['eventId']
            elif case=='runtime-replay':f.events[3]['details']['runtimeSequence']=1
            elif case=='mutation-time-regression':f.events[3]['occurredAt']=timestamp(START+2)
            elif case=='post-result-mutation':
                extra=copy.deepcopy(f.events[-1]);extra.update(eventId='attempt-C:3',sequence=9,occurredAt=timestamp(START+7))
                extra['details'].update(tool='mcp__infra__deployment_restart',runtimeSequence=3);f.events.append(extra)
            else:f.execution['result']['attemptId']='attempt-B'
            with self.subTest(case=case),self.assertRaises(ValueError):
                sealed=ingress.seal_ingress(f.run,f.read())
                ingress.seal_operation_binding(f.run,f.run/sealed['ingressFile']['basename'])
            self.assertFalse((f.run/'native-operation-binding.json').exists())
    def test_same_attempt_runtime_sequence_replay_refuses_at_collection(self):
        complete_native(self.f);self.f.events[-1]['details']['runtimeSequence']=1
        with self.assertRaises(ValueError):self.f.read()
    def test_returned_file_metadata_cannot_redirect_private_origin(self):
        resumed_native(self.f);sealed=ingress.seal_ingress(self.f.run,self.f.read())
        self.assertEqual(set(sealed),{'ingressSha256','record','ingressFile'})
        self.assertEqual(sealed['ingressFile']['identity'],file_record(self.f.run/sealed['ingressFile']['basename']))
        original=sealed['ingressFile']['basename'];forged=self.f.run/'native-ingress-9999.json';write(forged,sealed['record'])
        sealed['ingressFile'].update(basename=forged.name,identity=file_record(forged));sealed['ingressSha256']=sha(forged.read_bytes())
        with self.assertRaises(ValueError):ingress.seal_operation_binding(self.f.run,forged)
        binding=ingress.seal_operation_binding(self.f.run,self.f.run/original)
        self.assertEqual(binding['ingressSha256'],sha((self.f.run/original).read_bytes()))


if __name__=='__main__':unittest.main()
