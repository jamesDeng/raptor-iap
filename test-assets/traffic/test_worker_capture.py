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
