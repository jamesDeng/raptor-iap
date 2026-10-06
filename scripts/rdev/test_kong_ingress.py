import os,pathlib,subprocess,unittest,yaml
ROOT=pathlib.Path(__file__).resolve().parents[2]
HELM=os.environ.get('HELM','helm')
CHART=ROOT/'helm-chart/kong-ingress'
class KongIngress(unittest.TestCase):
 def render(self,owned=True):
  args=[HELM,'template','kong',str(CHART),'-n','kong-system','--api-versions','networking.k8s.io/v1/IngressClass']
  if owned: args += ['--set','ingress.gateway.proxy.annotations.service\\.beta\\.kubernetes\\.io/alibaba-cloud-loadbalancer-id=lb-owned-fixture']
  return subprocess.run(args,text=True,capture_output=True)
 def test_owned_https_only_proxy_and_private_management(self):
  self.assertTrue(CHART.exists(),'Kong packaging is missing')
  p=self.render(); self.assertEqual(p.returncode,0,p.stderr)
  docs=[d for d in yaml.safe_load_all(p.stdout) if d]
  lbs=[d for d in docs if d['kind']=='Service' and d['spec'].get('type','ClusterIP')=='LoadBalancer']
  self.assertEqual(len(lbs),1)
  self.assertEqual([p['port'] for p in lbs[0]['spec']['ports']],[443])
  self.assertEqual(lbs[0]['metadata']['annotations']['service.beta.kubernetes.io/alibaba-cloud-loadbalancer-id'],'lb-owned-fixture')
  for d in docs:
   if d['kind']=='Service' and 'admin' in d['metadata']['name']:self.assertEqual(d['spec'].get('type','ClusterIP'),'ClusterIP')
  ds=[d for d in docs if d['kind']=='Deployment'];self.assertEqual(len(ds),2)
  for d in ds:
   self.assertEqual(d['spec']['replicas'],1)
   for c in d['spec']['template']['spec']['containers']:
    self.assertIn('requests',c['resources']);self.assertIn('limits',c['resources'])
    if c['name']=='proxy': self.assertIn({'name':'KONG_DATABASE','value':'off'},c['env'])
  self.assertFalse(any(d['kind'] in ('Secret','PersistentVolumeClaim') for d in docs))
  classes=[d for d in docs if d['kind']=='IngressClass'];self.assertEqual(len(classes),1)
  self.assertEqual(classes[0]['metadata']['name'],'raptor-kong')
  self.assertEqual(classes[0]['metadata']['annotations']['ingressclass.kubernetes.io/is-default-class'],'false')
  for d in docs:
   if d['kind']=='ClusterRole':self.assertFalse(any('secrets' in r['resources'] or '*' in r['resources'] for r in d['rules']))
   if d['kind']=='Role':self.assertIn(d['metadata']['namespace'],['kong-system','raptor-system'])
 def test_missing_owned_id_fails_before_creation(self):
  self.assertTrue(CHART.exists(),'Kong packaging is missing')
  p=self.render(False);self.assertNotEqual(p.returncode,0);self.assertIn('owned load balancer',p.stderr)
class Routes(unittest.TestCase):
 def test_public_routes_are_https_and_exclude_management(self):
  p=ROOT/'infra-kubernetes/environments/rdev.ali/kong/routes.yaml'
  self.assertTrue(p.exists(),'Kong routes missing')
  docs=[d for d in yaml.safe_load_all(p.read_text()) if d]
  routes=[d for d in docs if d['kind']=='Ingress' and d['metadata']['name'].startswith('raptor-public-')]
  self.assertEqual(len(routes),2)
  expected={'raptor.rdev.raptor-iap.top':'raptor-frontend','api.rdev.raptor-iap.top':'raptor-open-api'}
  for r in routes:
   self.assertEqual(r['spec']['ingressClassName'],'raptor-kong')
   self.assertEqual(r['metadata']['annotations']['konghq.com/protocols'],'https')
   self.assertEqual(r['spec']['tls'][0]['secretName'],'raptor-public-tls')
   rule=r['spec']['rules'][0];host=rule['host'];self.assertIn(host,expected)
   self.assertEqual(r['spec']['tls'][0]['hosts'],[host])
   for path in rule['http']['paths']:self.assertEqual(path['backend']['service']['name'],expected[host])
  blocked=[d for d in docs if d['kind']=='Ingress' and d['metadata']['name'].startswith('raptor-private-')]
  self.assertEqual(len(blocked),2)
  for r in blocked:self.assertEqual(r['metadata']['annotations']['konghq.com/plugins'],'private-management-only')
  plugin=next(d for d in docs if d['kind']=='KongPlugin');self.assertEqual(plugin['plugin'],'request-termination');self.assertEqual(plugin['config']['status_code'],403)
  api=next(r for r in routes if r['spec']['rules'][0]['host'].startswith('api.'))
  self.assertEqual([p['path'] for p in api['spec']['rules'][0]['http']['paths']],['/mcp','/v1'])
  project=yaml.safe_load((p.parent/'project.yaml').read_text())
  self.assertEqual({d['namespace'] for d in project['spec']['destinations']},{'kong-system','raptor-system'})

if __name__=='__main__':unittest.main()
