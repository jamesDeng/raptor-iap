"""Render public manifests from the verified Terraform node address and image digests."""
import ipaddress,json,pathlib,re,sys
def render(private_ip,bridge_image,pi_image):
 ip=ipaddress.ip_address(private_ip)
 if ip not in ipaddress.ip_network('10.70.1.0/24') or ip==ipaddress.ip_address('10.70.1.0') or ip==ipaddress.ip_address('10.70.1.255'):raise ValueError('unexpected node subnet')
 for image in [bridge_image,pi_image]:
  if not re.fullmatch(r'localhost:5001/[a-z0-9-]+@sha256:[0-9a-f]{64}',image):raise ValueError('immutable image required')
 root=pathlib.Path(__file__).resolve().parents[2]
 s=(root/'infra-kubernetes/environments/rdev.ali/ecs-ax/bridge.template.json').read_text()
 for key,value in [('PRIVATE_IP',private_ip),('BRIDGE_IMAGE',bridge_image),('PI_IMAGE',pi_image)]:s=s.replace('${'+key+'}',value)
 return json.loads(s)
if __name__=='__main__':print(json.dumps(render(*sys.argv[1:]),indent=2))
