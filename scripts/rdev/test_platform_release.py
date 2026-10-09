import pathlib,unittest,yaml
ROOT=pathlib.Path(__file__).resolve().parents[2]
class Publication(unittest.TestCase):
 def test_publish_only_trusted_main_after_packaging(self):
  w=yaml.load((ROOT/'.github/workflows/platform-images.yml').read_text(),Loader=yaml.BaseLoader)
  self.assertIn('publish',w['jobs'])
  j=w['jobs']['publish'];self.assertIn("github.event_name != 'pull_request'",j['if']);self.assertIn("github.ref == 'refs/heads/main'",j['if'])
  self.assertEqual(j['needs'],['build-check','release-contracts','postgres14-acceptance','infra-read-tests','harness-tests']);self.assertEqual(j['permissions']['packages'],'write')
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

class RRSAPackaging(unittest.TestCase):
 def render(self, mode, extra=()):
  helm=os.environ.get('HELM',shutil.which('helm'))
  if not helm:self.skipTest('Helm required; dedicated release CI renders RRSA')
  args=[helm,'template','platform',str(ROOT/'helm-chart/raptor-platform'),'--namespace','raptor-system','--set','repository.revision='+'a'*40]
  for image in ['raptor','gateway']:args+=['--set',f'images.{image}=ghcr.io/jamesdeng/raptor-iap-{image}@sha256:'+'b'*64]
  for service in ['raptor-frontend','raptor-backend','raptor-open-api','raptor-admin','agent-gateway']:args+=['--set',f'appCodes.{service}=TEST-{service}']
  for v in ['gatewayLive.enabled=true','gatewayLive.serviceURL=https://api.example','gatewayLive.configSecretName=live-config','agentAccess.secretName=agent-auth',f'gatewayLive.credentialMode={mode}']+list(extra):args+=['--set',v]
  return subprocess.run(args,capture_output=True,text=True)
 def test_rrsa_projects_only_gateway_and_omits_static_secret(self):
  p=self.render('rrsa',['gatewayLive.rrsa.roleARN=acs:ram::1360282071200743:role/raptor-rdev-gateway-controller','gatewayLive.rrsa.providerARN=acs:ram::1360282071200743:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1'])
  self.assertEqual(p.returncode,0,p.stderr);docs=[d for d in yaml.safe_load_all(p.stdout) if d]
  sa=[d for d in docs if d['kind']=='ServiceAccount'];self.assertEqual([d['metadata']['name'] for d in sa],['agent-gateway'])
  for d in docs:
   if d['kind']!='Deployment':continue
   pod=d['spec']['template']['spec'];self.assertFalse(pod['automountServiceAccountToken'])
   projections=[v for v in pod['volumes'] if 'projected' in v]
   if d['metadata']['name']=='agent-gateway':
    self.assertEqual(d['spec']['strategy']['type'],'Recreate');self.assertEqual(d['spec']['replicas'],1);self.assertEqual(pod['serviceAccountName'],'agent-gateway')
    self.assertEqual(projections[0]['projected']['sources'][0]['serviceAccountToken']['audience'],'sts.aliyuncs.com');self.assertEqual(projections[0]['projected']['sources'][0]['serviceAccountToken']['expirationSeconds'],3600)
    env={e['name']:e.get('value') for e in pod['containers'][0]['env']};self.assertEqual(env['GATEWAY_CONTROLLER_CREDENTIAL_MODE'],'rrsa');self.assertNotIn('GATEWAY_CONTROLLER_CREDENTIAL_FILE',env)
    self.assertFalse(any(v['name']=='live-controller-source' for v in pod['volumes']));self.assertNotIn('/live-controller-source/',pod['initContainers'][0]['args'][0])
   else:self.assertEqual(projections,[])
 def test_static_sts_keeps_secret(self):
  p=self.render('static-sts',['gatewayLive.controllerSecretName=live-controller']);self.assertEqual(p.returncode,0,p.stderr)
  d=next(d for d in yaml.safe_load_all(p.stdout) if d and d['kind']=='Deployment' and d['metadata']['name']=='agent-gateway');pod=d['spec']['template']['spec']
  self.assertTrue(any(v['name']=='live-controller-source' for v in pod['volumes']))
 def test_reject_unknown_missing_and_mixed_credentials(self):
  for mode,extra in [('invalid',[]),('rrsa',[]),('rrsa',['gatewayLive.controllerSecretName=static']),('static-sts',[])]:
   with self.subTest(mode=mode,extra=extra):self.assertNotEqual(self.render(mode,extra).returncode,0)

class RRSAArgoPermissions(unittest.TestCase):
 def test_gateway_serviceaccount_allowed_without_wildcards(self):
  p=yaml.safe_load((ROOT/'infra-kubernetes/environments/rdev.ali/project.yaml').read_text())
  self.assertIn({'group':'','kind':'ServiceAccount'},p['spec']['namespaceResourceWhitelist'])
  self.assertNotIn({'group':'','kind':'Secret'},p['spec']['namespaceResourceWhitelist'])
  self.assertNotIn('*',str(p['spec']['namespaceResourceWhitelist']))

class ConversationRollout(unittest.TestCase):
 def render(self,*extra):
  helm=os.environ.get('HELM',shutil.which('helm'))
  if not helm:self.skipTest('Helm required')
  args=[helm,'template','platform',str(ROOT/'helm-chart/raptor-platform'),'-f',str(ROOT/'infra-kubernetes/environments/rdev.ali/values.yaml')]
  for value in extra:args+=['--set',value]
  p=subprocess.run(args,capture_output=True,text=True);self.assertEqual(p.returncode,0,p.stderr)
  return {d['metadata']['name']:d for d in yaml.safe_load_all(p.stdout) if d and d['kind']=='Deployment'}
 def test_conversation_flags_default_off(self):
  defaults=yaml.safe_load((ROOT/'helm-chart/raptor-platform/values.yaml').read_text())
  self.assertEqual(defaults['conversation'],{'gatewayEnabled':False,'raptorEnabled':False})
  d=self.render('conversation.gatewayEnabled=false','conversation.raptorEnabled=false')
  for name,key in [('agent-gateway','GATEWAY_CONVERSATION_ENABLED'),('raptor-backend','RAPTOR_CONVERSATION_ENABLED')]:
   env={e['name']:e.get('value') for e in d[name]['spec']['template']['spec']['containers'][0]['env']}
   self.assertEqual(env[key],'false')
 def test_private_gateway_acceptance_keeps_raptor_closed(self):
  d=self.render('conversation.gatewayEnabled=true','conversation.raptorEnabled=false','progress.publicURL=wss://raptor.rdev.raptor-iap.top/v1/progress','progress.origins=https://raptor.rdev.raptor-iap.top','progress.connectOrigin=wss://raptor.rdev.raptor-iap.top')
  for name,key,expected in [('agent-gateway','GATEWAY_CONVERSATION_ENABLED','true'),('raptor-backend','RAPTOR_CONVERSATION_ENABLED','false'),('raptor-frontend','RAPTOR_PROGRESS_CONNECT_ORIGIN','wss://raptor.rdev.raptor-iap.top')]:
   env={e['name']:e.get('value') for e in d[name]['spec']['template']['spec']['containers'][0]['env']};self.assertEqual(env[key],expected)
 def test_accepted_rdev_release_opens_raptor_conversation(self):
  d=self.render()
  for name,key in [('agent-gateway','GATEWAY_CONVERSATION_ENABLED'),('raptor-backend','RAPTOR_CONVERSATION_ENABLED')]:
   env={e['name']:e.get('value') for e in d[name]['spec']['template']['spec']['containers'][0]['env']}
   self.assertEqual(env[key],'true')
 def test_public_gateway_only_exact_progress_get(self):
  docs=list(yaml.safe_load_all((ROOT/'infra-kubernetes/environments/rdev.ali/kong/routes.yaml').read_text()))
  routes=[]
  for d in docs:
   if d.get('kind')!='Ingress':continue
   for rule in d['spec']['rules']:
    for path in rule['http']['paths']:
     if path['backend']['service']['name']=='agent-gateway':routes.append((d,path))
  self.assertEqual(len(routes),1)
  d,p=routes[0];self.assertEqual((p['path'],p['pathType']),('/v1/progress','Exact'));self.assertEqual(d['metadata']['annotations']['konghq.com/methods'],'GET');self.assertEqual(d['metadata']['annotations']['konghq.com/strip-path'],'false')
