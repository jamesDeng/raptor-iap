import importlib.util,json,pathlib,tempfile,unittest
ROOT=pathlib.Path(__file__).resolve().parents[2]
def load(name):
 s=importlib.util.spec_from_file_location(name,ROOT/'scripts/ecs_ax'/f'{name}.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
class Contracts(unittest.TestCase):
 def test_dynamic_private_ip(self):
  m=load('render_deployment');v=m.render('10.70.1.181','localhost:5001/bridge@sha256:'+'a'*64,'localhost:5001/pi@sha256:'+'b'*64)
  dep=next(x for x in v['items'] if x['kind']=='Deployment');self.assertEqual(dep['spec']['template']['spec']['containers'][0]['ports'][0]['hostIP'],'10.70.1.181')
  for ip in ['0.0.0.0','43.106.61.98','10.70.2.180']:
   with self.assertRaises(ValueError):m.render(ip,'bad','bad')
 def test_archive_and_patch_integrity(self):
  m=load('verify_sources')
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d);(p/'archive').write_bytes(b'pinned');(p/'patch').write_bytes(b'patch')
   import hashlib
   hashes={x:hashlib.sha256((p/x).read_bytes()).hexdigest() for x in ['archive','patch']}
   m.verify(p,hashes)
   for x in hashes:
    original=(p/x).read_bytes();(p/x).write_bytes(original+b'wrong')
    with self.assertRaises(ValueError):m.verify(p,hashes)
    (p/x).write_bytes(original)
if __name__=='__main__':unittest.main()
