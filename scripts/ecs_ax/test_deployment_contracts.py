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
 def test_canonical_archive_integrity(self):
  import tarfile,io,gzip
  m=load('verify_sources')
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'source.tar.gz'
   def archive(content=b'reviewed',mode=0o644,mtime=0,name='source.go'):
    buf=io.BytesIO()
    with tarfile.open(fileobj=buf,mode='w') as t:
     member=tarfile.TarInfo(name);member.mode=mode;member.mtime=mtime;member.size=len(content);t.addfile(member,io.BytesIO(content))
    p.write_bytes(gzip.compress(buf.getvalue(),mtime=mtime))
   archive();want=m.digest(p)
   archive(mtime=123456);self.assertEqual(want,m.digest(p),'transport timestamps must not invalidate identical source')
   for kwargs in [{'content':b'altered'},{'mode':0o755},{'name':'other.go'}]:
    archive(**kwargs);self.assertNotEqual(want,m.digest(p),'source content/mode/path mutation must be detected')
   archive(name='../source.go')
   with self.assertRaises(ValueError):m.digest(p)
 def test_router_and_worker_ingress_is_owned(self):
  v=load('render_deployment').render('10.70.1.180','localhost:5001/bridge@sha256:'+'a'*64,'localhost:5001/pi@sha256:'+'b'*64)
  policies={x['metadata']['name']:x for x in v['items'] if x['kind']=='NetworkPolicy'}
  self.assertEqual(policies['guest-router-private']['metadata']['namespace'],'ate-system')
  self.assertEqual(policies['ax-worker-private']['spec']['podSelector']['matchLabels'],{'ate.dev/worker-pool':'ax'})
  for name in ['guest-router-private','ax-worker-private']:
   for rule in policies[name]['spec']['ingress']:
    self.assertTrue(rule['from'])
    for peer in rule['from']:
     self.assertTrue(peer['namespaceSelector']['matchLabels']);self.assertTrue(peer['podSelector']['matchLabels'])
if __name__=='__main__':unittest.main()
