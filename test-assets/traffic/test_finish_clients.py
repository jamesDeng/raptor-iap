import json, tempfile, threading, time, unittest
from pathlib import Path
from unittest.mock import patch
import finish_clients as finish

ROSTER=[dict(name='client-'+str(i),uid='uid-'+str(i),containerId='container-'+str(i),processId='process-'+str(i)) for i in range(2)]

class FinishTests(unittest.TestCase):
    def test_exact_roster_ack_then_parallel_dispatch_and_private_files(self):
        import concurrent.futures
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';gate=threading.Barrier(2);seen=[]
            def execute(row):
                self.assertTrue((p/'ack.json').exists());gate.wait(timeout=2);seen.append(row);return 'accepted'
            def ack():
                for _ in range(100):
                    if (p/'request.json').exists():
                        request=json.loads((p/'request.json').read_text());finish.atomic_json(p/'ack.json',request);return
                    time.sleep(.01)
            with patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'exec_finish',side_effect=execute):
                t=threading.Thread(target=ack);t.start();receipt=finish.finish(p);t.join()
            self.assertEqual(len(seen),2);self.assertEqual(set(receipt['clients']),{'uid-0/process-0','uid-1/process-1'})
            self.assertEqual(p.stat().st_mode&0o777,0o700)
            for f in p.iterdir():self.assertEqual(f.stat().st_mode&0o777,0o600)
    def test_missing_changed_identity_refuses_without_exec(self):
        for rows in (ROSTER[:1],[dict(ROSTER[0],processId=''),ROSTER[1]]):
            with tempfile.TemporaryDirectory() as d,patch.object(finish,'live_roster',return_value=rows),patch.object(finish,'exec_finish') as execute:
                with self.assertRaises(ValueError):finish.finish(Path(d)/'boundary')
                execute.assert_not_called()
    def test_uncertain_exec_only_read_reconciliation(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary'
            def ack(path,request):finish.atomic_json(path/'ack.json',request)
            with patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish',side_effect=TimeoutError('secret raw output')) as execute:
                receipt=finish.finish(p)
            self.assertEqual(execute.call_count,2);self.assertEqual(receipt['status'],'INCONCLUSIVE');self.assertNotIn('secret',json.dumps(receipt));self.assertTrue(all(r['status']=='unknown' for r in receipt['clients'].values()))
    def test_changed_roster_after_ack_refuses(self):
        with tempfile.TemporaryDirectory() as d,patch.object(finish,'wait_ack'),patch.object(finish,'live_roster',side_effect=[ROSTER,[dict(ROSTER[0],uid='changed'),ROSTER[1]]]),patch.object(finish,'exec_finish') as execute:
            with self.assertRaises(ValueError):finish.finish(Path(d)/'boundary')
            execute.assert_not_called()


class BoundRosterTests(unittest.TestCase):
    """The request and actions must retain the caller's original identities."""
    def ack(self, directory, request):
        finish.atomic_json(directory/'ack.json', request)

    def test_first_live_replacement_refused_before_request(self):
        changed=[dict(row, **{field:'replacement-'+row[field] for field in row}) for row in ROSTER]
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';actions=[]
            with patch.object(finish,'live_roster',return_value=changed),patch.object(finish,'wait_ack',side_effect=self.ack),patch.object(finish,'exec_finish',side_effect=actions.append):
                with self.assertRaises(ValueError):finish.finish(p, expected_roster=ROSTER)
            self.assertFalse((p/'request.json').exists());self.assertEqual(actions,[])

    def test_second_live_change_refused_before_actions(self):
        # Every identity field participates in the binding, including container ID.
        for field in ('name','uid','containerId','processId'):
            with self.subTest(field=field),tempfile.TemporaryDirectory() as d:
                p=Path(d)/'boundary';actions=[]
                changed=[dict(ROSTER[0], **{field:'replacement'}),ROSTER[1]]
                with patch.object(finish,'live_roster',side_effect=[ROSTER,changed]),patch.object(finish,'wait_ack',side_effect=self.ack),patch.object(finish,'exec_finish',side_effect=actions.append):
                    with self.assertRaises(ValueError):finish.finish(p, expected_roster=ROSTER)
                self.assertEqual(finish.read_json(p/'request.json')['roster'],ROSTER)
                self.assertEqual(actions,[]);self.assertFalse((p/'receipt.json').exists())

    def test_malformed_or_duplicate_binding_refused_before_live_read(self):
        malformed=[None,{},[],ROSTER[:1],ROSTER+[dict(ROSTER[0])],[None,ROSTER[1]],['invalid',ROSTER[1]],
                   [dict(ROSTER[0],extra='invalid'),ROSTER[1]]]
        for field in ('name','uid','containerId','processId'):
            missing=dict(ROSTER[0]);missing.pop(field)
            malformed.extend(([missing,ROSTER[1]],[dict(ROSTER[0],**{field:ROSTER[1][field]}),ROSTER[1]]))
            for value in ('',1,False,None,'x'*257):
                malformed.append([dict(ROSTER[0],**{field:value}),ROSTER[1]])
        for rows in malformed:
            with self.subTest(rows=rows),tempfile.TemporaryDirectory() as d:
                p=Path(d)/'boundary';actions=[]
                def unexpected_read():self.fail('invalid binding reached live read')
                with patch.object(finish,'live_roster',side_effect=unexpected_read),patch.object(finish,'exec_finish',side_effect=actions.append):
                    with self.assertRaises(ValueError):finish.finish(p, expected_roster=rows)
                self.assertFalse((p/'request.json').exists());self.assertEqual(actions,[])

    def test_unchanged_bound_originals_use_exact_in_pod_arguments_once(self):
        import subprocess
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';commands=[]
            def execute(argv, **kwargs):
                commands.append(argv)
                self.assertIs(kwargs['stdout'],subprocess.DEVNULL)
                self.assertIs(kwargs['stderr'],subprocess.DEVNULL)
                self.assertEqual(kwargs['timeout'],3)
                return subprocess.CompletedProcess(argv,0)
            with patch.object(finish,'live_roster',return_value=list(reversed(ROSTER))),patch.object(finish,'wait_ack',side_effect=self.ack),patch.object(finish.subprocess,'run',side_effect=execute):
                result=finish.finish(p, expected_roster=list(reversed(ROSTER)))
            self.assertEqual(finish.read_json(p/'request.json')['roster'],ROSTER)
            self.assertEqual(result['status'],'ACKNOWLEDGED')
            self.assertEqual(sorted(commands),[
                ['kubectl','-n','raptor-test','exec','client-0','--','/client','finish','--expected-pod-uid','uid-0','--expected-process-id','process-0'],
                ['kubectl','-n','raptor-test','exec','client-1','--','/client','finish','--expected-pod-uid','uid-1','--expected-process-id','process-1']])

    def test_private_cli_roster_file_binds_request_without_added_roster_output(self):
        import contextlib,io
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';source=Path(d)/'expected.json';finish.atomic_json(source,ROSTER)
            output=io.StringIO();actions=[]
            argv=['finish_clients.py','--boundary-dir',str(p),'--expected-roster-file',str(source)]
            with patch('sys.argv',argv),patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=self.ack),patch.object(finish,'exec_finish',side_effect=actions.append),contextlib.redirect_stdout(output):
                finish.main()
            self.assertEqual(finish.read_json(p/'request.json')['roster'],ROSTER)
            self.assertEqual(sorted(actions,key=lambda r:r['uid']),ROSTER)
            self.assertEqual(json.loads(output.getvalue())['status'],'ACKNOWLEDGED')
            # The existing receipt uses UID/process keys; the CLI adds no roster,
            # Pod/container names, source JSON or raw external output.
            self.assertNotIn('client-0',output.getvalue());self.assertNotIn('container-0',output.getvalue())

    def test_cli_rejects_nonprivate_symlink_or_invalid_roster_file_before_request(self):
        import contextlib,io
        for kind in ('public','symlink','malformed','duplicate','null'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                p=Path(d)/'boundary';source=Path(d)/'expected.json';finish.atomic_json(source,ROSTER)
                if kind=='public':source.chmod(0o644)
                elif kind=='symlink':
                    target=Path(d)/'link.json';target.symlink_to(source);source=target
                elif kind=='malformed':source.write_text('{')
                elif kind=='null':source.write_text('null')
                else:source.write_text('[{"name":"secret-invalid","name":"client-0","uid":"uid-0","containerId":"container-0","processId":"process-0"},'+json.dumps(ROSTER[1])+']')
                actions=[];output=io.StringIO()
                argv=['finish_clients.py','--boundary-dir',str(p),'--expected-roster-file',str(source)]
                def unexpected_read():self.fail('invalid private file reached live read')
                with patch('sys.argv',argv),patch.object(finish,'live_roster',side_effect=unexpected_read),patch.object(finish,'exec_finish',side_effect=actions.append),contextlib.redirect_stdout(output),contextlib.redirect_stderr(output):
                    with self.assertRaises(SystemExit) as error:finish.main()
                self.assertEqual(str(error.exception),'finish refused; preserve evidence and reconcile exact identities')
                self.assertNotIn('secret',output.getvalue());self.assertEqual(actions,[]);self.assertFalse((p/'request.json').exists())

    def test_cli_bound_file_rejects_first_live_replacement(self):
        import contextlib,io
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';source=Path(d)/'expected.json';finish.atomic_json(source,ROSTER)
            changed=[dict(ROSTER[0],containerId='replacement'),ROSTER[1]];actions=[]
            argv=['finish_clients.py','--boundary-dir',str(p),'--expected-roster-file',str(source)]
            with patch('sys.argv',argv),patch.object(finish,'live_roster',return_value=changed),patch.object(finish,'wait_ack',side_effect=self.ack),patch.object(finish,'exec_finish',side_effect=actions.append),contextlib.redirect_stdout(io.StringIO()),contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:finish.main()
            self.assertEqual(str(error.exception),'finish refused; preserve evidence and reconcile exact identities')
            self.assertFalse((p/'request.json').exists());self.assertEqual(actions,[])

    def test_binding_snapshot_survives_caller_mutation_after_request(self):
        original=[dict(row) for row in ROSTER]
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';actions=[]
            def ack(directory,request):
                original[0]['processId']='replacement'
                self.ack(directory,request)
            with patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish',side_effect=actions.append):
                result=finish.finish(p, expected_roster=original)
            self.assertEqual(finish.read_json(p/'request.json')['roster'],ROSTER)
            self.assertEqual(sorted(actions,key=lambda r:r['uid']),ROSTER)
            self.assertEqual(result['status'],'ACKNOWLEDGED')

class DeadlineFinishTests(unittest.TestCase):
    def test_ack_and_second_roster_share_deadline_and_private_credential(self):
        ticks=[10.0];seen=[]
        def roster(**kw):
            seen.append(kw);ticks[0]+=1.6;return ROSTER
        def ack(directory,request,**kw):
            self.assertEqual(kw,{'deadline':14});finish.atomic_json(directory/'ack.json',request)
        def execute(row,**kw):
            self.assertEqual(kw,{'deadline':14,'kubeconfig':'/bound'});ticks[0]=14.1
        with tempfile.TemporaryDirectory() as d,patch.object(finish.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(finish,'live_roster',side_effect=roster),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish',side_effect=execute):
            receipt=finish.finish(Path(d)/'boundary',expected_roster=ROSTER,deadline=14,kubeconfig='/bound')
            self.assertEqual(receipt['status'],'INCONCLUSIVE')
            self.assertTrue(all(v['status']=='unknown' for v in receipt['clients'].values()))
        self.assertTrue(all(kw=={'deadline':14,'kubeconfig':'/bound'} for kw in seen))

    def test_late_thread_keeps_bound_environment_and_does_not_block_return(self):
        import os,subprocess
        entered=threading.Event();release=threading.Event();seen=[]
        def execute(argv,**kw):
            if 'client-1' in argv:
                entered.set();release.wait(2)
            seen.append(kw['env']['KUBECONFIG']);return subprocess.CompletedProcess(argv,0)
        def ack(directory,request,**kw):finish.atomic_json(directory/'ack.json',request)
        try:
            with tempfile.TemporaryDirectory() as d,patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish.subprocess,'run',side_effect=execute),patch.dict(os.environ,{'KUBECONFIG':'ambient'}):
                started=time.monotonic();receipt=finish.finish(Path(d)/'boundary',expected_roster=ROSTER,deadline=started+.2,kubeconfig='/bound')
                self.assertTrue(entered.is_set());self.assertLess(time.monotonic()-started,.7)
                self.assertEqual(receipt['status'],'INCONCLUSIVE');self.assertEqual(os.environ['KUBECONFIG'],'ambient')
                release.set()
                for _ in range(100):
                    if len(seen)==2:break
                    time.sleep(.001)
            self.assertEqual(seen,['/bound','/bound'])
        finally:release.set()

    def test_expired_before_roster_or_after_ack_never_executes(self):
        for expired in ('entry','ack'):
            ticks=[14 if expired=='entry' else 10]
            def ack(directory,request,**kw):ticks[0]=14
            with self.subTest(expired=expired),tempfile.TemporaryDirectory() as d,patch.object(finish.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish') as execute:
                with self.assertRaises(ValueError):finish.finish(Path(d)/'boundary',deadline=14,kubeconfig='/bound')
                execute.assert_not_called()

    def test_two_sequential_three_second_reads_cannot_share_four_second_success(self):
        import subprocess,json,worker_capture
        ticks=[10.0];timeouts=[]
        pod={'metadata':{'name':'client-0','uid':'uid-0'},'status':{'phase':'Running','podIP':'10.0.0.1','containerStatuses':[{'containerID':'container-0'}]}}
        def kube(argv,**kw):
            timeouts.append(kw['timeout']);ticks[0]+=3
            return subprocess.CompletedProcess(argv,0,json.dumps({'items':[pod,pod]}).encode())
        class Response:
            def __enter__(self):return self
            def __exit__(self,*a):pass
            def read(self,n):ticks[0]+=3;return b'{"sample":{"processId":"process-0"}}'
        def http(url,timeout):timeouts.append(timeout);return Response()
        with patch.object(finish.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(worker_capture.subprocess,'run',side_effect=kube),patch.object(worker_capture.urllib.request,'urlopen',side_effect=http):
            with self.assertRaises(ValueError):finish.live_roster(deadline=14,kubeconfig='/bound')
        self.assertEqual(timeouts,[3,1])

    def test_ack_sleeps_and_ack_file_read_cannot_reset_deadline(self):
        ticks=[10.0];sleeps=[]
        def sleep(value):sleeps.append(value);ticks[0]+=value
        with tempfile.TemporaryDirectory() as d,patch.object(finish.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(finish.time,'sleep',side_effect=sleep):
            p=Path(d)/'boundary';finish.private_directory(p)
            with self.assertRaises(ValueError):finish.wait_ack(p,{},deadline=10.4)
        self.assertEqual(len(sleeps),1);self.assertAlmostEqual(sleeps[0],.4)
        ticks[0]=10
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';finish.private_directory(p);request={'requestId':'f'*32,'roster':ROSTER};finish.atomic_json(p/'ack.json',request)
            actual=finish.read_json
            def late(path):
                value=actual(path);ticks[0]=14;return value
            with patch.object(finish.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(finish,'read_json',side_effect=late):
                with self.assertRaises(ValueError):finish.wait_ack(p,request,deadline=14)

    def test_partial_parallel_response_preserves_known_ack_and_never_replays(self):
        gate=threading.Event();seen=[]
        def execute(row,**kw):
            seen.append(row['uid'])
            if row['uid']=='uid-1':gate.wait(1);raise TimeoutError('private diagnostic')
        def ack(directory,request,**kw):finish.atomic_json(directory/'ack.json',request)
        try:
            with tempfile.TemporaryDirectory() as d,patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish',side_effect=execute):
                p=Path(d)/'boundary';receipt=finish.finish(p,expected_roster=ROSTER,deadline=time.monotonic()+.2,kubeconfig='/bound')
                self.assertEqual(receipt['clients'],{'uid-0/process-0':{'status':'acknowledged'},'uid-1/process-1':{'status':'unknown'}})
                self.assertEqual(receipt['status'],'INCONCLUSIVE');self.assertFalse(receipt['rosterUnchanged'])
                self.assertEqual(finish.read_json(p/'receipt.json'),receipt)
                with self.assertRaises(FileExistsError):finish.finish(p,expected_roster=ROSTER,deadline=time.monotonic()+1,kubeconfig='/bound')
                self.assertEqual(sorted(seen),['uid-0','uid-1'])
        finally:gate.set()

    def test_late_durable_ack_receipt_is_not_returned_as_success(self):
        ticks=[10.0];actual=finish.atomic_json
        def write(path,value):
            actual(path,value)
            if path.name=='receipt.json':ticks[0]=14
        def ack(directory,request,**kw):actual(directory/'ack.json',request)
        with tempfile.TemporaryDirectory() as d,patch.object(finish.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish'),patch.object(finish,'atomic_json',side_effect=write):
            p=Path(d)/'boundary'
            with self.assertRaises(ValueError):finish.finish(p,expected_roster=ROSTER,deadline=14,kubeconfig='/bound')
            self.assertTrue((p/'request.json').exists());self.assertTrue((p/'receipt.json').exists())
