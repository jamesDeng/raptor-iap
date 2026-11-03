"""Fixed native Gateway kernels: real private assets/receipts, fake external I/O."""
import ast
import base64
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import pgcat_native_controller as native
import pgcat_source_bootstrap as bootstrap
from test_pgcat_native_controller import Fixture
from test_pgcat_native_ingress import REQUEST

closure = None
if importlib.util.find_spec('pgcat_native_closure'):
    import pgcat_native_closure as closure


class GatewayClosureTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(closure, 'native Gateway kernel missing')
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(os.path.realpath(self.tmp.name)); self.root.chmod(0o700)
        self.f = Fixture(self.root,getattr(self,'fixture_kind','native-replacement'))
        self.f.plan['serviceBindings']['gateway'].update(deploymentName='agent-gateway',containerName='agent-gateway')
        from segmented_capture import canonical
        from test_pgcat_native_ingress import file_record
        (self.f.run/'native-plan.json').write_bytes(canonical(self.f.plan))
        self.f.prepared['planFile']=file_record(self.f.run/'native-plan.json')
        self.gate = self.f.run/'gateway-live-gate'; self.gate.write_bytes(b'#!/bin/sh\nexit 1\n'); self.gate.chmod(0o700)
        from test_pgcat_native_admission import assets_for
        self.assets=assets_for(self.f.plan);self.assets['gateBinarySha256']=hashlib.sha256(self.gate.read_bytes()).hexdigest()
        (self.f.run/'native-runtime-assets.json').write_bytes(canonical(self.assets))
        self.f.prepared['runtimeAssetsFile']=file_record(self.f.run/'native-runtime-assets.json')
        self.calls=[]; self.failure_mode=None; self.mutate=None
        self.enabled=False
        self.objects=copy.deepcopy(self.f.objects)
        dep=self.objects.pop(('Deployment','raptor-system','gateway'));self.objects[('Deployment','raptor-system','agent-gateway')]=dep
        dep['metadata'].update(name='agent-gateway',namespace='raptor-system',generation=1)
        dep['spec']['template']['spec']['containers'][0]['name']='agent-gateway'
        dep['spec']['template']['spec']['containers'][0]['image']=self.assets['gatewayImage']
        self.objects[('Pod','raptor-system',None)]=dict(items=[dict(metadata=dict(name='gateway-pod',namespace='raptor-system',uid='gateway-pod'),
            spec=dict(containers=[dict(name='agent-gateway',image=self.assets['gatewayImage'])]),
            status=dict(phase='Running',conditions=[dict(type='Ready',status='True')],containerStatuses=[dict(name='agent-gateway',ready=True,imageID=self.assets['gatewayImage'])]))])
        # Real pinned Gateway/init/private-volume/container structure.
        spec=dep['spec']['template']['spec'];image=self.assets['gatewayImage']
        spec['containers'][0].update(command=['/usr/local/bin/gateway'],env=[dict(name='GATEWAY_RUNTIME_MODE',value='live'),dict(name='GATEWAY_LIVE_CONFIG_FILE',value='/run/raptor/live/config.json')],volumeMounts=[dict(name='live-private',mountPath='/run/raptor/live',readOnly=True)])
        spec['volumes']=[dict(name='live-private',emptyDir=dict(medium='Memory')),dict(name='live-config-source',secret=dict(secretName='gateway-live-config'))]
        spec['initContainers']=[dict(name='live-private-files',image=image,command=['/bin/sh','-ec'],args=['umask 077; install -m 0600 /live-config-source/config.json /live-private/config.json'],volumeMounts=[dict(name='live-private',mountPath='/live-private'),dict(name='live-config-source',mountPath='/live-config-source',readOnly=True)])]
        dep['spec']['replicas']=1;dep['status']=dict(observedGeneration=1,replicas=1,readyReplicas=1,availableReplicas=1,updatedReplicas=1)
        pod=self.objects[('Pod','raptor-system',None)]['items'][0];pod['spec']=copy.deepcopy(spec)
        pod['metadata']['ownerReferences']=[dict(apiVersion='apps/v1',kind='ReplicaSet',name='gateway-rs',uid='gateway-rs',controller=True)]
        pod['status']['containerStatuses'][0].update(containerID='containerd://gateway-original',restartCount=0,state=dict(running=dict(startedAt='2026-10-20T00:00:00Z')))
        pod['status']['initContainerStatuses']=[dict(name='live-private-files',image=image,imageID=image,containerID='containerd://init-original',restartCount=0,state=dict(terminated=dict(exitCode=0,startedAt='2026-10-19T23:59:00Z',finishedAt='2026-10-19T23:59:01Z')))]
        def objects(adapter):return [json.loads(self.command(['kubectl','--kubeconfig',str(self.root/'kubeconfig'),'-n','raptor-system','get',kind]+([] if kind=='pods' else ['gateway-live-config' if kind=='secret' else 'agent-gateway'])+['-o','json','--request-timeout=2s']).stdout) for kind in ('secret','deployment','pods')]
        self.metadata_io=patch.object(closure.FixedGatewayAdapters,'_metadata_objects',new=objects,create=True)
        self.metadata_io.start();self.addCleanup(self.metadata_io.stop)
        self.source_guard=patch.object(bootstrap,'require_verified'); self.source_guard.start(); self.addCleanup(self.source_guard.stop)
        self.hashes=patch.object(bootstrap,'current_hashes',return_value=self.f.plan['sourceHashes']); self.hashes.start(); self.addCleanup(self.hashes.stop)
        self.io=patch.object(subprocess,'run',side_effect=self.command); self.io.start(); self.addCleanup(self.io.stop)

    def adapter(self, *, template=False, kind=None):
        plan=copy.deepcopy(self.f.plan)
        if kind is not None:
            plan['executionKind']=kind
            if kind=='disabled-proof':
                plan['plannedOperation']='disabled-client-restart';plan.pop('plannedInput');plan.pop('controllerScope')
                template=True
        if template:
            del plan['runId'];del plan['baselineSha256']
            reads=native.FixedWorkerReads(plan,self.root/'kubeconfig',run_id=self.f.plan['runId'])
        else:reads=native.FixedWorkerReads(plan,self.root/'kubeconfig',credential=self.f.prepared['workerCredential'])
        return closure.FixedGatewayAdapters(self.f.run,reads,runtime_assets=self.assets,
            prepared=None if template else self.f.prepared,action_id='99999999-9999-4999-8999-999999999999',raptor_request_id=REQUEST)

    def command(self, argv, **kwargs):
        self.calls.append(list(argv))
        if self.failure_mode and self.failure_mode in argv:raise subprocess.TimeoutExpired(argv,1)
        if argv[0]==str(self.gate):
            mode=argv[1]
            if mode=='set':
                self.enabled=argv[argv.index('--set')+1]=='true'
                secret=self.objects[('Secret','raptor-system','gateway-live-config')]
                secret['metadata']['resourceVersion']='2'
                secret['data']['config.json']=base64.b64encode(json.dumps(dict(replacementEnabled=self.enabled)).encode()).decode()
                output=dict(resourceVersion='2',changedAfter='2026-10-20T03:00:00Z',replacementEnabled=self.enabled)
            elif mode=='rollout':return SimpleNamespace(returncode=0,stdout=b'rollout=requested\n')
            else:
                output=dict(replacementEnabled=self.enabled,generation=1,ready=True,podUID='gateway-pod')
                if self.mutate:self.mutate()
            return SimpleNamespace(returncode=0,stdout=json.dumps(output).encode())
        if '--idle-check' in argv:
            return SimpleNamespace(returncode=0,stdout=b'idle=true\n' if self.failure_mode!='idle' else b'idle=false\n')
        if 'rollout' in argv:return SimpleNamespace(returncode=0,stdout=b'')
        kind=argv[argv.index('get')+1].capitalize(); i=argv.index('get')+2
        name=argv[i] if argv[i]!='-o' else None
        if kind=='Pods':kind='Pod';name=None
        value=self.objects[(kind,'raptor-system',name)]
        return SimpleNamespace(returncode=0,stdout=json.dumps(value).encode())

    def proof(self,adapter=None):return closure.runtime_proof(self.f.run,adapters=adapter or self.adapter(),now=lambda:self.f.now)

    def test_prefeed_actual_uuid_without_plan_or_ready_and_exact_false_record(self):
        adapter=self.adapter(template=True)
        (self.f.run/'native-plan.json').unlink();(self.f.run/'native-ready.json').unlink()
        result=self.proof(adapter)
        self.assertEqual(result,dict(schemaVersion=1,runId=self.f.plan['runId'],observedAt=self.f.now,
            secretUid='config-uid',secretResourceVersion='1',deploymentUid='gateway-uid',podUid='gateway-pod',generation=1,
            imageDigest='sha256:'+'e'*64,provenance=dict(kind='read-only-inspect'),
            verification=dict(replacementEnabled=False,generation=1,ready=True,podUID='gateway-pod')))
        self.assertEqual([a[1:] for a in self.calls if a[0]==str(self.gate)],[['inspect','--expected','false']])

    def test_fresh_proof_does_not_rewrite_historical_artifact(self):
        path=self.f.run/'native-gateway-runtime.json';before=path.read_bytes();inode=path.stat().st_ino
        result=self.proof();self.assertEqual(result['observedAt'],self.f.now)
        self.assertEqual(path.read_bytes(),before);self.assertEqual(path.stat().st_ino,inode)

    def test_source_and_binary_mismatch_refuse_before_io(self):
        adapter=self.adapter()
        self.gate.write_bytes(b'changed')
        with self.assertRaises(ValueError):self.proof(adapter)
        self.assertEqual(self.calls,[])
        self.gate.write_bytes(b'#!/bin/sh\nexit 1\n')
        with patch.object(bootstrap,'current_hashes',return_value={}):
            with self.assertRaises(ValueError):self.proof(adapter)
        self.assertEqual(self.calls,[])

    def test_substitution_of_original_credential_or_binary_inode_rejected(self):
        adapter=self.adapter()
        original=self.gate.read_bytes();self.gate.rename(self.f.run/'old-gate')
        self.gate.write_bytes(original);self.gate.chmod(0o700)
        with self.assertRaises(ValueError):self.proof(adapter)
        self.assertEqual(self.calls,[])
        adapter=self.adapter();path=self.root/'kubeconfig';old=path.read_bytes();path.rename(self.root/'old-config');path.write_bytes(old);path.chmod(0o600)
        with self.assertRaises(ValueError):self.proof(adapter)

    def test_binary_substitution_after_inspect_and_metadata_race_refused(self):
        adapter=self.adapter();self.mutate=lambda:self.gate.write_bytes(b'changed')
        with self.assertRaises(ValueError):self.proof(adapter)
        self.gate.write_bytes(b'#!/bin/sh\nexit 1\n');self.gate.chmod(0o700);adapter=self.adapter()
        self.mutate=lambda:self.objects[('Deployment','raptor-system','agent-gateway')]['metadata'].update(resourceVersion='raced')
        with self.assertRaises(ValueError):self.proof(adapter)

    def test_forged_future_clock_and_forged_provenance_are_refused(self):
        adapter=self.adapter()
        with self.assertRaises(ValueError):closure.runtime_proof(self.f.run,adapters=adapter,now=lambda:float('nan'))
        # A merely shaped historical legacy receipt never selects verify.
        (self.f.run/'control').mkdir(mode=0o700)
        p=self.f.run/'control'/'disable.set.json';p.write_text(json.dumps(dict(resourceVersion='1',changedAfter='2026-10-20T03:00:00Z',replacementEnabled=False)));p.chmod(0o600)
        result=self.proof(adapter);self.assertEqual(result['provenance'],dict(kind='read-only-inspect'))
        self.assertNotIn('verify',[a[1] for a in self.calls if a[0]==str(self.gate)])

    def test_pins_and_context_required_and_no_generic_executor(self):
        with self.assertRaises((ValueError,TypeError)):closure.FixedGatewayAdapters(self.f.run,SimpleNamespace(plan=self.f.plan),runtime_assets=self.assets)
        assets=copy.deepcopy(self.assets);del assets['gateBinarySha256']
        reads=native.FixedWorkerReads(self.f.plan,self.root/'kubeconfig',credential=self.f.prepared['workerCredential'])
        with self.assertRaises(ValueError):closure.FixedGatewayAdapters(self.f.run,reads,runtime_assets=assets,prepared=self.f.prepared)
        with self.assertRaises((TypeError,ValueError)):closure.FixedGatewayAdapters(self.f.run,reads,runtime_assets=self.assets,prepared=self.f.prepared,command=['sh'])

    def settlement(self, *, status='completed',control='',cleanup='confirmed',recovery=False):
        # Complete real Execution input is the authenticated Raptor context fixture.
        definition=dict(self.f.plan['plannedInput'],connectionVersion=1)
        context=dict(requestId=REQUEST,definition=definition,skills=self.f.plan['plannedInput']['skills'],definitionSha256='a'*64,environment={})
        execution=dict(requestId=REQUEST,attemptId='attempt',input=context,appliedSkills=context['skills'],status=status,runtimeMode='live',stage='finished',cleanup=cleanup,recoveryNeeded=recovery)
        return context,execution,dict(requestId=REQUEST,status=status,controlState=control)

    def http(self,origin,path,credentials):
        self.assertEqual(credentials,('gateway-username','gateway-private-password') if ':8874' in origin else ('openapi-username','openapi-private-password'))
        context,execution,control=self.settlement_value
        value=context if path.endswith('/context') else control if path.endswith('/control') else execution
        return json.dumps(dict(data=value)).encode()

    def disable(self,adapter=None):
        return closure.gateway_disable(self.f.run,adapters=adapter or self.adapter(),now=lambda:self.f.now)

    def test_already_false_disable_requires_authenticated_settlement_and_actual_idle(self):
        self.settlement_value=self.settlement()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):receipt=self.disable()
        self.assertEqual(receipt['action'],'native-gateway-disable');self.assertEqual(receipt['requestId'],'99999999-9999-4999-8999-999999999999')
        self.assertIsNone(receipt['outputs']['setReceipt']);self.assertFalse(receipt['outputs']['runtime']['verification']['replacementEnabled'])
        self.assertTrue(any('--idle-check' in a for a in self.calls));self.assertFalse(any(a[0]==str(self.gate) and a[1]=='set' for a in self.calls))
        self.assertEqual(json.loads((self.f.run/'native-gateway-disable.json').read_text()),receipt)
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http),self.assertRaises(ValueError):self.disable()

    def test_running_uncertain_cleanup_recovery_and_pending_control_never_disable(self):
        for change in (dict(status='running'),dict(cleanup='unknown'),dict(recovery=True),dict(control='cancel_requested')):
            self.settlement_value=self.settlement(**change)
            with self.subTest(change=change),patch.object(native.FixedHTTP,'__call__',side_effect=self.http),self.assertRaises(ValueError):self.disable()
        self.assertFalse(any('--idle-check' in a or (a[0]==str(self.gate)) for a in self.calls))

    def test_already_false_busy_cannot_seal_safe_disable(self):
        self.failure_mode='idle';self.settlement_value=self.settlement()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http),self.assertRaises(ValueError):self.disable()
        self.assertFalse((self.f.run/'native-gateway-disable.json').exists())

    def test_unknown_set_response_is_fenced_and_never_retried(self):
        self.enabled=True
        secret=self.objects[('Secret','raptor-system','gateway-live-config')]
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode()
        self.failure_mode='set';self.settlement_value=self.settlement()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):
            for unused in range(2):
                with self.assertRaises(ValueError):self.disable()
        self.assertEqual(sum(a[0]==str(self.gate) and a[1]=='set' for a in self.calls),1)
        self.assertTrue((self.f.run/'native-gateway-disable.intent').exists())

    def test_disable_retains_genuine_set_provenance_and_verified_identity(self):
        self.enabled=True;secret=self.objects[('Secret','raptor-system','gateway-live-config')]
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode();self.settlement_value=self.settlement()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):receipt=self.disable()
        self.assertEqual(receipt['outputs']['setReceipt'],dict(resourceVersion='2',changedAfter='2026-10-20T03:00:00Z',replacementEnabled=False))
        self.assertEqual(receipt['outputs']['runtime']['provenance'],dict(kind='guarded-verify',resourceVersion='2',changedAfter='2026-10-20T03:00:00Z'))
        self.assertEqual([a[1] for a in self.calls if a[0]==str(self.gate)],['set','rollout','verify'])

    def test_enable_forbidden_for_diagnostic_and_without_real_qualification(self):
        from pgcat_lane_protocol import _release_admission,Refused
        for adapter in (self.adapter(),self.adapter(kind='disabled-proof')):
            with self.assertRaises(ValueError):closure.gateway_enable(self.f.run,adapters=adapter,now=lambda:self.f.now)
        self.assertEqual(self.calls,[])
        for action in ('native-gateway-enable','native-gateway-disable'):
            with self.assertRaises(Refused):_release_admission('full-replacement-direct-v2',action)

    def test_closure_is_in_full_sources_but_not_collector_context(self):
        import pgcat_collector_control as collector
        self.assertEqual(bootstrap.NATIVE_SOURCES['nativeClosure'],'pgcat_native_closure.py')
        self.assertEqual(len(bootstrap.NATIVE_SOURCES),27);self.assertEqual(len(collector.NATIVE_SOURCES),12)
        self.assertNotIn('nativeClosure',bootstrap.SOURCES)
        ast.parse(Path(closure.__file__).read_text(),feature_version=(3,6))

    def test_support_artifact_is_actual_identity_and_replacement_is_rejected(self):
        self.assertTrue(hasattr(closure,'support_artifact'),'sealed support artifact accessor missing')
        self.settlement_value=self.settlement();adapter=self.adapter()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):record=self.disable(adapter)
        artifact=closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter)
        self.assertEqual(set(artifact),{'basename','identity'})
        self.assertEqual(set(artifact['identity']),{'device','inode','uid','mode','nlink','size','sha256'})
        read=closure.read_support(self.f.run,'native-gateway-disable',plan=self.f.plan,original=artifact)
        self.assertEqual(read,record)
        path=self.f.run/artifact['basename'];raw=path.read_bytes();path.rename(self.f.run/'old-action');path.write_bytes(raw);path.chmod(0o600)
        with self.assertRaises(ValueError):closure.read_support(self.f.run,'native-gateway-disable',plan=self.f.plan,original=artifact)

    def test_strict_support_schema_rejects_boolean_settlement_and_runtime_type_confusion(self):
        self.assertTrue(hasattr(closure,'validate_support'),'support schema validator missing')
        self.settlement_value=self.settlement();adapter=self.adapter()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):record=self.disable(adapter)
        closure.validate_support(record,self.f.plan,'native-gateway-disable')
        for change in (lambda r:r['outputs'].update(settlement=True),
                       lambda r:r['outputs']['runtime'].pop('kind'),
                       lambda r:r.update(staged={}),
                       lambda r:r['outputs'].update(arbitrary='value')):
            value=copy.deepcopy(record);change(value)
            with self.subTest(value=value),self.assertRaises(ValueError):closure.validate_support(value,self.f.plan,'native-gateway-disable')

    def test_forged_set_timestamp_never_rolls_out(self):
        self.enabled=True
        secret=self.objects[('Secret','raptor-system','gateway-live-config')]
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode()
        self.settlement_value=self.settlement();original=self.command
        def future(argv,**kwargs):
            result=original(argv,**kwargs)
            if argv[0]==str(self.gate) and argv[1]=='set':
                value=json.loads(result.stdout);value['changedAfter']='2099-01-01T00:00:00Z';result.stdout=json.dumps(value).encode()
            return result
        with patch.object(subprocess,'run',side_effect=future),patch.object(native.FixedHTTP,'__call__',side_effect=self.http),self.assertRaises(ValueError):self.disable()
        self.assertFalse(any(a[0]==str(self.gate) and a[1]=='rollout' for a in self.calls))

    def test_normal_import_source_guard_refuses_and_full_snapshot_imports_closure(self):
        self.source_guard.stop();self.hashes.stop()
        with self.assertRaises(ValueError):closure._source(self.f.plan,self.f.run)
        code=bootstrap.ROOT.replace('sys.exit(m.main())',
            "import json;m.install(json.loads(sys.argv[2]));import pgcat_native_closure as c;from pathlib import Path;plan=json.loads((Path(m.__file__).parent/'native-plan.json').read_text());c._source(plan,Path(m.__file__).parent);print('verified')")
        # Only this harmless isolated source-loader child is real; kubectl stays fake.
        self.io.stop()
        result=subprocess.run([__import__('sys').executable,'-I','-S','-c',code,str(self.f.run/'pgcat_source_bootstrap.py'),
            self.f.plan['sourceHashes']['sourceBootstrap'],'nativeController',json.dumps(self.f.plan['sourceHashes'])],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(result.stdout.strip(),b'verified')

    def test_fixed_command_boundary_refuses_arbitrary_paths_arguments_and_credential(self):
        adapter=self.adapter()
        for argv in (['sh','-c','echo unsafe'],[str(self.gate),'inspect','--expected','false','--config','/tmp/config'],
                     ['kubectl','--kubeconfig','/tmp/other','-n','raptor-system','get','secret','gateway-live-config','-o','json']):
            with self.subTest(argv=argv),self.assertRaises(ValueError):adapter._command(argv)
        self.assertEqual(self.calls,[])

    def test_returned_record_mutation_or_private_file_rewrite_cannot_issue_artifact(self):
        self.settlement_value=self.settlement();adapter=self.adapter()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):record=self.disable(adapter)
        record['outputs']['settlement']['status']='running'
        artifact=closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter)
        original=closure.read_support(self.f.run,'native-gateway-disable',plan=self.f.plan,original=artifact)
        self.assertEqual(original['outputs']['settlement']['status'],'completed')
        path=self.f.run/artifact['basename'];value=json.loads(path.read_text());value['outputs']['settlement']['status']='running'
        path.write_text(json.dumps(value))
        with self.assertRaises(ValueError):closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter)

    def test_supervisor_checks_native_closure_digest_as_part_of_full_registry(self):
        import pgcat_lane_supervisor as supervisor
        from pgcat_lane_protocol import Refused
        here=Path(__file__).parent
        owner=object.__new__(supervisor.LaneSupervisor)
        owner.bindings=dict(mode='full-replacement-direct-v2',sourceHashes=dict(self.f.plan['sourceHashes']),
            laneWrapper=str(here/'pgcat_workbench_lane.py'),providerFeedPath=str(here/'provider_feed.py'))
        with patch.object(supervisor,'validate_bindings'):
            owner._verify_sources()
            owner.bindings['sourceHashes']['nativeClosure']='0'*64
            with self.assertRaises(Refused):owner._verify_sources()

    def test_original_context_cannot_be_readopted_by_mutating_prepared_input(self):
        from test_pgcat_native_ingress import file_record
        adapter=self.adapter()
        path=self.f.run/'native-baseline.json';raw=path.read_bytes();path.rename(self.f.run/'old-baseline');path.write_bytes(raw);path.chmod(0o600)
        self.f.prepared['baselineFile']=file_record(path)
        with self.assertRaises(ValueError):self.proof(adapter)

    def test_disable_rejects_secret_replacement_across_guarded_transition(self):
        self.enabled=True;secret=self.objects[('Secret','raptor-system','gateway-live-config')]
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode();self.settlement_value=self.settlement()
        original=self.command
        def replace(argv,**kwargs):
            result=original(argv,**kwargs)
            if argv[0]==str(self.gate) and argv[1]=='set':secret['metadata']['uid']='replaced-secret'
            return result
        with patch.object(subprocess,'run',side_effect=replace),patch.object(native.FixedHTTP,'__call__',side_effect=self.http),self.assertRaises(ValueError):self.disable()
        self.assertFalse((self.f.run/'native-gateway-disable.json').exists())

    def test_support_identity_and_timestamp_reject_bool_integer_equivalence(self):
        self.settlement_value=self.settlement();adapter=self.adapter()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):record=self.disable(adapter)
        artifact=closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter)
        artifact['identity']['nlink']=True
        with self.assertRaises(ValueError):closure.read_support(self.f.run,'native-gateway-disable',plan=self.f.plan,original=artifact)
        record['outputs']['observedAt']=1;record['outputs']['runtime']['observedAt']=True
        with self.assertRaises(ValueError):closure.validate_support(record,self.f.plan,'native-gateway-disable')

    @unittest.skipUnless(hasattr(os,'fork'),'requires real process fork')
    def test_fork_cannot_inherit_issued_artifact_or_guarded_provenance(self):
        import select
        self.enabled=True
        secret=self.objects[('Secret','raptor-system','gateway-live-config')]
        secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode()
        self.settlement_value=self.settlement();adapter=self.adapter()
        with patch.object(native.FixedHTTP,'__call__',side_effect=self.http):record=self.disable(adapter)
        artifact=closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter)
        expected=dict(kind='guarded-verify',resourceVersion='2',changedAfter='2026-10-20T03:00:00Z')
        self.assertEqual(closure._observe(adapter,False,lambda:self.f.now)['provenance'],expected)
        reader,writer=os.pipe();child=os.fork()
        if child==0:
            os.close(reader)
            result={}
            try:
                for name,operation in (
                        ('artifact',lambda:closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter)),
                        ('guarded',lambda:closure._observe(adapter,False,lambda:self.f.now))):
                    try:operation()
                    except ValueError:result[name]='refused'
                    else:result[name]='inherited'
                os.write(writer,json.dumps(result).encode())
                os.close(writer);os._exit(0)
            except BaseException:
                os.close(writer);os._exit(2)
        os.close(writer)
        try:
            ready,_,_=select.select([reader],[],[],5)
            if not ready:
                os.kill(child,9);os.waitpid(child,0)
                self.fail('forked proof child exceeded bounded test budget')
            result=json.loads(os.read(reader,4096))
            _,status=os.waitpid(child,0)
        finally:os.close(reader)
        self.assertEqual(status,0)
        self.assertEqual(result,dict(artifact='refused',guarded='refused'))
        self.assertEqual(closure.support_artifact(self.f.run,'native-gateway-disable',adapters=adapter),artifact)
        self.assertEqual(closure._observe(adapter,False,lambda:self.f.now)['provenance'],expected)
        self.assertEqual(json.loads((self.f.run/artifact['basename']).read_text()),record)


class ActionRuntimeTests(unittest.TestCase):
    """Exercise issuance via real staging and the actual source worker entry."""
    def setUp(self):
        self.fixture_kind='disabled-proof';GatewayClosureTests.setUp(self)

    adapter=GatewayClosureTests.adapter
    command=GatewayClosureTests.command
    proof=GatewayClosureTests.proof

    def entry(self,perform,action='native-disabled-restart'):
        from segmented_capture import sha,canonical
        f=self.f
        envelope=dict(schemaVersion=1,action=action,localRequestId=str(__import__('uuid').uuid4()),runId=f.plan['runId'],
            executionKind='disabled-proof',planSha256=sha(canonical(f.plan)),baselineSha256=f.plan['baselineSha256'],
            feedBinding=f.feed,deadlineMonotonic=1.,prepared=f.prepared,readySha256='a'*64,feedBindingSha256='b'*64)
        # Effect kernel replaced below admission: no PATCH or receipt production.
        # Registration, source/prepared checks and final cleanup stay real.
        if action=='native-ingress':envelope['priorIngress']=None
        target='pgcat_native_ingress.restart_disabled' if action=='native-disabled-restart' else 'pgcat_native_controller.collect_ingress'
        with patch.object(native,'_closed'),patch(target,side_effect=perform),patch.object(native,'validate_action_result',side_effect=lambda value,*args:value):
            return native.worker_entry(action,f.run,envelope,{})

    def run_entry(self,body):
        from test_pgcat_native_ingress import write
        def perform(run,gate,**kwargs):
            body(gate);write(run/'native-restart-receipt.json',{})
            return {}
        return self.entry(perform)

    def test_actual_worker_entry_cold_once_warm_twice_and_cleanup(self):
        records=[];retained=[]
        def body(gate):
            retained.append(gate.reads)
            for _ in range(3):records.append(gate.gateway_disabled())
            records[0]['verification']['ready']=False
            self.assertTrue(records[1]['verification']['ready'])
        self.run_entry(body)
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)
        self.assertEqual(records[1],records[2])
        self.assertEqual(len([a for a in self.calls if 'get' in a]),18)
        retained[0].gateway_disabled()
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),2)

    def test_warm_drift_is_terminal_even_after_restoration(self):
        pod=self.objects[('Pod','raptor-system',None)]['items'][0]
        def body(gate):
            gate.gateway_disabled();old=pod['status']['containerStatuses'][0]['restartCount']
            pod['status']['containerStatuses'][0]['restartCount']=old+1
            with self.assertRaises(ValueError):gate.gateway_disabled()
            pod['status']['containerStatuses'][0]['restartCount']=old
            with self.assertRaises(ValueError):gate.gateway_disabled()
        self.run_entry(body)
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_unregistered_and_copied_reads_inspect_freshly(self):
        a=self.adapter();self.proof(a);self.proof(a)
        self.assertEqual(len([v for v in self.calls if v[0]==str(self.gate)]),2)
        self.calls=[]
        def body(gate):
            gate.gateway_disabled()
            copied=copy.copy(gate.reads);copied.gateway_disabled();gate.gateway_disabled()
        self.run_entry(body)
        self.assertEqual(len([v for v in self.calls if v[0]==str(self.gate)]),2)

    def test_actual_entry_failure_removes_eligibility(self):
        retained=[]
        def body(gate):
            retained.append(gate.reads);gate.gateway_disabled();raise ValueError('effect refused')
        with self.assertRaises(ValueError):self.run_entry(body)
        retained[0].gateway_disabled()
        self.assertEqual(len([v for v in self.calls if v[0]==str(self.gate)]),2)

    def test_false_idle_completion_rejects_aging_after_actual_runtime(self):
        a=self.adapter();idle=a._idle;count=[0]
        def delayed():
            idle();count[0]+=1
            if count[0]==2:self.f.now+=30
        with patch.object(a,'_idle',side_effect=delayed),self.assertRaises(ValueError):
            closure.gateway_false_idle(self.f.run,adapters=a,now=lambda:self.f.now)

    def test_all_pinned_metadata_drift_poison_without_favorable_reissue(self):
        original=copy.deepcopy(self.objects)
        for change in ('secret-uid','secret-version','config-bytes','deployment-version','generation','readiness','deployment-spec','pod-spec','owners','container-id','started','restart','image-id','init-id','init-state','ephemeral','deletion','ambiguous'):
            self.objects=copy.deepcopy(original);self.calls=[]
            def body(gate):
                gate.gateway_disabled()
                dep=self.objects[('Deployment','raptor-system','agent-gateway')]
                secret=self.objects[('Secret','raptor-system','gateway-live-config')]
                pod=self.objects[('Pod','raptor-system',None)]['items'][0]
                container=pod['status']['containerStatuses'][0];init=pod['status']['initContainerStatuses'][0]
                if change=='secret-uid':secret['metadata']['uid']='other'
                if change=='secret-version':secret['metadata']['resourceVersion']='2'
                if change=='config-bytes':secret['data']['config.json']=base64.b64encode(b'{"replacementEnabled":false,"other":1}').decode()
                if change=='deployment-version':dep['metadata']['resourceVersion']='2'
                if change=='generation':dep['metadata']['generation']=2;dep['status']['observedGeneration']=2
                if change=='readiness':dep['status']['readyReplicas']=0
                if change=='deployment-spec':dep['spec']['template'].setdefault('metadata',{})['annotations']={'changed':'yes'}
                if change=='pod-spec':pod['spec']['terminationGracePeriodSeconds']=10
                if change=='owners':pod['metadata']['ownerReferences'][0]['uid']='other-owner'
                if change=='container-id':container['containerID']='containerd://other'
                if change=='started':container['state']['running']['startedAt']='2026-10-21T00:00:00Z'
                if change=='restart':container['restartCount']=1
                if change=='image-id':container['imageID']='sha256:'+'0'*64
                if change=='init-id':init['containerID']='containerd://other'
                if change=='init-state':init['state']['terminated']['exitCode']=1
                if change=='ephemeral':pod['spec']['ephemeralContainers']=[dict(name='debug',volumeMounts=[dict(name='live-private',mountPath='/read')])]
                if change=='deletion':pod['metadata']['deletionTimestamp']='2026-10-21T00:00:00Z'
                if change=='ambiguous':self.objects[('Pod','raptor-system',None)]['items'].append(copy.deepcopy(pod))
                with self.assertRaises(ValueError):gate.gateway_disabled()
                self.objects=copy.deepcopy(original)
                with self.assertRaises(ValueError):gate.gateway_disabled()
            with self.subTest(change=change):self.run_entry(body)
            self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_harmless_pod_resource_version_preserves_original_observed_at(self):
        results=[]
        def body(gate):
            results.append(gate.gateway_disabled())
            self.objects[('Pod','raptor-system',None)]['items'][0]['metadata']['resourceVersion']='new-unrelated-status'
            results.append(gate.gateway_disabled())
        self.run_entry(body)
        self.assertEqual(results[0],results[1])
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_original_pid_request_deadline_and_wall_clock_cannot_be_renewed(self):
        for change in ('pid','request','deadline','clock','expired','stale'):
            self.calls=[]
            def body(gate):
                with patch.object(native.time,'time',return_value=self.f.now):first=gate.gateway_disabled()
                with __import__('contextlib').ExitStack() as stack:
                    if change=='pid':stack.enter_context(patch.object(closure.os,'getpid',return_value=os.getpid()+1))
                    if change=='request':gate.envelope['localRequestId']=str(__import__('uuid').uuid4())
                    if change=='deadline':gate.reads.deadline+=1
                    if change=='clock':stack.enter_context(patch.object(closure.time,'monotonic',return_value=0.))
                    if change=='expired':stack.enter_context(patch.object(closure.time,'monotonic',return_value=gate.deadline))
                    stack.enter_context(patch.object(native.time,'time',return_value=first['observedAt']+(30 if change=='stale' else 0)))
                    with self.assertRaises(ValueError):gate.gateway_disabled()
                # Restore apparent values; a poisoned action still cannot reissue.
                gate.reads.deadline=gate.deadline
                with self.assertRaises(ValueError):gate.gateway_disabled()
            with self.subTest(change=change):self.run_entry(body)
            self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_failed_cold_inspection_never_retries_even_after_failure_clears(self):
        def body(gate):
            self.failure_mode='inspect'
            with self.assertRaises(ValueError):gate.gateway_disabled()
            self.failure_mode=None
            with self.assertRaises(ValueError):gate.gateway_disabled()
        self.run_entry(body)
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_binary_source_and_credential_drift_poison_context(self):
        for change in ('binary','source','credential','prepared'):
            self.calls=[]
            def body(gate):
                gate.gateway_disabled()
                path={'binary':self.gate,'source':self.f.run/'worker_capture.py','credential':self.root/'kubeconfig','prepared':self.f.run/'native-baseline.json'}[change]
                old=path.read_bytes();path.write_bytes(old+b'changed')
                with self.assertRaises(ValueError):gate.gateway_disabled()
                path.write_bytes(old)
                with self.assertRaises(ValueError):gate.gateway_disabled()
            with self.subTest(change=change):self.run_entry(body)
            self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_fork_inherits_bytes_without_original_issuance_authority(self):
        if not hasattr(os,'fork'):self.skipTest('requires process inheritance')
        def body(gate):
            gate.gateway_disabled();reader,writer=os.pipe();child=os.fork()
            if child==0:
                os.close(reader)
                try:
                    try:gate.gateway_disabled();value=b'accepted'
                    except ValueError:value=b'refused'
                    os.write(writer,value)
                finally:os.close(writer);os._exit(0)
            os.close(writer)
            try:
                import select
                ready,_,_=select.select([reader],[],[],2)
                if not ready:
                    os.kill(child,9);os.waitpid(child,0);self.fail('forked child exceeded test budget')
                self.assertEqual(os.read(reader,32),b'refused');os.waitpid(child,0)
            finally:os.close(reader)
            gate.gateway_disabled()
        self.run_entry(body)
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),1)

    def test_false_idle_retains_actual_observed_at_and_both_idle_checks(self):
        a=self.adapter();idle=a._idle;count=[0]
        def trailing():
            idle();count[0]+=1
            if count[0]==2:self.f.now+=.25
        initial=self.f.now
        with patch.object(a,'_idle',side_effect=trailing):result=closure.gateway_false_idle(self.f.run,adapters=a,now=lambda:self.f.now)
        self.assertEqual(count,[2]);self.assertEqual(result['observedAt'],initial)
        self.assertEqual(self.f.now,initial+.25)

    def test_cold_fingerprint_rejects_incomplete_or_mutable_loaded_config_binding(self):
        original=copy.deepcopy(self.objects)
        for change in ('missing-init','failed-init','writable','wrong-command','wrong-copy','foreign-writer','wrong-volume'):
            self.objects=copy.deepcopy(original);self.calls=[]
            def body(gate):
                pod=self.objects[('Pod','raptor-system',None)]['items'][0];spec=pod['spec']
                if change=='missing-init':del pod['status']['initContainerStatuses']
                if change=='failed-init':pod['status']['initContainerStatuses'][0]['state']['terminated']['exitCode']=1
                if change=='writable':spec['containers'][0]['volumeMounts'][0]['readOnly']=False
                if change=='wrong-command':spec['containers'][0]['command']=['/bin/sh','-c','gateway']
                if change=='wrong-copy':spec['initContainers'][0]['args']=['cp another config']
                if change=='foreign-writer':spec['containers'].append(dict(name='writer',volumeMounts=[dict(name='live-private',mountPath='/private')]))
                if change=='wrong-volume':spec['volumes'][0]['emptyDir']['medium']=''
                with self.assertRaises(ValueError):gate.gateway_disabled()
                self.objects=copy.deepcopy(original)
                with self.assertRaises(ValueError):gate.gateway_disabled()
            with self.subTest(change=change):self.run_entry(body)
            self.assertFalse([a for a in self.calls if a[0]==str(self.gate)])

    def test_template_and_actual_native_ingress_entry_never_reuse(self):
        template=self.adapter(template=True)
        self.proof(template);self.proof(template)
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),2)
        self.calls=[]
        def collect(run,selector,gate,**kwargs):
            for _ in range(3):gate.gateway_disabled()
            return {}
        self.entry(collect,action='native-ingress')
        self.assertEqual(len([a for a in self.calls if a[0]==str(self.gate)]),3)

    def test_native_abort_transport_revalidates_actual_returned_false_result(self):
        from pgcat_lane_client import LaneClient
        owner=object.__new__(native.NativeController);client=LaneClient();owner.client=client
        client.journal=SimpleNamespace(native_gateway=lambda action:dict(state='VERIFIED',claim={},launch={}))
        plan=dict(self.f.plan,executionKind='native-replacement');prepared=dict(self.f.prepared,plan=plan)
        native._native_owners[owner]={}
        try:
            for result in (None,dict(self.f.runtime,observedAt=self.f.now-30),dict(self.f.runtime,runId=REQUEST)):
                with self.subTest(result=result),patch.object(owner,'_abort_originals',return_value={'prepared':prepared,'client':client}),patch.object(owner,'_owned_worker',return_value=result),patch.object(native.time,'time',return_value=self.f.now),self.assertRaises(ValueError):
                    owner._abort_gateway_false()
        finally:native._native_owners.pop(owner,None)

    def test_stale_abort_keeps_fail_precedence_and_attempts_both_cleanups(self):
        from test_pgcat_native_assessment import AbortReviewTests
        helper=AbortReviewTests();owner,paths=helper.owner(self.root,prepared=dict(self.f.prepared,plan=self.f.plan));calls=[]
        remote=helper.remote(paths,calls)
        def transport(verb,payload):
            if verb=='gateway-false-idle':
                calls.append((verb,payload));return dict(self.f.runtime,observedAt=self.f.now-30)
            return remote(verb,payload)
        with patch.object(native,'_require_verified'),patch.object(owner,'_retained_ordinary_status',return_value='FAIL'),patch.object(owner,'_owned_worker',side_effect=transport),patch.object(native.time,'time',return_value=self.f.now):
            result=closure.abort_owned(self.root,adapters=owner,now=lambda:self.f.now)
        self.assertEqual(result['gatewayState'],'unknown');self.assertEqual(result['status'],'FAIL')
        self.assertEqual([verb for verb,_ in calls].count('credential-cleanup'),1)
        self.assertTrue(all(not path.exists() for path in paths))

    def test_warm_completion_cannot_cross_original_monotonic_deadline(self):
        def body(gate):
            with patch.object(native.time,'time',return_value=self.f.now):gate.gateway_disabled()
            clock=[__import__('time').monotonic()];calls=[0]
            def decision():
                calls[0]+=1
                if calls[0]==2:clock[0]=gate.deadline+.001
                return self.f.now
            with patch.object(native.time,'time',side_effect=decision),patch.object(closure.time,'monotonic',side_effect=lambda:clock[0]),self.assertRaises(ValueError):
                gate.gateway_disabled()
            with self.assertRaises(ValueError):gate.gateway_disabled()
        self.run_entry(body)

    def test_abort_transport_result_requires_fresh_exact_false_runtime(self):
        owner=object.__new__(native.NativeController)
        prepared=dict(self.f.prepared,plan=self.f.plan)
        for change in ('stale','schema','run','deployment','future'):
            result=copy.deepcopy(self.f.runtime)
            if change=='stale':result['observedAt']=self.f.now-30
            if change=='future':result['observedAt']=self.f.now+1
            if change=='schema':result['extra']='unexpected'
            if change=='run':result['runId']=REQUEST
            if change=='deployment':result['deploymentUid']='foreign'
            with self.subTest(change=change),patch.object(owner,'_abort_originals',return_value={'prepared':prepared}),patch.object(owner,'_owned_worker',return_value=result),patch.object(native.time,'time',return_value=self.f.now),self.assertRaises(ValueError):
                owner._abort_gateway_false()


class GatewayOwnedBatchTests(unittest.TestCase):
    """Only disposable local Popen children; never kubectl or credentials."""
    def adapter(self):
        a=object.__new__(closure.FixedGatewayAdapters)
        a.reads=SimpleNamespace(credential={'path':'/offline/kubeconfig'});a.gate=Path('/offline/gateway-live-gate')
        a.deadline=__import__('time').monotonic()+2
        a._check=lambda:None
        return a

    def batch(self,programs,deadline=2):
        import sys,time
        a=self.adapter();a.deadline=time.monotonic()+deadline;children=[];actual=subprocess.Popen
        def launch(argv,**kwargs):
            kind=argv[argv.index('get')+1]
            self.assertEqual(argv,['kubectl','--kubeconfig','/offline/kubeconfig','-n','raptor-system','get',kind]+([] if kind=='pods' else ['gateway-live-config' if kind=='secret' else 'agent-gateway'])+['-o','json','--request-timeout=2s'])
            child=actual([sys.executable,'-c',programs[kind]],**kwargs);children.append(child);return child
        with patch.object(subprocess,'Popen',side_effect=launch):
            try:return a._metadata_objects()
            finally:
                self.assertEqual(len(children),3)
                self.assertTrue(all(child.poll() is not None for child in children),'owned sibling still running')
                for child in children:
                    self.assertIsNotNone(child.returncode)

    def test_three_owned_reads_overlap_and_keep_exact_channels(self):
        # Every actual child must start before any can complete its read.
        # A sequential communicate/wait implementation cannot cross the barrier.
        with tempfile.TemporaryDirectory() as folder:
            programs={}
            for kind in ('secret','deployment','pods'):
                programs[kind]="import os,time,json\np=%r\nopen(os.path.join(p,%r),'w').close()\nend=time.monotonic()+1\nwhile len(os.listdir(p))<3 and time.monotonic()<end:time.sleep(.01)\nassert len(os.listdir(p))==3\nprint(json.dumps({'kind':%r}))"%(folder,kind,kind)
            result=self.batch(programs)
        self.assertEqual([r['kind'] for r in result],['secret','deployment','pods'])

    def test_failed_malformed_oversized_and_slow_reads_reap_actual_siblings(self):
        for bad in ('raise SystemExit(1)','print("bad-json")','import os;os.write(1,b"x"*(2*1024*1024+1))','import time;time.sleep(4)'):
            with self.subTest(bad=bad),self.assertRaises((ValueError,TimeoutError)):
                self.batch(dict(secret=bad,deployment='import time;time.sleep(4)',pods='import time;time.sleep(4)'),deadline=.3)

    def test_main_thread_interruption_reaps_every_owned_child(self):
        with patch('select.select',side_effect=KeyboardInterrupt),self.assertRaises(KeyboardInterrupt):
            self.batch({k:'import time;time.sleep(4)' for k in ('secret','deployment','pods')})

    def test_batch_refuses_non_main_thread_before_launch(self):
        import threading
        results=[]
        def run():
            try:self.adapter()._metadata_objects()
            except BaseException as error:results.append(type(error))
        with patch.object(subprocess,'Popen',side_effect=AssertionError('child launched outside main thread')):
            thread=threading.Thread(target=run);thread.start();thread.join(timeout=2)
        self.assertFalse(thread.is_alive());self.assertEqual(results,[ValueError])

    def test_one_wait_failure_still_closes_and_reaps_other_owned_siblings(self):
        import sys,time
        a=self.adapter();a.deadline=time.monotonic()+.1;children=[];waits=[];actual=subprocess.Popen
        def launch(argv,**kwargs):
            child=actual([sys.executable,'-c','import time;time.sleep(4)'],**kwargs)
            children.append(child);original=child.wait;waits.append(original)
            if len(children)==1:
                def failing_wait(timeout=None):
                    original(timeout=timeout);raise subprocess.TimeoutExpired('offline wait failure',timeout)
                child.wait=failing_wait
            return child
        try:
            with patch.object(subprocess,'Popen',side_effect=launch),self.assertRaises((ValueError,subprocess.TimeoutExpired)):
                a._metadata_objects()
            self.assertTrue(all(child.stdout.closed for child in children))
            self.assertTrue(all(child.poll() is not None for child in children))
        finally:
            for child,wait in zip(children,waits):
                if child.poll() is None:child.kill()
                wait(timeout=1);child.stdout.close()
