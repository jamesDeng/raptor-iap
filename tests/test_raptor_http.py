"""Exercise real loopback HTTP, SQLite and local request boundaries."""
import http.client
import json
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch
import uuid
from test_raptor_store import APP
from tools.task_store.store import Store
from tools.task_store.models import TaskBinding, OutcomeReport


class HttpTests(unittest.TestCase):
    def setUp(self):
        from components.raptor.server import make_server
        self.make_server=make_server
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name).resolve();self.store=Store(self.root/'store')
        self.start()
        self.addCleanup(self.stop)
        self.token=self.request('GET','/api/session')[1]['data']['token']

    def start(self):
        self.server=self.make_server(self.store,self.root/'web-state',port=0)
        self.port=self.server.server_address[1]
        self.origin=f'http://127.0.0.1:{self.port}'
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True);self.thread.start()

    def stop(self):
        self.server.shutdown();self.server.server_close();self.thread.join(3)

    def request(self,method,path,value=None,headers=None,raw=None):
        conn=http.client.HTTPConnection('127.0.0.1',self.port,timeout=5)
        body=json.dumps(value).encode() if value is not None else raw
        base={}
        if method!='GET':base={'Origin':self.origin,'X-Raptor-Token':self.token,'Content-Type':'application/json'}
        base.update(headers or {})
        conn.request(method,path,body=body,headers=base)
        response=conn.getresponse();payload=response.read();status=response.status
        response_headers=dict(response.getheaders());conn.close()
        return status,json.loads(payload),response_headers

    def app_task(self):
        status,response,_=self.request('POST','/api/apps',APP);self.assertEqual(status,201)
        app=response['data']
        value=dict(app_id=app['app_id'],question='What is missing? <script>alert(1)</script>',model='gpt-5.6-luna',idempotency_key=str(uuid.uuid4()))
        status,response,_=self.request('POST','/api/tasks',value);self.assertEqual(status,201)
        return app,response['data'],value

    def test_registration_edit_submit_detail_and_duplicate(self):
        with patch('tools.sandbox_lifecycle.cloud.Cloud.from_operator_profile',side_effect=AssertionError('HTTP must not authenticate cloud')):
            app,task,value=self.app_task()
            status,result,_=self.request('POST','/api/tasks',value)
            self.assertEqual(result['data']['task_id'],task['task_id'])
            self.assertEqual(self.request('POST','/api/tasks',dict(value,question='different'))[0],409)
            status,result,_=self.request('PUT','/api/apps/'+app['app_id'],dict(expected_version=1,app=dict(APP,name='changed')))
            self.assertEqual(status,200);self.assertEqual(result['data']['version'],2)
            self.assertEqual(self.request('PUT','/api/apps/'+app['app_id'],dict(expected_version=1,app=APP))[0],409)
            status,result,headers=self.request('GET','/api/tasks/'+task['task_id'])
            self.assertEqual(result['data']['snapshot']['name'],'db-client')
            self.assertIn('<script>',result['data']['question'])
            self.assertEqual(headers['Cache-Control'],'no-store')
            self.assertEqual(headers['X-Content-Type-Options'],'nosniff')
            listed=self.request('GET','/api/tasks?app_id='+app['app_id'])[1]['data']
            self.assertEqual(len(listed),1);self.assertNotIn('question',listed[0]);self.assertNotIn('answer',listed[0])

    def test_restart_retains_tasks_and_token(self):
        _,task,_=self.app_task();token=self.token
        self.stop();self.start()
        self.assertEqual(self.request('GET','/api/session')[1]['data']['token'],token)
        self.assertEqual(self.request('GET','/api/tasks/'+task['task_id'])[1]['data']['question'],task['question'])

    def test_host_origin_and_token_boundary(self):
        for headers in ({'Host':'attacker.example'}, {'Host':'localhost:'+str(self.port)},
                        {'Origin':'https://attacker.example'}, {'Sec-Fetch-Site':'cross-site'}):
            self.assertEqual(self.request('GET','/api/session',headers=headers)[0],403)
        for headers in ({'Origin':'https://attacker.example'}, {'Origin':'null'}, {'Origin':''},
                        {'X-Raptor-Token':'wrong'}, {'X-Raptor-Token':''}, {'Host':'attacker.example'}):
            self.assertEqual(self.request('POST','/api/apps',APP,headers=headers)[0],403)

    def test_missing_host_and_origin_and_malformed_transport(self):
        for method,headers in (('GET',{}),('POST',{'Host':f'127.0.0.1:{self.port}','X-Raptor-Token':self.token,'Content-Type':'application/json'})):
            conn=http.client.HTTPConnection('127.0.0.1',self.port,timeout=5)
            conn.putrequest(method,'/api/session' if method=='GET' else '/api/apps',skip_host=True)
            for k,v in headers.items():conn.putheader(k,v)
            conn.putheader('Content-Length','2');conn.endheaders(b'{}')
            response=conn.getresponse();response.read();self.assertEqual(response.status,403);conn.close()
        self.assertEqual(self.request('GET','/api/tasks?app_id=')[0],400)
        self.assertEqual(self.request('POST','/api/apps',APP,headers={'X-Raptor-Token':'é'})[0],403)
        self.assertEqual(self.request('POST','/api/apps',raw=b'{}',headers={'Content-Length':'-1'})[0],400)
        self.assertEqual(self.request('POST','/api/apps',raw=b'{}',headers={'Transfer-Encoding':'chunked'})[0],400)

    def test_body_limits_unknown_fields_and_safe_errors(self):
        for value in (dict(APP,extra='synthetic-secret'), dict(APP,owner='sk-synthetic-secret')):
            self.assertEqual(self.request('POST','/api/apps',value)[0],400)
        for raw in (b'{"name":"a","name":"b"}', b'\xff', b'[]', b'null'):
            self.assertEqual(self.request('POST','/api/apps',raw=raw)[0],400)
        self.assertEqual(self.request('POST','/api/apps',raw=b'x'*65537)[0],413)
        self.assertEqual(self.request('POST','/api/apps',APP,headers={'Content-Type':'text/plain'})[0],400)
        with patch.object(self.store,'create_app',side_effect=OSError('synthetic-private-secret')):
            status,response,_=self.request('POST','/api/apps',APP)
        self.assertEqual(status,500);self.assertNotIn('synthetic-private-secret',json.dumps(response))

    def test_cancel_retry_and_recover_routes(self):
        _,task,_=self.app_task();attempt=task['attempts'][0]
        path='/api/tasks/'+task['task_id']
        self.assertEqual(self.request('POST',path+'/cancel',dict(attempt_id=attempt['attempt_id']))[0],200)
        _,task,_=self.app_task();attempt=self.store.claim_next()
        binding=TaskBinding(task['task_id'],attempt['attempt_id'],task['snapshot_sha256'])
        self.store.finish(binding,OutcomeReport('failed','saved','confirmed','failed','ModelFailed'),None)
        path='/api/tasks/'+task['task_id'];key=str(uuid.uuid4())
        first=self.request('POST',path+'/retry',dict(idempotency_key=key))[1]['data']
        second=self.request('POST',path+'/retry',dict(idempotency_key=key))[1]['data']
        self.assertEqual(first['attempt_id'],second['attempt_id'])
        claimed=self.store.claim_next();binding=TaskBinding(task['task_id'],claimed['attempt_id'],task['snapshot_sha256'])
        self.store.block(binding,'RunnerOutcomeUnknown')
        status,_,_=self.request('POST',path+'/recover',dict(attempt_id=binding.attempt_id,idempotency_key=str(uuid.uuid4())))
        self.assertEqual(status,202);self.assertEqual(self.store.claim_recovery(),binding)

    def test_task_routes_cannot_substitute_artifact_paths(self):
        _,task,_=self.app_task()
        for path in ('/api/tasks/../auth.json','/api/tasks/'+str(uuid.uuid4()),'/.raptor-local/tasks.sqlite3'):
            self.assertIn(self.request('GET',path)[0],(400,404))
        value=dict(attempt_id=task['attempts'][0]['attempt_id'],answer_ref='../auth.json')
        self.assertEqual(self.request('POST','/api/tasks/'+task['task_id']+'/cancel',value)[0],400)
