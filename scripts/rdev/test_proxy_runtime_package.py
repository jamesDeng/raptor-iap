import pathlib, unittest, yaml
ROOT = pathlib.Path(__file__).resolve().parents[2]
class ProxyRuntimePackage(unittest.TestCase):
 def test_explicit_runtime_package_preserves_private_function(self):
  base=yaml.safe_load((ROOT/'components/infra-api/s.rdev.yaml').read_text())
  runtime=yaml.safe_load((ROOT/'components/infra-api/s.rdev-proxy.yaml').read_text())
  p=runtime['resources']['infra-api']['props']; b=base['resources']['infra-api']['props']
  for key in ['functionName','role','vpcConfig','triggers','concurrencyConfig']:
   self.assertEqual(p[key],b[key])
  env=p['environmentVariables']
  self.assertEqual(env['INFRA_ENABLE_PROXY_COMMANDS'],'true')
  self.assertEqual(env['RAPTOR_APPROVAL_ORIGIN'],'https://api.rdev.raptor-iap.top')
  for key in ['RAPTOR_SERVICE_USERNAME','RAPTOR_SERVICE_PASSWORD']:
   self.assertEqual(env[key],'${env('+key+')}')
  self.assertNotIn('INFRA_ENABLE_PROXY_COMMANDS',b['environmentVariables'])
if __name__=='__main__': unittest.main()
