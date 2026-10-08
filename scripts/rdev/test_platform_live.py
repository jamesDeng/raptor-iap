import os,shutil,subprocess,unittest,yaml,pathlib
ROOT=pathlib.Path(__file__).resolve().parents[2]
class LiveManifest(unittest.TestCase):
 def render(self,live):
  args=[os.environ.get('HELM') or shutil.which('helm'),'template','platform',str(ROOT/'helm-chart/raptor-platform'),'-f',str(ROOT/'infra-kubernetes/environments/rdev.ali/values.yaml')]
  if not live:args+=['--set','gatewayLive.enabled=false']
  if live:args+=['--set','gatewayLive.enabled=true','--set','gatewayLive.configSecretName=gateway-live-config','--set','gatewayLive.controllerSecretName=gateway-live-controller','--set','gatewayLive.serviceURL=https://api.rdev.raptor-iap.top','--set','agentAccess.secretName=raptor-agent-access']
  p=subprocess.run(args,capture_output=True,text=True);self.assertEqual(p.returncode,0,p.stderr);return list(yaml.safe_load_all(p.stdout))
 def test_default_disables_runtime(self):
  d=next(d for d in self.render(False) if d and d.get('kind')=='Deployment' and d['metadata']['name']=='agent-gateway');env={v['name']:v.get('value') for v in d['spec']['template']['spec']['containers'][0]['env']};self.assertEqual(env.get('GATEWAY_RUNTIME_MODE'),'disabled')
 def test_live_private_files_and_auth_wiring(self):
  docs=self.render(True);d=next(d for d in docs if d and d.get('kind')=='Deployment' and d['metadata']['name']=='agent-gateway');pod=d['spec']['template']['spec'];env={v['name']:v.get('value') for v in pod['containers'][0]['env']};self.assertEqual(env.get('GATEWAY_RUNTIME_MODE'),'live');self.assertEqual(env['RAPTOR_OPEN_API_URL'],'https://api.rdev.raptor-iap.top');init=next(c for c in pod.get('initContainers',[]) if c['name']=='live-private-files');self.assertEqual(pod['securityContext']['runAsUser'],10001);self.assertIn('0600',init['args'][0]);vols={v['name']:v for v in pod['volumes']};self.assertIn('emptyDir',vols['live-private']);self.assertEqual(vols['live-config-source']['secret']['defaultMode'],288);self.assertEqual(env['GATEWAY_LIVE_CONFIG_FILE'],'/run/raptor/live/config.json')
  for name in ['raptor-backend','raptor-open-api']:
   c=next(d for d in docs if d and d.get('kind')=='Deployment' and d['metadata']['name']==name)['spec']['template']['spec']['containers'][0];env={v['name']:v for v in c['env']};self.assertEqual(env['AGENT_INTROSPECTION_PASSWORD']['valueFrom']['secretKeyRef']['name'],'raptor-agent-access')
  self.assertFalse(any(d and d.get('kind')=='Secret' for d in docs))
 def test_live_without_secret_refs_fails_closed(self):
  p=subprocess.run([os.environ.get('HELM') or shutil.which('helm'),'template','platform',str(ROOT/'helm-chart/raptor-platform'),'-f',str(ROOT/'infra-kubernetes/environments/rdev.ali/values.yaml'),'--set','gatewayLive.enabled=true','--set','gatewayLive.configSecretName=','--set','gatewayLive.controllerSecretName='],capture_output=True,text=True);self.assertNotEqual(p.returncode,0)
 def test_direct_restart_requires_opt_in(self):
  docs=self.render(False)
  for d in docs:
   if d and d.get('kind')=='Deployment':
    env={v['name']:v.get('value') for v in d['spec']['template']['spec']['containers'][0]['env']}
    if d['metadata']['name']=='raptor-backend':self.assertEqual(env.get('RAPTOR_ENABLE_RESTART'),'false')
    else:self.assertNotIn('RAPTOR_ENABLE_RESTART',env)
if __name__=='__main__':unittest.main()
