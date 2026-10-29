"""Run on the verified ACK worker. Read-only; no cloud credentials or mutations."""
import argparse, concurrent.futures, datetime, json, math, os, re, subprocess, sys, time, urllib.parse, urllib.request
from pathlib import Path
from typing import Optional
from segmented_capture import (SegmentedCapture, open_directory, check_directory, read_private,
    decode_private, publish_private, canonical, sha, require, read_frame)
from capture_window import seconds as evidence_seconds, clean_inventory

def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def remaining(deadline, maximum):
    if deadline is None:return maximum
    value=deadline-time.monotonic()
    if not math.isfinite(value) or value<=0:raise ValueError('worker deadline exhausted')
    return min(maximum,value)

def get(url, *, deadline=None):
    with urllib.request.urlopen(url,timeout=remaining(deadline,2)) as r:
        raw=r.read(2*1024*1024+1)
        remaining(deadline,2)
        if len(raw)>2*1024*1024:raise ValueError('response bound exceeded')
        return json.loads(raw)
def kubectl(*args, deadline=None, kubeconfig=None):
    options=dict(stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=remaining(deadline,3))
    if kubeconfig is not None:
        options['env']=dict(os.environ,KUBECONFIG=str(kubeconfig))
    r=subprocess.run(['kubectl','-n','raptor-test']+list(args),**options)
    remaining(deadline,3)
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
def read_pod(p,final_snapshots, *, deadline=None):
    remaining(deadline,2)
    uid=p['metadata']['uid'];cid=container_id(p);key=(uid,cid)
    snapshot=final_snapshots.get(key)
    if snapshot is None:
        url='http://'+p['status']['podIP']+':9090/evidence'
        snapshot=get(url) if deadline is None else get(url,deadline=deadline)
        if snapshot.get('final') is True:final_snapshots[key]=snapshot
    remaining(deadline,2)
    conditions=p['status'].get('conditions',[])
    ready_rows=[row for row in conditions if isinstance(row,dict) and row.get('type')=='Ready'] if isinstance(conditions,list) else []
    status=ready_rows[0].get('status') if len(ready_rows)==1 else None
    ready=True if status=='True' else False if status=='False' else None
    return {'uid':uid,'name':p['metadata'].get('name',''),'containerId':cid,'ready':ready,'snapshot':snapshot}
def native_owner_evidence(deployment, replicasets, pods, context):
    owners,starting=_native_owner_records(deployment,replicasets,pods,context)
    require(not starting)
    return owners


def _native_owner_records(deployment, replicasets, pods, context, allow_starting=False):
    from pgcat_native_evidence import validate_context, token
    context=validate_context(context)
    return _native_owner_records_for_deployment(deployment,replicasets,pods,context['deployment'],allow_starting)


def _native_owner_records_for_deployment(deployment, replicasets, pods, bound, allow_starting=False):
    from pgcat_native_evidence import validate_deployment, token
    bound=validate_deployment(bound)
    def controller(obj,kind):
        refs=obj['metadata'].get('ownerReferences',[])
        require(type(refs) is list and all(type(row) is dict for row in refs))
        rows=[row for row in refs if row.get('controller') is True]
        require(len(rows)==1 and rows[0].get('kind')==kind and token(rows[0].get('uid')))
        return rows[0]
    def image(obj):
        containers=obj['spec']['containers']
        require(type(containers) is list and len(containers)==1 and containers[0]['name']=='client')
        value=containers[0]['image']
        require(type(value) is str and value.endswith('@'+bound['imageDigest']))
    meta=deployment['metadata']
    require(all(meta.get(key)==bound[key] for key in ('name','namespace','uid')))
    require(type(meta.get('generation')) is int and meta['generation'] in (bound['generation'],bound['generation']+1))
    image(deployment['spec']['template'])
    require(type(replicasets) is list and type(pods) is list and 1<=len(pods)<=4)
    indexed={};successors=set()
    for rs in replicasets:
        meta=rs['metadata'];uid=meta['uid'];owner=controller(rs,'Deployment')
        require(token(uid) and uid not in indexed and meta.get('namespace')==bound['namespace'])
        require(owner['uid']==bound['uid'] and owner.get('name')==bound['name'])
        template=meta['labels']['pod-template-hash']
        require(token(template) and template==rs['spec']['template']['metadata']['labels']['pod-template-hash'])
        image(rs['spec']['template'])
        if uid==bound['initialReplicaSetUid']:require(template==bound['initialTemplateHash'])
        indexed[uid]=template
    result={};starting=[];seen=set()
    for pod in pods:
        meta=pod['metadata'];uid=meta['uid'];owner=controller(pod,'ReplicaSet');rsuid=owner['uid']
        require(token(uid) and uid not in seen and meta.get('namespace')==bound['namespace'] and rsuid in indexed)
        seen.add(uid)
        template=indexed[rsuid];require(meta['labels']['pod-template-hash']==template)
        image(pod);statuses=pod['status'].get('containerStatuses',[])
        if rsuid!=bound['initialReplicaSetUid']:
            require(template!=bound['initialTemplateHash']);successors.add((rsuid,template));require(len(successors)==1)
        no_runtime=(statuses==[] or (type(statuses) is list and len(statuses)==1 and type(statuses[0]) is dict and statuses[0].get('name')=='client' and not statuses[0].get('containerID') and not statuses[0].get('imageID')))
        if allow_starting and no_runtime:
            require(rsuid!=bound['initialReplicaSetUid'] and pod['status'].get('phase')=='Pending' and not _pod_ready(pod) and meta.get('deletionTimestamp') is None)
            starting.append(dict(name=meta['name'],uid=uid,deploymentUid=bound['uid'],replicaSetUid=rsuid,templateHash=template,containerId=None))
            continue
        require(type(statuses) is list and len(statuses)==1 and statuses[0]['name']=='client' and token(statuses[0].get('containerID')))
        runtime=statuses[0]['imageID']
        require(type(runtime) is str and (runtime==bound['imageDigest'] or runtime.endswith('@'+bound['imageDigest']) or runtime=='docker-pullable://'+bound['imageDigest']))
        result[uid]=dict(deploymentUid=bound['uid'],replicaSetUid=rsuid,templateHash=template,
                        imageDigest=bound['imageDigest'],terminating=meta.get('deletionTimestamp') is not None)
    return result,starting


def _pod_ready(pod):
    conditions=pod['status'].get('conditions',[])
    require(type(conditions) is list and all(type(row) is dict for row in conditions))
    rows=[row for row in conditions if row.get('type')=='Ready']
    require(len(rows)<=1 and (not rows or rows[0].get('status') in ('True','False','Unknown')))
    return bool(rows and rows[0]['status']=='True')


def native_replicasets(deployment):
    selector=deployment['spec']['selector']
    require(type(selector) is dict and set(selector)=={'matchLabels'} and type(selector['matchLabels']) is dict and selector['matchLabels'])
    labels=selector['matchLabels']
    require(all(type(key) is str and type(value) is str and re.fullmatch(r'[A-Za-z0-9_./-]{1,253}',key) and re.fullmatch(r'[A-Za-z0-9_.-]{1,63}',value) for key,value in labels.items()))
    return kubectl('get','replicasets','-l',','.join(key+'='+labels[key] for key in sorted(labels)),'-o','json')['items']


def capture_frame(inventory_path,final_snapshots=None,*,native_context=None):
    if final_snapshots is None:final_snapshots={}
    stamp=utc();pods=kubectl('get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json')['items']
    read_errors={};starting=[]
    unavailable=(ValueError,KeyError,TypeError,IndexError,OverflowError,OSError,RuntimeError,subprocess.TimeoutExpired)
    def one(p):
        try:return read_pod(p,final_snapshots)
        except unavailable as exc:
            if native_context is not None:read_errors[p['metadata']['uid']]=exc
            return None
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
        if native_context is not None:
            owners,pending=_native_owner_records(deployment,native_replicasets(deployment),pods,native_context,allow_starting=True)
            starting=list(pending);starting_uids={row['uid'] for row in starting}
            present={row['uid'] for row in rows}
            initial={row['uid'] for row in native_context['initialRoster']}
            for pod in pods:
                uid=pod['metadata']['uid']
                if uid in present:continue
                if uid in starting_uids:continue
                exc=read_errors.get(uid)
                # Only a transport-level refusal/timeout before first evidence is
                # startup. HTTP errors and malformed returned bodies fail closed.
                transport=isinstance(exc,(ConnectionError,TimeoutError)) or (isinstance(exc,urllib.error.URLError) and not isinstance(exc,urllib.error.HTTPError) and isinstance(exc.reason,(ConnectionError,TimeoutError)))
                require(transport and uid not in initial and not _pod_ready(pod) and not owners[uid]['terminating'])
                owner=owners[uid]
                starting.append(dict(name=pod['metadata']['name'],uid=uid,deploymentUid=owner['deploymentUid'],replicaSetUid=owner['replicaSetUid'],templateHash=owner['templateHash'],containerId=container_id(pod)))
            require(len(rows)+len(starting)==len(pods))
            for row in rows:row['ownership']=owners[row['uid']]
            expected=deployment['spec']['replicas']
            if starting:require(sum(row['snapshot'].get('final') is False for row in rows)>=2)
    except unavailable:
        if native_context is not None:expected=0
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
    result={'at':stamp,'expectedReplicas':expected,'pods':rows,'deployment':deployment_identity,'inventory':inventory,'metrics':metrics}
    if native_context is not None:result['startingPods']=sorted(starting,key=lambda row:row['uid'])
    return result
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
    native=w.native_cohort is not None;terminal=None;operation=None;operation_raw=None
    if native:
        from pgcat_native_evidence import read_terminal_clients, validate_operation_binding
    def native_terminal():
        require(read_private(w.progress_fd,'native-operation-binding.json')==operation_raw)
        result=read_terminal_clients(output,w.progress_context,operation)
        if terminal is not None:require(result==terminal)
        return result
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
                try:
                    frame=capture_frame(inventory,final_snapshots,native_context=w.progress_context) if native else capture_frame(inventory,final_snapshots)
                finally:capture_ms=round((time.monotonic()-read_start)*1000,3)
                if boundary_dir is not None and request is None and (boundary_dir/'request.json').exists():
                    request_raw=boundary_read('request.json') if boundary_fd is not None else None
                    request=decode_private(request_raw) if request_raw is not None else read_json(boundary_dir/'request.json')
                    if set(request)!={'requestId','roster'} or not isinstance(request['requestId'],str) or not request['requestId'] or len(request['requestId'])>256:raise ValueError('invalid boundary')
                    candidate=validate_roster(request['roster'])
                    current=validate_roster([dict(name=p['name'],uid=p['uid'],containerId=p['containerId'],processId=p['snapshot']['sample']['processId']) for p in frame['pods']])
                    if current!=candidate:raise ValueError('boundary identity mismatch')
                    if native:
                        operation_raw=read_private(w.progress_fd,'native-operation-binding.json')
                        operation=validate_operation_binding(decode_private(operation_raw),w.progress_context)
                        terminal=native_terminal()
                        require(candidate==terminal['roster'])
                    elif progress_context is not None:require(candidate==w.progress_context['originalRoster'])
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
                        originalRosterSha256=sha(canonical(w.progress_context.get('originalRoster'))),startedMonotonic=settle_start,
                        deadlineMonotonic=settle_start+5,firstSettlingFrame=w.progress['latest'])
                    if native:
                        boundary_start.pop('originalRosterSha256')
                        boundary_start.update(schemaVersion=2,terminalSha256=terminal['terminalSha256'],executionKind=w.progress_context['executionKind'])
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
                                require(not any(w.progress['flags'][key] for key in ('identityChanged','skipped','invalidEvidence')))
                                predecessor_finals=[]
                                if native:
                                    native_terminal()
                                    snapshot=w.native_cohort.snapshot()
                                    require(len(snapshot['cohort'])==4 and all(row['finalFrame'] is not None for row in snapshot['cohort']))
                                    initial={(row['uid'],row['processId']) for row in w.progress_context['initialRoster']}
                                    require(set(w.final_refs)==bound|initial)
                                    sealed=decode_private(read_private(w.progress_fd,'terminal-clients.json'))
                                    predecessor_finals=sealed['predecessorFinals']
                                    require({(row['uid'],row['processId']) for row in predecessor_finals}==initial)
                                    for row in predecessor_finals:
                                        require(w.final_refs[(row['uid'],row['processId'])]==row['frame'])
                                        read_frame(w.progress_fd,row['frame'])
                                else:require(set(w.final_refs)==bound)
                                finals=[]
                                for uid,pid in sorted(bound):
                                    ref=w.final_refs[(uid,pid)];actual=read_frame(w.progress_fd,ref)
                                    pod=next(p for p in actual['pods'] if (p['uid'],p['snapshot']['sample']['processId'])==(uid,pid))
                                    snap=pod['snapshot'];sample=snap['sample']
                                    require(snap['final'] is True and type(snap['connected']) is int and snap['connected']==0 and sample['Scheduled']==sample['Attempts']+sample['Skipped'] and sample['Skipped']==0)
                                    finals.append(dict(uid=uid,processId=pid,frame=ref))
                                durable=time.monotonic();require(durable<=boundary_start['deadlineMonotonic'])
                                completion=dict(schemaVersion=1,bindingSha256=w.progress['bindingSha256'],requestSha256=sha(request_raw),requestId=request['requestId'],
                                    startSha256=sha(boundary_start_raw),receiptSha256=sha(receipt_raw),finalDurableMonotonic=durable,finals=finals,lastFrame=w.progress['latest'])
                                if native:completion.update(schemaVersion=2,terminalSha256=terminal['terminalSha256'],executionKind=w.progress_context['executionKind'],predecessorFinals=predecessor_finals)
                                boundary_write('boundary-completed.json',completion)
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
