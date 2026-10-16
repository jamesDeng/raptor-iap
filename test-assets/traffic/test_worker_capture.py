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
            if case=='duplicate value' and q.startswith('pgcat_pools_cl_idle'):rows.append(rows[0])
            if case=='missing timestamp' and q.startswith('timestamp(pgcat_pools_cl_idle'):rows.pop()
            if case=='missing triplet' and 'pgcat_pools_cl_idle' in q:rows.pop()
            if case=='wrong identity' and 'pgcat_pools' in q:
                for row in rows:row['metric']['pool']='other'
            if case in ('NaN','inf','0.5','-1') and q.startswith('pgcat_pools_cl_active'):rows[0]['value'][1]=case
            if case=='nonfinite timestamp' and q.startswith('timestamp(pgcat_pools'):rows[0]['value'][1]='NaN'
        for case in ('duplicate up','missing up','foreign up','duplicate value','missing timestamp','missing triplet','wrong identity','NaN','inf','0.5','-1','nonfinite timestamp'):
            with self.subTest(case=case),self.assertRaises(ValueError):self.query(lambda q,rows:alter(q,rows,case))
    def test_raw_allowlist(self):
        m=self.query(lambda q,rows:None)
        self.assertTrue(all(set(r)=={'node','pool','user','kind','value','sourceAt'} for r in m['raw']))


class FrameMetricsFailure(unittest.TestCase):
    def test_invalid_metrics_leave_client_evidence_available(self):
        import tempfile, json
        from pathlib import Path
        pod={'metadata':{'uid':'old'},'status':{'podIP':'10.0.0.1','phase':'Running'}}
        final={'final':True,'sample':{'processId':'old-process'}}
        def kube(*args):
            if args[1]=='pods':return {'items':[pod]}
            if args[1]=='deployment':return {'spec':{'replicas':1}}
            return {'spec':{'clusterIP':'10.0.0.2'}}
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'inventory.json';path.write_text(json.dumps({'members':['a','b']}))
            for error in (ValueError('invalid series'),KeyError('label'),TypeError('malformed result')):
                with self.subTest(error=type(error).__name__),patch.object(worker,'kubectl',side_effect=kube),patch.object(worker,'query_metrics',side_effect=error):
                    frame=worker.capture_frame(str(path),{'old':final})
                self.assertEqual(frame['pods'],[{'uid':'old','snapshot':final}])
                self.assertFalse(frame['metrics']['available'])
                self.assertEqual(frame['metrics']['raw'],[])


class PodLifecycle(unittest.TestCase):
    def test_final_snapshot_cached_before_pod_endpoint_disappears(self):
        cache={};pod={'metadata':{'uid':'old'},'status':{'podIP':'10.0.0.1'}}
        snapshot={'sample':{'processId':'old-process'},'final':True,'connected':0}
        with patch.object(worker,'get',return_value=snapshot) as read:
            self.assertEqual(worker.read_pod(pod,cache)['snapshot'],snapshot)
            read.assert_called_once()
        with patch.object(worker,'get',side_effect=OSError('gone')) as read:
            self.assertEqual(worker.read_pod(pod,cache)['snapshot'],snapshot)
            read.assert_not_called()
    def test_nonfinal_endpoint_loss_still_refuses(self):
        cache={};pod={'metadata':{'uid':'old'},'status':{'podIP':'10.0.0.1'}}
        with patch.object(worker,'get',return_value={'final':False}):worker.read_pod(pod,cache)
        self.assertEqual(cache,{})
        with patch.object(worker,'get',side_effect=OSError('gone')):
            with self.assertRaises(OSError):worker.read_pod(pod,cache)
    def test_cached_final_does_not_cross_pod_uids(self):
        cache={'old':{'final':True}};pod={'metadata':{'uid':'new'},'status':{'podIP':'10.0.0.1'}}
        with patch.object(worker,'get',side_effect=OSError('unready')):
            with self.assertRaises(OSError):worker.read_pod(pod,cache)

if __name__=='__main__':unittest.main()
