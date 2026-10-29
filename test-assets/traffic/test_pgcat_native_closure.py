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
        self.f = Fixture(self.root)
        self.f.plan['serviceBindings']['gateway'].update(deploymentName='agent-gateway',containerName='agent-gateway')
        from segmented_capture import canonical
        from test_pgcat_native_ingress import file_record
        (self.f.run/'native-plan.json').write_bytes(canonical(self.f.plan))
        self.f.prepared['planFile']=file_record(self.f.run/'native-plan.json')
        self.gate = self.f.run/'gateway-live-gate'; self.gate.write_bytes(b'#!/bin/sh\nexit 1\n'); self.gate.chmod(0o700)
        self.assets = dict(schemaVersion=1, sourceBundleSha256='a'*64, infraApiRevision='b'*40,
            gatewayImage='ghcr.io/jamesdeng/raptor-iap-gateway@sha256:'+'e'*64,
            axImage='ax@sha256:'+'1'*64, clientImage='client@sha256:'+'2'*64,
            gateBinarySha256=hashlib.sha256(self.gate.read_bytes()).hexdigest(),
            skillsTag='skills-v0.4.0',skillsCommit='f'*40,scope=self.f.plan['controllerScope'],application=self.f.plan['application'])
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
        self.assertEqual(len(bootstrap.NATIVE_SOURCES),24);self.assertEqual(len(collector.NATIVE_SOURCES),12)
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
