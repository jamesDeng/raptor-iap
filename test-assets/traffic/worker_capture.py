"""Run on the verified ACK worker. Read-only; no cloud credentials or mutations."""
import argparse, concurrent.futures, datetime, json, math, os, re, subprocess, sys, time, urllib.parse, urllib.request
from pathlib import Path
from typing import Optional
from segmented_capture import (SegmentedCapture, open_directory, check_directory, read_private,
    decode_private, publish_private, canonical, sha, require, read_frame)
from capture_window import seconds as evidence_seconds, clean_inventory

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
    if not 1<=len(nodes)<=4 or any(type(n) is not str or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}',n) for n in nodes) or len(set(nodes))!=len(nodes):raise ValueError('invalid scrape evidence')
    def numeric(value, count=False):
        value=float(value)
        if not math.isfinite(value) or (count and (value<0 or not value.is_integer() or value>2**53)):raise ValueError('invalid raw observation')
        return value
    up_stamps=[numeric(r['value'][1]) for r in results['up']]
    up_source=dict(zip(nodes,up_stamps))
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
    return {'observedAt':utc(),'sourceAt':min(up_stamps),'upSourceAt':up_source,
            'nodes':nodes,'available':True,'raw':raw}
def container_id(p):
    statuses=p['status'].get('containerStatuses',[])
    if len(statuses)!=1 or not statuses[0].get('containerID'):raise ValueError('exact client container identity unavailable')
    return statuses[0]['containerID']
def read_pod(p,final_snapshots):
    uid=p['metadata']['uid'];cid=container_id(p);key=(uid,cid)
    snapshot=final_snapshots.get(key)
    if snapshot is None:
        snapshot=get('http://'+p['status']['podIP']+':9090/evidence')
        if snapshot.get('final') is True:final_snapshots[key]=snapshot
    conditions=p['status'].get('conditions',[])
    ready_rows=[row for row in conditions if isinstance(row,dict) and row.get('type')=='Ready'] if isinstance(conditions,list) else []
    status=ready_rows[0].get('status') if len(ready_rows)==1 else None
    ready=True if status=='True' else False if status=='False' else None
    return {'uid':uid,'name':p['metadata'].get('name',''),'containerId':cid,'ready':ready,'snapshot':snapshot}
def capture_frame(inventory_path,final_snapshots=None):
    if final_snapshots is None:final_snapshots={}
    stamp=utc();pods=kubectl('get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json')['items']
    unavailable=(ValueError,KeyError,TypeError,IndexError,OverflowError,OSError,RuntimeError,subprocess.TimeoutExpired)
    def one(p):
        try:return read_pod(p,final_snapshots)
        except unavailable:return None
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        observations=list(pool.map(one,[p for p in pods if p['status'].get('podIP') and p['status'].get('phase') in ('Running','Pending')]))
    rows=[row for row in observations if row is not None]
    # Zero is an invalid roster expectation in the retained evidence contract.
    # Keep successful observations while ensuring incomplete reads cannot PASS.
    expected=0;deployment_identity=None
    try:
        deployment=kubectl('get','deployment','rdev-test-client-client','-o','json')
        if not isinstance(deployment,dict) or any(not isinstance(deployment.get(key),dict) for key in ('metadata','status','spec')):
            raise ValueError('Deployment identity mappings unavailable')
        deployment_identity={'uid':deployment['metadata'].get('uid'),'generation':deployment['metadata'].get('generation'),
                             'observedGeneration':deployment.get('status',{}).get('observedGeneration'),
                             'desired':deployment['spec'].get('replicas'),'ready':deployment.get('status',{}).get('readyReplicas')}
        if all(row is not None for row in observations):expected=deployment['spec']['replicas']
    except unavailable:pass
    inventory=clean_inventory({'schemaVersion':2})
    try:
        source=json.loads(Path(inventory_path).read_text())
        inventory=clean_inventory(source)
    except unavailable:pass
    metrics={'observedAt':utc(),'sourceAt':stamp,'nodes':[],'available':False,'raw':[]}
    try:
        svc=kubectl('get','service','rdev-test-metrics-metrics','-o','json');base='http://'+svc['spec']['clusterIP']+':9090'
        metrics=query_metrics(base,stamp)
    except (ValueError,KeyError,TypeError,IndexError,OverflowError,OSError,RuntimeError):pass
    return {'at':stamp,'expectedReplicas':expected,'pods':rows,'deployment':deployment_identity,'inventory':inventory,'metrics':metrics}
def run_capture(output: Path, inventory: Path, seconds: int, boundary_dir: Optional[Path] = None, timing_output: Optional[Path] = None, *, progress_context=None) -> dict:
    from finish_clients import private_directory, read_json, atomic_json, validate_roster
    if not 5<=seconds<=5400:raise ValueError('bounded capture duration required')
    if boundary_dir is not None:
        boundary_dir=Path(boundary_dir);private_directory(boundary_dir)
        if any(boundary_dir.iterdir()):raise ValueError('fresh boundary directory required')
    if progress_context is not None and boundary_dir is None:raise ValueError('witness boundary required')
    w=SegmentedCapture(Path(output),max_total_seconds=seconds,progress_context=progress_context)
    boundary_fd=open_directory(boundary_dir) if progress_context is not None else None
    boundary_start=None;boundary_start_raw=None;request_raw=None
    def boundary_read(name):
        check_directory(boundary_fd,boundary_dir)
        raw=read_private(boundary_fd,name)
        check_directory(boundary_fd,boundary_dir)
        return raw
    def boundary_write(name,value):
        check_directory(boundary_fd,boundary_dir)
        raw=publish_private(boundary_fd,name,value)
        check_directory(boundary_fd,boundary_dir)
        return raw
    timing_stream=None
    if timing_output is not None:
        flags=os.O_WRONLY|os.O_CREAT|os.O_EXCL
        if hasattr(os,'O_NOFOLLOW'):flags|=os.O_NOFOLLOW
        fd=os.open(str(timing_output),flags,0o600)
        os.fchmod(fd,0o600)
        timing_stream=os.fdopen(fd,'w',buffering=1)
    start=time.monotonic();end=start+seconds;tick=start;cancelled=False;errors=[];final_snapshots={};request=None;settle_start=None;bound_roster=None;boundary_completed=False
    attempt=0
    def error(reason):errors.append({'at':utc(),'reason':reason})
    try:
        while True:
            now=time.monotonic()
            if settle_start is not None and now>settle_start+5:break
            if settle_start is None and now>=end:
                if boundary_dir is not None:error('client boundary unavailable');break
                settle_start=now
            frame=None;capture_ms=None;append_ms=None;outcome='unavailable';attempt+=1;append_attempted=False
            try:
                read_start=time.monotonic()
                try:frame=capture_frame(inventory,final_snapshots)
                finally:capture_ms=round((time.monotonic()-read_start)*1000,3)
                if boundary_dir is not None and request is None and (boundary_dir/'request.json').exists():
                    request_raw=boundary_read('request.json') if boundary_fd is not None else None
                    request=decode_private(request_raw) if request_raw is not None else read_json(boundary_dir/'request.json')
                    if set(request)!={'requestId','roster'} or not isinstance(request['requestId'],str) or not request['requestId'] or len(request['requestId'])>256:raise ValueError('invalid boundary')
                    candidate=validate_roster(request['roster'])
                    current=validate_roster([dict(name=p['name'],uid=p['uid'],containerId=p['containerId'],processId=p['snapshot']['sample']['processId']) for p in frame['pods']])
                    if current!=candidate:raise ValueError('boundary identity mismatch')
                    if progress_context is not None:require(candidate==w.progress_context['originalRoster'])
                    bound_roster=candidate;settle_start=now
                if settle_start is not None:frame['phase']='settling'
                append_start=time.monotonic()
                append_attempted=True
                try:w.append(frame)
                finally:append_ms=round((time.monotonic()-append_start)*1000,3)
                outcome='recorded'
                if settle_start is not None and time.monotonic()>settle_start+5:
                    error('settling period exceeded');break
                if boundary_fd is not None and bound_roster is not None and boundary_start is None:
                    require(boundary_read('request.json')==request_raw)
                    boundary_start=dict(schemaVersion=1,bindingSha256=w.progress['bindingSha256'],requestSha256=sha(request_raw),requestId=request['requestId'],
                        originalRosterSha256=sha(canonical(w.progress_context['originalRoster'])),startedMonotonic=settle_start,
                        deadlineMonotonic=settle_start+5,firstSettlingFrame=w.progress['latest'])
                    boundary_start_raw=boundary_write('boundary-start.json',boundary_start)
                    if time.monotonic()>settle_start+5:error('settling period exceeded');break
                if boundary_dir is not None and bound_roster is not None:
                    current=validate_roster([dict(name=p['name'],uid=p['uid'],containerId=p['containerId'],processId=p['snapshot']['sample']['processId']) for p in frame['pods']])
                    if current!=bound_roster:error('client identity changed after boundary')
                    if not (boundary_dir/'ack.json').exists():atomic_json(boundary_dir/'ack.json',request)
                    if boundary_fd is not None:require(decode_private(boundary_read('ack.json'))==request)
                if settle_start is not None:
                    if boundary_dir is None:
                        if frame['pods'] and all(p['snapshot']['sample']['Scheduled']==p['snapshot']['sample']['Attempts']+p['snapshot']['sample']['Skipped'] for p in frame['pods']):break
                    elif bound_roster is not None and w.finals and all(w.finals.values()):
                        bound={(r['uid'],r['processId']) for r in bound_roster}
                        if bound.issubset(w.finals) and (boundary_dir/'receipt.json').exists():
                            receipt_raw=boundary_read('receipt.json') if boundary_fd is not None else None
                            receipt=decode_private(receipt_raw) if receipt_raw is not None else read_json(boundary_dir/'receipt.json')
                            expected={uid+'/'+pid for uid,pid in bound}
                            if (set(receipt)!={'requestId','status','clients'} or receipt['requestId']!=request['requestId']
                                or receipt['status']!='ACKNOWLEDGED' or not isinstance(receipt['clients'],dict)
                                or set(receipt['clients'])!=expected
                                or any(result!={'status':'acknowledged'} for result in receipt['clients'].values())):
                                error('invalid or uncertain finish receipt');break
                            if time.monotonic()>settle_start+5:
                                error('finish receipt exceeded settling deadline');break
                            if boundary_fd is not None:
                                require(boundary_start is not None and boundary_read('request.json')==request_raw and boundary_read('boundary-start.json')==boundary_start_raw)
                                require(decode_private(boundary_read('ack.json'))==request and boundary_read('receipt.json')==receipt_raw)
                                require(not any(w.progress['flags'][key] for key in ('identityChanged','skipped','invalidEvidence')) and set(w.final_refs)==bound)
                                finals=[]
                                for uid,pid in sorted(bound):
                                    ref=w.final_refs[(uid,pid)];actual=read_frame(w.progress_fd,ref)
                                    pod=next(p for p in actual['pods'] if (p['uid'],p['snapshot']['sample']['processId'])==(uid,pid))
                                    snap=pod['snapshot'];sample=snap['sample']
                                    require(snap['final'] is True and type(snap['connected']) is int and snap['connected']==0 and sample['Scheduled']==sample['Attempts']+sample['Skipped'] and sample['Skipped']==0)
                                    finals.append(dict(uid=uid,processId=pid,frame=ref))
                                durable=time.monotonic();require(durable<=boundary_start['deadlineMonotonic'])
                                boundary_write('boundary-completed.json',dict(schemaVersion=1,bindingSha256=w.progress['bindingSha256'],requestSha256=sha(request_raw),requestId=request['requestId'],
                                    startSha256=sha(boundary_start_raw),receiptSha256=sha(receipt_raw),finalDurableMonotonic=durable,finals=finals,lastFrame=w.progress['latest']))
                            boundary_completed=True;break
            except Exception:
                error('client, boundary or provider snapshot unavailable')
                # Retain SQL evidence even when the boundary itself is malformed.
                if frame is not None and (boundary_fd is None or not append_attempted) and (w.last is None or w.last!=evidence_seconds(frame['at'])):
                    append_start=time.monotonic()
                    try:w.append(frame);outcome='partial'
                    except Exception:pass
                    finally:append_ms=round((time.monotonic()-append_start)*1000,3)
                if boundary_fd is not None:w.note_poll_error(frame['at'] if frame is not None else utc(),attempt=w.progress['attempts']<attempt)
            finally:
                if timing_stream is not None:
                    try:
                        timing_stream.write(json.dumps({'version':1,'attempt':attempt,'at':utc(),'captureMs':capture_ms,'appendMs':append_ms,'outcome':outcome},separators=(',',':'))+'\n')
                    except OSError:
                        print('optional timing output unavailable',file=sys.stderr)
                        failed_stream=timing_stream;timing_stream=None
                        try:failed_stream.close()
                        except OSError:pass
            if settle_start is not None and now>=settle_start+5:break
            tick+=1;time.sleep(max(0,tick-time.monotonic()))
    except KeyboardInterrupt:cancelled=True
    if boundary_dir is not None:
        if bound_roster is None or not w.finals or not all(w.finals.values()):error('missing client final marker or boundary')
        if not boundary_completed:error('exact client finalization not completed within settling deadline')
    try:return w.finish(completed=not cancelled,cancelled=cancelled,errors=errors)
    finally:
        if boundary_fd is not None:os.close(boundary_fd)
        if timing_stream is not None:
            try:timing_stream.close()
            except OSError:print('optional timing output unavailable',file=sys.stderr)

def main(*,progress_context=None):
    a=argparse.ArgumentParser();a.add_argument('--output',required=True);a.add_argument('--inventory',required=True);a.add_argument('--seconds',type=int,default=60);a.add_argument('--boundary-dir');a.add_argument('--timing-output');v=a.parse_args()
    os.umask(0o077)
    print(json.dumps(run_capture(Path(v.output),Path(v.inventory),v.seconds,Path(v.boundary_dir) if v.boundary_dir else None,Path(v.timing_output) if v.timing_output else None,progress_context=progress_context)))
if __name__=='__main__':main()
