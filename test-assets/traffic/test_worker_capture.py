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

if __name__=='__main__':unittest.main()
