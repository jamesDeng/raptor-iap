import pathlib,unittest,yaml,importlib.util
ROOT=pathlib.Path(__file__).resolve().parents[2]
class DiscoveryPackage(unittest.TestCase):
 def test_private_fc_package(self):
  path=ROOT/'components/infra-api/s.rdev.yaml'
  self.assertTrue(path.exists(),'persistent rdev FC package missing')
  d=yaml.safe_load(path.read_text());r=d['resources']['infra-api'];p=r['props']
  self.assertEqual(r['component'],'fc3@0.1.25')
  self.assertEqual(p['region'],'ap-southeast-1')
  self.assertEqual(p['functionName'],'raptor-rdev-infra-read')
  self.assertEqual(p['vpcConfig']['vpcId'],'vpc-t4n4fi6r1a7bi6n93ftq3')
  self.assertEqual(p['vpcConfig']['vSwitchIds'],['vsw-t4nop9qf6v46gw2sa8l7d'])
  self.assertEqual(p['vpcConfig']['securityGroupId'],'${env(INFRA_SECURITY_GROUP_ID)}')
  self.assertEqual(p['triggers'][0]['triggerConfig'],{'authType':'function','methods':['GET','POST']})
  self.assertEqual(p['concurrencyConfig']['reservedConcurrency'],2)
  self.assertNotIn('provisionConfig',p)
  self.assertEqual(p['environmentVariables']['INFRA_AUTH_HEADER'],'X-Infra-Authorization')
  for key in ['INFRA_USERNAME','INFRA_PASSWORD']:
   self.assertTrue(p['environmentVariables'][key].startswith('${env('))
 def test_persistent_tf_uses_ack_only(self):
  path=ROOT/'infra-terraform/environments/rdev-infra-api/main.tf'
  self.assertTrue(path.exists(),'persistent Terraform entrypoint missing')
  text=path.read_text();self.assertIn('ack_only',text);self.assertIn('true',text)
  self.assertIn('c92787e953503492ea141a744c81498f1',text)
  self.assertNotIn('alicloud_cs_managed_kubernetes',text)
  self.assertIn('var.gateway_instance_id',text)

class EnvironmentUpdate(unittest.TestCase):
 def module(self):
  path=ROOT/'scripts/rdev/infra_discovery_bootstrap.py'
  self.assertTrue(path.exists(),'guarded environment update missing')
  spec=importlib.util.spec_from_file_location('infra_discovery_bootstrap',path);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
 def before(self):
  return {'code':'rdev.ali','groupCode':'raptor','stage':'dev','config':{'cloud':'aliyun','cloudAccountId':'1360282071200743','region':'ap-southeast-1','ackClusterId':'c92787e953503492ea141a744c81498f1','terraformRepo':'keep','custom':{'keep':True}}}
 def after(self):
  import copy
  d=copy.deepcopy(self.before());d['config'].update(clusterId=d['config']['ackClusterId'],infraApiUrl='https://infra-api.raptor-iap.top');return d
 def test_preserve_environment_and_reject_foreign_or_insecure_changes(self):
  import copy
  m=self.module();self.assertIsNone(m.validate_environment_update(self.before(),self.after()))
  for key,value in [('clusterId','other'),('infraApiUrl','http://infra-api.raptor-iap.top'),('infraApiUrl','https://foreign.example'),('terraformRepo','changed'),('cloudAccountId','other'),('infraApiUrl','https://infra-api.raptor-iap.top/?password=secret')]:
   d=self.after();d['config'][key]=value
   with self.assertRaises(ValueError):m.validate_environment_update(self.before(),d)
  for key,value in [('code','other'),('groupCode','other'),('stage','prod')]:
   d=self.after();d[key]=value
   with self.assertRaises(ValueError):m.validate_environment_update(self.before(),d)
  d=self.after();del d['config']['custom']
  with self.assertRaises(ValueError):m.validate_environment_update(self.before(),d)

class BackendSecret(unittest.TestCase):
 def test_infra_secret_is_backend_only(self):
  import subprocess,os,shutil
  helm=os.environ.get('HELM',shutil.which('helm'))
  if not helm:self.skipTest('Helm unavailable; release CI requires rendering tools')
  r=subprocess.run([helm,'template','platform',str(ROOT/'helm-chart/raptor-platform'),'-f',str(ROOT/'infra-kubernetes/environments/rdev.ali/values.yaml'),'--set','infraReader.secretName=infra-reader-auth'],text=True,capture_output=True)
  self.assertEqual(r.returncode,0,r.stderr)
  docs=[d for d in yaml.safe_load_all(r.stdout) if d and d['kind']=='Deployment']
  for d in docs:
   c=d['spec']['template']['spec']['containers'][0];refs=[x['secretRef']['name'] for x in c.get('envFrom',[])]
   self.assertNotIn('infra-reader-auth',refs,'reader Secret must not overwrite unrelated environment variables')
   keys={x['name']:x.get('valueFrom',{}).get('secretKeyRef',{}) for x in c.get('env',[]) if x['name'].startswith('INFRA_') and x['name'] not in {'INFRA_AUTH_HEADER','INFRA_AGENT_MCP_URL'}}
   self.assertEqual(set(keys),{'INFRA_USERNAME','INFRA_PASSWORD'} if d['metadata']['name']=='raptor-backend' else set())
   for key,ref in keys.items():self.assertEqual(ref,{'name':'infra-reader-auth','key':key})
   if d['metadata']['name']=='raptor-backend':
    self.assertIn('raptor-runtime',refs)
    if any(x['name']=='INFRA_AGENT_MCP_URL' for x in c['env']):
     self.assertEqual(next(x for x in c['env'] if x['name']=='INFRA_AGENT_MCP_URL'),{'name':'INFRA_AGENT_MCP_URL','value':'https://infra-api.raptor-iap.top/mcp'})
    self.assertEqual(next(x['value'] for x in c['env'] if x['name']=='INFRA_AUTH_HEADER'),'X-Infra-Authorization')
  self.assertNotIn('kind: Secret',r.stdout)
