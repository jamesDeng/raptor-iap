"""Bounded allowlisted archives. Never prints credential data."""
import argparse,gzip,hashlib,io,json,os,shutil,stat,tarfile,tempfile
from pathlib import Path
LIMIT=16*1024*1024

def safe(path):
 p=Path(path).absolute()
 if any(x.is_symlink() for x in (p,*p.parents)):raise ValueError('UnsafePath')
 return p

def auth_valid(data):
 try:
  value=json.loads(data);c=value['openai']
  if not isinstance(value,dict) or c.get('type')!='oauth' or not isinstance(c.get('access'),str) or not c['access'] or not isinstance(c.get('refresh'),str) or not c['refresh'] or type(c.get('expires')) not in (int,float):raise ValueError()
 except (ValueError,KeyError,TypeError,AttributeError):raise ValueError('InvalidAuthRecord') from None

def pack_state(root,archive,limit=LIMIT):
 root=safe(root);archive=safe(archive)
 paths=[root/'auth.json',root/'sessions']
 paths+=sorted((root/'sessions').rglob('*'))
 if not (root/'sessions').is_dir() or len(paths)>1024:raise ValueError('InvalidState')
 total=0
 for path in paths:
  if path.is_symlink() or not (path.is_dir() or path.is_file()):raise ValueError('UnsafeState')
  if path.is_file():total+=path.stat().st_size
 if total>limit:raise ValueError('StateTooLarge')
 auth_valid((root/'auth.json').read_bytes())
 with archive.open('xb') as f:
  os.fchmod(f.fileno(),0o600)
  with tarfile.open(fileobj=f,mode='w:gz') as t:
   for path in paths:t.add(path,arcname=path.relative_to(root).as_posix(),recursive=False)
 size=archive.stat().st_size
 if size>limit:archive.unlink();raise ValueError('ArchiveTooLarge')
 return {'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'bytes':size}

def restore_state(archive,root,expected_sha256,limit=LIMIT):
 archive=safe(archive);root=safe(root)
 if root.exists():raise ValueError('RestoreTargetExists')
 if archive.stat().st_size>limit:raise ValueError('ArchiveTooLarge')
 data=archive.read_bytes()
 if hashlib.sha256(data).hexdigest()!=expected_sha256:raise ValueError('ChecksumMismatch')
 try:
  with gzip.GzipFile(fileobj=io.BytesIO(data)) as g:expanded=g.read(limit+1024*1024+1)
  if len(expanded)>limit+1024*1024:raise ValueError('ExpandedTooLarge')
  with tarfile.open(fileobj=io.BytesIO(expanded),mode='r:') as t:
   members=t.getmembers()
   if len(members)>1024:raise ValueError('TooManyEntries')
   names=set();total=0
   for member in members:
    name=member.name.rstrip('/')
    if name in names or name.startswith('/') or any(x in ('','.', '..') for x in name.split('/')) or not (name=='auth.json' or name=='sessions' or name.startswith('sessions/')) or not (member.isfile() or member.isdir()):raise ValueError('UnsafeArchive')
    if name=='auth.json' and not member.isfile():raise ValueError('UnsafeArchive')
    names.add(name);total+=member.size
   if total>limit or 'auth.json' not in names:raise ValueError('InvalidArchive')
   auth_valid(t.extractfile('auth.json').read())
   root.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
   stage=Path(tempfile.mkdtemp(prefix='.restore-',dir=root.parent))
   try:
    for member in members:
     out=stage/member.name.rstrip('/')
     out.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
     if member.isdir():out.mkdir(exist_ok=True,mode=0o700)
     else:
      with out.open('xb') as f:os.fchmod(f.fileno(),0o600);shutil.copyfileobj(t.extractfile(member),f)
    (stage/'sessions').mkdir(exist_ok=True,mode=0o700)
    os.rename(stage,root)
   finally:
    if stage.exists():shutil.rmtree(stage)
 except (tarfile.TarError,EOFError,gzip.BadGzipFile):raise ValueError('InvalidArchive') from None

def main():
 p=argparse.ArgumentParser();p.add_argument('action',choices=['pack','restore']);p.add_argument('root');p.add_argument('archive');p.add_argument('--sha256');a=p.parse_args()
 try:
  if a.action=='pack':out=pack_state(a.root,a.archive)
  else:restore_state(a.archive,a.root,a.sha256);out={'restored':True}
  print(json.dumps(out));return 0
 except Exception:print('{"error":"CheckpointArchiveFailed"}');return 1
if __name__=='__main__':raise SystemExit(main())
