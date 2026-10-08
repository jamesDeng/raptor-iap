"""Reject altered source archives or compatibility patches before extraction."""
import hashlib,json,pathlib,sys
def verify(directory,hashes):
 for name,want in hashes.items():
  p=pathlib.Path(directory)/name
  if hashlib.sha256(p.read_bytes()).hexdigest()!=want:raise ValueError('source integrity mismatch: '+name)
if __name__=='__main__':
 pins=json.loads(pathlib.Path(sys.argv[1]).read_text());verify(sys.argv[2],pins['source_sha256'])
