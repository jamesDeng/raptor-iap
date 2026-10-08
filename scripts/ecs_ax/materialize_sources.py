"""Materialize deterministic archives from verified upstream Git objects."""
import gzip,json,pathlib,subprocess,sys
from verify_sources import verify
root=pathlib.Path(__file__).resolve().parents[2]
def materialize(ax,substrate,output):
 pins=json.loads((root/'third_party/ax/pins.json').read_text());out=pathlib.Path(output);out.mkdir(parents=True,exist_ok=True)
 for checkout,key,name in [(ax,'ax_commit','ecs-ax-upstream-ax.tar.gz'),(substrate,'substrate_commit','ecs-ax-substrate.tar.gz')]:
  revision=pins[key]
  actual=subprocess.check_output(['git','-C',checkout,'rev-parse',revision+'^{commit}'],text=True).strip()
  if actual!=revision:raise ValueError('upstream revision mismatch')
  data=subprocess.check_output(['git','-C',checkout,'archive','--format=tar',revision])
  (out/name).write_bytes(gzip.compress(data,compresslevel=9,mtime=0))
 (out/pins['patch']).write_bytes((root/'third_party/ax'/pins['patch']).read_bytes())
 verify(out,pins['source_sha256'])
 (out/'pins.json').write_text(json.dumps(pins,indent=2)+'\n')
if __name__=='__main__':materialize(*sys.argv[1:])
