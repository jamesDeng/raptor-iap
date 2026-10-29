"""Native startup security contracts. Local files and harmless children only."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import pgcat_lane_actions as actions
import pgcat_source_bootstrap as bootstrap
from pgcat_lane_protocol import Refused
from test_pgcat_native_ingress import Fixture as IngressFixture, REQUEST, file_record, write

native = None
if importlib.util.find_spec('pgcat_native_controller'):
    import pgcat_native_controller as native


def Fixture(root,kind='native-replacement'):
    from test_pgcat_native_ingress import plan as original_plan
    sources=bootstrap.NATIVE_SOURCES;here=Path(__file__).parent
    hashes={k:hashlib.sha256((here/v).read_bytes()).hexdigest() for k,v in sources.items()}
    def pinned(kind='native-replacement'):
        value=original_plan(kind);value['sourceHashes']=hashes;return value
    with patch('test_pgcat_native_ingress.plan',side_effect=pinned):value=IngressFixture(root,kind)
    for name in sources.values():
        shutil.copyfile(here/name,value.run/name);(value.run/name).chmod(0o600)
    credential=root/'kubeconfig';credential.write_bytes(b'private-offline-fixture');credential.chmod(0o600)
    value.prepared=dict(planFile=file_record(value.run/'native-plan.json'),baselineFile=file_record(value.run/'native-baseline.json'),gatewayRuntimeFile=file_record(value.run/'native-gateway-runtime.json'),workerCredential=native.credential_identity(credential))
    return value


class NativeStartupTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(native, 'native startup kernel missing')
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(os.path.realpath(self.tmp.name));self.root.chmod(0o700)

    def test_exact_native_closure_and_subset(self):
        self.assertEqual(len(bootstrap.source_names('full-replacement-direct-v2')),24)
        self.assertEqual(len(bootstrap.source_names('no-scale')),20)
        self.assertEqual(set(native.collector_sources()),set(__import__('pgcat_collector_control').SOURCES)|{'nativeEvidence'})
        with self.assertRaises(ValueError):bootstrap.source_names('direct-v2')
        with self.assertRaises(ValueError):bootstrap.select_sources(dict.fromkeys(list(bootstrap.SOURCES)[:-1],'a'*64))

    def test_catalog_rejects_all_legacy_actions_before_templates(self):
        from pgcat_lane_protocol import ACTIONS
        b=dict(mode='full-replacement-direct-v2',executionKind='native-replacement')
        for action in ACTIONS|{'native-ingress','native-disabled-restart','native-cleanup'}:
            with self.subTest(action=action),self.assertRaises(Refused):
                actions.ActionCatalog.command(action,b)

    def test_all_public_entrypoints_remain_release_closed(self):
        from pgcat_lane_client import LaneClient
        c=object.__new__(native.NativeController)
        with patch('subprocess.Popen',side_effect=AssertionError('spawned')):
            for method,args in [('prepare',('/missing','/missing')),('start_capture',()),('ingest_request',(REQUEST,)),('restart_disabled',())]:
                with self.subTest(method=method),self.assertRaises(Refused):getattr(c,method)(*args)
            with self.assertRaises(Refused):LaneClient.start({},native.MODE,self.root/'lane',native_plan={})
            with self.assertRaises(Refused):native.worker_entry('native-ingress',self.root)
        self.assertEqual(list(self.root.iterdir()),[])

    def test_credentials_bound_to_opened_inode_and_never_name_only(self):
        path=self.root/'kubeconfig';path.write_bytes(b'private-fixture');path.chmod(0o600)
        original=native.credential_identity(path)
        self.assertEqual(set(original),{'path','device','inode','uid','mode','size','sha256'})
        native.verify_credential(original)
        path.rename(self.root/'original');path.write_bytes(b'private-fixture');path.chmod(0o600)
        with self.assertRaises(ValueError):native.verify_credential(original)
        path.unlink();path.symlink_to(self.root/'original')
        with self.assertRaises((OSError,ValueError)):native.credential_identity(path)

    def test_prefeed_reader_binds_allocated_run_without_future_hashes(self):
        from test_pgcat_native_ingress import plan
        value=plan();run_id=value['runId'];template=dict(value)
        del template['runId'];del template['baselineSha256']
        path=self.root/'kubeconfig';path.write_bytes(b'private-fixture');path.chmod(0o600)
        reader=native.FixedWorkerReads(template,path,run_id=run_id)
        self.assertEqual(reader.run_id,run_id)
        self.assertNotIn('runId',reader.plan);self.assertNotIn('baselineSha256',reader.plan)
        self.assertEqual(native.FixedWorkerReads(value,path,credential=reader.credential).run_id,run_id)
        with self.assertRaises(ValueError):native.FixedWorkerReads(value,path)
        with self.assertRaises(ValueError):native.FixedWorkerReads(template,path)
        with self.assertRaises(ValueError):native.FixedWorkerReads(value,path,run_id='00000000-0000-0000-0000-000000000000')

    def test_native_ready_requires_committed_frames_and_live_processes(self):
        f=Fixture(self.root);(f.run/'native-ready.json').unlink()
        ready=native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,probe_process=f.probe_process,
            live_clients=f.live_clients,now=lambda:f.now)
        self.assertEqual(ready['frames'],f.ready['frames'])
        self.assertEqual(ready['sources'],f.ready['sources'])
        with self.assertRaises((OSError,ValueError)):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,
            probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)
        (f.run/'native-ready.json').unlink();f.append(5)
        with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,
            probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)
        self.assertFalse((f.run/'native-ready.json').exists())

    def test_ready_rejects_process_reuse_and_changed_sources_before_publish(self):
        f=Fixture(self.root);(f.run/'native-ready.json').unlink()
        for probe in (lambda pid:None,lambda pid:dict(f.probe_process(pid),startTicks=999)):
            with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,
                probe_process=probe,live_clients=f.live_clients,now=lambda:f.now)
        self.assertFalse((f.run/'native-ready.json').exists())

    def test_nonterminal_ingress_observation_and_exact_final_mirror(self):
        f=Fixture(self.root)
        result=native.collect_ingress(f.run,dict(requestId=REQUEST,approvalIds=[]),gate=f,
            kube_read=f.kube,http_get=f.http,now=lambda:f.now)
        self.assertEqual(result['status'],'OBSERVED')
        self.assertEqual(result['ingressFile']['identity'],file_record(f.run/result['ingressFile']['basename']))
        self.assertNotIn('operationFile',result)
        self.assertFalse((f.run/'native-operation-binding.json').exists())
        source=f.run/'native-operation-binding.json';source.write_bytes(b'{ "original": true }\n');source.chmod(0o600)
        result=native.mirror_operation_binding(f.run)
        self.assertEqual((f.run/'native-capture'/'native-operation-binding.json').read_bytes(),source.read_bytes())
        self.assertEqual(result['sha256'],hashlib.sha256(source.read_bytes()).hexdigest())
        with self.assertRaises(FileExistsError):native.mirror_operation_binding(f.run)

    def test_actual_child_handle_and_pid_reuse(self):
        child=subprocess.Popen([sys.executable,'-I','-S','-c','import time;time.sleep(30)'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        self.addCleanup(lambda: child.poll() is None and (child.kill(),child.wait()))
        identity=dict(pid=child.pid,bootId='offline-worker',startTicks=1,commandSha256='a'*64)
        self.assertEqual(native.owned_process(child,lambda pid:identity),identity)
        with self.assertRaises(ValueError):native.owned_process(child,lambda pid:dict(identity,pid=child.pid+1))
        child.terminate();child.wait(timeout=5)
        with self.assertRaises(ValueError):native.owned_process(child,lambda pid:identity)


class NativeSourceProcessTests(unittest.TestCase):
    def test_native_transitive_tamper_refuses_before_import_side_effect(self):
        self.assertTrue(hasattr(bootstrap,'NATIVE_SOURCES'),'native closure missing')
        with tempfile.TemporaryDirectory() as d:
            root=Path(os.path.realpath(d));here=Path(__file__).parent
            for name in bootstrap.NATIVE_SOURCES.values():shutil.copyfile(here/name,root/name)
            hashes={k:hashlib.sha256((root/v).read_bytes()).hexdigest() for k,v in bootstrap.NATIVE_SOURCES.items()}
            marker=root/'executed'
            script=bootstrap.ROOT.replace('sys.exit(m.main())',
                "m.install(__import__('json').loads(sys.argv[1]));__import__('importlib').import_module('pgcat_native_evidence');print('VERIFIED')")
            argv=[sys.executable,'-I','-S','-c',script,str(root/'pgcat_source_bootstrap.py'),hashes['sourceBootstrap'],json.dumps(hashes)]
            healthy=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            self.assertEqual(healthy.returncode,0,healthy.stderr);self.assertEqual(healthy.stdout,b'VERIFIED\n')
            (root/'pgcat_native_evidence.py').write_text('open(%r,"w").write("bad")\n'%str(marker))
            result=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            self.assertNotEqual(result.returncode,0);self.assertFalse(marker.exists());self.assertEqual(result.stdout,b'')

class NativeHTTPTests(unittest.TestCase):
    def test_fixed_http_rejects_redirect_and_error_body_without_leaking_credentials(self):
        self.assertTrue(hasattr(native,'FixedHTTP'),'bounded fixed HTTP adapter missing')
        import socket
        import threading
        from test_pgcat_native_ingress import plan
        mode=[200];seen=[]
        def connect(connection):
            self.assertEqual((connection.host,connection.port),('10.0.0.10',8874))
            client,server=socket.socketpair();client.settimeout(connection.timeout)
            status=mode[0]
            def serve():
                with server:
                    data=b''
                    while b'\r\n\r\n' not in data:data+=server.recv(4096)
                    seen.append(data)
                    body=b'{"data":{}}' if status==200 else b'private-error-password'
                    server.sendall(('HTTP/1.1 %d Test\r\nContent-Length: %d\r\nLocation: http://untrusted/\r\n\r\n'%(status,len(body))).encode()+body)
            threading.Thread(target=serve,daemon=True).start()
            connection.sock=client
        client=native.FixedHTTP(plan())
        with patch.dict(os.environ,{'HTTP_PROXY':'http://should-not-be-used.invalid'}),patch('http.client.HTTPConnection.connect',new=connect):
            self.assertEqual(client('http://10.0.0.10:8874','/v1/requests/'+REQUEST,('u','private-password')),b'{"data":{}}')
            for status in (302,401,500):
                mode[0]=status
                with self.assertRaisesRegex(ValueError,'native HTTP') as caught:client('http://10.0.0.10:8874','/v1/requests/'+REQUEST,('u','private-password'))
                self.assertIsNone(caught.exception.__context__)
        self.assertEqual(len(seen),4)
        for origin,path in [('http://evil:8874','/v1/requests/'+REQUEST),('http://10.0.0.10:8874','/v1/requests/'+REQUEST+'/context'),('http://10.0.0.10:8874','//evil'),('http://10.0.0.10:8874','/v1/requests/'+REQUEST+'?x=1')]:
            with self.assertRaises(ValueError):client(origin,path,('u','p'))

    def test_http_header_drip_has_real_total_deadline(self):
        self.assertTrue(hasattr(native,'FixedHTTP'),'bounded fixed HTTP adapter missing')
        import socket,threading
        from test_pgcat_native_ingress import plan
        client_socket,server=socket.socketpair();self.addCleanup(client_socket.close)
        def slow():
            with server:
                server.recv(4096)
                for byte in b'HTTP/1.1 200 OK\r\n':
                    try:server.send(bytes([byte]));time.sleep(.1)
                    except OSError:break
        threading.Thread(target=slow,daemon=True).start()
        client=native.FixedHTTP(plan());client.deadline=time.monotonic()+.25
        started=time.monotonic()
        with patch('http.client.HTTPConnection.connect',new=lambda connection:setattr(connection,'sock',client_socket)):
            with self.assertRaises(ValueError):client('http://10.0.0.10:8874','/v1/requests/'+REQUEST,('u','p'))
        self.assertLess(time.monotonic()-started,1.)
        self.assertGreater(time.monotonic()-started,.20)

class NativeBaselineTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(os.path.realpath(self.tmp.name));self.root.chmod(0o700)
        self.f=Fixture(self.root)
        from segmented_capture import read_progress
        self.f.frame=read_progress(self.f.run/'native-capture',self.f.context)[2]
        dep=self.f.live['deployment'];dep['status'].update(replicas=2,updatedReplicas=2,readyReplicas=2,availableReplicas=2)
        for pod in self.f.live['pods']:
            pod['status']['containerStatuses'][0].update(ready=True,state={'running':{}})
        self.f.objects[('Pod','raptor-test',None)]={'items':self.f.live['pods']}
        self.f.objects[('ReplicaSet','raptor-test',None)]={'items':self.f.live['replicaSets']}
        self.f.objects[('Service','raptor-test','rdev-test-metrics-metrics')]={'metadata':{'name':'rdev-test-metrics-metrics','namespace':'raptor-test','uid':'metrics'},'spec':{'clusterIP':'10.2.0.1','ports':[{'port':9090}]}}

    def readers(self):
        f=self.f
        def provider():
            return dict(schemaVersion=2,observedAt=f.now,privateAddresses={'a':'10.1.0.1','b':'10.1.0.2'},**f.baseline['fleet'])
        def pod_read(pod):
            return copy.deepcopy(next(p for p in f.frame['pods'] if p['uid']==pod['metadata']['uid']))
        def metrics(origin):
            self.assertEqual(origin,'http://10.2.0.1:9090');return copy.deepcopy(f.frame['metrics'])
        return dict(provider_read=provider,kube_read=f.kube,read_pod=pod_read,read_metrics=metrics,now=lambda:f.now)

    def test_actual_prefeed_baseline_derives_owner_and_metrics_without_future_hashes(self):
        self.assertTrue(hasattr(native,'collect_baseline'),'actual baseline collection missing')
        f=self.f
        result=native.collect_baseline(f.plan['runId'],f.plan,**self.readers())
        self.assertEqual(result,dict(f.baseline,observedAt=f.now))
        self.assertFalse({'planSha256','feedSession','contextSha256'} & set(result))
        for change in ('wrong-image','foreign-rs','gateway','cm','final','metrics'):
            readers=self.readers();original=copy.deepcopy(f.objects)
            if change=='wrong-image':f.live['pods'][0]['status']['containerStatuses'][0]['imageID']='wrong'
            elif change=='foreign-rs':f.live['replicaSets'][0]['metadata']['ownerReferences'][0]['uid']='foreign'
            elif change=='gateway':f.objects[('Deployment','raptor-system','gateway')]['metadata']['uid']='other'
            elif change=='cm':f.objects[('ConfigMap','raptor-test','pgcat-provider-inventory')]['data']['targets.json']='[]'
            elif change=='final':readers['read_pod']=lambda pod:dict(self.readers()['read_pod'](pod),snapshot={'final':True})
            else:readers['read_metrics']=lambda origin:dict(f.frame['metrics'],raw=[])
            with self.subTest(change=change),self.assertRaises((ValueError,KeyError)):native.collect_baseline(f.plan['runId'],f.plan,**readers)
            f.objects=original
            f.live['pods']=f.objects[('Pod','raptor-test',None)]['items'];f.live['replicaSets']=f.objects[('ReplicaSet','raptor-test',None)]['items']

    def test_plan_seal_is_acyclic_and_single_use(self):
        self.assertTrue(hasattr(native,'prepare_records'),'immutable plan preparation missing')
        f=self.f;target=self.root/'new'/'run';target.mkdir(mode=0o700,parents=True);target.parent.chmod(0o700)
        template={k:v for k,v in f.plan.items() if k not in ('runId','baselineSha256')}
        for name in bootstrap.NATIVE_SOURCES.values():
            shutil.copyfile(f.run/name,target/name);(target/name).chmod(0o600)
        result=native.prepare_records(target,template,f.plan['runId'],**self.readers())
        plan=json.loads((target/'native-plan.json').read_bytes());baseline=json.loads((target/'native-baseline.json').read_bytes())
        self.assertEqual(plan['runId'],baseline['runId'])
        self.assertEqual(plan['baselineSha256'],hashlib.sha256((target/'native-baseline.json').read_bytes()).hexdigest())
        self.assertNotIn('planSha256',baseline);self.assertEqual(result['planFile'],file_record(target/'native-plan.json'))
        with self.assertRaises(FileExistsError):native.prepare_records(target,template,f.plan['runId'],**self.readers())

    def test_collection_uses_advancing_observation_clock_and_keeps_freshness(self):
        f=self.f
        for offset in (0,-4,2):
            with self.subTest(sample_offset=offset):
                readers=self.readers();clock=[f.now];pod=readers['read_pod'];metrics=readers['read_metrics']
                def advancing_pod(value):
                    clock[0]+=.1;row=pod(value);row['snapshot']['sample']['at']=clock[0]+offset;return row
                def advancing_metrics(origin):
                    clock[0]+=1.2;row=metrics(origin);row['observedAt']=clock[0];return row
                readers.update(read_pod=advancing_pod,read_metrics=advancing_metrics,now=lambda:clock[0])
                if offset:
                    with self.assertRaises(ValueError):native.collect_baseline(f.plan['runId'],f.plan,**readers)
                else:
                    result=native.collect_baseline(f.plan['runId'],f.plan,**readers)
                    self.assertEqual(result['observedAt'],clock[0]);self.assertGreater(clock[0],f.now+1)

    def test_collection_rejects_stale_or_future_metrics_on_current_clock(self):
        f=self.f
        for offset in (-4,2):
            readers=self.readers();original=readers['read_metrics']
            readers['read_metrics']=lambda origin:dict(original(origin),observedAt=f.now+offset)
            with self.subTest(offset=offset),self.assertRaises(ValueError):native.collect_baseline(f.plan['runId'],f.plan,**readers)

class NativeCaptureContextTests(unittest.TestCase):
    def test_native_feed_checks_originals_before_first_actual_publication(self):
        import provider_feed as feed
        for name in (None,'native-plan.json','native-baseline.json','native-gateway-runtime.json','kubeconfig'):
            with self.subTest(replaced=name),tempfile.TemporaryDirectory() as d:
                root=Path(d).resolve();root.chmod(0o700);f=Fixture(root)
                context=json.loads((f.source/'run-context.json').read_bytes())['runContext']
                inventory=f.run/'provider-inventory.json';value=json.loads(inventory.read_bytes());value['sequence']=1
                raw=json.dumps(value,separators=(',',':')).encode('ascii');shutil.rmtree(f.source);inventory.unlink()
                prefix=native.prepared_source_prefix(f.run,f.plan,f.prepared)
                if name is not None:
                    path=(root if name=='kubeconfig' else f.run)/name;data=path.read_bytes()
                    path.rename(path.with_name('held-'+name));path.write_bytes(data);path.chmod(0o600)
                script=prefix+'import hashlib,json,os,stat,sys,time; from pathlib import Path\n'+feed._PRIVATE_PUBLICATION_SOURCE+'\nwrite_private_publication('+','.join(repr(v) for v in (
                    str(root/'contract'),context,f.context['feedSession'],1,f.now,{'a':'10.1.0.1','b':'10.1.0.2'},str(inventory),raw))+')\n'
                result=subprocess.run([sys.executable,'-I','-S','-c',script],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
                if name is None:
                    self.assertEqual(result.returncode,0,result.stderr);self.assertTrue((f.source/'000001.json').exists())
                    self.assertEqual(inventory.read_bytes(),raw)
                else:
                    self.assertNotEqual(result.returncode,0);self.assertFalse(f.source.exists());self.assertFalse(inventory.exists())

    def test_actual_child_keeps_original_digest_when_intent_and_objects_are_replaced(self):
        from segmented_capture import canonical,sha
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();root.chmod(0o700);f=Fixture(root)
            job=f.run/'collector-job';job.mkdir(mode=0o700)
            started=dict(runId=f.plan['runId'],startedAt=int(f.now),profile=native.MODE,captureMaxSeconds=75,prepared=f.prepared)
            write(job/'intent.json',started);expected=sha(canonical(f.prepared))
            script=bootstrap.ROOT.replace('sys.exit(m.main())',
                "m.install(__import__('json').loads(sys.argv[1]));c=__import__('pgcat_collector_control');from pathlib import Path;"
                "c.native_prepared(Path(sys.argv[2]),__import__('json').loads((Path(sys.argv[2])/'collector-job'/'intent.json').read_bytes()),sys.argv[3]);print('BOUND')")
            argv=[sys.executable,'-I','-S','-c',script,str(f.run/'pgcat_source_bootstrap.py'),f.plan['sourceHashes']['sourceBootstrap'],json.dumps(f.plan['sourceHashes']),str(f.run),expected]
            healthy=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
            self.assertEqual(healthy.returncode,0,healthy.stderr);self.assertEqual(healthy.stdout,b'BOUND\n')
            replaced=copy.deepcopy(f.prepared)
            for key,name in (('planFile','native-plan.json'),('baselineFile','native-baseline.json'),('gatewayRuntimeFile','native-gateway-runtime.json'),('workerCredential','kubeconfig')):
                path=(root if key=='workerCredential' else f.run)/name;raw=path.read_bytes()
                path.rename(path.with_name('held-'+name));path.write_bytes(raw);path.chmod(0o600)
                replaced[key]=native.credential_identity(path) if key=='workerCredential' else file_record(path)
            # Each newly adopted identity is self-consistent, but none was in
            # the original source-fixed launch digest inherited by this child.
            native.verify_prepared(f.run,f.plan,replaced)
            (job/'intent.json').rename(job/'held-intent.json');write(job/'intent.json',dict(started,prepared=replaced))
            refused=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
            self.assertNotEqual(refused.returncode,0);self.assertEqual(refused.stdout,b'')

    def test_original_prepared_objects_reject_each_same_byte_inode_replacement(self):
        for name in ('native-plan.json','native-baseline.json','native-gateway-runtime.json','kubeconfig'):
            with self.subTest(name=name),tempfile.TemporaryDirectory() as d:
                root=Path(d).resolve();root.chmod(0o700);f=Fixture(root)
                native.verify_prepared(f.run,f.plan,f.prepared)
                path=(root if name=='kubeconfig' else f.run)/name
                raw=path.read_bytes();old=path.stat().st_ino;path.rename(path.with_name('held-'+name));path.write_bytes(raw);path.chmod(0o600)
                self.assertNotEqual(old,path.stat().st_ino)
                with self.assertRaises(ValueError):native.verify_prepared(f.run,f.plan,f.prepared)
                (f.run/'native-ready.json').unlink()
                with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)
                self.assertFalse((f.run/'native-ready.json').exists())
                (f.run/'native-context.json').unlink();job=f.run/'collector-job';job.mkdir(mode=0o700)
                identity=dict(f.context['collectorIdentity'],pid=os.getpid());write(job/'identity.json',identity)
                with self.assertRaises(ValueError):native.seal_capture_context(f.run,f.plan['sourceHashes'],prepared=f.prepared,probe_process=lambda pid:identity,live_clients=f.live_clients,now=lambda:f.now)
                self.assertFalse((f.run/'native-context.json').exists())
                if name=='kubeconfig':
                    with self.assertRaises(ValueError):native.FixedWorkerReads(f.plan,path,credential=f.prepared['workerCredential'])

    def test_child_seals_own_context_and_rejects_first_publication_mismatch(self):
        self.assertTrue(hasattr(native,'seal_capture_context'),'capture child context missing')
        with tempfile.TemporaryDirectory() as d:
            root=Path(os.path.realpath(d));root.chmod(0o700);f=Fixture(root)
            (f.run/'native-context.json').unlink()
            job=f.run/'collector-job';job.mkdir(mode=0o700)
            identity=dict(f.context['collectorIdentity'],pid=os.getpid())
            write(job/'identity.json',identity)
            result=native.seal_capture_context(f.run,f.plan['sourceHashes'],prepared=f.prepared,probe_process=lambda pid:identity,live_clients=f.live_clients,now=lambda:f.now)
            self.assertEqual(result['collectorIdentity']['pid'],os.getpid())
            self.assertEqual(result['sourceHashes'],{k:f.plan['sourceHashes'][k] for k in native.collector_sources()})
            self.assertEqual(result['providerRunContextSha256'],hashlib.sha256((f.source/'run-context.json').read_bytes()).hexdigest())
            (f.run/'native-context.json').unlink();(f.run/'pgcat_native_ingress.py').write_text('changed')
            with self.assertRaises(ValueError):native.seal_capture_context(f.run,f.plan['sourceHashes'],prepared=f.prepared,probe_process=lambda pid:identity,live_clients=f.live_clients,now=lambda:f.now)
            self.assertFalse((f.run/'native-context.json').exists())

    def test_ready_rejects_pinned_source_tamper(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(os.path.realpath(d));root.chmod(0o700);f=Fixture(root)
            (f.run/'native-ready.json').unlink();(f.run/'pgcat_native_evidence.py').write_text('changed')
            with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)
            self.assertFalse((f.run/'native-ready.json').exists())

class NativeProfileTests(unittest.TestCase):
    def test_capture_cap_reserves_hold_and_closure_and_refuses_after_cutoff(self):
        self.assertTrue(hasattr(native,'capture_budget'),'native closure budget missing')
        from test_pgcat_native_ingress import plan
        value=plan();value['deadline']['captureMaxSeconds']=5400
        import datetime
        at=datetime.datetime(2026,10,29,13,29,tzinfo=datetime.timezone.utc).timestamp()
        self.assertEqual(native.capture_budget(value,at),3360)
        with self.assertRaises(ValueError):native.capture_budget(value,at+60)
        value['deadline']['captureMaxSeconds']=75
        self.assertEqual(native.capture_budget(value,at),75)

    def test_native_collector_direct_entry_refuses_before_spawn_and_keeps_legacy_profile(self):
        import pgcat_collector_control as collector
        self.assertEqual(collector.profile_values('no-scale'),('collector-capture',65,80,90))
        self.assertTrue(hasattr(collector,'NATIVE_SOURCES'),'native collector profile missing')
        for entry in (collector.launch,collector.run_child,collector.status):
            with patch('subprocess.Popen',side_effect=AssertionError('spawned')):
                with self.assertRaises((Refused,ValueError)):
                    if entry is collector.status:entry('/missing','invalid',{},profile=native.MODE)
                    else:entry('/missing','invalid','/missing',{},profile=native.MODE)

class NativeJournalTests(unittest.TestCase):
    def test_journal_native_plan_is_separate_from_provider_bundle_and_rechecked(self):
        from pgcat_lane_protocol import Journal,encode,digest
        self.assertTrue(hasattr(Journal,'native_plan'),'native Journal binding missing')
        with tempfile.TemporaryDirectory() as d:
            root=Path(os.path.realpath(d));root.chmod(0o700);f=Fixture(root)
            lane=root/'lane';lane.mkdir(mode=0o700);journal=Journal(lane)
            raw=(f.run/'native-plan.json').read_bytes();(lane/'native-plan.json').write_bytes(raw);(lane/'native-plan.json').chmod(0o600)
            # Durable fixture only; no admission override or native action executes.
            with journal._transaction() as db:
                db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',(f.plan['runId'],native.MODE,encode(f.plan['target']).decode(),encode(dict(f.plan['sourceHashes'],bundleSha256=digest(f.plan['sourceHashes']))).decode(),'active',None,None))
                db.execute('INSERT INTO native_plan VALUES (1,?,?)',(raw,encode(file_record(lane/'native-plan.json')).decode()))
                db.execute('INSERT INTO native_prepared VALUES (1,?)',(encode(f.prepared).decode(),))
                db.execute('INSERT INTO feed_binding VALUES (1,?)',(encode(f.feed).decode(),))
            self.assertEqual(journal.native_plan(),f.plan)
            request=dict(requestId=f.admitted['localRequestId'],action='native-baseline',deadlineMonotonic=123.)
            write(lane/('native-selector-'+request['requestId']+'.json'),{})
            with journal._transaction() as db:
                journal._native_admit_in_tx(db,request)
                actual=json.loads(db.execute('SELECT proof FROM native_admission').fetchone()['proof'])
                self.assertEqual(actual['envelope']['prepared'],f.prepared)
                self.assertEqual(journal._native_prepared_in_tx(db,f.plan),f.prepared)
                self.assertNotEqual(file_record(lane/'native-plan.json'),f.prepared['planFile'])
            changed=copy.deepcopy(f.plan);changed['executionKind']='disabled-proof';write(lane/'native-plan.json',changed)
            with self.assertRaises((Refused,ValueError)):journal.native_plan()

    def test_native_start_plan_kind_run_and_hash_are_exact(self):
        self.assertTrue(hasattr(native,'validate_start_plan'),'native start plan validation missing')
        from test_pgcat_native_ingress import plan
        from pgcat_lane_protocol import digest
        p=plan();bindings=dict(mode=native.MODE,executionKind=p['executionKind'],target=p['target'],sourceHashes=p['sourceHashes'])
        self.assertEqual(native.validate_start_plan(bindings,p),p)
        for key,bad in [('executionKind','disabled-proof'),('sourceHashes',{}),('target',{})]:
            with self.assertRaises(ValueError):native.validate_start_plan(dict(bindings,**{key:bad}),p)

class NativeDispatchTests(unittest.TestCase):
    def test_fixed_typed_staging_rejects_kind_action_and_reuse(self):
        self.assertTrue(hasattr(native,'stage_action'),'typed native staging missing')
        with tempfile.TemporaryDirectory() as d:
            root=Path(os.path.realpath(d));root.chmod(0o700);f=Fixture(root)
            envelope=dict(schemaVersion=1,action='native-ingress',localRequestId=f.admitted['localRequestId'],runId=f.plan['runId'],
                executionKind=f.plan['executionKind'],planSha256=f.admitted['planSha256'],baselineSha256=f.plan['baselineSha256'],
                feedBinding=f.feed,prepared=f.prepared,deadlineMonotonic=1.0,readySha256=f.admitted['readySha256'],feedBindingSha256=f.ready['feedBindingSha256'])
            selector=dict(requestId=REQUEST,approvalIds=[])
            result=native.stage_action(f.run,f.plan,envelope,selector)
            self.assertEqual(result['admission']['sha256'],hashlib.sha256((f.run/('native-admission-'+f.admitted['localRequestId']+'.json')).read_bytes()).hexdigest())
            with self.assertRaises(FileExistsError):native.stage_action(f.run,f.plan,envelope,selector)
            for change in ({'action':'scaleout-two-to-three'},{'action':'native-disabled-restart'},{'executionKind':'disabled-proof'}):
                with self.assertRaises((ValueError,Refused)):native.stage_action(f.run,f.plan,dict(envelope,**change),selector)

class NativePreflightTests(unittest.TestCase):
    def test_cost_quiescence_and_single_operator_are_bounded_preparation_evidence(self):
        self.assertTrue(hasattr(native,'validate_preflight'),'native preparation constraints missing')
        from test_pgcat_native_ingress import plan,START
        value=dict(schemaVersion=1,bindingSha256='a'*64,checkedAt=START,expiresAt=START+300,
            singleOperator=True,producersQuiesced=True,cost=dict(day='2026-10-29',net=100,reserve=100,allowance=100,ceiling=1800))
        native.validate_preflight(value,'a'*64,plan(),START+1)
        for change in ({'singleOperator':False},{'producersQuiesced':False},{'bindingSha256':'b'*64},{'expiresAt':START+301},
            {'cost':dict(value['cost'],net=1700)},{'cost':dict(value['cost'],day='2026-10-28')}):
            with self.assertRaises(ValueError):native.validate_preflight(dict(value,**change),'a'*64,plan(),START+1)
        with self.assertRaises(ValueError):native.validate_preflight(value,'a'*64,plan(),START+300)

class NativePythonCompatibilityTests(unittest.TestCase):
    def test_native_worker_entry_keeps_python36_syntax(self):
        import ast
        for name in ('pgcat_native_controller','pgcat_collector_control','pgcat_source_bootstrap'):
            path=Path(__file__).with_name(name+'.py')
            ast.parse(path.read_text(),filename=str(path),feature_version=(3,6))

    def test_native_prod_helpers_and_reserved_actions_never_grant_runtime(self):
        from pgcat_lane_protocol import _release_admission,NATIVE_RESERVED,NATIVE_ACTIONS
        for action in NATIVE_RESERVED|NATIVE_ACTIONS:
            with self.subTest(action=action),self.assertRaises(Refused):_release_admission(native.MODE,action)
        reads=object.__new__(native.FixedWorkerReads)
        with self.assertRaises(ValueError):reads.gateway_disabled()
        with patch('subprocess.run',side_effect=AssertionError('runtime action')):
            with self.assertRaises(Refused):reads.restart_patch('raptor-test','rdev-test-client-client',{})
