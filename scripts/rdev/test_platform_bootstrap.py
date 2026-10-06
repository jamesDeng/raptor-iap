import unittest,importlib.util,pathlib,tempfile
P=pathlib.Path(__file__).with_name('platform_bootstrap.py')
class Bootstrap(unittest.TestCase):
 def module(self):
  self.assertTrue(P.exists(),'bootstrap implementation missing')
  spec=importlib.util.spec_from_file_location('platform_bootstrap',P);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
 def config(self):return {'account':'1360282071200743','region':'ap-southeast-1','cluster':'c92787e953503492ea141a744c81498f1','database':'pgm-t4n389ymwx7otro7','host':'private.pgsql.singapore.rds.aliyuncs.com','source_sha':'a'*40,'raptor_image':'ghcr.io/jamesdeng/raptor-iap-raptor@sha256:'+'b'*64,'gateway_image':'ghcr.io/jamesdeng/raptor-iap-gateway@sha256:'+'c'*64,'postgres_image':'postgres@sha256:'+'d'*64}
 def test_scope_and_release_reject_other_targets(self):
  m=self.module();c=self.config();m.validate(c)
  for key,value in [('account','999'),('cluster','other'),('database','other'),('region','cn-hangzhou'),('source_sha','main'),('raptor_image','image:latest'),('postgres_image','postgres:14'),('host','host; touch /tmp/pwn')]:
   bad=dict(c);bad[key]=value
   with self.assertRaises(ValueError):m.validate(bad)
 def test_private_kit_refuses_overwrite_and_has_distinct_roles(self):
  m=self.module()
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d)/'kit';m.write_kit(root,self.config())
   self.assertEqual(root.stat().st_mode&0o777,0o700)
   for p in root.iterdir():self.assertEqual(p.stat().st_mode&0o777,0o600)
   sql=(root/'roles.sql').read_text();self.assertIn('raptor_owner NOLOGIN',sql);self.assertIn('gateway_owner NOLOGIN',sql);self.assertIn('NOINHERIT',sql);self.assertIn('REVOKE ALL ON SCHEMA public FROM PUBLIC',sql)
   self.assertNotIn('GRANT raptor_owner TO raptor_runtime',sql)
   with self.assertRaises(ValueError):m.write_kit(root,self.config())
 def test_preview_has_no_cloud_mutations(self):
  m=self.module();c=self.config();actions=m.preview(c)
  self.assertEqual(actions['execute'],False);self.assertNotIn('password',str(actions).lower());self.assertEqual(actions['new_compute_resources'],0)


class TLSPlan(unittest.TestCase):
 def test_only_owned_database_tls_update_is_allowed(self):
  m=Bootstrap().module()
  db={'actions':['update'],'before':{'id':m.OWNED['database'],'ssl_action':'Close','ssl_status':'OFF'},'after':{'id':m.OWNED['database'],'ssl_action':'Open','ssl_status':'ON'},'after_unknown':{}}
  plan={'resource_changes':[{'address':'module.foundation.alicloud_db_instance.platform','change':db}]}
  self.assertIsNone(m.validate_tls_plan(plan))
  import copy
  for key,value in [('id','other'),('instance_type','larger'),('ssl_action','Close')]:
   bad=copy.deepcopy(plan);bad['resource_changes'][0]['change']['after'][key]=value
   with self.assertRaises(ValueError):m.validate_tls_plan(bad)
  bad=copy.deepcopy(plan);bad['resource_changes'].append({'address':'other','change':{'actions':['create'],'after':{}}})
  with self.assertRaises(ValueError):m.validate_tls_plan(bad)
 def test_no_unresolved_scope_changes(self):
  m=Bootstrap().module();p={'resource_changes':[{'address':'module.foundation.alicloud_db_instance.platform','change':{'actions':['update'],'before':{'id':m.OWNED['database']},'after':{'id':m.OWNED['database'],'ssl_action':'Open'},'after_unknown':{'id':True}}}]}
  with self.assertRaises(ValueError):m.validate_tls_plan(p)

class AccountPrepare(unittest.TestCase):
 class API:
  def __init__(self):self.writes=[];self.tls='on';self.account='1360282071200743'
  def call(self,service,op,params=None):
   if op=='GetCallerIdentity':return {'AccountId':self.account}
   if op=='DescribeDBInstanceAttribute':return {'Items':{'DBInstanceAttribute':[{'DBInstanceId':'pgm-t4n389ymwx7otro7','RegionId':'ap-southeast-1','VpcId':'vpc-t4n4fi6r1a7bi6n93ftq3','Engine':'PostgreSQL','EngineVersion':'14.0','DBInstanceStatus':'Running'}]}}
   if op=='DescribeDBInstanceSSL':return {'SSLEnabled':self.tls}
   if op=='DescribeTags':return {'Items':{'TagInfos':[{'TagKey':k,'TagValue':v,'DBInstanceIds':{'DBInstanceIds':['pgm-t4n389ymwx7otro7']}} for k,v in {'Project':'raptor-iap','Environment':'rdev.ali','Owner':'rdev-foundation'}.items()]}}
   if op=='DescribeDBInstanceNetInfo':return {'DBInstanceNetInfos':{'DBInstanceNetInfo':[{'IPType':'Private','VPCId':'vpc-t4n4fi6r1a7bi6n93ftq3','ConnectionString':'private.pgsql.singapore.rds.aliyuncs.com'}]}}
   if service=='cs':return {'cluster_id':'c92787e953503492ea141a744c81498f1','state':'running','vpc_id':'vpc-t4n4fi6r1a7bi6n93ftq3'}
   if op=='DescribeAccounts':return {'Accounts':{'DBInstanceAccount':[]}}
   if op=='CreateAccount':self.writes.append(params);return {'RequestId':'synthetic'}
   raise AssertionError(op)
 def test_preview_and_tls_failure_never_create_account(self):
  m=Bootstrap().module();api=self.API();c=Bootstrap().config()
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d)/'kit';m.write_kit(root,c);m.prepare_account(api,c,root,False);self.assertEqual(api.writes,[])
   api.tls='off'
   with self.assertRaises(ValueError):m.prepare_account(api,c,root,True)
   self.assertEqual(api.writes,[])
 def test_account_creation_bound_and_not_replayed(self):
  m=Bootstrap().module();api=self.API();c=Bootstrap().config()
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d)/'kit';m.write_kit(root,c);m.prepare_account(api,c,root,True);self.assertEqual(len(api.writes),1);self.assertEqual(api.writes[0]['DBInstanceId'],c['database']);self.assertEqual(api.writes[0]['AccountName'],'raptor_bootstrap')
   with self.assertRaises(ValueError):m.prepare_account(api,c,root,True)
   self.assertEqual(len(api.writes),1)
 def test_wrong_caller_refuses(self):
  m=Bootstrap().module();api=self.API();api.account='other'
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d)/'kit';c=Bootstrap().config();m.write_kit(root,c)
   with self.assertRaises(ValueError):m.prepare_account(api,c,root,True)
   self.assertEqual(api.writes,[])



class ReviewRegressions(unittest.TestCase):
 def test_jobs_preserve_reconciliation_evidence(self):
  m=Bootstrap().module();j=m._job('check','image',['true'],'secret')
  self.assertNotIn('ttlSecondsAfterFinished',j['spec'])
 def test_create_timeout_is_sanitized_and_intent_retained(self):
  import subprocess
  m=Bootstrap().module();api=AccountPrepare.API();original=api.call
  def call(service,action,parameters=None):
   if action=='CreateAccount':raise subprocess.TimeoutExpired(['aliyun','--AccountPassword','DUMMY_SECRET'],60)
   return original(service,action,parameters)
  api.call=call
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d)/'kit';c=Bootstrap().config();m.write_kit(root,c)
   with self.assertRaises(ValueError) as caught:m.prepare_account(api,c,root,True)
   self.assertNotIn('DUMMY_SECRET',str(caught.exception));self.assertTrue((root/'account-create-receipt.json').exists())



class PrivateProxyGuard(unittest.TestCase):
 def test_guard_accepts_only_exact_owned_worker(self):
  import subprocess,os
  script=pathlib.Path(__file__).resolve().parent/'platform-bootstrap.sh'
  with tempfile.TemporaryDirectory() as d:
   mock=pathlib.Path(d)/'kubectl';mock.write_text('#!/bin/sh\nif [ "$1 $2" = "get nodes" ]; then printf "%s" "$MOCK_WORKER"; fi\n');mock.chmod(0o700)
   for identity,expected in [('ap-southeast-1.i-t4nj1bitmcz7cuoch82h',0),('other',1),('ap-southeast-1.i-t4nj1bitmcz7cuoch82h another',1)]:
    e=dict(os.environ,PATH=d+':'+os.environ['PATH'],MOCK_WORKER=identity)
    r=subprocess.run(['bash',str(script),'preview'],env=e,capture_output=True,text=True);self.assertEqual(r.returncode,expected)
if __name__=='__main__':unittest.main()
