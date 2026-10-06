import pathlib,unittest,yaml
ROOT=pathlib.Path(__file__).resolve().parents[2]
class Publication(unittest.TestCase):
 def test_publish_only_trusted_main_after_packaging(self):
  w=yaml.load((ROOT/'.github/workflows/platform-images.yml').read_text(),Loader=yaml.BaseLoader)
  self.assertIn('publish',w['jobs'])
  j=w['jobs']['publish'];self.assertIn("github.event_name != 'pull_request'",j['if']);self.assertIn("github.ref == 'refs/heads/main'",j['if'])
  self.assertEqual(j['needs'],['build-check','release-contracts','postgres14-acceptance']);self.assertEqual(j['permissions']['packages'],'write')
  text=yaml.dump(j);self.assertIn('GITHUB_TOKEN',text);self.assertNotIn('GHCR_TOKEN',text);self.assertIn('linux/amd64',text)
  build=w['jobs']['build-check'];self.assertNotIn('write',yaml.dump(build.get('permissions',{})))
  self.assertEqual(w['permissions'],{'contents':'read'})

import os,subprocess,shutil
class Packaging(unittest.TestCase):
 def test_private_restricted_real_platform_render(self):
  chart=ROOT/'helm-chart/raptor-platform';self.assertTrue(chart.exists(),'platform chart is missing')
  helm=os.environ.get('HELM',shutil.which('helm'))
  if not helm:self.skipTest('Helm tool required; dedicated release CI runs rendering')
  args=[helm,'template','raptor-platform',str(chart),'--namespace','raptor-system','--set','repository.revision='+'a'*40]
  for image in ['raptor','gateway']:args+=['--set',f'images.{image}=ghcr.io/jamesdeng/raptor-iap-{image}@sha256:'+'b'*64]
  for service in ['raptor-frontend','raptor-backend','raptor-open-api','raptor-admin','agent-gateway']:args+=['--set',f'appCodes.{service}=TEST-{service}']
  p=subprocess.run(args,capture_output=True,text=True);self.assertEqual(p.returncode,0,p.stderr)
  docs=list(yaml.safe_load_all(p.stdout));deploys=[d for d in docs if d and d.get('kind')=='Deployment'];services=[d for d in docs if d and d.get('kind')=='Service']
  self.assertEqual(len(deploys),5);self.assertEqual(len(services),5)
  for d in deploys:
   self.assertEqual(d['spec']['replicas'],1);pod=d['spec']['template']['spec'];self.assertFalse(pod['automountServiceAccountToken'])
   c=pod['containers'][0];self.assertRegex(c['image'],r'@sha256:[a-f0-9]{64}$');self.assertIn('requests',c['resources']);self.assertIn('limits',c['resources'])
   self.assertTrue(pod['securityContext']['runAsNonRoot']);self.assertFalse(c['securityContext']['allowPrivilegeEscalation']);self.assertIn('readinessProbe',c)
   self.assertIn('raptor.appcode',d['spec']['template']['metadata']['labels']);self.assertEqual(d['spec']['template']['metadata']['labels']['raptor.env'],'rdev.ali')
   env={v['name']:v.get('value') for v in c.get('env',[])};self.assertNotEqual(env.get('RAPTOR_SIMULATION'),'true');self.assertNotEqual(env.get('GATEWAY_SIMULATION'),'true')
   for k,v in env.items():
    if k.endswith('_ADDR'):self.assertTrue(v.startswith('0.0.0.0:'))
  for d in services:self.assertEqual(d['spec']['type'],'ClusterIP')
  self.assertFalse(any(d and d.get('kind') in ['Ingress','Secret','PersistentVolumeClaim'] for d in docs))
  for d in deploys:
   if d['metadata']['name'] in ['raptor-backend','agent-gateway']:
    pod=d['spec']['template']['spec'];refs=[v['secretRef']['name'] for v in pod['containers'][0]['envFrom']];self.assertIn('raptor-runtime' if d['metadata']['name']=='raptor-backend' else 'gateway-runtime',refs)
    self.assertNotIn('platform-migrator',str(pod))
 def test_unresolved_release_cannot_render(self):
  helm=os.environ.get('HELM',shutil.which('helm'))
  if not helm:self.skipTest('Helm required')
  p=subprocess.run([helm,'template','platform',str(ROOT/'helm-chart/raptor-platform')],capture_output=True,text=True)
  self.assertNotEqual(p.returncode,0)
 def test_argo_scope_no_automatic_pruning(self):
  p=ROOT/'infra-kubernetes/environments/rdev.ali/application.yaml';self.assertTrue(p.exists(),'Argo Application missing')
  app=yaml.safe_load(p.read_text());self.assertFalse(app['spec']['syncPolicy']['automated']['prune']);self.assertTrue(app['spec']['syncPolicy']['automated']['selfHeal'])
  self.assertEqual(app['spec']['source']['repoURL'],'https://github.com/jamesDeng/raptor-iap.git');self.assertEqual(app['spec']['source']['path'],'helm-chart/raptor-platform')
  project=yaml.safe_load((p.parent/'project.yaml').read_text());self.assertEqual(project['spec']['destinations'],[{'server':'https://kubernetes.default.svc','namespace':'raptor-system'}]);self.assertNotIn('*',project['spec']['sourceRepos'])

class ReviewChecksum(unittest.TestCase):
 def test_download_name_matches_upstream_checksum(self):
  s=(ROOT/'.github/workflows/platform-images.yml').read_text()
  self.assertIn('-o "$RUNNER_TEMP/helm-v4.3.0-linux-amd64.tar.gz"',s)
  self.assertNotIn('"$RUNNER_TEMP/helm.tar.gz"',s)
