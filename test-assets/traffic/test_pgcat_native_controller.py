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
    from test_pgcat_native_admission import assets_for
    write(value.run/'native-runtime-assets.json',assets_for(value.plan))
    value.prepared=dict(runtimeAssetsFile=file_record(value.run/'native-runtime-assets.json'),planFile=file_record(value.run/'native-plan.json'),baselineFile=file_record(value.run/'native-baseline.json'),gatewayRuntimeFile=file_record(value.run/'native-gateway-runtime.json'),workerCredential=native.credential_identity(credential))
    return value


class NativeStartupTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(native, 'native startup kernel missing')
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(os.path.realpath(self.tmp.name));self.root.chmod(0o700)

    def test_exact_native_closure_and_subset(self):
        self.assertEqual(len(bootstrap.source_names('full-replacement-direct-v2')),27)
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

    def test_ready_accepts_latest_pair_after_three_or_more_frames(self):
        from segmented_capture import read_progress
        f=Fixture(self.root);(f.run/'native-ready.json').unlink()
        for last in (2,3,4):
            f.append(last)
            ready=native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,
                probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)
            progress,_,_=read_progress(f.run/'native-capture',f.context)
            self.assertEqual(ready['frames'],{k:progress[k] for k in ('previous','latest')})
            self.assertEqual(ready['readyAt'],f.now)
            (f.run/'native-ready.json').unlink()

    def test_ready_accepts_healthy_tail_advancement_during_live_read(self):
        from segmented_capture import read_progress
        f=Fixture(self.root);(f.run/'native-ready.json').unlink()
        def advancing_clients():
            for n in (2,3,4):f.append(n)
            return f.live_clients()
        ready=native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,
            probe_process=f.probe_process,live_clients=advancing_clients,now=lambda:f.now)
        progress,_,_=read_progress(f.run/'native-capture',f.context)
        self.assertEqual(ready['frames'],{k:progress[k] for k in ('previous','latest')})
        self.assertEqual(ready['readyAt'],f.now)

    def test_ready_tail_advancement_never_masks_invalid_evidence_or_identity(self):
        for case in ('stale','history','segment-inode','context-inode','source','process','owner','sticky','bad-frame'):
            with self.subTest(case=case),tempfile.TemporaryDirectory() as d:
                root=Path(d).resolve();root.chmod(0o700);f=Fixture(root)
                (f.run/'native-ready.json').unlink()
                if case=='history':
                    f.append(2);f.append(3)
                def changed_clients():
                    f.append(4 if case=='history' else 2)
                    if case=='stale':f.now+=10
                    if case=='history':
                        path=f.run/'native-capture'/'segment-0001.jsonl'
                        rows=path.read_bytes().splitlines(keepends=True)
                        # Interior frame: outside both the first-frame refs and
                        # the previous/latest pair, but inside the pinned prefix.
                        rows[1]=rows[1].replace(b'"observedAt":',b'"observedAx":',1)
                        path.write_bytes(b''.join(rows))
                    if case in ('segment-inode','context-inode'):
                        path=f.run/('native-capture/segment-0001.jsonl' if case=='segment-inode' else 'native-context.json')
                        raw=path.read_bytes();path.rename(path.with_name('held-'+path.name));path.write_bytes(raw);path.chmod(0o600)
                    if case=='source':(f.run/'worker_capture.py').write_text('changed')
                    if case=='process':f.guardian['startTicks']+=1
                    if case=='owner':f.live['replicaSets'][0]['metadata']['ownerReferences'][0]['uid']='foreign'
                    if case=='sticky':f.writer.note_poll_error(f.now)
                    if case=='bad-frame':
                        value=copy.deepcopy(f.frame);value['pods'][0]['ownership']['imageDigest']='sha256:'+'b'*64
                        f.append(3,value)
                    return f.live_clients()
                guardian=copy.deepcopy(f.guardian)
                with self.assertRaises((ValueError,OSError)):
                    native.seal_ready(f.run,prepared=f.prepared,guardian_identity=guardian,
                        probe_process=f.probe_process,live_clients=changed_clients,now=lambda:f.now)
                self.assertFalse((f.run/'native-ready.json').exists())

    def test_ready_rechecks_freshness_after_source_verification(self):
        f=Fixture(self.root);(f.run/'native-ready.json').unlink()
        original=native.verify_source_inventory;reads=[]
        def delayed_verification(*args):
            result=original(*args);reads.append(result)
            if len(reads)==2:f.now+=10
            return result
        with patch.object(native,'verify_source_inventory',side_effect=delayed_verification):
            with self.assertRaises(ValueError):
                native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,
                    probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)
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

    def test_diagnostic_ingress_waits_for_retained_successors_before_binding(self):
        from pgcat_native_ingress import restart_disabled
        from test_pgcat_native_evidence import rolling,kubernetes
        f=Fixture(self.root,'disabled-proof')
        def restart(ns,name,payload):
            dep=f.objects[('Deployment',ns,name)];dep['metadata'].update(generation=2,resourceVersion='2')
            dep['spec']['template'].setdefault('metadata',{})['annotations']=payload['spec']['template']['metadata']['annotations']
            return copy.deepcopy(dep)
        restart_disabled(f.run,gate=f,kube_read=f.kube,kube_patch=restart,now=lambda:f.now)
        frame=rolling(1);frame['inventory']=copy.deepcopy(f.frame['inventory']);f.append(2,frame);f.live=kubernetes(1)
        def collect():
            from test_pgcat_causal_integration import causal_fixture
            with causal_fixture(f):return native.collect_ingress(f.run,{},gate=f,kube_read=f.kube,http_get=f.http,now=lambda:f.now)
        self.assertEqual(collect()['status'],'OBSERVED')
        self.assertFalse((f.run/'native-operation-binding.json').exists())
        for n in (3,4,5):
            frame=rolling(n-1);frame['inventory']=copy.deepcopy(f.frame['inventory']);f.append(n,frame)
        f.live=kubernetes(4)
        self.assertEqual(collect()['status'],'BOUND')

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
        for name in (None,'native-plan.json','native-baseline.json','native-gateway-runtime.json','native-runtime-assets.json','kubeconfig'):
            with self.subTest(replaced=name),tempfile.TemporaryDirectory() as d:
                root=Path(d).resolve();root.chmod(0o700);f=Fixture(root,'disabled-proof')
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
            for key,name in (('planFile','native-plan.json'),('baselineFile','native-baseline.json'),('gatewayRuntimeFile','native-gateway-runtime.json'),('runtimeAssetsFile','native-runtime-assets.json'),('workerCredential','kubeconfig')):
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

class NativeProviderSnapshotTests(unittest.TestCase):
    def setUp(self):
        import provider_feed as provider
        from test_provider_feed import FeedTests
        self.provider=provider
        self.controller=object.__new__(native.NativeController)
        self.controller.bindings={'laneBindings':{'aliyunCli':'/offline/aliyun',
            'target':{'accountId':'offline-account','scalingGroupId':provider.GROUP_ID}}}
        self.controller.feed=None
        pages,group,ecs,nlb=FeedTests().fixture()
        group.update(ScalingGroupId=provider.GROUP_ID,MinSize=2,MaxSize=4)
        for page in pages:
            for member in page['ScalingInstances']['ScalingInstance']:
                member['ScalingGroupId']=provider.GROUP_ID
        for server in nlb['Servers']:server['ServerGroupId']=provider.SERVER_GROUP_ID
        nlb['Listener'].update(ListenerId=provider.LISTENER_ID,ServerGroupId=provider.SERVER_GROUP_ID)
        health=nlb['Health']['ListenerHealthStatus'][0]
        health['ListenerId']=provider.LISTENER_ID
        health['ServerGroupInfos'][0]['ServerGroupId']=provider.SERVER_GROUP_ID
        self.pages=pages
        self.responses={('sts','GetCallerIdentity'):{'AccountId':'offline-account'},
            ('ess','DescribeScalingActivities'):{'TotalCount':0,'RequestId':'offline-request','PageSize':50,'PageNumber':1},
            ('ess','DescribeScalingGroups'):{'TotalCount':1,'PageNumber':1,'ScalingGroups':{'ScalingGroup':[group]}},
            ('ecs','DescribeInstances'):{'TotalCount':2,'PageNumber':1,'Instances':{'Instance':list(ecs.values())}},
            ('nlb','ListServerGroupServers'):{'TotalCount':2,'Servers':nlb['Servers']},
            ('nlb','GetListenerAttribute'):nlb['Listener'],
            ('nlb','GetListenerHealthStatus'):nlb['Health']}
        self.calls=[]
        self.raw_responses={};self.returncode=0

    def cli(self,argv,**kwargs):
        from types import SimpleNamespace
        self.assertEqual(argv[0],'/offline/aliyun')
        self.assertEqual(argv[3:7],['--profile','infra-ops-poc','--region','ap-southeast-1'])
        self.assertGreater(kwargs['timeout'],0)
        self.assertLessEqual(kwargs['timeout'],10)
        self.calls.append(argv)
        key=tuple(argv[1:3]);options=dict(zip(argv[7::2],argv[8::2]))
        if key==('ess','DescribeScalingActivities'):
            self.assertEqual(options,{'--ScalingGroupId':self.provider.GROUP_ID,'--StatusCode':'InProgress','--PageNumber':'1','--PageSize':'50'})
        if key==('ess','DescribeScalingInstances'):
            body=self.pages[int(options['--PageNumber'])-1]
        else:body=self.responses[key]
        return SimpleNamespace(returncode=self.returncode,
            stdout=self.raw_responses.get(key,json.dumps(body,ensure_ascii=False).encode('utf-8')))

    def snapshot(self):
        with patch.object(native.subprocess,'run',side_effect=self.cli):
            return self.controller._provider_snapshot()

    def test_zero_activity_omitted_list_reaches_validated_fleet_snapshot(self):
        value=self.snapshot()
        self.assertEqual(value['desired'],2)
        self.assertEqual(value['members'],['i-0','i-1'])
        self.assertEqual(value['healthyRegistered'],['i-0','i-1'])
        self.assertEqual(value['privateAddresses'],{'i-0':'10.0.0.1','i-1':'10.0.0.2'})

    def test_zero_activity_present_empty_list_remains_valid(self):
        self.responses[('ess','DescribeScalingActivities')]['ScalingActivities']={'ScalingActivity':[]}
        self.assertEqual(self.snapshot()['members'],['i-0','i-1'])

    def test_provider_json_accepts_valid_utf8_instance_name(self):
        self.responses[('ess','DescribeScalingActivities')]['ScalingActivities']={'ScalingActivity':[]}
        self.responses[('ecs','DescribeInstances')]['Instances']['Instance'][0]['InstanceName']='数据库代理'
        self.assertEqual(self.snapshot()['members'],['i-0','i-1'])

    def test_provider_json_rejects_invalid_utf8_and_oversized_output(self):
        for raw in (b'{"AccountId":"offline-account","Name":"\xff"}',b' '* (2*1024*1024+1)):
            with self.subTest(raw_length=len(raw)):
                self.raw_responses[('sts','GetCallerIdentity')]=raw
                with self.assertRaises(ValueError):self.snapshot()
        self.raw_responses.clear();self.returncode=1
        with self.assertRaises(ValueError):self.snapshot()

    def test_optional_activity_list_never_relaxes_exact_zero_count(self):
        for count in (1,-1,True,False,None,0.0,'0',[],{}):
            for present in (False,True):
                with self.subTest(count=count,present=present):
                    body={'TotalCount':count}
                    if present:body['ScalingActivities']={'ScalingActivity':[]}
                    self.responses[('ess','DescribeScalingActivities')]=body
                    with self.assertRaises((ValueError,KeyError)):self.snapshot()
        for body in ({},{'ScalingActivities':{'ScalingActivity':[]}}):
            self.responses[('ess','DescribeScalingActivities')]=body
            with self.assertRaises((ValueError,KeyError)):self.snapshot()

    def test_present_activity_wrapper_must_contain_empty_list(self):
        for wrapper in (None,[],{},'invalid',{'ScalingActivity':None},{'ScalingActivity':{}},
                        {'ScalingActivity':''},{'ScalingActivity':[{}]}):
            with self.subTest(wrapper=wrapper):
                self.responses[('ess','DescribeScalingActivities')]={'TotalCount':0,'ScalingActivities':wrapper}
                with self.assertRaises((ValueError,TypeError,KeyError)):self.snapshot()

    def test_omitted_activity_list_preserves_account_group_and_fleet_guards(self):
        original=copy.deepcopy(self.responses)
        for case in ('account','desired','minimum','maximum','fleet'):
            with self.subTest(case=case):
                self.responses=copy.deepcopy(original)
                if case=='account':self.responses[('sts','GetCallerIdentity')]['AccountId']='foreign'
                elif case=='fleet':self.pages[0]['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId']='foreign'
                else:
                    key={'desired':'DesiredCapacity','minimum':'MinSize','maximum':'MaxSize'}[case]
                    self.responses[('ess','DescribeScalingGroups')]['ScalingGroups']['ScalingGroup'][0][key]=3
                with self.assertRaises((ValueError,self.provider.FeedStop)):self.snapshot()

    def test_snapshot_refuses_active_feed_and_expired_read_deadline_before_cli(self):
        self.controller.feed=object()
        with self.assertRaises(ValueError):self.snapshot()
        self.controller.feed=None
        with patch.object(native.time,'monotonic',side_effect=[0.,30.]):
            with self.assertRaises(ValueError):self.snapshot()
        self.assertEqual(self.calls,[])


class NativeProfileTests(unittest.TestCase):
    def test_capture_cap_reserves_hold_and_closure_and_refuses_after_cutoff(self):
        self.assertTrue(hasattr(native,'capture_budget'),'native closure budget missing')
        from test_pgcat_native_ingress import plan
        value=plan();value['deadline']['captureMaxSeconds']=900
        import datetime
        at=datetime.datetime(2026,10,29,13,29,tzinfo=datetime.timezone.utc).timestamp()
        self.assertEqual(native.capture_budget(value,at),900)
        value['deadline']['closureDeadline']='2026-10-29T13:45:00Z'
        self.assertEqual(native.capture_budget(value,at),660)
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
            with patch('pgcat_lane_protocol._release_admission'),journal._transaction() as db:
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
                feedBinding=f.feed,prepared=f.prepared,priorIngress=None,deadlineMonotonic=1.0,readySha256=f.admitted['readySha256'],feedBindingSha256=f.ready['feedBindingSha256'],
                qualifiedEnable=dict(basename='native-gateway-enable.json',identity=file_record(f.run/'native-ready.json')))
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
            singleOperator=True,producersQuiesced=True,cost=dict(day='2026-10-29',net=100,reserve=100,allowance=100,ceiling=3000))
        native.validate_preflight(value,'a'*64,plan(),START+1)
        for change in ({'singleOperator':False},{'producersQuiesced':False},{'bindingSha256':'b'*64},{'expiresAt':START+301},
            {'cost':dict(value['cost'],net=2900)},{'cost':dict(value['cost'],day='2026-10-28')}):
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

class NativeCausalTransportTests(unittest.TestCase):
    def test_metrics_discovery_uses_only_fixed_metrics_selector_and_config_name(self):
        self.assertTrue(hasattr(native.FixedWorkerReads,'_metrics_kubernetes'),'fixed metrics transport missing')
        from types import SimpleNamespace
        calls=[]
        reader=object.__new__(native.FixedWorkerReads);reader.credential={'path':'/tmp/fixture'};reader.deadline=time.monotonic()+10
        def run(args,**kwargs):
            calls.append(args);return SimpleNamespace(returncode=0,stdout=b'{"items":[]}')
        with patch.object(native,'verify_credential'),patch('subprocess.run',side_effect=run):reader._metrics_kubernetes()
        self.assertEqual(len(calls),4)
        for args in calls:
            self.assertEqual(args[3:6],['-n','raptor-test','get'])
            self.assertNotIn('app.kubernetes.io/name=rdev-test-client-client',args)
            if args[6] in ('pod','replicaset'):
                self.assertEqual(args[7:9],['-l','app.kubernetes.io/name=rdev-test-metrics-metrics'])
            else:self.assertEqual(args[7],'rdev-test-metrics-metrics')
    def test_fixed_prometheus_transport_rejects_redirect_proxy_path_and_origin_escape(self):
        self.assertTrue(hasattr(native.FixedWorkerReads,'prometheus_get'),'fixed metrics transport missing')
        import socket,threading
        reader=object.__new__(native.FixedWorkerReads);reader.deadline=time.monotonic()+10;reader._metrics_origin='http://10.1.2.3:9090'
        mode=[200];seen=[]
        def connect(connection):
            self.assertEqual((connection.host,connection.port),('10.1.2.3',9090))
            client,server=socket.socketpair();client.settimeout(connection.timeout)
            def serve():
                with server:
                    data=b''
                    while b'\r\n\r\n' not in data:data+=server.recv(4096)
                    seen.append(data)
                    body=b'{"status":"success","data":{}}' if mode[0]==200 else b'private error'
                    server.sendall(('HTTP/1.1 %d Test\r\nContent-Length: %d\r\nLocation: http://evil/\r\n\r\n'%(mode[0],len(body))).encode()+body)
            threading.Thread(target=serve,daemon=True).start();connection.sock=client
        with patch.dict(os.environ,{'HTTP_PROXY':'http://evil/','HTTPS_PROXY':'http://evil/'}),patch('http.client.HTTPConnection.connect',new=connect):
            self.assertEqual(reader.prometheus_get('/api/v1/status/tsdb')['status'],'success')
            self.assertEqual(reader.prometheus_get('/api/v1/rules')['status'],'success')
            for status in (302,401,500):
                mode[0]=status
                with self.assertRaises(ValueError) as caught:reader.prometheus_get('/api/v1/status/tsdb')
                self.assertNotIn('private',str(caught.exception));self.assertIsNone(caught.exception.__context__)
        self.assertEqual(len(seen),5)
        self.assertNotIn(b'Authorization:',seen[0])
        for path in ('http://evil/api/v1/status/tsdb','//evil','/api/v1/query?query=up','/api/v1/status/tsdb?x=1','/api/v1/admin/tsdb/delete_series','/api/v1/rules?type=record'):
            with self.assertRaises(ValueError):reader.prometheus_get(path)
        for origin in ('http://127.0.0.1:9090','http://evil:9090','http://10.1.2.3:9090@evil','https://10.1.2.3:9090'):
            reader._metrics_origin=origin
            with self.assertRaises(ValueError):reader.prometheus_get('/api/v1/status/tsdb')
    def test_metrics_http_header_drip_respects_total_deadline(self):
        self.assertTrue(hasattr(native.FixedWorkerReads,'prometheus_get'),'fixed metrics transport missing')
        import socket,threading
        reader=object.__new__(native.FixedWorkerReads);reader.deadline=time.monotonic()+.2;reader._metrics_origin='http://10.1.2.3:9090'
        client,server=socket.socketpair();self.addCleanup(client.close)
        def serve():
            with server:
                server.recv(4096)
                for byte in b'HTTP/1.1 200 OK\r\n':
                    try:server.send(bytes([byte]));time.sleep(.1)
                    except OSError:break
        threading.Thread(target=serve,daemon=True).start();started=time.monotonic()
        with patch('http.client.HTTPConnection.connect',new=lambda conn:setattr(conn,'sock',client)):
            with self.assertRaises(ValueError):reader.prometheus_get('/api/v1/status/tsdb')
        self.assertLess(time.monotonic()-started,1.)


class StartupBarrierTests(unittest.TestCase):
    def fixture(self):
        self.assertTrue(hasattr(native,'validate_launch'),'launch-only authenticated startup barrier missing')
        directory=tempfile.TemporaryDirectory();self.addCleanup(directory.cleanup)
        root=Path(directory.name).resolve();root.chmod(0o700);return Fixture(root,'disabled-proof')

    def launch(self,f):
        from pgcat_collector_control import startup_slot
        folder=f.run/'collector-job';folder.mkdir(mode=0o700,exist_ok=True)
        if not (folder/'intent.json').exists():write(folder/'intent.json',dict(runId=f.plan['runId'],startedAt=int(f.now),profile=native.MODE,captureMaxSeconds=75,prepared=f.prepared))
        fd=os.open(str(folder/'startup-result.json'),os.O_RDWR|os.O_CREAT|os.O_EXCL,0o600);self.addCleanup(os.close,fd)
        binding=dict(runId=f.plan['runId'],planSha256=hashlib.sha256((f.run/'native-plan.json').read_bytes()).hexdigest(),
            collectorIntentFile=file_record(folder/'intent.json'),guardianIdentity=f.guardian,collectorIdentity=f.context['collectorIdentity'],
            resultPath=str(folder/'startup-result.json'),resultSlot=startup_slot(os.fstat(fd)),deadlineMonotonic=time.monotonic()+4.)
        return dict(status='LAUNCHED',**binding,resultKey='a'*64),fd

    def result(self,f,launch):
        from pgcat_collector_control import publish_startup_result
        fd=os.open(launch['resultPath'],os.O_RDWR);self.addCleanup(os.close,fd)
        return publish_startup_result(f.run,fd,bytes.fromhex(launch['resultKey']),{k:v for k,v in launch.items() if k not in ('status','resultKey')},f.ready)

    def test_launch_schema_has_no_readiness_and_status_authenticates_original(self):
        f=self.fixture();launch,fd=self.launch(f)
        native.validate_launch(launch,f.plan)
        with self.assertRaises(ValueError):native.validate_launch(dict(launch,ready=f.ready),f.plan)
        result=self.result(f,launch)
        native.verify_startup_result(result,launch,f.plan,f.prepared)
        self.assertEqual(result['ready']['readyAt'],f.ready['readyAt'])
        for field in ('readyAt','guardianIdentity','runId'):
            bad=copy.deepcopy(result)
            if field=='readyAt':bad['ready']['readyAt']+=1
            elif field=='runId':bad['binding'][field]=REQUEST
            else:bad['binding'][field]['startTicks']+=1
            with self.subTest(field=field),self.assertRaises(ValueError):native.verify_startup_result(bad,launch,f.plan,f.prepared)
        with self.assertRaises(ValueError):native.verify_startup_result(result,dict(launch,resultKey='b'*64),f.plan,f.prepared)

    def test_status_preserves_original_ready_time_after_runtime_ages(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);self.result(f,launch)
        f.now+=31
        with patch('pgcat_lane_protocol._release_admission'),patch.object(control,'context',return_value=(f.run,control.directory(f.run/'collector-job'),{})):
            output=control.status(f.run.parent/'contract',f.plan['runId'],f.plan['sourceHashes'],profile=native.MODE,prepared=f.prepared,launch={k:v for k,v in launch.items() if k!='resultKey'},probe=f.probe_process,now=lambda:f.now)
        self.assertEqual(output['status'],'READY',output)
        native.verify_startup_result(output['result'],launch,f.plan,f.prepared)
        self.assertEqual(output['ready'],f.ready)
        (f.run/'native-ready.json').unlink()
        with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now)

    def test_final_ready_publication_checks_original_monotonic_deadline(self):
        f=self.fixture();(f.run/'native-ready.json').unlink()
        deadline=time.monotonic()-1
        with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now,deadline=deadline)
        self.assertFalse((f.run/'native-ready.json').exists())

    def test_result_publication_never_accepts_late_or_duplicate_completion(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);binding={k:v for k,v in launch.items() if k not in ('status','resultKey')}
        binding['deadlineMonotonic']=time.monotonic()-1
        with self.assertRaises(ValueError):control.publish_startup_result(f.run,fd,bytes.fromhex(launch['resultKey']),binding,f.ready)
        self.assertEqual(os.fstat(fd).st_size,0)
        # A failed proof never renews. Duplicate publication is checked on a
        # separate successful original launch, not a retry of the failed one.
        f=self.fixture();launch,fd=self.launch(f);self.result(f,launch)
        with self.assertRaises(ValueError):self.result(f,launch)

    def test_status_requires_original_result_inode_ready_bytes_process_and_intent(self):
        import pgcat_collector_control as control
        for change in ('missing','partial','foreign-result','ready-bytes','ready-inode','intent-inode','process-reuse','guardian-dead','collector-dead','original-runtime-stale','source'):
            with self.subTest(change=change):
                f=self.fixture();launch,fd=self.launch(f)
                if change=='original-runtime-stale':
                    f.ready['readyAt']+=31;write(f.run/'native-ready.json',f.ready)
                if change!='missing':self.result(f,launch)
                path=Path(launch['resultPath'])
                if change=='partial':path.write_bytes(b'{')
                elif change=='foreign-result':raw=path.read_bytes();path.rename(path.with_suffix('.old'));path.write_bytes(raw);path.chmod(0o600)
                elif change in ('ready-bytes','ready-inode','intent-inode'):
                    path=f.run/'native-ready.json' if change.startswith('ready') else f.run/'collector-job'/'intent.json'
                    raw=path.read_bytes()
                    if change.endswith('inode'):path.rename(path.with_suffix('.old'))
                    path.write_bytes(raw+b' ' if change=='ready-bytes' else raw);path.chmod(0o600)
                elif change=='source':(f.run/'pgcat_native_controller.py').write_bytes(b'changed')
                probe=(lambda pid:dict(f.probe_process(pid),startTicks=999)) if change=='process-reuse' else f.probe_process
                if change in ('guardian-dead','collector-dead'):
                    dead=f.guardian['pid'] if change=='guardian-dead' else f.context['collectorIdentity']['pid']
                    probe=lambda pid:None if pid==dead else f.probe_process(pid)
                with patch('pgcat_lane_protocol._release_admission'),patch.object(control,'context',return_value=(f.run,control.directory(f.run/'collector-job'),{})):
                    output=control.status(f.run.parent/'contract',f.plan['runId'],f.plan['sourceHashes'],profile=native.MODE,prepared=f.prepared,launch={k:v for k,v in launch.items() if k!='resultKey'},probe=probe,now=lambda:f.now)
                self.assertEqual(output,dict(status='INCONCLUSIVE'))


    def test_result_fsync_crossing_original_deadline_leaves_no_usable_ready(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);deadline=launch['deadlineMonotonic'];clock=[deadline-.1]
        original=control.os.fsync
        def delayed(fd):original(fd);clock[0]=deadline+.01
        with patch.object(control.time,'monotonic',side_effect=lambda:clock[0]),patch.object(control.os,'fsync',side_effect=delayed):
            with self.assertRaises(ValueError):self.result(f,launch)
        self.assertEqual(Path(launch['resultPath']).read_bytes(),b'')

    def test_bulk_publication_is_unprovable_until_original_deadline_checked_commit(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);original=control.os.fsync;observed=[]
        def inspect(fd):
            raw=Path(launch['resultPath']).read_bytes()
            try:
                value=json.loads(raw);native.verify_startup_result(value,launch,f.plan,f.prepared)
            except (ValueError,KeyError):observed.append(False)
            else:observed.append(True)
            original(fd)
        with patch.object(control.os,'fsync',side_effect=inspect):self.result(f,launch)
        self.assertFalse(observed[0],'bulk write exposes READY before original deadline commit')
        self.assertTrue(observed[-1])

    def test_status_refuses_parseable_result_while_final_fsync_is_unfinished(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);original=control.os.fsync;observed=[]
        def status():
            with patch('pgcat_lane_protocol._release_admission'),patch.object(control,'context',return_value=(f.run,control.directory(f.run/'collector-job'),{})):
                return control.status(f.run.parent/'contract',f.plan['runId'],f.plan['sourceHashes'],profile=native.MODE,prepared=f.prepared,launch={k:v for k,v in launch.items() if k!='resultKey'},probe=f.probe_process,now=lambda:f.now)['status']
        def unfinished(fd):
            try:json.loads(Path(launch['resultPath']).read_bytes())
            except ValueError:pass
            else:observed.append(status())
            original(fd)
        with patch.object(control.os,'fsync',side_effect=unfinished):self.result(f,launch)
        self.assertEqual(observed,['INCONCLUSIVE'])
        self.assertEqual(status(),'READY')

    def test_post_signing_fsync_overrun_cannot_leave_an_authenticated_ready(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);deadline=launch['deadlineMonotonic'];clock=[deadline-.1];calls=[0];original=control.os.fsync
        def late(fd):
            original(fd);calls[0]+=1
            if calls[0]==2:clock[0]=deadline+.01
        with patch.object(control.time,'monotonic',side_effect=lambda:clock[0]),patch.object(control.os,'fsync',side_effect=late):
            with self.assertRaises(ValueError):self.result(f,launch)
        self.assertEqual(Path(launch['resultPath']).read_bytes(),b'')

    def test_failed_late_invalidation_keeps_signed_result_locked_while_guardian_lives(self):
        import pgcat_collector_control as control
        f=self.fixture();launch,fd=self.launch(f);deadline=launch['deadlineMonotonic'];clock=[deadline-.1];calls=[0];original=control.os.fsync
        def late(fd):
            original(fd);calls[0]+=1
            if calls[0]==2:clock[0]=deadline+.01
        with patch.object(control.time,'monotonic',side_effect=lambda:clock[0]),patch.object(control.os,'fsync',side_effect=late),patch.object(control.os,'ftruncate',side_effect=OSError('offline invalidation failure')):
            with self.assertRaises(OSError):self.result(f,launch)
        # The bytes alone are parseable and keyed, but still convey no status
        # authority: the original owner's inherited FD remains exclusively held.
        native.verify_startup_result(json.loads(Path(launch['resultPath']).read_bytes()),launch,f.plan,f.prepared)
        with patch('pgcat_lane_protocol._release_admission'),patch.object(control,'context',return_value=(f.run,control.directory(f.run/'collector-job'),{})):
            output=control.status(f.run.parent/'contract',f.plan['runId'],f.plan['sourceHashes'],profile=native.MODE,prepared=f.prepared,launch={k:v for k,v in launch.items() if k!='resultKey'},probe=f.probe_process,now=lambda:f.now)
        self.assertEqual(output,dict(status='INCONCLUSIVE'))

    def test_ready_final_source_work_cannot_extend_worker_deadline(self):
        f=self.fixture();(f.run/'native-ready.json').unlink();clock=[0.];original=native.verify_source_inventory;count=[0]
        def verify(*args):
            value=original(*args);count[0]+=1
            if count[0]==2:clock[0]=4.01
            return value
        with patch.object(native,'verify_source_inventory',side_effect=verify),patch.object(native.time,'monotonic',side_effect=lambda:clock[0]):
            with self.assertRaises(ValueError):native.seal_ready(f.run,prepared=f.prepared,guardian_identity=f.guardian,probe_process=f.probe_process,live_clients=f.live_clients,now=lambda:f.now,deadline=4.)
        self.assertFalse((f.run/'native-ready.json').exists())

    def test_acknowledged_launch_retains_both_originals_when_status_fails(self):
        from types import SimpleNamespace
        from pgcat_lane_client import LaneClient
        from segmented_capture import canonical
        f=self.fixture();launch,fd=self.launch(f);root=f.run.parent
        prepared=dict(f.prepared,plan=f.plan,macCredential=f.prepared['workerCredential']);write(root/'native-prepared.json',prepared)
        lane=root/'lane';lane.mkdir(mode=0o700);global_root=root/'global';global_root.mkdir(mode=0o700)
        (global_root/'owner.lock').write_bytes(b'');(global_root/'owner.lock').chmod(0o600)
        feed=subprocess.Popen([sys.executable,'-c','import time;time.sleep(30)'])
        def reap():
            if feed.poll() is None:feed.kill()
            feed.wait(timeout=2)
        self.addCleanup(reap)
        client=SimpleNamespace(root=lane,process=feed,feed_binding=f.feed,start_feed=lambda *a:feed,close=lambda:None)
        owner=object.__new__(native.NativeController);owner.prepared=prepared;owner.client=None;owner.binding_path=root/'bindings';owner.phase='startup'
        b=dict(providerFeedPath='/offline/feed',laneWrapper='/offline/wrapper',receiptPath=str(root/'receipt'),workbenchCli='/offline/cli',laneLockPath=str(root/'lock'),contractDir=str(root/'contract'),workerKubeconfig=str(root/'kubeconfig'))
        owner.bindings=dict(captureRoot=str(root),laneRoot=str(lane),laneBindings=b,workerInventory='/offline/inventory',workerKubeconfig=str(root/'kubeconfig'))
        native._native_owners[owner]=dict(pid=os.getpid(),bindings=canonical(owner.bindings),runId=f.plan['runId'],mac=prepared['macCredential'],worker=prepared['workerCredential'],prepared=canonical(prepared),feed=None,feedIdentity=None,client=None,workerProcesses={})
        seen=[]
        def call(action):
            seen.append(action)
            if action=='native-collector-launch':return dict(outputs=launch)
            if action=='native-collector-status':raise RuntimeError('offline status refusal')
            return dict(outputs={})
        with patch.object(native,'_closed'),patch.object(native,'_require_verified'),patch.object(LaneClient,'start',return_value=client),patch.object(owner,'_call',side_effect=call),patch('pgcat_rehearsal_controller._feed_path',return_value=os.defpath),patch('pgcat_lane_supervisor.GLOBAL_ROOT',global_root),patch.object(native.time,'time',return_value=f.now):
            with self.assertRaisesRegex(RuntimeError,'offline status refusal'):owner.start_capture()
            originals=owner._abort_originals()
        self.assertEqual(seen,['native-baseline','native-collector-launch','native-collector-status'])
        self.assertEqual(originals['workerProcesses'],dict(collector=launch['collectorIdentity'],guardian=launch['guardianIdentity']))
        self.assertEqual(owner.phase,'startup')


class StartupJournalTests(unittest.TestCase):
    def fixture(self):
        from pgcat_lane_protocol import Journal,encode,digest
        f=StartupBarrierTests.fixture(self);lane=f.run.parent/'lane';lane.mkdir(mode=0o700);j=Journal(lane)
        raw=(f.run/'native-plan.json').read_bytes();write(lane/'native-plan.json',f.plan)
        with j._transaction() as db:
            db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',(f.plan['runId'],native.MODE,encode(f.plan['target']).decode(),encode(dict(f.plan['sourceHashes'],bundleSha256=digest(f.plan['sourceHashes']))).decode(),'active',None,None))
            db.execute('INSERT INTO native_plan VALUES (1,?,?)',(raw,encode(file_record(lane/'native-plan.json')).decode()))
            db.execute('INSERT INTO native_prepared VALUES (1,?)',(encode(f.prepared).decode(),))
            db.execute('INSERT INTO feed_binding VALUES (1,?)',(encode(f.feed).decode(),))
        return f,j

    def admit(self,f,j,action):
        import uuid
        from pgcat_lane_protocol import encode,monotonic
        request=dict(requestId=str(uuid.uuid4()),action=action,runId=f.plan['runId'],deadlineMonotonic=monotonic()+5.)
        write(j.root/('native-selector-'+request['requestId']+'.json'),{})
        with patch('pgcat_lane_protocol._release_admission'),j._transaction() as db:
            db.execute('INSERT INTO requests VALUES (?,?,?,?,?,?)',(request['requestId'],f.plan['runId'],request['requestId'],encode(request).decode(),'dispatching',None))
            j._native_admit_in_tx(db,request)
            db.execute('UPDATE native_admission SET claimed=1 WHERE request_id=?',(request['requestId'],))
            proof=json.loads(db.execute('SELECT proof FROM native_admission WHERE request_id=?',(request['requestId'],)).fetchone()['proof'])
        return request,proof

    def store_launch(self,f,j,state='complete'):
        from pgcat_lane_protocol import encode
        launch,_=StartupBarrierTests.launch(self,f)
        wrapper=startup_wrapper(f,'native-collector-launch',launch,'11111111-1111-4111-8111-111111111111')
        with j._transaction() as db:
            db.execute('INSERT INTO requests VALUES (?,?,?,?,?,?)',(wrapper['requestId'],f.plan['runId'],wrapper['requestId'],encode(dict(action=wrapper['action'])).decode(),state,None))
            db.execute('INSERT INTO native_support VALUES (?,?)',(wrapper['requestId'],encode(wrapper).decode()))
        return launch

    def test_status_cannot_admit_before_a_completed_original_launch(self):
        for state in (None,'dispatching','uncertain'):
            f,j=self.fixture()
            if state:self.store_launch(f,j,state)
            with self.subTest(state=state),self.assertRaises(Refused):self.admit(f,j,'native-collector-status')

    def test_status_envelope_binds_launch_but_never_transfers_result_key_back(self):
        f,j=self.fixture();launch=self.store_launch(f,j)
        request,proof=self.admit(f,j,'native-collector-status')
        self.assertIn('launch',proof['envelope'])
        self.assertNotIn('resultKey',proof['envelope']['launch'])
        self.assertTrue(proof['envelope']['launch']=={k:v for k,v in launch.items() if k!='resultKey'})
        with self.assertRaises(Refused):self.admit(f,j,'native-collector-status')

    def test_launch_only_cannot_admit_ingress_or_restart(self):
        for action in ('native-ingress','native-disabled-restart'):
            f,j=self.fixture();self.store_launch(f,j)
            with self.subTest(action=action),self.assertRaises(Refused):self.admit(f,j,action)

    def test_journal_authenticates_exact_status_before_ready_admission(self):
        import pgcat_collector_control as control
        for change in (None,'signature','binding','ready-time','prepared'):
            f,j=self.fixture();launch=self.store_launch(f,j);signed=StartupBarrierTests.result(self,f,launch)
            request,proof=self.admit(f,j,'native-collector-status')
            output=dict(status='READY',ready=signed['ready'],readyFile=signed['readyFile'],result=signed,resultFile=file_record(Path(launch['resultPath'])))
            if change=='signature':output['result']['seal']['authenticator']='b'*64
            elif change=='binding':output['result']['binding']['collectorIdentity']['startTicks']+=1
            elif change=='ready-time':output['ready']['readyAt']+=1
            elif change=='prepared':output['result']['ready']['sources']['gatewayRuntime']['inode']+=1
            payload=startup_wrapper(f,'native-collector-status',output,request['requestId'],proof)
            result=dict(state='complete',category='ok',payload=payload)
            with patch('pgcat_lane_protocol._release_admission'):
                if change:
                    with self.subTest(change=change),self.assertRaises((ValueError,Refused)):j.seal_native(request['requestId'],result)
                else:
                    j.seal_native(request['requestId'],result)
                    request2,proof2=self.admit(f,j,'native-disabled-restart')
                    self.assertEqual(proof2['envelope']['readySha256'],output['readyFile']['sha256'])
                    with self.assertRaises(Refused):self.admit(f,j,'native-collector-launch')


def startup_wrapper(f,action,output,request_id,proof=None):
    from pgcat_lane_protocol import digest
    staged=dict(admission=file_record(f.run/'native-plan.json'),selector=file_record(f.run/'native-baseline.json'))
    if proof:
        staged['admission']=dict(staged['admission'],sha256=digest(proof['envelope']))
        staged['selector']=dict(staged['selector'],sha256=digest(proof['selector']))
    return dict(schemaVersion=1,action=action,runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=digest(f.plan),requestId=request_id,staged=staged,outputs=output)


def record_startup_fixture(test,f,j):
    """Durable completed launch, then real authenticated Journal status sealing."""
    with j._connection() as db:f.feed=json.loads(db.execute('SELECT proof FROM feed_binding').fetchone()['proof'])
    write(f.run/'native-feed-binding.json',f.feed)
    f.ready['feedBindingSha256']=hashlib.sha256(json.dumps(f.feed,sort_keys=True,separators=(',',':')).encode('ascii')).hexdigest()
    f.ready['sources']['feedBinding']=file_record(f.run/'native-feed-binding.json');write(f.run/'native-ready.json',f.ready)
    launch=StartupJournalTests.store_launch(test,f,j)
    signed=StartupBarrierTests.result(test,f,launch)
    request,proof=StartupJournalTests.admit(test,f,j,'native-collector-status')
    output=dict(status='READY',ready=signed['ready'],readyFile=signed['readyFile'],result=signed,resultFile=file_record(Path(launch['resultPath'])))
    payload=startup_wrapper(f,'native-collector-status',output,request['requestId'],proof)
    with patch('pgcat_lane_protocol._release_admission'):j.seal_native(request['requestId'],dict(state='complete',category='ok',payload=payload))
    return launch,output


class NativeClientBatchTests(unittest.TestCase):
    """Fixed client commands, real local children and fake external Pod I/O."""
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(os.path.realpath(self.tmp.name));self.root.chmod(0o700)
        self.f=Fixture(self.root)
        from segmented_capture import read_progress
        self.f.frame=read_progress(self.f.run/'native-capture',self.f.context)[2]
        self.reads=object.__new__(native.FixedWorkerReads)
        self.reads.credential={'path':'/offline/client-kubeconfig'}
        self.reads.plan=self.f.plan;self.reads.deadline=time.monotonic()+2
        self.children=[];self.actual_popen=subprocess.Popen

    def programs(self,objects=None):
        if objects is None:
            objects=dict(deployment=self.f.live['deployment'],replicaset={'items':self.f.live['replicaSets']},pod={'items':self.f.live['pods']})
        return {kind:'import os;os.write(1,%r)'%json.dumps(value).encode() for kind,value in objects.items()}

    def batch(self,programs=None,*,live=False,verify=None,launch_hook=None):
        programs=self.programs() if programs is None else programs
        def launch(argv,**kwargs):
            kind=argv[6]
            tail=['rdev-test-client-client'] if kind=='deployment' else ['-l','app.kubernetes.io/name=rdev-test-client-client']
            self.assertIn(kind,('deployment','replicaset','pod'))
            self.assertEqual(argv,['kubectl','--kubeconfig','/offline/client-kubeconfig','-n','raptor-test','get',kind]+tail+['-o','json','--request-timeout=2s'])
            self.assertEqual(kwargs,dict(stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,
                close_fds=True,env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'}))
            if launch_hook is not None:launch_hook(len(self.children))
            child=self.actual_popen([sys.executable,'-I','-S','-c',programs[kind]],**kwargs)
            self.children.append(child);return child
        with patch.object(subprocess,'Popen',side_effect=launch),patch.object(native,'verify_credential',side_effect=verify):
            try:return self.reads.live_clients() if live else self.reads._client_metadata()
            finally:
                for child in self.children:
                    self.assertIsNotNone(child.poll(),'owned client child still running')
                    self.assertTrue(child.stdout.closed,'owned client pipe still open')
                    with self.assertRaises(ChildProcessError):os.waitpid(child.pid,os.WNOHANG)

    def test_three_fixed_children_overlap_and_preserve_projection(self):
        # Serial communication cannot cross this three-process barrier.
        with tempfile.TemporaryDirectory() as folder:
            programs=self.programs()
            for kind,program in list(programs.items()):
                programs[kind]="import os,time\np=%r\nopen(os.path.join(p,%r),'w').close()\nend=time.monotonic()+1\nwhile len(os.listdir(p))<3 and time.monotonic()<end:time.sleep(.005)\nassert len(os.listdir(p))==3\n"%(folder,kind)+program
            def read(pod,unused,*,deadline):
                return next(p for p in self.f.frame['pods'] if p['uid']==pod['metadata']['uid'])
            with patch('worker_capture.read_pod',side_effect=read):result=self.batch(programs,live=True)
        self.assertEqual(len(self.children),3)
        self.assertEqual({k:result[k] for k in ('deployment','replicaSets','pods')},
            {k:self.f.live[k] for k in ('deployment','replicaSets','pods')})

    def test_failed_malformed_missing_oversized_and_slow_child_reap_siblings(self):
        for bad in ('raise SystemExit(1)','print("bad-json")','print("{}")',
                'import os;os.write(1,b"x"*(2*1024*1024+1))','import time;time.sleep(4)'):
            self.children=[];self.reads.deadline=time.monotonic()+.35
            programs={k:'import time;time.sleep(4)' for k in ('deployment','replicaset','pod')};programs['replicaset']=bad
            with self.subTest(bad=bad),self.assertRaises((ValueError,KeyError,TimeoutError)):
                self.batch(programs)
            self.assertEqual(len(self.children),3)

    def test_all_successful_responses_decode_before_any_metadata_is_returned(self):
        for kind,bad in (('deployment',[]),('replicaset',{'items':None}),('pod',{'other':[]})):
            objects=dict(deployment=self.f.live['deployment'],replicaset={'items':self.f.live['replicaSets']},pod={'items':self.f.live['pods']})
            objects[kind]=bad;self.children=[];self.reads.deadline=time.monotonic()+2
            with self.subTest(kind=kind),self.assertRaises((ValueError,KeyError,TypeError)):
                self.batch(self.programs(objects))

    def test_interruption_and_second_launch_failure_reap_all_owned_children(self):
        with patch('select.select',side_effect=KeyboardInterrupt),self.assertRaises(KeyboardInterrupt):self.batch()
        self.assertEqual(len(self.children),3);self.children=[]
        def fail(index):
            if index==1:raise OSError('offline launch failure')
        with self.assertRaises(ValueError):self.batch(launch_hook=fail)
        self.assertEqual(len(self.children),1)

    def test_original_deadline_includes_launch_and_has_no_new_budget(self):
        self.reads.deadline=time.monotonic()+.1;original=self.reads.deadline
        def delay(index):
            if index==0:time.sleep(.15)
        with self.assertRaises((ValueError,TimeoutError)):self.batch(launch_hook=delay)
        self.assertEqual(self.reads.deadline,original);self.assertEqual(len(self.children),1)
        self.children=[]
        with self.assertRaises((ValueError,TimeoutError)):self.batch()
        self.assertEqual(self.children,[])

    def test_credential_drift_before_launch_or_after_child_reads_refuses_group(self):
        for stage in ('before','after'):
            self.children=[];self.reads.deadline=time.monotonic()+2
            def verify(credential):
                self.assertEqual(credential,{'path':'/offline/client-kubeconfig'})
                if stage=='before' or len(self.children)==3:raise ValueError('offline drift')
            with self.subTest(stage=stage),self.assertRaises(ValueError):self.batch(verify=verify)
            self.assertEqual(len(self.children),0 if stage=='before' else 3)

    def test_live_projection_and_sequential_pod_http_follow_completed_batch(self):
        calls=[]
        def read(pod,unused,*,deadline):
            self.assertEqual(len(self.children),3)
            self.assertTrue(all(c.poll() is not None and c.stdout.closed for c in self.children))
            self.assertEqual(deadline,self.reads.deadline);calls.append(pod['metadata']['uid'])
            return copy.deepcopy(next(p for p in self.f.frame['pods'] if p['uid']==pod['metadata']['uid']))
        with patch('worker_capture.read_pod',side_effect=read):result=self.batch(live=True)
        self.assertEqual(result,dict(self.f.live,roster=[dict(name=p['name'],uid=p['uid'],containerId=p['containerId'],processId=p['snapshot']['sample']['processId']) for p in self.f.frame['pods']]))
        self.assertEqual(calls,[p['metadata']['uid'] for p in self.f.live['pods']])

    def test_foreign_owner_image_and_missing_metadata_refuse_before_pod_http(self):
        original=copy.deepcopy(self.f.live)
        for case in ('owner','image','missing'):
            self.f.live=copy.deepcopy(original);self.children=[];self.reads.deadline=time.monotonic()+2
            if case=='owner':self.f.live['replicaSets'][0]['metadata']['ownerReferences'][0]['uid']='foreign'
            if case=='image':self.f.live['pods'][0]['status']['containerStatuses'][0]['imageID']='foreign'
            if case=='missing':del self.f.live['deployment']['metadata']['uid']
            with self.subTest(case=case),patch('worker_capture.read_pod',side_effect=AssertionError('foreign metadata reached Pod HTTP')),self.assertRaises((ValueError,KeyError)):
                self.batch(live=True)

    def test_batch_refuses_non_main_thread_before_any_child(self):
        import threading
        results=[]
        def run():
            try:self.reads._client_metadata()
            except BaseException as error:results.append(type(error))
        with patch.object(subprocess,'Popen',side_effect=AssertionError('non-main child')):
            thread=threading.Thread(target=run);thread.start();thread.join(timeout=2)
        self.assertFalse(thread.is_alive());self.assertEqual(results,[ValueError])

    def test_each_stdout_cap_accepts_exact_limit_and_rejects_one_more_byte(self):
        objects=dict(deployment={},replicaset={'items':[]},pod={'items':[]})
        programs={kind:'import os\nraw=%r\nos.write(1,raw+b" "*(2*1024*1024-len(raw)))'%json.dumps(value).encode() for kind,value in objects.items()}
        self.assertEqual(self.batch(programs),({},[],[]))
        for kind in ('deployment','replicaset','pod'):
            self.children=[];self.reads.deadline=time.monotonic()+2
            oversized=self.programs(objects);oversized[kind]=programs[kind]+'\nos.write(1,b" ")'
            with self.subTest(kind=kind),self.assertRaises(ValueError):self.batch(oversized)

    def test_eof_does_not_renew_deadline_for_child_that_stays_alive(self):
        programs=self.programs();programs['pod']='import os,time;os.close(1);time.sleep(4)'
        self.reads.deadline=time.monotonic()+.15;original=self.reads.deadline;started=time.monotonic()
        with self.assertRaises((ValueError,subprocess.TimeoutExpired)):self.batch(programs)
        self.assertEqual(self.reads.deadline,original);self.assertLess(time.monotonic()-started,1.)

    def test_pending_successors_skip_http_but_initial_and_running_clients_remain_sequential(self):
        from test_pgcat_native_evidence import kubernetes,rolling
        original=kubernetes(1)
        for case in ('pending','refused','ready-refused','initial-refused','foreign'):
            self.f.live=copy.deepcopy(original);self.children=[];self.reads.deadline=time.monotonic()+2;calls=[]
            for pod in self.f.live['pods'][2:]:
                pod['status']['conditions']=[dict(type='Ready',status='True' if case=='ready-refused' else 'False')]
                if case=='pending':pod['status'].update(phase='Pending',containerStatuses=[])
                if case=='foreign':pod['metadata']['ownerReferences'][0]['uid']='foreign'
            def read(pod,unused,*,deadline):
                calls.append(pod['metadata']['uid'])
                if case=='initial-refused' or pod['metadata']['uid'].startswith('new-'):raise ConnectionError('offline unavailable')
                return next(p for p in rolling(1)['pods'] if p['uid']==pod['metadata']['uid'])
            with self.subTest(case=case),patch('worker_capture.read_pod',side_effect=read):
                if case in ('pending','refused'):
                    value=self.batch(live=True)
                    self.assertEqual([p['uid'] for p in value['roster']],['uid-0','uid-1'])
                    self.assertEqual(calls,['uid-0','uid-1']+([] if case=='pending' else ['new-uid-0','new-uid-1']))
                else:
                    with self.assertRaises(ValueError):self.batch(live=True)
                    if case=='foreign':self.assertEqual(calls,[])

    def test_credential_drift_between_each_launch_stops_before_next_child(self):
        for count in (1,2):
            self.children=[];self.reads.deadline=time.monotonic()+2
            def verify(credential):
                if len(self.children)==count:raise ValueError('offline drift')
            with self.subTest(count=count),self.assertRaises(ValueError):self.batch(verify=verify)
            self.assertEqual(len(self.children),count)

    def test_batch_errors_keep_worker_read_sanitization(self):
        programs=self.programs();programs['pod']='import os;os.write(1,b"\\xffprivate-response")'
        try:self.batch(programs)
        except Exception as error:
            self.assertEqual(type(error),ValueError)
            self.assertEqual(str(error),'native worker read')
        else:self.fail('malformed metadata accepted')

    def test_each_completed_child_checks_credential_before_next_response(self):
        completed=[0];verified=[0];actual=self.actual_popen
        def launch(argv,**kwargs):
            child=actual(argv,**kwargs);wait=child.wait;seen=[False]
            def checked_wait(timeout=None):
                status=wait(timeout=timeout)
                if not seen[0]:
                    self.assertEqual(verified[0],completed[0],'previous response skipped credential check')
                    seen[0]=True;completed[0]+=1
                return status
            child.wait=checked_wait;return child
        self.actual_popen=launch
        def verify(credential):verified[0]=completed[0]
        self.batch(verify=verify)
        self.assertEqual(completed,[3]);self.assertEqual(verified,[3])

    def test_one_wait_failure_still_reaps_and_closes_every_other_child(self):
        actual=self.actual_popen;created=[]
        def launch(argv,**kwargs):
            child=actual(argv,**kwargs);created.append(child)
            if len(created)==1:
                wait=child.wait
                def failing_wait(timeout=None):
                    wait(timeout=timeout);raise subprocess.TimeoutExpired('offline wait failure',timeout)
                child.wait=failing_wait
            return child
        self.actual_popen=launch
        with self.assertRaises(ValueError):self.batch()
        self.assertEqual(len(self.children),3)
