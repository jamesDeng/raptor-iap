"""Render real dedicated charts; assertions inspect emitted Kubernetes objects."""
import json, os, pathlib, subprocess, unittest
import yaml
ROOT=pathlib.Path(__file__).resolve().parents[1]
HELM=os.environ.get('HELM_BIN',str(ROOT/'.raptor-local/bin/helm'))
DIGEST='a'*64
class Charts(unittest.TestCase):
 def render(self,name,settings,ok=True):
  args=[HELM,'template','fixture',str(ROOT/'helm-chart'/name),'--namespace','raptor-test']
  for k,v in settings.items():args += ['--set',f'{k}={v}']
  p=subprocess.run(args,capture_output=True,text=True)
  if not ok:self.assertNotEqual(p.returncode,0);return []
  self.assertEqual(p.returncode,0,p.stderr);return list(yaml.safe_load_all(p.stdout))
 def client(self):return {'appCode':'app-fixture','environment':'fixture.env','image':f'example.invalid/client@sha256:{DIGEST}','dsnSecretName':'fixture-secret'}
 def metrics(self):return {'image':f'example.invalid/prometheus@sha256:{DIGEST}','environment':'fixture.env','proxyCode':'proxy-fixture','targetDbCode':'db-fixture','inventoryConfigMapName':'reviewed-ess-inventory','inventoryRevision':'b'*40}
 def test_client_rolling_restart_and_private_secret(self):
  docs=self.render('infra-test-client',self.client());d=next(x for x in docs if x['kind']=='Deployment');spec=d['spec'];pod=spec['template']['spec'];self.assertEqual(spec['replicas'],2);self.assertEqual(spec['strategy']['rollingUpdate'],{'maxUnavailable':0,'maxSurge':1});self.assertEqual(d['metadata']['labels']['raptor.appcode'],'app-fixture');self.assertEqual(spec['template']['metadata']['labels']['raptor.env'],'fixture.env');self.assertFalse(pod['automountServiceAccountToken']);self.assertGreaterEqual(pod['terminationGracePeriodSeconds'],10);self.assertEqual(pod['volumes'][0]['secret']['secretName'],'fixture-secret');self.assertIn('readinessProbe',pod['containers'][0]);self.assertTrue(all(x['spec']['type']=='ClusterIP' for x in docs if x['kind']=='Service'));self.assertFalse(any(x['kind']=='Secret' for x in docs))
 def test_missing_inputs_and_mutable_images_refused(self):
  for key in self.client():
   vals=self.client();vals[key]='';self.render('infra-test-client',vals,False)
  vals=self.client();vals['image']='example.invalid/client:latest';self.render('infra-test-client',vals,False)
 def test_metrics_uses_external_authoritative_inventory(self):
  docs=self.render('infra-test-metrics',self.metrics());d=next(x for x in docs if x['kind']=='Deployment');pod=d['spec']['template']['spec'];self.assertFalse(pod['automountServiceAccountToken']);self.assertEqual(pod['volumes'][1]['configMap']['name'],'reviewed-ess-inventory');self.assertTrue(all(x['spec']['type']=='ClusterIP' for x in docs if x['kind']=='Service'));c=next(x for x in docs if x['kind']=='ConfigMap');cfg=yaml.safe_load(c['data']['prometheus.yml']);job=next(x for x in cfg['scrape_configs'] if x['job_name']=='pgcat');self.assertEqual(job['file_sd_configs'][0]['files'],['/inventory/targets.json']);self.assertFalse(job['honor_labels']);self.assertFalse(job['honor_timestamps']);self.assertEqual(cfg['global']['scrape_interval'],'5s')
 def test_metrics_missing_inventory_or_mutable_image_refused(self):
  for key in ['inventoryConfigMapName','inventoryRevision','proxyCode','targetDbCode']:
   vals=self.metrics();vals[key]='';self.render('infra-test-metrics',vals,False)
  vals=self.metrics();vals['image']='example.invalid/prometheus:latest';self.render('infra-test-metrics',vals,False)
 def test_environment_selector_is_literal(self):
  import re
  docs=self.render('infra-test-metrics',self.metrics());cfg=yaml.safe_load(next(x for x in docs if x['kind']=='ConfigMap')['data']['prometheus.yml']);rule=cfg['scrape_configs'][0]['relabel_configs'][0];self.assertIsNotNone(re.fullmatch(rule['regex'],'fixture.env'));self.assertIsNone(re.fullmatch(rule['regex'],'fixtureXenv'))

 def test_client_replica_discovery_is_headless(self):
  vals=self.client();vals['headless']=True
  docs=self.render('infra-test-client',vals)
  service=next(x for x in docs if x['kind']=='Service')
  self.assertEqual(service['spec'].get('clusterIP'),'None')
 def test_metrics_discovers_all_replica_addresses(self):
  vals=self.metrics();vals['clientDiscoveryNames[0]']='traffic-client.raptor-test.svc.cluster.local'
  docs=self.render('infra-test-metrics',vals)
  cfg=yaml.safe_load(next(x for x in docs if x['kind']=='ConfigMap')['data']['prometheus.yml'])
  job=next(x for x in cfg['scrape_configs'] if x['job_name']=='test-client')
  self.assertEqual(job['dns_sd_configs'][0]['type'],'A')
  self.assertEqual(job['dns_sd_configs'][0]['port'],9090)

 def test_metrics_reader_is_exact_namespaced_service_proxy(self):
  vals=self.metrics();vals['infraReaderUser']='300172700753867300'
  docs=self.render('infra-test-metrics',vals)
  role=next(x for x in docs if x['kind']=='Role')
  binding=next(x for x in docs if x['kind']=='RoleBinding')
  self.assertEqual(role['metadata']['namespace'],'raptor-test')
  self.assertEqual(role['rules'],[{'apiGroups':[''],'resources':['services/proxy'],'resourceNames':['http:fixture-metrics:http'],'verbs':['get']}])
  self.assertEqual(binding['roleRef']['name'],role['metadata']['name'])
  self.assertEqual(binding['subjects'],[{'kind':'User','name':'300172700753867300','apiGroup':'rbac.authorization.k8s.io'}])
  self.assertFalse(any(x['kind'].startswith('ClusterRole') for x in docs))
  vals['infraReaderUser']='system:admin';self.render('infra-test-metrics',vals,False)
 def test_metrics_reader_rbac_is_opt_in(self):
  docs=self.render('infra-test-metrics',self.metrics());self.assertFalse(any(x['kind'] in ['Role','RoleBinding'] for x in docs))
