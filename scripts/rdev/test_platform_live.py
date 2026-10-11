import os,shutil,subprocess,unittest,yaml,pathlib
ROOT=pathlib.Path(__file__).resolve().parents[2]
class LiveManifest(unittest.TestCase):
 def render(self,live,restart=None,ax=False,observer=False,replacement=False):
  args=[os.environ.get('HELM') or shutil.which('helm'),'template','platform',str(ROOT/'helm-chart/raptor-platform'),'-f',str(ROOT/'infra-kubernetes/environments/rdev.ali/values.yaml')]
  if restart is not None:args+=['--set','directRestart.enabled='+str(restart).lower()]
  if not live:args+=['--set','gatewayLive.enabled=false']
  if live:args+=['--set','gatewayLive.enabled=true','--set','gatewayLive.credentialMode=static-sts','--set','gatewayLive.configSecretName=gateway-live-config','--set','gatewayLive.controllerSecretName=gateway-live-controller','--set','gatewayLive.serviceURL=https://api.rdev.raptor-iap.top','--set','agentAccess.secretName=raptor-agent-access']
  if replacement:args += ['--set','replacement.scopeConfigMapName=pgcat-replacement-scope']
  args += ['--set','gatewayLive.observer.enabled='+str(observer).lower()]
  if ax:args += ["--set","gatewayLive.axTLSSecretName=gateway-ax-client-tls"]
  p=subprocess.run(args,capture_output=True,text=True);self.assertEqual(p.returncode,0,p.stderr);return list(yaml.safe_load_all(p.stdout))
 def test_observer_gateway_has_pinned_readonly_skill_checkout(self):
  for enabled in (False,True):
   docs=self.render(True,observer=enabled)
   pod=next(d for d in docs if d and d.get('kind')=='Deployment' and d['metadata']['name']=='agent-gateway')['spec']['template']['spec']
   clones=[c for c in pod.get('initContainers',[]) if c['name']=='skills-repository']
   if not enabled:
    self.assertFalse(clones);continue
   self.assertEqual(len(clones),1)
   clone=clones[0]
   self.assertIn('git clone --no-checkout https://github.com/jamesDeng/raptor-iap.git',clone['args'][0])
   self.assertIn('checkout --detach "$SOURCE_REVISION"',clone['args'][0])
   revision=next(e['value'] for e in clone['env'] if e['name']=='SOURCE_REVISION')
   self.assertRegex(revision,r'^[a-f0-9]{40}$')
   mount=next(v for v in pod['containers'][0]['volumeMounts'] if v['name']=='skills-repository')
   self.assertEqual(mount['mountPath'],'/repo');self.assertTrue(mount['readOnly'])
   self.assertTrue(any(v['name']=='skills-repository' and 'emptyDir' in v for v in pod['volumes']))
 def test_observation_route_exposes_only_fixed_authenticated_post(self):
  for enabled in (False,True):
   docs=self.render(True,observer=enabled)
   routes=[d for d in docs if d and d.get('kind')=='Ingress' and d['metadata']['name']=='raptor-gateway-observations']
   if not enabled:
    self.assertFalse(routes);continue
   self.assertEqual(len(routes),1);route=routes[0]
   self.assertEqual(route['metadata']['annotations']['konghq.com/protocols'],'https')
   self.assertEqual(route['metadata']['annotations']['konghq.com/methods'],'POST')
   rules=route['spec']['rules'];self.assertEqual(len(rules),1);self.assertEqual(rules[0]['host'],'api.rdev.raptor-iap.top')
   paths=rules[0]['http']['paths'];self.assertEqual(len(paths),1)
   self.assertEqual((paths[0]['path'],paths[0]['pathType']),('/v1/gateway-observations','Exact'))
   self.assertEqual(paths[0]['backend']['service']['name'],'agent-gateway')
 def test_default_disables_runtime(self):
  d=next(d for d in self.render(False) if d and d.get('kind')=='Deployment' and d['metadata']['name']=='agent-gateway');env={v['name']:v.get('value') for v in d['spec']['template']['spec']['containers'][0]['env']};self.assertEqual(env.get('GATEWAY_RUNTIME_MODE'),'disabled')
 def test_live_private_files_and_auth_wiring(self):
  docs=self.render(True);d=next(d for d in docs if d and d.get('kind')=='Deployment' and d['metadata']['name']=='agent-gateway');pod=d['spec']['template']['spec'];env={v['name']:v.get('value') for v in pod['containers'][0]['env']};self.assertEqual(env.get('GATEWAY_RUNTIME_MODE'),'live');self.assertEqual(env['RAPTOR_OPEN_API_URL'],'https://api.rdev.raptor-iap.top');init=next(c for c in pod.get('initContainers',[]) if c['name']=='live-private-files');self.assertEqual(pod['securityContext']['runAsUser'],10001);self.assertIn('0600',init['args'][0]);vols={v['name']:v for v in pod['volumes']};self.assertIn('emptyDir',vols['live-private']);self.assertEqual(vols['live-config-source']['secret']['defaultMode'],288);self.assertEqual(env['GATEWAY_LIVE_CONFIG_FILE'],'/run/raptor/live/config.json')
  for name in ['raptor-backend','raptor-open-api']:
   c=next(d for d in docs if d and d.get('kind')=='Deployment' and d['metadata']['name']==name)['spec']['template']['spec']['containers'][0];env={v['name']:v for v in c['env']};self.assertEqual(env['AGENT_INTROSPECTION_PASSWORD']['valueFrom']['secretKeyRef']['name'],'raptor-agent-access')
  self.assertFalse(any(d and d.get('kind')=='Secret' for d in docs))
 def test_replacement_binding_mounts_only_into_backend(self):
  for enabled in [False,True]:
   docs=self.render(True,replacement=enabled)
   for doc in docs:
    if not doc or doc.get('kind')!='Deployment':continue
    pod=doc['spec']['template']['spec'];container=pod['containers'][0];env={v['name']:v.get('value') for v in container['env']}
    if enabled and doc['metadata']['name']=='raptor-backend':
     self.assertEqual(env.get('RAPTOR_REPLACEMENT_SCOPE_FILE'),'/run/raptor/replacement/scope.json')
     self.assertTrue(any(v['name']=='replacement-scope' and v['readOnly'] for v in container['volumeMounts']))
     volume=next(v for v in pod['volumes'] if v['name']=='replacement-scope');self.assertEqual(volume['configMap']['name'],'pgcat-replacement-scope')
    else:self.assertNotIn('RAPTOR_REPLACEMENT_SCOPE_FILE',env);self.assertFalse(any(v['name']=='replacement-scope' for v in pod['volumes']))
 def test_ax_mutual_tls_stays_in_gateway_private_files(self):
  docs=self.render(True,ax=True);d=next(v for v in docs if v and v.get('kind')=='Deployment' and v['metadata']['name']=='agent-gateway');pod=d['spec']['template']['spec'];vols={v['name']:v for v in pod['volumes']};self.assertEqual(vols['ax-tls-source']['secret']['secretName'],'gateway-ax-client-tls');init=pod['initContainers'][0];self.assertIn('install -m 0600 /ax-tls-source/tls.key /live-private/ax-client.key',init['args'][0]);self.assertFalse(any(v['name']=='ax-tls-source' for v in pod['containers'][0]['volumeMounts']))
  for other in docs:
   if other and other.get('kind')=='Deployment' and other['metadata']['name']!='agent-gateway':self.assertFalse(any(v['name']=='ax-tls-source' for v in other['spec']['template']['spec'].get('volumes',[])))
 def test_live_without_secret_refs_fails_closed(self):
  p=subprocess.run([os.environ.get('HELM') or shutil.which('helm'),'template','platform',str(ROOT/'helm-chart/raptor-platform'),'-f',str(ROOT/'infra-kubernetes/environments/rdev.ali/values.yaml'),'--set','gatewayLive.enabled=true','--set','gatewayLive.credentialMode=static-sts','--set','gatewayLive.configSecretName=','--set','gatewayLive.controllerSecretName='],capture_output=True,text=True);self.assertNotEqual(p.returncode,0)
 def test_direct_restart_requires_opt_in(self):
  for enabled in [False,True]:
   docs=self.render(False,restart=enabled)
   for d in docs:
    if d and d.get('kind')=='Deployment':
     env={v['name']:v.get('value') for v in d['spec']['template']['spec']['containers'][0]['env']}
     if d['metadata']['name']=='raptor-backend':self.assertEqual(env.get('RAPTOR_ENABLE_RESTART'),str(enabled).lower())
     else:self.assertNotIn('RAPTOR_ENABLE_RESTART',env)
 def test_observer_issuer_projects_only_api_identity(self):
  docs=self.render(True,observer=True)
  gateway=next(d for d in docs if d and d.get('kind')=='Deployment' and d['metadata']['name']=='agent-gateway');pod=gateway['spec']['template']['spec'];self.assertFalse(pod['automountServiceAccountToken']);self.assertEqual(pod['serviceAccountName'],'agent-gateway')
  volume=next(v for v in pod['volumes'] if v['name']=='observer-api-token')
  source=volume['projected']['sources'][0]['serviceAccountToken'];self.assertEqual(source['expirationSeconds'],900);self.assertNotIn('audience',source)
  self.assertTrue(any(d and d.get('kind')=='ServiceAccount' and d['metadata']['name']=='agent-gateway' for d in docs))
if __name__=='__main__':unittest.main()
