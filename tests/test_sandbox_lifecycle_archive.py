import importlib.util,io,json,tarfile,tempfile,unittest,hashlib,gzip
from pathlib import Path
spec=importlib.util.spec_from_file_location('checkpoint_archive',Path(__file__).parents[1]/'components/agent-harness/archive.py');a=importlib.util.module_from_spec(spec);spec.loader.exec_module(a)
class ArchiveTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.p=Path(self.tmp.name).resolve();self.root=self.p/'live';self.root.mkdir();(self.root/'sessions').mkdir();(self.root/'auth.json').write_text(json.dumps({'openai':{'type':'oauth','access':'synthetic-access','refresh':'synthetic-refresh','expires':1000,'clientId':'keep-issued','opaque':{'x':1}}}));(self.root/'sessions/session.jsonl').write_text('synthetic history');(self.root/'host-id').write_text('host-not-portable')
 def test_roundtrip_preserves_full_record_and_history_excludes_host(self):
  archive=self.p/'a.tgz';d=a.pack_state(self.root,archive);target=self.p/'new';a.restore_state(archive,target,d['sha256']);self.assertEqual(json.loads((target/'auth.json').read_text())['openai']['clientId'],'keep-issued');self.assertEqual((target/'sessions/session.jsonl').read_text(),'synthetic history');self.assertFalse((target/'host-id').exists());self.assertEqual((target/'auth.json').stat().st_mode&511,384)
 def crafted(self,name,kind=tarfile.REGTYPE,duplicate=False):
  p=self.p/'bad.tgz'
  with tarfile.open(p,'w:gz') as t:
   auth=tarfile.TarInfo('auth.json');data=(self.root/'auth.json').read_bytes();auth.size=len(data);t.addfile(auth,io.BytesIO(data))
   member=tarfile.TarInfo(name);member.type=kind;member.linkname='/outside';member.size=0;t.addfile(member)
   if duplicate:t.addfile(member)
  return p
 def test_unsafe_members_and_duplicates_rejected_before_publication(self):
  for name,kind,duplicate in [('/outside',tarfile.REGTYPE,False),('../outside',tarfile.REGTYPE,False),('sessions/x',tarfile.SYMTYPE,False),('sessions/x',tarfile.LNKTYPE,False),('sessions/dev',tarfile.CHRTYPE,False),('host-id',tarfile.REGTYPE,False),('sessions/x',tarfile.REGTYPE,True)]:
   p=self.crafted(name,kind,duplicate);target=self.p/'output'
   with self.subTest(name=name),self.assertRaises(ValueError):a.restore_state(p,target,hashlib.sha256(p.read_bytes()).hexdigest())
   self.assertFalse(target.exists())
 def test_checksum_mismatch_stops_restore(self):
  p=self.p/'a.tgz';a.pack_state(self.root,p)
  with self.assertRaises(ValueError):a.restore_state(p,self.p/'output','0'*64)
 def test_size_and_gzip_bomb_bounded(self):
  (self.root/'sessions/large').write_bytes(b'x'*2048)
  with self.assertRaises(ValueError):a.pack_state(self.root,self.p/'a.tgz',limit=1024)
  p=self.p/'bomb.tgz';p.write_bytes(gzip.compress(b'x'*(2*1024*1024)))
  with self.assertRaises(ValueError):a.restore_state(p,self.p/'output',hashlib.sha256(p.read_bytes()).hexdigest(),limit=1024)
 def test_source_symlink_and_malformed_auth_secret_not_exposed(self):
  (self.root/'sessions/link').symlink_to(self.root/'auth.json')
  with self.assertRaises(ValueError):a.pack_state(self.root,self.p/'a.tgz')
  (self.root/'sessions/link').unlink();(self.root/'auth.json').write_text('synthetic-secret-not-JSON')
  with self.assertRaises(ValueError) as e:a.pack_state(self.root,self.p/'a.tgz')
  self.assertNotIn('synthetic-secret',str(e.exception))
 def test_oauth_bootstrap_never_transfers_conversation(self):
  archive=self.p/'auth.tgz';meta=a.pack_auth(self.root,archive);target=self.p/'new-auth';a.restore_auth(archive,target,meta['sha256']);self.assertEqual(list((target/'sessions').iterdir()),[]);self.assertEqual(json.loads((target/'auth.json').read_text())['openai']['clientId'],'keep-issued')
 def test_bootstrap_restore_refuses_history_archive(self):
  archive=self.p/'history.tgz';meta=a.pack_state(self.root,archive)
  with self.assertRaises(ValueError):a.restore_auth(archive,self.p/'wrong-bootstrap',meta['sha256'])
  self.assertFalse((self.p/'wrong-bootstrap').exists())
