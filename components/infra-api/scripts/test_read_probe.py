import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location('probe',Path(__file__).with_name('read-probe.py'))
probe=importlib.util.module_from_spec(spec)
if spec.loader: spec.loader.exec_module(probe)
class ProbeTests(unittest.TestCase):
 def test_probe_classifies_authentication_and_cloud_read(self):
  good={'valid':(200,{'data':{'envCode':'dev','accountMatches':True,'evidenceMode':'live'}}),'missing':(401,{}),'wrong':(401,{}),'direct':(403,{'ErrorCode':'AccessDenied'})}
  self.assertTrue(probe.classify(good,'dev')['passed'])
  app_denial=dict(good);app_denial['direct']=(401,{'error':{'code':'Unauthenticated'}})
  self.assertFalse(probe.classify(app_denial,'dev')['passed'])
  for field,replace in [('valid',(200,{'data':{'ready':True}})),('wrong',(200,{})),('direct',(200,{})),('valid',(200,{'data':{'envCode':'dev','accountMatches':True,'evidenceMode':'simulated'}}))]:
   bad=dict(good);bad[field]=replace;self.assertFalse(probe.classify(bad,'dev')['passed'])
 def test_fc3_native_error_proves_iam_rejection(self):
  import io,urllib.error
  from unittest.mock import patch
  error=urllib.error.HTTPError('https://function.example/',403,'Forbidden',{},io.BytesIO(b'{"Code":"InvalidAccessKeyID"}'))
  with patch.object(probe.urllib.request,'build_opener') as opener:
   opener.return_value.open.side_effect=error
   rejection=probe.read('https://function.example/')
  good={'valid':(200,{'data':{'envCode':'dev','accountMatches':True,'evidenceMode':'live'}}),'missing':(401,{}),'wrong':(401,{}),'direct':rejection}
  self.assertTrue(probe.classify(good,'dev')['passed'])
  good['direct']=(403,{'ErrorCode':'UnknownFailure'})
  self.assertFalse(probe.classify(good,'dev')['passed'])
 def test_cleanup_owns_only_recorded_resources(self):
  good={'acknowledged':True,'resources':[{'id':'exact','owner':'terraform','kind':'gateway'}]}
  self.assertEqual(len(probe.owned_resources(good)),1)
  for bad in [{'acknowledged':False,'resources':good['resources']},{'acknowledged':True,'resources':[{'id':'','owner':'terraform','kind':'gateway'}]},{'acknowledged':True,'resources':[{'id':'exact','owner':'external','kind':'gateway'}]}]:
   with self.assertRaises(ValueError):probe.owned_resources(bad)
if __name__=='__main__':unittest.main()
