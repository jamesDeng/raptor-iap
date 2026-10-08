"""Verify archive contents, independent of Git/tar/gzip implementation metadata."""
import hashlib,json,pathlib,sys,tarfile

def digest(path):
 p=pathlib.Path(path)
 if not p.name.endswith('.tar.gz'):return hashlib.sha256(p.read_bytes()).hexdigest()
 h=hashlib.sha256()
 with tarfile.open(p,'r:gz') as t:
  for m in sorted(t.getmembers(),key=lambda x:x.name):
   name=pathlib.PurePosixPath(m.name)
   if name.is_absolute() or '..' in name.parts:raise ValueError('unsafe archive path')
   if m.isdir():continue
   if not (m.isfile() or m.issym()):raise ValueError('unsupported archive entry')
   header=json.dumps([m.name,'file' if m.isfile() else 'symlink',m.mode & 0o777,m.linkname],separators=(',',':')).encode()
   data=t.extractfile(m).read() if m.isfile() else b''
   h.update(len(header).to_bytes(8,'big'));h.update(header);h.update(len(data).to_bytes(8,'big'));h.update(data)
 return h.hexdigest()
def verify(directory,hashes):
 for name,want in hashes.items():
  if digest(pathlib.Path(directory)/name)!=want:raise ValueError('source integrity mismatch: '+name)
if __name__=='__main__':
 pins=json.loads(pathlib.Path(sys.argv[1]).read_text());verify(sys.argv[2],pins['source_sha256'])
