"""Run on the verified ACK worker. Read-only; no cloud credentials or mutations."""
import argparse, concurrent.futures, datetime, json, os, subprocess, time, urllib.parse, urllib.request
from pathlib import Path
from capture_window import CaptureWindow, retain

def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def get(url):
    with urllib.request.urlopen(url,timeout=2) as r:
        raw=r.read(2*1024*1024+1)
        if len(raw)>2*1024*1024:raise ValueError('response bound exceeded')
        return json.loads(raw)
def kubectl(*args):
    r=subprocess.run(['kubectl','-n','raptor-test']+list(args),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=3)
    if r.returncode:raise RuntimeError('Kubernetes read failed')
    return json.loads(r.stdout)
def query_metrics(base,stamp):
    selector='{env="rdev.ali",db_proxy_code="b5267fa4-33b9-408c-a799-0ed56501061f",target_db_code="6b704ca0-401e-41cb-b71f-2d310c6943ba"}'
    queries={'up':'timestamp(up{job="pgcat"} == 1)'}
    for kind in ('active','idle','waiting'):
        name='pgcat_pools_cl_'+kind+selector;queries[kind]=name;queries[kind+'_timestamp']='timestamp('+name+')'
    def fetch(item):
        key,q=item;r=get(base+'/api/v1/query?'+urllib.parse.urlencode({'query':q}))
        if r['status']!='success' or r['data']['resultType']!='vector':raise ValueError('invalid query evidence')
        return key,r['data']['result']
    with concurrent.futures.ThreadPoolExecutor(max_workers=7) as pool:results=dict(pool.map(fetch,queries.items()))
    nodes=[r['metric']['ecs_instance_id'] for r in results['up']]
    if not nodes:raise ValueError('no scrape evidence')
    raw=[]
    for kind in ('active','idle','waiting'):
        def key(row):return (row['metric']['ecs_instance_id'],row['metric']['pool'],row['metric']['user'])
        stamps={key(r):float(r['value'][1]) for r in results[kind+'_timestamp']}
        values=results[kind]
        if not values or len(stamps)!=len(results[kind+'_timestamp']) or set(stamps)!=set(key(r) for r in values):raise ValueError('incomplete source evidence')
        for row in values:
            node,pool,user=key(row);raw.append({'node':node,'pool':pool,'user':user,'kind':kind,'value':float(row['value'][1]),'sourceAt':stamps[key(row)]})
    return {'observedAt':utc(),'sourceAt':min(float(r['value'][1]) for r in results['up']),'nodes':nodes,'available':True,'raw':raw}
def capture_frame(inventory_path):
    stamp=utc();pods=kubectl('get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json')['items']
    def one(p):return {'uid':p['metadata']['uid'],'snapshot':get('http://'+p['status']['podIP']+':9090/evidence')}
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:rows=list(pool.map(one,[p for p in pods if p['status'].get('podIP') and p['status'].get('phase') in ('Running','Pending')]))
    deployment=kubectl('get','deployment','rdev-test-client-client','-o','json')
    expected=deployment['spec']['replicas']
    inventory=json.loads(Path(inventory_path).read_text())
    metrics={'observedAt':utc(),'sourceAt':stamp,'nodes':[],'available':False,'raw':[]}
    try:
        svc=kubectl('get','service','rdev-test-metrics-metrics','-o','json');base='http://'+svc['spec']['clusterIP']+':9090'
        metrics=query_metrics(base,stamp)
    except (ValueError,KeyError,OSError,RuntimeError):pass
    return {'at':stamp,'expectedReplicas':expected,'pods':rows,'inventory':inventory,'metrics':metrics}
def main():
    a=argparse.ArgumentParser();a.add_argument('--output',required=True);a.add_argument('--inventory',required=True);a.add_argument('--seconds',type=int,default=60);v=a.parse_args()
    if not 5<=v.seconds<=1800:raise SystemExit('bounded capture duration required')
    os.umask(0o077)
    if Path(v.output).exists():raise SystemExit('capture output already exists')
    w=CaptureWindow(v.output);start=time.monotonic();end=start+v.seconds;cancelled=False;errors=[]
    try:
        tick=start
        while time.monotonic()<end:
            try:w.append(capture_frame(v.inventory))
            except Exception:errors.append({'at':utc(),'reason':'client or provider snapshot unavailable'})
            tick+=1;time.sleep(max(0,tick-time.monotonic()))
        # Extend the end boundary at most five seconds to settle finite operations.
        settle_end=time.monotonic()+5
        while time.monotonic()<settle_end:
            try:
                frame=capture_frame(v.inventory);frame['phase']='settling';w.append(frame)
                if frame['pods'] and all(p['snapshot']['sample']['Scheduled']==p['snapshot']['sample']['Attempts']+p['snapshot']['sample']['Skipped'] for p in frame['pods']):break
            except Exception:errors.append({'at':utc(),'reason':'settling snapshot unavailable'})
            time.sleep(0.2)
    except KeyboardInterrupt:cancelled=True
    # Any unavailable poll is retained separately and prevents acceptance.
    result=retain(v.output,completed=not cancelled,cancelled=cancelled)
    if errors:
        result['status']='FAIL' if result['status']=='FAIL' else 'INCONCLUSIVE';result['reasons'].append('unavailable capture poll')
    Path(v.output+'.summary.json').write_text(json.dumps(result,indent=2)+'\n')
    Path(v.output+'.completion.json').write_text(json.dumps({'completed':not cancelled,'errors':errors,'end':utc(),'pollSeconds':1})+'\n')
    print(json.dumps(result))
if __name__=='__main__':main()
