import unittest
from unittest.mock import patch
import worker_capture as worker

class MetricsExtraction(unittest.TestCase):
    def response(self,url):
        from urllib.parse import urlparse,parse_qs
        q=parse_qs(urlparse(url).query)['query'][0]
        timestamp=q.startswith('timestamp(')
        rows=[]
        for node in ['a','b']:
            metric={'ecs_instance_id':node}
            if 'pgcat_pools' in q:metric.update(pool='test',user='poc_app')
            rows.append({'metric':metric,'value':[1000,'1000' if timestamp else '0']})
        return {'status':'success','data':{'resultType':'vector','result':rows}}
    def test_complete_raw_values_and_original_timestamps(self):
        with patch.object(worker,'get',side_effect=self.response):
            m=worker.query_metrics('http://fixture',1000)
        self.assertTrue(m['available']);self.assertEqual(len(m['raw']),6);self.assertTrue(all(x['sourceAt']==1000 for x in m['raw']))
    def test_per_node_up_timestamps_reveal_stale_candidate_when_min_advances(self):
        from urllib.parse import urlparse, parse_qs
        def sample(up):
            def get(url):
                response=self.response(url)
                query=parse_qs(urlparse(url).query)['query'][0]
                if query.startswith('timestamp(up{'):
                    for row in response['data']['result']:
                        row['value'][1]=str(up[row['metric']['ecs_instance_id']])
                return response
            with patch.object(worker,'get',side_effect=get):
                return worker.query_metrics('http://fixture',1000)
        first=sample({'a':1000,'b':1001})
        second=sample({'a':1002,'b':1001})
        self.assertEqual((first['sourceAt'],second['sourceAt']),(1000,1001))
        self.assertEqual(first.get('upSourceAt'),{'a':1000,'b':1001})
        self.assertEqual(second.get('upSourceAt'),{'a':1002,'b':1001})
    def test_up_only_cannot_pass(self):
        def get(url):
            r=self.response(url)
            if 'pgcat_pools' in url:r['data']['result']=[]
            return r
        with patch.object(worker,'get',side_effect=get):
            with self.assertRaises(ValueError):worker.query_metrics('http://fixture',1000)
    def test_missing_source_timestamp_refuses(self):
        def get(url):
            r=self.response(url)
            if 'timestamp%28pgcat_pools_cl_idle' in url:r['data']['result'].pop()
            return r
        with patch.object(worker,'get',side_effect=get):
            with self.assertRaises(ValueError):worker.query_metrics('http://fixture',1000)

class StrictMetrics(MetricsExtraction):
    def query(self, mutate):
        def get(url):
            from urllib.parse import urlparse, parse_qs
            q=parse_qs(urlparse(url).query)['query'][0]
            r=self.response(url);mutate(q,r['data']['result']);return r
        with patch.object(worker,'get',side_effect=get):
            return worker.query_metrics('http://fixture',1000)
    def test_exact_scoped_queries(self):
        queries=[]
        self.query(lambda q,rows:queries.append(q))
        selector='{env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"}'
        self.assertEqual(set(queries),{'timestamp(up{job="pgcat",env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"} == 1)'} |
                         {q for k in ('active','idle','waiting') for q in ('pgcat_pools_cl_'+k+selector,'timestamp(pgcat_pools_cl_'+k+selector+')')})
    def test_reject_invalid_series(self):
        def alter(q,rows,case):
            if case=='duplicate up' and 'up{' in q:rows.append(rows[0])
            if case=='missing up' and 'up{' in q:rows.pop()
            if case=='foreign up' and 'up{' in q:rows[0]['metric']['ecs_instance_id']='foreign'
            if case=='nonfinite up timestamp' and 'up{' in q:rows[0]['value'][1]='NaN'
            if case=='invalid up identity' and 'up{' in q:rows[0]['metric']['ecs_instance_id']='https://private.example/secret'
            if case=='too many up identities' and 'up{' in q:
                for i in range(3):rows.append({'metric':{'ecs_instance_id':'extra'+str(i)},'value':[1000,'1000']})
            if case=='duplicate value' and q.startswith('pgcat_pools_cl_idle'):rows.append(rows[0])
            if case=='missing timestamp' and q.startswith('timestamp(pgcat_pools_cl_idle'):rows.pop()
            if case=='missing triplet' and 'pgcat_pools_cl_idle' in q:rows.pop()
            if case=='wrong identity' and 'pgcat_pools' in q:
                for row in rows:row['metric']['pool']='other'
            if case in ('NaN','inf','0.5','-1') and q.startswith('pgcat_pools_cl_active'):rows[0]['value'][1]=case
            if case=='nonfinite timestamp' and q.startswith('timestamp(pgcat_pools'):rows[0]['value'][1]='NaN'
        for case in ('duplicate up','missing up','foreign up','nonfinite up timestamp','invalid up identity','too many up identities','duplicate value','missing timestamp','missing triplet','wrong identity','NaN','inf','0.5','-1','nonfinite timestamp'):
            with self.subTest(case=case),self.assertRaises(ValueError):self.query(lambda q,rows:alter(q,rows,case))
    def test_raw_allowlist(self):
        m=self.query(lambda q,rows:None)
        self.assertTrue(all(set(r)=={'node','pool','user','kind','value','sourceAt'} for r in m['raw']))


class FrameMetricsFailure(unittest.TestCase):
    def test_invalid_metrics_leave_client_evidence_available(self):
        import tempfile, json
        from pathlib import Path
        pod={'metadata':{'uid':'old'},'status':{'containerStatuses':[{'containerID':'container'}],'podIP':'10.0.0.1','phase':'Running'}}
        final={'final':True,'sample':{'processId':'old-process'}}
        def kube(*args):
            if args[1]=='pods':return {'items':[pod]}
            if args[1]=='deployment':return {'spec':{'replicas':1}}
            return {'spec':{'clusterIP':'10.0.0.2'}}
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'inventory.json';path.write_text(json.dumps({'members':['a','b']}))
            for error in (ValueError('invalid series'),KeyError('label'),TypeError('malformed result')):
                with self.subTest(error=type(error).__name__),patch.object(worker,'kubectl',side_effect=kube),patch.object(worker,'query_metrics',side_effect=error):
                    frame=worker.capture_frame(str(path),{('old','container'):final})
                self.assertEqual(frame['pods'],[{'uid':'old','name':'','containerId':'container','ready':None,'snapshot':final}])
                self.assertFalse(frame['metrics']['available'])
                self.assertEqual(frame['metrics']['raw'],[])


class PodLifecycle(unittest.TestCase):
    def test_final_snapshot_cached_before_pod_endpoint_disappears(self):
        cache={};pod={'metadata':{'uid':'old'},'status':{'containerStatuses':[{'containerID':'container'}],'podIP':'10.0.0.1'}}
        snapshot={'sample':{'processId':'old-process'},'final':True,'connected':0}
        with patch.object(worker,'get',return_value=snapshot) as read:
            self.assertEqual(worker.read_pod(pod,cache)['snapshot'],snapshot)
            read.assert_called_once()
        with patch.object(worker,'get',side_effect=OSError('gone')) as read:
            self.assertEqual(worker.read_pod(pod,cache)['snapshot'],snapshot)
            read.assert_not_called()
    def test_nonfinal_endpoint_loss_still_refuses(self):
        cache={};pod={'metadata':{'uid':'old'},'status':{'containerStatuses':[{'containerID':'container'}],'podIP':'10.0.0.1'}}
        with patch.object(worker,'get',return_value={'final':False}):worker.read_pod(pod,cache)
        self.assertEqual(cache,{})
        with patch.object(worker,'get',side_effect=OSError('gone')):
            with self.assertRaises(OSError):worker.read_pod(pod,cache)
    def test_cached_final_does_not_cross_pod_uids(self):
        cache={'old':{'final':True}};pod={'metadata':{'uid':'new'},'status':{'containerStatuses':[{'containerID':'container'}],'podIP':'10.0.0.1'}}
        with patch.object(worker,'get',side_effect=OSError('unready')):
            with self.assertRaises(OSError):worker.read_pod(pod,cache)


class FinishBoundary(unittest.TestCase):
    def test_same_pod_new_container_reads_new_process(self):
        p={'metadata':{'uid':'pod'},'status':{'podIP':'ip','containerStatuses':[{'name':'client','containerID':'old'}]}}
        cache={}
        with patch.object(worker,'get',return_value={'final':True,'sample':{'processId':'old'}}):worker.read_pod(p,cache)
        p['status']['containerStatuses'][0]['containerID']='new'
        with patch.object(worker,'get',return_value={'final':False,'sample':{'processId':'new'}}) as read:
            self.assertEqual(worker.read_pod(p,cache)['snapshot']['sample']['processId'],'new');read.assert_called_once()
    def run_boundary(self,kind):
        import tempfile,json,copy
        from pathlib import Path
        from test_capture_window import frame
        from test_finish_clients import ROSTER
        clock=[0];seen=[]
        def capture(*args):
            t=clock[0];f=frame(t,t);f['expectedReplicas']=2;f['pods']=[]
            for i,row in enumerate(ROSTER):
                n=min(t,2+i)
                pod=copy.deepcopy(frame(t,n)['pods'][0]);pod.update(uid=row['uid'],name=row['name'],containerId=row['containerId']);s=pod['snapshot'];s['sample']['processId']=row['processId']
                for e in s['events']:e['processId']=row['processId']
                s['final']=t>=(7 if kind=='late' else 2+i) and kind not in ('missing','failure');s['connected']=0 if s['final'] else 4
                if kind=='new' and t>=2:pod['containerId']='replacement';s['sample']['processId']='replacement'
                if kind=='failure' and t>=2:s['sample']['Failure']=1;s['sample']['Success']-=1;s['events'][-1]['outcome']='failure'
                f['pods'].append(pod)
            if t==1:
                request={'requestId':'one','roster':ROSTER}
                if kind=='malformed':request={'requestId':'one'}
                if kind in ('uid-mismatch','pid-mismatch'):
                    request['roster']=[dict(r) for r in ROSTER];request['roster'][0]['uid' if kind=='uid-mismatch' else 'processId']='changed'
                from finish_clients import atomic_json
                atomic_json(boundary/'request.json',request)
            if t==2 and kind not in ('malformed','uid-mismatch','pid-mismatch','no-receipt'):
                from finish_clients import atomic_json
                receipt={'requestId':'one','status':'INCONCLUSIVE' if kind=='uncertain' else 'ACKNOWLEDGED','clients':{r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in ROSTER}}
                if kind=='receipt-no-clients':receipt.pop('clients')
                if kind=='receipt-missing-client':receipt['clients'].pop('uid-0/process-0')
                if kind=='receipt-unknown':receipt['clients']['uid-0/process-0']['status']='unknown'
                if kind=='receipt-mismatched-client':receipt['clients']['wrong/process']=receipt['clients'].pop('uid-0/process-0')
                atomic_json(boundary/'receipt.json',receipt)
            if t>=2:self.assertTrue((boundary/'ack.json').exists() or kind in ('malformed','uid-mismatch','pid-mismatch'))
            if kind=='slow-final' and t==3:clock[0]=7
            seen.append(f);return f
        with tempfile.TemporaryDirectory() as d:
            boundary=Path(d)/'boundary';boundary.mkdir(mode=0o700)
            import finish_clients
            original_read=finish_clients.read_json
            def read(path):
                value=original_read(path)
                if kind=='receipt-late-read' and path.name=='receipt.json':clock[0]=7
                return value
            with patch.object(finish_clients,'read_json',side_effect=read),patch.object(worker,'capture_frame',side_effect=capture),patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]),patch.object(worker.time,'sleep',side_effect=lambda _:clock.__setitem__(0,clock[0]+1)):
                result=worker.run_capture(Path(d)/'out',Path(d)/'inventory',8,boundary)
            if kind in ('uid-mismatch','pid-mismatch','malformed'):self.assertFalse((boundary/'ack.json').exists())
            retained=[json.loads(line) for p in sorted((Path(d)/'out').glob('segment-*.jsonl')) for line in p.read_text().splitlines()]
            return result,retained
    def test_boundary_ack_and_one_final_does_not_close(self):
        result,rows=self.run_boundary('good');self.assertEqual(result['status'],'PASS');self.assertEqual(rows[-1]['at'],3);self.assertEqual(rows[1]['phase'],'settling');self.assertFalse(rows[2]['pods'][1]['snapshot']['final'])
    def test_missing_malformed_and_new_process_cannot_pass(self):
        for kind in ('missing','malformed','new','late','uid-mismatch','pid-mismatch','no-receipt','uncertain','slow-final'):
            with self.subTest(kind=kind):self.assertEqual(self.run_boundary(kind)[0]['status'],'INCONCLUSIVE')
    def test_observed_failure_wins_over_missing_final(self):
        self.assertEqual(self.run_boundary('failure')[0]['status'],'FAIL')

    def test_exact_receipt_required_before_deadline(self):
        for kind in ('receipt-no-clients','receipt-missing-client','receipt-unknown','receipt-mismatched-client','receipt-late-read'):
            with self.subTest(kind=kind):self.assertEqual(self.run_boundary(kind)[0]['status'],'INCONCLUSIVE')

class PartialFrameRetention(unittest.TestCase):
    def check_partial(self, kind, sql_failure):
        import json, tempfile
        from pathlib import Path
        from test_capture_window import frame
        from segmented_capture import SegmentedCapture
        with tempfile.TemporaryDirectory() as directory:
            inventory=Path(directory)/'inventory.json'
            inventory.write_text('{invalid' if kind=='inventory' else json.dumps(frame(0,0)['inventory']))
            writer=SegmentedCapture(Path(directory)/'capture')
            writer.append(frame(0,0))
            for t in (1,2):
                fixture=frame(t,t,final=t==2)
                snapshot=fixture['pods'][0]['snapshot']
                if sql_failure:
                    snapshot['sample']['Failure']=1;snapshot['sample']['Success']-=1
                    snapshot['events'][1]['outcome']='failure'
                pods=[{'metadata':{'uid':uid},'status':{'podIP':uid,'phase':'Running','containerStatuses':[{'containerID':'container'}]}} for uid in ('pod','peer')]
                def kube(*args):
                    if args[1]=='pods':return {'items':pods if kind=='peer' else pods[:1]}
                    if args[1]=='deployment':
                        if kind=='deployment':raise RuntimeError('unavailable')
                        return {'spec':{'replicas':1}}
                    return {'spec':{'clusterIP':'metrics'}}
                def get(url):
                    if 'peer' in url:raise OSError('peer lost')
                    return snapshot
                with patch.object(worker,'utc',return_value=t),patch.object(worker,'kubectl',side_effect=kube),patch.object(worker,'get',side_effect=get),patch.object(worker,'query_metrics',return_value=fixture['metrics']):
                    observed=worker.capture_frame(inventory)
                self.assertEqual(observed['pods'][0]['snapshot'],snapshot)
                if t==2:observed['phase']='settling'
                writer.append(observed)
            result=writer.finish(completed=True,cancelled=False,errors=[])
            self.assertEqual(result['status'],'FAIL' if sql_failure else 'INCONCLUSIVE')
            retained=[json.loads(line) for p in (Path(directory)/'capture').glob('segment-*.jsonl') for line in p.read_text().splitlines()]
            self.assertEqual(retained[-1]['pods'][0]['snapshot']['sample']['Failure'],int(sql_failure))
    def test_sql_failure_survives_peer_loss(self):self.check_partial('peer',True)
    def test_sql_failure_survives_deployment_error(self):self.check_partial('deployment',True)
    def test_sql_failure_survives_inventory_error(self):self.check_partial('inventory',True)
    def test_partial_coverage_cannot_pass(self):
        for kind in ('peer','deployment','inventory'):
            with self.subTest(kind=kind):self.check_partial(kind,False)

class OptionalTimingDiagnostic(unittest.TestCase):
    def test_timing_disk_errors_cannot_prevent_capture_completion(self):
        import os, tempfile
        from pathlib import Path
        from test_capture_window import frame
        real_fdopen=os.fdopen
        class BrokenTiming:
            def __init__(self,stream,kind):self.stream=stream;self.kind=kind
            def write(self,value):
                if self.kind=='write':raise OSError('disk full')
                return self.stream.write(value)
            def close(self):
                self.stream.close()
                raise OSError('flush failed')
        for kind in ('write','close'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);clock=[0.0];first=[True]
                def fdopen(fd,*args,**kwargs):
                    stream=real_fdopen(fd,*args,**kwargs)
                    if first[0]:first[0]=False;return BrokenTiming(stream,kind)
                    return stream
                def capture(*_):return frame(int(clock[0]),int(clock[0]),final=clock[0]>=5)
                with patch.object(worker.os,'fdopen',side_effect=fdopen),patch.object(worker,'capture_frame',side_effect=capture),patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]),patch.object(worker.time,'sleep',side_effect=lambda delay:clock.__setitem__(0,clock[0]+delay)):
                    result=worker.run_capture(root/'capture',root/'inventory',5,timing_output=root/'timing.jsonl')
                self.assertEqual(result['status'],'PASS')
                self.assertTrue((root/'capture'/'completion.json').exists())
                self.assertTrue((root/'capture'/'summary.json').exists())

    def test_failed_read_is_timed_without_changing_retained_evidence(self):
        import json, os, tempfile
        from pathlib import Path
        from test_capture_window import frame
        def run(root, diagnostic):
            clock=[0.0]
            def capture(*_):
                second=int(clock[0])
                clock[0]+=.25 if second==1 else .125
                if second==1:raise TimeoutError('private endpoint detail')
                return frame(second,second,final=second>=5)
            with patch.object(worker,'capture_frame',side_effect=capture),patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]),patch.object(worker.time,'sleep',side_effect=lambda delay:clock.__setitem__(0,clock[0]+delay)):
                verdict=worker.run_capture(root/'capture',root/'inventory',5,timing_output=root/'timing.jsonl' if diagnostic else None)
            evidence=[p.read_bytes() for p in sorted((root/'capture').glob('segment-*.jsonl'))]
            completion=json.loads((root/'capture'/'completion.json').read_text())
            return verdict,evidence,completion
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);plain=root/'plain';timed=root/'timed';plain.mkdir();timed.mkdir()
            a=run(plain,False);b=run(timed,True)
            self.assertEqual(a[0],b[0]);self.assertEqual(a[1],b[1]);self.assertEqual(a[2]['missingPollCount'],b[2]['missingPollCount'])
            self.assertEqual(b[0]['status'],'INCONCLUSIVE')
            rows=[json.loads(line) for line in (timed/'timing.jsonl').read_text().splitlines()]
            self.assertEqual(len(rows),6)
            self.assertEqual([row['outcome'] for row in rows].count('unavailable'),1)
            self.assertEqual(rows[1]['captureMs'],250)
            self.assertIsNone(rows[1]['appendMs'])
            self.assertEqual(os.stat(timed/'timing.jsonl').st_mode&0o777,0o600)
            self.assertNotIn('private endpoint detail',(timed/'timing.jsonl').read_text())


class TransitionInventoryCapture(unittest.TestCase):
    def test_retains_schema_two_and_unavailable_without_losing_clients(self):
        import tempfile,json,copy
        from pathlib import Path
        from test_capture_window import transition_frame
        original=transition_frame(0)
        pod={'metadata':{'uid':'pod'},'status':{'containerStatuses':[{'containerID':'container'}],'podIP':'private','phase':'Running'}}
        def kube(*args):
            if args[1]=='pods':return {'items':[pod]}
            if args[1]=='deployment':return {'spec':{'replicas':1}}
            return {'spec':{'clusterIP':'private'}}
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'inventory.json'
            for case in ('complete','missing field','missing row key'):
                source=copy.deepcopy(original['inventory']);source['privateAddresses']={'a':'private'}
                if case=='missing field':del source['backendStates']
                if case=='missing row key':del source['memberStates'][0]['health']
                path.write_text(json.dumps(source))
                with self.subTest(case=case),patch.object(worker,'kubectl',side_effect=kube),patch.object(worker,'get',return_value=original['pods'][0]['snapshot']),patch.object(worker,'query_metrics',return_value=original['metrics']):
                    got=worker.capture_frame(path)
                    self.assertEqual(got['inventory'].get('schemaVersion'),2)
                    self.assertNotIn('private',str(got['inventory']))
                    self.assertEqual(got['pods'][0]['snapshot'],original['pods'][0]['snapshot'])
                    if case=='complete':self.assertEqual(got['inventory'],original['inventory'])
                    else:self.assertEqual(got['inventory']['desired'],0)


class PartialPodWorkerEvidence(unittest.TestCase):
    def test_worker_retains_one_poll_failure_when_other_pod_is_malformed(self):
        import tempfile,json
        from pathlib import Path
        from test_capture_window import partial_pod_failure_frame,transition_frame
        for event_only in (False,True):
            with self.subTest(event_only=event_only),tempfile.TemporaryDirectory() as d:
                root=Path(d);clock=[0.0]
                def capture(*_):
                    t=int(clock[0])
                    if t==0:return partial_pod_failure_frame(event_only)
                    f=transition_frame(t)
                    if t>=5:f['pods'][0]['snapshot'].update(final=True,connected=0)
                    return f
                with patch.object(worker,'capture_frame',side_effect=capture),patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]),patch.object(worker.time,'sleep',side_effect=lambda delay:clock.__setitem__(0,clock[0]+delay)):
                    result=worker.run_capture(root/'capture',root/'inventory',5)
                self.assertEqual(result['status'],'FAIL')
                raw=''.join(p.read_text() for p in (root/'capture').glob('segment-*.jsonl'))
                self.assertNotIn('10.20.30.40',raw);self.assertNotIn('do-not-retain',raw)
                completion=json.loads((root/'capture'/'completion.json').read_text())
                self.assertEqual(sum(s['frames'] for s in completion['segments']),6)




class LaneIdentityCapture(unittest.TestCase):
    def capture(self, conditions, deployment_change=None, sql_failure=False):
        import tempfile, json
        from pathlib import Path
        from test_capture_window import transition_frame
        fixture=transition_frame(1)
        if sql_failure:
            fixture['pods'][0]['snapshot']['sample'].update(Success=0,Failure=1)
            fixture['pods'][0]['snapshot']['events'][-1]['outcome']='failure'
        pod={'metadata':{'uid':'pod','name':'client-0','private':'opaque'},
             'status':{'podIP':'10.0.0.1','phase':'Running','conditions':conditions,
                       'containerStatuses':[{'containerID':'containerd://abc'}]}}
        deployment={'metadata':{'uid':'deployment-uid','generation':7,'private':'opaque'},
                    'spec':{'replicas':2},'status':{'observedGeneration':7,'readyReplicas':2}}
        if deployment_change:deployment_change(deployment)
        def kube(*args):
            if args[1]=='pods':return {'items':[pod]}
            if args[1]=='deployment':return deployment
            return {'spec':{'clusterIP':'10.0.0.2'}}
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'inventory.json';path.write_text(json.dumps(fixture['inventory']))
            with patch.object(worker,'kubectl',side_effect=kube),patch.object(worker,'get',return_value=fixture['pods'][0]['snapshot']),patch.object(worker,'query_metrics',return_value=fixture['metrics']):
                return worker.capture_frame(path)

    def test_capture_exact_identity_and_ready_without_kubernetes_dump(self):
        got=self.capture([{'type':'Ready','status':'True'},{'type':'ContainersReady','status':'True'}])
        self.assertEqual(got.get('deployment'),{'uid':'deployment-uid','generation':7,'observedGeneration':7,'desired':2,'ready':2})
        self.assertEqual({k:got['pods'][0].get(k) for k in ('name','containerId','ready')},
                         {'name':'client-0','containerId':'containerd://abc','ready':True})
        self.assertNotIn('10.0.0.1',str(got));self.assertNotIn('opaque',str(got))

    def test_ready_is_exact_condition_with_uncertainty_visible(self):
        for conditions,want in [([{'type':'Ready','status':'False'}],False),
                                ([{'type':'Ready','status':'Unknown'}],None),([],None),
                                ([{'type':'Ready','status':True}],None),
                                ([{'type':'Ready','status':'True'}]*2,None)]:
            with self.subTest(conditions=conditions):
                got=self.capture(conditions)
                self.assertIn('ready',got['pods'][0]);self.assertIs(got['pods'][0]['ready'],want)

    def test_partial_deployment_read_remains_explicit(self):
        got=self.capture([],lambda d:d['status'].pop('observedGeneration'))
        self.assertIn('deployment',got)
        self.assertIsNone((got['deployment'] or {}).get('observedGeneration'))
        from capture_window import clean
        self.assertEqual(clean(got)['expectedReplicas'],0)


    def test_malformed_deployment_mappings_retain_observed_sql_failure(self):
        import tempfile, json
        from pathlib import Path
        from segmented_capture import SegmentedCapture, assess_manifest
        for field in ('metadata','status','spec'):
            for invalid in (None,[], 'opaque', 1):
                with self.subTest(field=field,invalid=invalid):
                    got=self.capture([{'type':'Ready','status':'True'}],
                                     lambda d:d.__setitem__(field,invalid),sql_failure=True)
                    self.assertIsNone(got['deployment'])
                    self.assertEqual(got['pods'][0]['snapshot']['sample']['Failure'],1)
                    with tempfile.TemporaryDirectory() as directory:
                        writer=SegmentedCapture(Path(directory)/'capture')
                        writer.append(got)
                        result=writer.finish(completed=True,cancelled=False,errors=[])
                        self.assertEqual(result['status'],'FAIL')
                        self.assertEqual(assess_manifest(Path(directory)/'capture')['status'],'FAIL')
                        retained=json.loads(next((Path(directory)/'capture').glob('segment-*.jsonl')).read_text())
                        self.assertEqual(retained['expectedReplicas'],0)
                        self.assertEqual(retained['pods'][0]['snapshot']['sample']['Failure'],1)

if __name__ == '__main__':
    unittest.main()

class CollectorWitnessBoundary(unittest.TestCase):
    # Breaking seal order, restarting the clock at ACK, or accepting a foreign
    # final/receipt must fail these real-file collector fixtures.
    def run_witness(self,kind='good'):
        import tempfile,json,os,copy
        from pathlib import Path
        from test_segmented_capture import witness_context,witness_frame
        from finish_clients import atomic_json
        from segmented_capture import read_progress
        context=witness_context();clock=[0];seen={};original_finish=worker.SegmentedCapture.finish
        with tempfile.TemporaryDirectory() as d:
            root=Path(os.path.realpath(d));boundary=root/'boundary';boundary.mkdir(mode=0o700)
            def capture(*args):
                t=clock[0]
                if kind=='initial-error' and t==0:raise OSError('poll')
                if t==1:atomic_json(boundary/'request.json',dict(requestId='one',roster=context['originalRoster']))
                if t==2:
                    self.assertTrue((boundary/'boundary-start.json').exists())
                    self.assertTrue((boundary/'ack.json').exists())
                    receipt=dict(requestId='wrong' if kind=='receipt' else 'one',status='ACKNOWLEDGED',
                        clients={r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in context['originalRoster']})
                    atomic_json(boundary/'receipt.json',receipt)
                first=2;second=6 if kind=='exact-five' else 7 if kind=='late' else 3
                finals=tuple(i for i,limit in enumerate((first,second)) if t>=limit and kind!='missing')
                f=witness_frame(t,n=min(t,2),finals=finals)
                for i in finals:f['pods'][i]['snapshot']['sample']['at']=(first,second)[i]
                if t>=3 and kind=='connected':f['pods'][1]['snapshot']['connected']=1
                if t>=3 and kind=='counter':f['pods'][1]['snapshot']['sample']['Scheduled']+=1
                if t>=3 and kind=='foreign':f['pods'][1]['containerId']='foreign'
                if t==1 and kind=='slow-start':clock[0]=7
                return f
            def finish(writer,**kwargs):
                seen['beforeFinish']=clock[0];seen['start']=json.loads((boundary/'boundary-start.json').read_text()) if (boundary/'boundary-start.json').exists() else None
                seen['complete']=json.loads((boundary/'boundary-completed.json').read_text()) if (boundary/'boundary-completed.json').exists() else None
                seen['progress']=read_progress(root/'capture',context)[0]
                seen['rows']={p.name:p.read_bytes() for p in (root/'capture').glob('segment-*.jsonl')}
                if kind=='slow-finish':
                    self.assertIsNotNone(seen['complete']);clock[0]=99
                result=original_finish(writer,**kwargs)
                seen['result']=result
                return result
            with patch.object(worker,'capture_frame',side_effect=capture),patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]), \
                 patch.object(worker.time,'sleep',side_effect=lambda _:clock.__setitem__(0,clock[0]+1)),patch.object(worker.SegmentedCapture,'finish',finish):
                worker.run_capture(root/'capture',root/'inventory',8,boundary,progress_context=context)
            seen['files']={p.name:p.read_bytes() for p in boundary.iterdir()}
        return seen
    def test_exact_seals_before_slow_finish_and_original_final_pair(self):
        import hashlib,json
        for kind in ('good','slow-finish','exact-five'):
            with self.subTest(kind=kind):
                seen=self.run_witness(kind);start=seen['start'];complete=seen['complete']
                self.assertEqual((start['startedMonotonic'],start['deadlineMonotonic']),(1,6))
                self.assertEqual(complete['finalDurableMonotonic'],6 if kind=='exact-five' else 3)
                self.assertEqual(set(start),{'schemaVersion','bindingSha256','requestSha256','requestId','originalRosterSha256','startedMonotonic','deadlineMonotonic','firstSettlingFrame'})
                self.assertEqual(set(complete),{'schemaVersion','bindingSha256','requestSha256','requestId','startSha256','receiptSha256','finalDurableMonotonic','finals','lastFrame'})
                self.assertEqual(complete['startSha256'],hashlib.sha256(seen['files']['boundary-start.json']).hexdigest())
                self.assertEqual(complete['receiptSha256'],hashlib.sha256(seen['files']['receipt.json']).hexdigest())
                self.assertEqual(start['requestSha256'],hashlib.sha256(seen['files']['request.json']).hexdigest())
                self.assertEqual([row['frame']['at'] for row in complete['finals']],[2,6 if kind=='exact-five' else 3])
                for row in complete['finals']:
                    ref=row['frame'];raw=seen['rows'][ref['basename']][ref['offset']:ref['offset']+ref['length']]
                    self.assertEqual(ref['sha256'],hashlib.sha256(raw).hexdigest())
                    actual=json.loads(raw);pod=next(p for p in actual['pods'] if p['uid']==row['uid'])
                    self.assertTrue(pod['snapshot']['final']);self.assertEqual(pod['snapshot']['connected'],0)
    def test_missing_late_foreign_unsettled_and_bad_receipt_never_seal(self):
        for kind in ('missing','late','receipt','connected','counter','foreign','slow-start'):
            with self.subTest(kind=kind):self.assertIsNone(self.run_witness(kind)['complete'])
    def test_initial_poll_error_remains_after_final_frames(self):
        seen=self.run_witness('initial-error')
        self.assertTrue(seen['progress']['flags']['pollError']);self.assertEqual(seen['progress']['attempts'],4)

class WitnessFinalSnapshotRegression(unittest.TestCase):
    def test_previously_recorded_final_cannot_reopen_connections(self):
        from test_segmented_capture import witness_context,witness_frame
        from segmented_capture import SegmentedCapture,read_progress
        import tempfile,os
        from pathlib import Path
        with tempfile.TemporaryDirectory() as d:
            p=Path(os.path.realpath(d))/'capture';w=SegmentedCapture(p,progress_context=witness_context())
            w.append(witness_frame(0));first=witness_frame(1,n=1,finals=(0,));w.append(first)
            later=witness_frame(2,n=1,finals=(0,1));later['pods'][0]['snapshot']['sample']['at']=1
            later['pods'][0]['snapshot']['connected']=1;w.append(later)
            self.assertTrue(read_progress(p,witness_context())[0]['flags']['invalidEvidence'])

class MalformedPeerWorkerRegression(unittest.TestCase):
    def test_partial_bad_sample_retains_peer_fail_through_later_clean_polls(self):
        import tempfile,os,json
        from pathlib import Path
        from test_segmented_capture import witness_context,witness_frame
        from segmented_capture import read_progress
        for reverse in (False,True):
            with self.subTest(reverse=reverse),tempfile.TemporaryDirectory() as d:
                root=Path(os.path.realpath(d));boundary=root/'boundary';boundary.mkdir(mode=0o700);clock=[0]
                def capture(*args):
                    row=witness_frame(clock[0])
                    if clock[0]==0:
                        row['pods'][0]['snapshot']['sample']=[]
                        row['pods'][1]['snapshot']['sample'].update(Scheduled=1,Attempts=1,Failure=1)
                        if reverse:row['pods'].reverse()
                    return row
                with patch.object(worker,'capture_frame',side_effect=capture),patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]), \
                     patch.object(worker.time,'sleep',side_effect=lambda _:clock.__setitem__(0,clock[0]+1)):
                    result=worker.run_capture(root/'capture',root/'inventory',5,boundary,progress_context=witness_context())
                progress,_,_=read_progress(root/'capture',witness_context())
                self.assertTrue(progress['flags']['invalidEvidence']);self.assertTrue(progress['flags']['sqlFailure'])
                self.assertEqual(progress['committedFrames'],5);self.assertEqual(result['status'],'FAIL')
                retained=json.loads((root/'capture'/'segment-0001.jsonl').read_text().splitlines()[0])
                self.assertTrue(any(p['snapshot']['sample']['Failure']==1 for p in retained['pods']))

class DeadlineReads(unittest.TestCase):
    def test_kubernetes_uses_remaining_budget_and_bound_environment(self):
        import subprocess,os
        commands=[];ticks=[10.0]
        def run(argv,**kw):
            commands.append((argv,kw));ticks[0]=10.75
            return subprocess.CompletedProcess(argv,0,b'{}')
        with patch.object(worker.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(worker.subprocess,'run',side_effect=run),patch.dict(os.environ,{'KUBECONFIG':'ambient'}):
            self.assertEqual(worker.kubectl('get','pods',deadline=11,kubeconfig='/bound'),{})
            self.assertEqual(os.environ['KUBECONFIG'],'ambient')
        self.assertEqual(commands[0][1]['timeout'],1)
        self.assertEqual(commands[0][1]['env']['KUBECONFIG'],'/bound')

    def test_expired_before_or_after_io_refuses(self):
        import subprocess
        for before,after in ((11,11),(10,11)):
            ticks=[before];calls=[]
            def run(argv,**kw):
                calls.append(argv);ticks[0]=after
                return subprocess.CompletedProcess(argv,0,b'{}')
            with self.subTest(before=before),patch.object(worker.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(worker.subprocess,'run',side_effect=run):
                with self.assertRaises(ValueError):worker.kubectl('get','pods',deadline=11)
                self.assertEqual(len(calls),int(before<11))

    def test_http_read_checks_after_response_and_caps_timeout(self):
        ticks=[5];seen=[]
        class Response:
            def __enter__(self):return self
            def __exit__(self,*a):pass
            def read(self,n):ticks[0]=6;return b'{}'
        def urlopen(url,timeout):seen.append(timeout);return Response()
        with patch.object(worker.time,'monotonic',side_effect=lambda:ticks[0]),patch.object(worker.urllib.request,'urlopen',side_effect=urlopen):
            with self.assertRaises(ValueError):worker.get('http://fixture',deadline=6)
        self.assertEqual(seen,[1])

class NativeRollingBoundary(unittest.TestCase):
    def run_native(self, kind='good', execution='native-replacement', real_reads=False):
        import copy,json,os,tempfile
        from pathlib import Path
        from test_pgcat_native_evidence import context,operation,rolling,kubernetes
        from segmented_capture import open_directory,publish_private
        from finish_clients import atomic_json
        import pgcat_native_evidence as native
        clock=[0];ctx=context(execution);op=operation(execution);seen={}
        with tempfile.TemporaryDirectory() as folder:
            root=Path(os.path.realpath(folder));output=root/'capture';boundary=root/'boundary';boundary.mkdir(mode=0o700)
            def capture(*args,**kwargs):
                t=clock[0];value=rolling(t)
                if kind in ('old-failure','new-failure'):
                    target=0 if kind=='old-failure' else 2
                    if t==1:
                        snap=value['pods'][target]['snapshot'];snap['sample'].update(Scheduled=1,Attempts=1,Success=0,Failure=1)
                    if t==6:value['pods'][0]['snapshot']['sample']['Success']='malformed'
                if t==5:
                    fd=open_directory(output)
                    try:publish_private(fd,'native-operation-binding.json',op)
                    finally:os.close(fd)
                    if kind not in ('missing-seal','old-failure','new-failure'):
                        native.seal_terminal_clients(output,ctx,op,lambda:kubernetes(),now=lambda:4)
                    roster=kubernetes()['roster']
                    if kind=='original-request':roster=ctx['initialRoster']
                    atomic_json(boundary/'request.json',dict(requestId='finish-one',roster=roster))
                if t>=6:
                    for index,pod in enumerate(value['pods']):
                        final_at=(11 if kind=='late-final' else 7) if index else 6
                        if t>=final_at:
                            saved=rolling(final_at)['pods'][index]['snapshot'];pod['snapshot']=saved
                            saved.update(final=True,connected=0)
                    if kind=='changed-final' and t==7:value['pods'][0]['snapshot']['sample']['Success']+=1
                    if kind=='extra':value['pods'].append(copy.deepcopy(value['pods'][0]))
                    if kind=='wrong-process':value['pods'][0]['snapshot']['sample']['processId']='wrong'
                    if t==6:
                        roster=kubernetes()['roster']
                        if kind=='original-receipt':roster=ctx['initialRoster']
                        receipt=dict(requestId='finish-one',status='ACKNOWLEDGED',clients={r['uid']+'/'+r['processId']:{'status':'unknown' if kind=='unknown-ack' else 'acknowledged'} for r in roster})
                        atomic_json(boundary/'receipt.json',receipt)
                        if kind=='changed-binding':
                            (output/'native-operation-binding.json').write_text(json.dumps(dict(op,ingressSha256='0'*64)))
                return value
            active={}
            def kube(*args):
                if args[1]=='pods':
                    active['frame']=capture();value=active['frame'];live=kubernetes(clock[0])
                    live['deployment']['metadata']['generation']=value['deployment']['generation']
                    live['deployment']['status'].update(observedGeneration=value['deployment']['observedGeneration'],readyReplicas=value['deployment']['ready'])
                    for index,pod in enumerate(live['pods']):
                        retained=value['pods'][index]
                        pod['status']['podIP']='client-'+str(index)
                        pod['status']['conditions'][0]['status']='True' if retained['ready'] else 'False'
                        if retained['ownership']['terminating']:pod['metadata']['deletionTimestamp']='2026-10-29T00:00:00Z'
                    active['live']=live;(root/'inventory').write_text(json.dumps(value['inventory']))
                    return dict(items=live['pods'])
                if args[1]=='deployment':return active['live']['deployment']
                if args[1]=='replicasets':return dict(items=active['live']['replicaSets'])
                return dict(spec=dict(clusterIP='metrics'))
            def http(url,**kwargs):
                if '/evidence' in url:
                    index=int(url.split('client-')[1].split(':')[0])
                    return copy.deepcopy(active['frame']['pods'][index]['snapshot'])
                from urllib.parse import urlparse,parse_qs
                query=parse_qs(urlparse(url).query)['query'][0]
                rows=[]
                for node in ('a','b'):
                    metric=dict(ecs_instance_id=node)
                    if 'pgcat_pools' in query:metric.update(pool='test',user='poc_app')
                    rows.append(dict(metric=metric,value=[clock[0],str(clock[0] if query.startswith('timestamp(') else 0)]))
                return dict(status='success',data=dict(resultType='vector',result=rows))
            from contextlib import ExitStack
            with ExitStack() as stack:
                stack.enter_context(patch.object(worker.time,'monotonic',side_effect=lambda:clock[0]))
                stack.enter_context(patch.object(worker.time,'sleep',side_effect=lambda _:clock.__setitem__(0,clock[0]+1)))
                if real_reads:
                    stack.enter_context(patch.object(worker,'kubectl',side_effect=kube))
                    stack.enter_context(patch.object(worker,'get',side_effect=http))
                    stack.enter_context(patch.object(worker,'utc',side_effect=lambda:clock[0]))
                else:stack.enter_context(patch.object(worker,'capture_frame',side_effect=capture))
                seen['result']=worker.run_capture(output,root/'inventory',12,boundary,progress_context=ctx)
            seen['files']={path.name:json.loads(path.read_text()) for path in boundary.iterdir()}
            seen['progress']=json.loads((output/'progress.json').read_text())
            seen['rows']=[json.loads(line) for path in sorted(output.glob('segment-*.jsonl')) for line in path.read_text().splitlines()]
        return seen

    def test_native_boundary_targets_terminal_successors_and_keeps_all_four_finals(self):
        for execution in ('native-replacement','disabled-proof'):
            seen=self.run_native(execution=execution);complete=seen['files'].get('boundary-completed.json')
            self.assertIsNotNone(complete,seen['result'])
            start=seen['files']['boundary-start.json']
            self.assertEqual(start['schemaVersion'],2);self.assertEqual(start['executionKind'],execution)
            self.assertNotIn('originalRosterSha256',start)
            self.assertEqual(complete['terminalSha256'],start['terminalSha256'])
            self.assertEqual(complete['executionKind'],execution)
            self.assertEqual([r['uid'] for r in complete['finals']],['new-uid-0','new-uid-1'])
            self.assertEqual([r['frame']['at'] for r in complete['predecessorFinals']],[2,2])
            self.assertTrue(all(row['finalFrame'] is not None for row in seen['progress']['nativeCohort']['cohort']))

    def test_native_worker_uses_actual_owner_reads_and_source_frame_files(self):
        seen=self.run_native(real_reads=True)
        self.assertIn('boundary-completed.json',seen['files'],seen['result'])
        self.assertEqual([row['frame']['at'] for row in seen['files']['boundary-completed.json']['finals']],[6,7])
        self.assertEqual(seen['rows'][1]['deployment']['observedGeneration'],1)
        self.assertTrue(seen['rows'][2]['pods'][0]['ownership']['terminating'])

    def test_native_boundary_refuses_wrong_or_missing_or_changed_proof(self):
        for kind in ('missing-seal','original-request','original-receipt','unknown-ack','changed-binding','late-final','changed-final','extra','wrong-process'):
            with self.subTest(kind=kind):self.assertNotIn('boundary-completed.json',self.run_native(kind)['files'])

    def test_native_failure_survives_late_binding_and_finish_error(self):
        for kind in ('old-failure','new-failure'):
            with self.subTest(kind=kind):
                seen=self.run_native(kind)
                self.assertEqual(seen['result']['status'],'FAIL')
                self.assertTrue(seen['progress']['flags']['sqlFailure'])
                self.assertNotIn('boundary-completed.json',seen['files'])

class NativeStartupReads(unittest.TestCase):
    def observe(self,case):
        import copy,json,tempfile
        from pathlib import Path
        from test_pgcat_native_evidence import context,kubernetes,rolling
        import urllib.error
        live=kubernetes(1);live['deployment']['metadata']['generation']=2
        live['deployment']['status']['observedGeneration']=1
        initial=set(r['uid'] for r in context()['initialRoster'])
        for index,pod in enumerate(live['pods']):
            pod['status']['podIP']='ip-'+str(index)
            if pod['metadata']['uid'] not in initial:
                pod['status']['conditions']=[dict(type='Ready',status='False')]
                if case in ('pending','waiting'):
                    pod['status'].update(phase='Pending',containerStatuses=[] if case=='pending' else [dict(name='client',imageID='',state=dict(waiting=dict(reason='ContainerCreating')))]);pod['status'].pop('podIP')
                if case=='ready':pod['status']['conditions'][0]['status']='True'
                if case=='image':pod['status']['containerStatuses'][0]['imageID']='repo@sha256:'+'b'*64
                if case=='foreign':pod['metadata']['ownerReferences'][0]['uid']='other'
        if case=='traffic':live['pods'][0]['status']['podIP']='unavailable-initial'
        def kube(*args):
            if args[1]=='pods':return dict(items=live['pods'])
            if args[1]=='deployment':return live['deployment']
            if args[1]=='replicasets':return dict(items=live['replicaSets'])
            return dict(spec=dict(clusterIP='metrics'))
        def get(url,**kwargs):
            if 'unavailable-initial' in url:raise urllib.error.URLError(ConnectionRefusedError())
            index=int(url.split('ip-')[1].split(':')[0])
            if index>=2:
                if case=='malformed':return {'sample':'broken'}
                raise urllib.error.URLError(ConnectionRefusedError())
            return copy.deepcopy(rolling(1)['pods'][index]['snapshot'])
        with tempfile.TemporaryDirectory() as directory:
            inventory=Path(directory)/'inventory';inventory.write_text(json.dumps(rolling(1)['inventory']))
            with patch.object(worker,'kubectl',side_effect=kube),patch.object(worker,'get',side_effect=get),patch.object(worker,'query_metrics',return_value=rolling(1)['metrics']),patch.object(worker,'utc',return_value=1):
                return worker.capture_frame(inventory,native_context=context())

    def test_owned_pending_and_transport_refusal_are_startup_only(self):
        from test_pgcat_native_evidence import starting_row
        for case in ('pending','waiting','refused'):
            with self.subTest(case=case):
                value=self.observe(case)
                self.assertEqual(value['expectedReplicas'],2)
                self.assertEqual(len(value['pods']),2)
                self.assertEqual(value.get('startingPods'),[starting_row(i,None if case in ('pending','waiting') else 'new-container-'+str(i)) for i in range(2)])

    def test_malformed_ready_foreign_wrong_image_and_missing_initial_are_not_startup(self):
        from test_pgcat_native_evidence import context,rolling
        from pgcat_native_evidence import NativeCohort
        for case in ('malformed','ready','foreign','image','traffic'):
            with self.subTest(case=case):
                cohort=NativeCohort(context());cohort.observe(rolling(0));cohort.observe(self.observe(case))
                self.assertTrue(any(cohort.snapshot()['flags'].values()))
