"""Run on the verified ACK worker. Read-only; no cloud credentials or mutations."""
import argparse, concurrent.futures, datetime, json, math, os, subprocess, time, urllib.parse, urllib.request
from pathlib import Path
from segmented_capture import SegmentedCapture

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
    selector='{env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"}'
    queries={'up':'timestamp(up{job="pgcat",'+selector[1:]+' == 1)'}
    for kind in ('active','idle','waiting'):
        name='pgcat_pools_cl_'+kind+selector;queries[kind]=name;queries[kind+'_timestamp']='timestamp('+name+')'
    def fetch(item):
        key,q=item;r=get(base+'/api/v1/query?'+urllib.parse.urlencode({'query':q}))
        if r['status']!='success' or r['data']['resultType']!='vector':raise ValueError('invalid query evidence')
        return key,r['data']['result']
    with concurrent.futures.ThreadPoolExecutor(max_workers=7) as pool:results=dict(pool.map(fetch,queries.items()))
    nodes=[r['metric']['ecs_instance_id'] for r in results['up']]
    if not nodes or any(not isinstance(n,str) or not n for n in nodes) or len(set(nodes))!=len(nodes):raise ValueError('invalid scrape evidence')
    def numeric(value, count=False):
        value=float(value)
        if not math.isfinite(value) or (count and (value<0 or not value.is_integer() or value>2**53)):raise ValueError('invalid raw observation')
        return value
    up_stamps=[numeric(r['value'][1]) for r in results['up']]
    raw=[]
    for kind in ('active','idle','waiting'):
        def key(row):return (row['metric']['ecs_instance_id'],row['metric']['pool'],row['metric']['user'])
        stamps={key(r):numeric(r['value'][1]) for r in results[kind+'_timestamp']}
        values=results[kind]
        if not values or len(stamps)!=len(results[kind+'_timestamp']) or len(values)!=len(stamps) or set(stamps)!=set(key(r) for r in values):raise ValueError('incomplete source evidence')
        for row in values:
            node,pool,user=key(row)
            if node not in nodes or not pool or not user:raise ValueError('foreign or invalid raw coverage')
            raw.append({'node':node,'pool':pool,'user':user,'kind':kind,'value':numeric(row['value'][1],count=True),'sourceAt':stamps[key(row)]})
    triplets={}
    for row in raw:triplets.setdefault((row['node'],row['pool'],row['user']),set()).add(row['kind'])
    if any(k!={'active','idle','waiting'} for k in triplets.values()) or any((n,'test','poc_app') not in triplets for n in nodes):raise ValueError('missing expected raw triplets')
    return {'observedAt':utc(),'sourceAt':min(up_stamps),'nodes':nodes,'available':True,'raw':raw}
def read_pod(p,final_snapshots):
    uid=p['metadata']['uid']
    if uid in final_snapshots:return {'uid':uid,'snapshot':final_snapshots[uid]}
    snapshot=get('http://'+p['status']['podIP']+':9090/evidence')
    if snapshot.get('final') is True:final_snapshots[uid]=snapshot
    return {'uid':uid,'snapshot':snapshot}
def capture_frame(inventory_path,final_snapshots=None):
    if final_snapshots is None:final_snapshots={}
    stamp=utc();pods=kubectl('get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json')['items']
    def one(p):return read_pod(p,final_snapshots)
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:rows=list(pool.map(one,[p for p in pods if p['status'].get('podIP') and p['status'].get('phase') in ('Running','Pending')]))
    deployment=kubectl('get','deployment','rdev-test-client-client','-o','json')
    expected=deployment['spec']['replicas']
    inventory=json.loads(Path(inventory_path).read_text())
    metrics={'observedAt':utc(),'sourceAt':stamp,'nodes':[],'available':False,'raw':[]}
    try:
        svc=kubectl('get','service','rdev-test-metrics-metrics','-o','json');base='http://'+svc['spec']['clusterIP']+':9090'
        metrics=query_metrics(base,stamp)
    except (ValueError,KeyError,TypeError,IndexError,OverflowError,OSError,RuntimeError):pass
    return {'at':stamp,'expectedReplicas':expected,'pods':rows,'inventory':inventory,'metrics':metrics}
def main():
    a=argparse.ArgumentParser();a.add_argument('--output',required=True);a.add_argument('--inventory',required=True);a.add_argument('--seconds',type=int,default=60);v=a.parse_args()
    if not 5<=v.seconds<=5400:raise SystemExit('bounded capture duration required')
    os.umask(0o077)
    if Path(v.output).exists():raise SystemExit('capture output already exists')
    w=SegmentedCapture(Path(v.output),max_total_seconds=v.seconds);start=time.monotonic();end=start+v.seconds;cancelled=False;errors=[];final_snapshots={}
    try:
        tick=start
        while time.monotonic()<end:
            try:w.append(capture_frame(v.inventory,final_snapshots))
            except Exception:errors.append({'at':utc(),'reason':'client or provider snapshot unavailable'})
            tick+=1;time.sleep(max(0,tick-time.monotonic()))
        # Extend the end boundary at most five seconds to settle finite operations.
        settle_end=time.monotonic()+5
        while time.monotonic()<settle_end:
            try:
                frame=capture_frame(v.inventory,final_snapshots);frame['phase']='settling';w.append(frame)
                if frame['pods'] and all(p['snapshot']['sample']['Scheduled']==p['snapshot']['sample']['Attempts']+p['snapshot']['sample']['Skipped'] for p in frame['pods']):break
            except Exception:errors.append({'at':utc(),'reason':'settling snapshot unavailable'})
            tick+=1;time.sleep(max(0,tick-time.monotonic()))
    except KeyboardInterrupt:cancelled=True
    result=w.finish(completed=not cancelled,cancelled=cancelled,errors=errors)
    print(json.dumps(result))
if __name__=='__main__':main()
