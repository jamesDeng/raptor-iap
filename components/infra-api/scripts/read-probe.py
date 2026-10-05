"""Bounded identity acceptance. No provider credentials or mutation API."""
import argparse,base64,datetime,hashlib,json,os,stat,urllib.request,urllib.error,urllib.parse

def classify(results,env):
 valid_status,valid=results.get('valid',(0,{}));data=valid.get('data',{}) if isinstance(valid,dict) else {}
 direct_status,direct_body=results.get('direct',(0,{}))
 direct_code=direct_body.get('ErrorCode',direct_body.get('Code','')) if isinstance(direct_body,dict) else ''
 checks={'authenticatedLiveIdentity':valid_status==200 and data=={'envCode':env,'accountMatches':True,'evidenceMode':'live'},'missingCallerRejected':results.get('missing',(0,{}))[0]==401,'wrongCallerRejected':results.get('wrong',(0,{}))[0]==401,'directIAMProtected':direct_status in (401,403) and direct_code in ('AccessDenied','SignatureNotMatch','MissingAccessKeyId','InvalidAccessKeyId','InvalidSecurityToken','InvalidAccessKeyID')}
 return {'evidenceMode':'live' if checks['authenticatedLiveIdentity'] else 'unverified','checks':checks,'passed':all(checks.values()),'ackAcceptance':False}

def owned_resources(inventory):
 if inventory.get('acknowledged') is not True or not isinstance(inventory.get('resources'),list) or not inventory['resources']:raise ValueError('UnconfirmedOwnership')
 seen=set()
 for r in inventory['resources']:
  if not isinstance(r,dict) or not r.get('id') or r.get('owner') not in ('terraform','serverless-devs') or r.get('kind') not in ('gateway','api','ram-role','ram-policy','fc-function','fc-trigger') or r['id'] in seen:raise ValueError('UnconfirmedOwnership')
  seen.add(r['id'])
 return list(inventory['resources'])

class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None

def read(url,credential=None,extra_headers=None):
 u=urllib.parse.urlsplit(url)
 if u.scheme!='https' or not u.hostname or u.username or u.fragment: return 0,{}
 headers=dict(extra_headers or {})
 if credential:headers['X-Infra-Authorization']=credential
 request=urllib.request.Request(url,headers=headers)
 try:
  with urllib.request.build_opener(NoRedirect()).open(request,timeout=30) as r:
   data=r.read(65537)
   if len(data)>65536:return 0,{}
   return r.status,json.loads(data)
 except urllib.error.HTTPError as e:
  try:
   body=json.loads(e.read(65536))
   return e.code,{'ErrorCode':body.get('ErrorCode',body.get('Code',''))}
  except Exception:return e.code,{}
 except Exception:return 0,{}

def main():
 parser=argparse.ArgumentParser();parser.add_argument('--config',required=True);args=parser.parse_args()
 try:
  info=os.stat(args.config)
  if stat.S_IMODE(info.st_mode)&0o077:raise ValueError()
  with open(args.config) as f:cfg=json.load(f)
  auth='Basic '+base64.b64encode((cfg['username']+':'+cfg['password']).encode()).decode()
  query=urllib.parse.urlencode({'envCode':cfg['envCode']});url=cfg['gatewayUrl'].rstrip('/')+'/v1/cloud/identity?'+query
  # A shaped but deliberately invalid ACS3 signature reaches the IAM gate.
  direct_headers={'x-acs-date':datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),'x-acs-content-sha256':hashlib.sha256(b'').hexdigest(),'Authorization':'ACS3-HMAC-SHA256 Credential=invalid,SignedHeaders=host;x-acs-content-sha256;x-acs-date,Signature='+('0'*64)}
  results={'missing':read(url),'wrong':read(url,'Basic '+base64.b64encode(b'invalid:invalid').decode()),'valid':read(url,auth),'direct':read(cfg['functionUrl'].rstrip('/')+'/v1/cloud/identity?'+query,extra_headers=direct_headers)}
  report=classify(results,cfg['envCode'])
 except Exception:report={'passed':False,'error':'ProbeConfigurationUnavailable','ackAcceptance':False}
 print(json.dumps(report));return 0 if report['passed'] else 1
if __name__=='__main__':raise SystemExit(main())
