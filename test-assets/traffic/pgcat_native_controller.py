"""Source-bound native startup kernels and reserved, release-closed entrypoints.

Offline readers exercise the same immutable evidence kernels. No injected
adapter, environment variable, or command-line option grants runtime admission.
C must qualify loaded Gateway configuration and complete closure before release.
"""
import hashlib
import os
from pathlib import Path
import stat
import subprocess
import sys
import time

MODE = 'full-replacement-direct-v2'


def check(value):
    if not value:raise ValueError('native startup')


def collector_sources():
    from pgcat_collector_control import SOURCES
    return dict(SOURCES,nativeEvidence='pgcat_native_evidence.py')


def _require_verified():
    from pgcat_source_bootstrap import require_verified
    from pgcat_lane_protocol import Refused
    try:require_verified(__name__)
    except (ValueError,KeyError):raise Refused('source') from None


def _closed(action=None):
    from pgcat_lane_protocol import _release_admission
    _release_admission(MODE,action)


def credential_identity(path):
    """Bind bytes to the opened private regular inode, never just its name."""
    from pgcat_native_ingress import _identity
    path=Path(path);check(path.is_absolute() and '..' not in path.parts)
    raw,identity=_identity(path,65536)
    check(identity['mode']==0o600 and identity['nlink']==1)
    return dict(path=str(path),**{k:identity[k] for k in ('device','inode','uid','mode','size','sha256')})


def verify_credential(identity):
    check(type(identity) is dict and set(identity)=={'path','device','inode','uid','mode','size','sha256'})
    check(credential_identity(identity['path'])==identity)


def _write_bytes(path,raw):
    from segmented_capture import open_directory,check_directory
    check(type(raw) is bytes and len(raw)<=32*1024*1024)
    path=Path(path);fd=open_directory(path.parent)
    try:
        item=os.open(path.name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=fd)
        try:
            os.fchmod(item,0o600);remaining=raw
            while remaining:
                size=os.write(item,remaining);check(size>0);remaining=remaining[size:]
            os.fsync(item);created=os.fstat(item)
        finally:os.close(item)
        os.fsync(fd);check_directory(fd,path.parent)
        from pgcat_native_ingress import _identity
        actual,identity=_identity(path,32*1024*1024)
        check(actual==raw and (identity['device'],identity['inode'])==(created.st_dev,created.st_ino))
        return identity
    finally:os.close(fd)


def owned_process(child,probe):
    """Only an actual retained Popen can establish the spawned PID."""
    from pgcat_collector_control import valid_identity
    check(isinstance(child,subprocess.Popen) and child.poll() is None)
    identity=probe(child.pid);valid_identity(identity)
    check(identity['pid']==child.pid and child.poll() is None and probe(child.pid)==identity)
    return dict(identity)


def collect_baseline(run_id,expected,*,provider_read,kube_read,read_pod,read_metrics,now):
    """Derive the pre-feed baseline from bounded raw observations, no context."""
    import re
    from types import SimpleNamespace
    from pgcat_native_ingress import (_Calls,_gateway_config,_baseline,_fleet,identifier,private_ip,deployment,detached,number)
    from pgcat_lane_protocol import target_valid
    from pgcat_rehearsal_contract import _read_configmap
    from pgcat_pending_proof_worker import complete_metrics
    from worker_capture import _native_owner_records_for_deployment,_pod_ready
    from finish_clients import validate_roster
    from capture_window import clean_client_pods,seconds
    from segmented_capture import canonical,sha
    identifier(run_id);target_valid(expected['target']);check(expected['executionKind'] in ('native-replacement','disabled-proof'))
    calls=_Calls();started=now();check(number(started));ids=expected['target']['originalInstanceIds']
    provider=calls.call(provider_read)
    check(set(provider)=={'schemaVersion','observedAt','privateAddresses','desired','members','healthyRegistered','memberStates','backendStates'} and type(provider['schemaVersion']) is int and provider['schemaVersion']==2)
    check(0<=now()-provider['observedAt']<30)
    fleet={k:provider[k] for k in ('desired','members','healthyRegistered','memberStates','backendStates')};_fleet(fleet,ids)
    addresses=provider['privateAddresses'];check(type(addresses) is dict and sorted(addresses)==ids and len(set(addresses.values()))==2)
    for address in addresses.values():private_ip(address)
    cm=_read_configmap(lambda *args:calls.call(kube_read,'ConfigMap','raptor-test','pgcat-provider-inventory'),addresses)
    dep=calls.call(kube_read,'Deployment','raptor-test','rdev-test-client-client')
    pods=calls.call(kube_read,'Pod','raptor-test',None)['items']
    rss=calls.call(kube_read,'ReplicaSet','raptor-test',None)['items']
    meta=dep['metadata'];check(not meta.get('deletionTimestamp') and type(dep['spec']['replicas']) is int and dep['spec']['replicas']==2)
    check(dep['status']['observedGeneration']==meta['generation'] and all(type(dep['status'][k]) is int and dep['status'][k]==2 for k in ('replicas','updatedReplicas','readyReplicas','availableReplicas')))
    check(type(pods) is list and len(pods)==2)
    rs_ids=set();templates=set();digests=set();readings=[]
    for pod in pods:
        pm=pod['metadata'];status=pod['status'];check(not pm.get('deletionTimestamp') and status['phase']=='Running' and _pod_ready(pod))
        refs=[r for r in pm.get('ownerReferences',[]) if r.get('controller') is True]
        check(len(refs)==1 and refs[0]['kind']=='ReplicaSet');rs_ids.add(refs[0]['uid']);templates.add(pm['labels']['pod-template-hash'])
        containers=status['containerStatuses'];check(len(containers)==1 and containers[0]['ready'] is True and 'running' in containers[0]['state'])
        runtime=containers[0]['imageID'];match=re.search(r'(sha256:[0-9a-f]{64})$',runtime);check(match is not None);digests.add(match.group(1))
        reading=calls.call(read_pod,pod)
        check(reading['uid']==pm['uid'] and reading['name']==pm['name'] and reading['containerId']==containers[0]['containerID'])
        readings.append(reading)
    check(len(rs_ids)==len(templates)==len(digests)==1)
    bound={k:meta[k] for k in ('name','namespace','uid','generation')}
    bound.update(initialReplicaSetUid=next(iter(rs_ids)),initialTemplateHash=next(iter(templates)),imageDigest=next(iter(digests)))
    deployment(bound);check(bound==expected['application'])
    owners,starting_pods=_native_owner_records_for_deployment(dep,rss,pods,bound)
    check(not starting_pods and len(owners)==2 and all(not r['terminating'] for r in owners.values()))
    _,complete=clean_client_pods(readings);check(complete)
    for reading in readings:
        snapshot=reading['snapshot'];sample=snapshot['sample']
        check(reading['ready'] is True and snapshot['final'] is False and snapshot['truncated'] is False and snapshot['connected']>0)
        check(0<=now()-seconds(sample['at'])<=3 and sample['Attempts']>0 and sample['Success']>0)
        check(all(sample[k]==0 for k in ('Failure','Timeout','Ambiguous','Skipped')))
    roster=validate_roster([dict(name=r['name'],uid=r['uid'],containerId=r['containerId'],processId=r['snapshot']['sample']['processId']) for r in readings])
    service=calls.call(kube_read,'Service','raptor-test','rdev-test-metrics-metrics')
    check(service['metadata']['name']=='rdev-test-metrics-metrics' and service['metadata']['namespace']=='raptor-test')
    private_ip(service['spec']['clusterIP']);check(any(type(p.get('port')) is int and p['port']==9090 for p in service['spec']['ports']))
    metrics=calls.call(read_metrics,'http://'+service['spec']['clusterIP']+':9090')
    metrics_at=now()
    try:complete_metrics(dict(metrics=metrics,at=metrics_at),ids,metrics_at)
    except Exception:raise ValueError('native baseline metrics') from None
    groups=sorted({(r['node'],r['pool'],r['user']) for r in metrics['raw']});check(1<=len(groups)<=256)
    for label in ('gateway','openapi'):
        row=expected['serviceBindings'][label]
        svc=calls.call(kube_read,'Service','raptor-system',row['serviceName'])
        check(svc['metadata']['uid']==row['serviceUid'] and svc['spec']['type']=='ClusterIP' and svc['spec']['clusterIP']==row['clusterIP'])
        check(any(p.get('port')==row['port'] and p.get('protocol','TCP')=='TCP' for p in svc['spec']['ports']))
        actual=calls.call(kube_read,'Deployment','raptor-system',row['deploymentName'])
        check(actual['metadata']['uid']==row['deploymentUid'] and any(c['name']==row['containerName'] for c in actual['spec']['template']['spec']['containers']))
    config=_gateway_config(SimpleNamespace(plan=expected),calls,kube_read);check(config['replacementEnabled'] is False)
    latest=calls.call(provider_read);check({k:latest[k] for k in provider if k!='observedAt'}=={k:provider[k] for k in provider if k!='observedAt'} and 0<=now()-latest['observedAt']<30)
    check(calls.call(kube_read,'Deployment','raptor-test','rdev-test-client-client')==dep)
    check(calls.call(kube_read,'Pod','raptor-test',None)['items']==pods)
    ended=now();check(number(ended) and 0<=ended-started<30)
    check(all(0<=ended-seconds(r['snapshot']['sample']['at'])<=3 for r in readings))
    check(0<=ended-latest['observedAt']<30)
    try:complete_metrics(dict(metrics=metrics,at=ended),ids,ended)
    except Exception:raise ValueError('native baseline metrics') from None
    result=dict(schemaVersion=1,mode=MODE,executionKind=expected['executionKind'],runId=run_id,observedAt=ended,
        targetSha256=sha(canonical(expected['target'])),originals=[dict(id=n,address=addresses[n]) for n in ids],
        configMap=cm,roster=roster,metricGroups=[dict(node=n,pool=p,user=u) for n,p,u in groups],deployment=bound,fleet=fleet,
        gateway=dict(deploymentUid=expected['serviceBindings']['gateway']['deploymentUid'],replacementEnabled=False))
    _baseline(result,dict(expected,runId=run_id));return detached(result)


def prepare_records(run_dir,template,run_id,**readers):
    """Collect and seal the baseline before hashing/sealing the immutable plan."""
    from pgcat_native_ingress import validate_plan,identifier
    from segmented_capture import canonical,sha
    run=Path(run_dir);check(run.is_absolute() and run.name=='run');identifier(run_id)
    check('runId' not in template and 'baselineSha256' not in template)
    sources=verify_source_inventory(run,template['sourceHashes'])
    baseline=collect_baseline(run_id,template,**readers)
    plan=validate_plan(dict(template,runId=run_id,baselineSha256=sha(canonical(baseline))))
    check(verify_source_inventory(run,template['sourceHashes'])==sources)
    baseline_file=_write_bytes(run/'native-baseline.json',canonical(baseline))
    plan_file=_write_bytes(run/'native-plan.json',canonical(plan))
    return dict(plan=plan,baseline=baseline,planFile=plan_file,baselineFile=baseline_file)


def validate_start_plan(bindings,plan):
    from pgcat_native_ingress import validate_plan
    from pgcat_lane_protocol import digest
    plan=validate_plan(plan)
    check(bindings['mode']==MODE and bindings['executionKind']==plan['executionKind'])
    check(bindings['target']==plan['target'] and bindings['sourceHashes']==plan['sourceHashes'])
    return plan


def capture_budget(plan,at):
    from pgcat_native_ingress import validate_plan,instant,number
    plan=validate_plan(plan);check(number(at))
    limits=plan['deadline'];check(at<instant(limits['mutationCutoff']))
    # Actual 120-second final hold plus bounded assessment, restore and cleanup.
    budget=min(limits['captureMaxSeconds'],int(instant(limits['closureDeadline'])-at)-300)
    check(75<=budget<=5400);return budget


def verify_source_inventory(run_dir,hashes):
    from pgcat_source_bootstrap import NATIVE_SOURCES
    from pgcat_native_ingress import _identity
    check(type(hashes) is dict and set(hashes)==set(NATIVE_SOURCES))
    identities={}
    for key,name in NATIVE_SOURCES.items():
        raw,identity=_identity(Path(run_dir)/name,262144)
        check(identity['sha256']==hashes[key]);identities[key]=identity
    return identities


def seal_capture_context(run_dir,hashes,*,prepared,probe_process,live_clients,now):
    """Called by the actual capture child after guardian records its PID tuple."""
    from pgcat_native_ingress import _identity,validate_plan,_baseline,_fleet
    from pgcat_native_evidence import validate_context
    from pgcat_collector_control import valid_identity
    from pgcat_rehearsal_contract import fresh_provider_source
    from worker_capture import _native_owner_records_for_deployment
    from segmented_capture import decode_private,canonical,sha
    run=Path(run_dir);check(run.is_absolute() and run.name=='run')
    sources=verify_source_inventory(run,hashes)
    plan_raw,plan_file=_identity(run/'native-plan.json');plan=validate_plan(decode_private(plan_raw));check(plan['sourceHashes']==hashes)
    verify_prepared(run,plan,prepared)
    baseline_raw,baseline_file=_identity(run/'native-baseline.json');baseline=decode_private(baseline_raw);_baseline(baseline,plan)
    check(plan['baselineSha256']==sha(baseline_raw))
    identity=probe_process(os.getpid());valid_identity(identity);check(identity['pid']==os.getpid())
    recorded,_=_identity(run/'collector-job'/'identity.json');check(decode_private(recorded)==identity)
    provider_raw,_=_identity(run.parent/'contract'/'provider-source'/'run-context.json');provider=decode_private(provider_raw)
    source=fresh_provider_source(run.parent/'contract',plan['runId'],run/'provider-inventory.json',require_fresh=False,mode=MODE)
    check(source['providerRunContextSha256']==sha(provider_raw) and 0<=now()-source['observedAt']<30)
    check(source['targetSha256']==baseline['targetSha256'] and source['privateAddresses']=={r['id']:r['address'] for r in baseline['originals']})
    raw,_=_identity(run/'provider-inventory.json');check(sha(raw)==source['inventoryRawSha256']);inventory=decode_private(raw)
    _fleet({k:inventory[k] for k in baseline['fleet']},plan['target']['originalInstanceIds'])
    # The immutable first publication must independently match the pre-feed addresses.
    first_raw,_=_identity(run.parent/'contract'/'provider-source'/'000001.json');first=decode_private(first_raw)
    check(first['sequence']==1 and first['runId']==plan['runId'] and first['feedSession']==source['feedSession'] and first['targetSha256']==baseline['targetSha256'] and first['privateAddresses']==source['privateAddresses'])
    rc=provider['runContext'];check(rc['bundleSha256']==sha(canonical(hashes)) and rc['runId']==plan['runId'])
    live=live_clients();check(set(live)=={'deployment','replicaSets','pods','roster'})
    owners,starting=_native_owner_records_for_deployment(live['deployment'],live['replicaSets'],live['pods'],baseline['deployment'])
    check(not starting and len(owners)==2 and sorted(live['roster'],key=lambda r:r['uid'])==baseline['roster'])
    check(live['deployment']['metadata']['generation']==baseline['deployment']['generation'])
    result=validate_context(dict(schemaVersion=2,mode=MODE,executionKind=plan['executionKind'],runId=plan['runId'],
        feedSession=source['feedSession'],providerRunContextSha256=sha(provider_raw),bundleSha256=rc['bundleSha256'],
        sourceHashes={k:hashes[k] for k in collector_sources()},baselineSha256=sha(baseline_raw),planSha256=sha(plan_raw),
        initialRoster=baseline['roster'],deployment=baseline['deployment'],collectorIdentity=identity))
    check(verify_source_inventory(run,hashes)==sources and probe_process(os.getpid())==identity)
    check(_identity(run/'native-plan.json')==(plan_raw,plan_file) and _identity(run/'native-baseline.json')==(baseline_raw,baseline_file))
    verify_prepared(run,plan,prepared)
    _write_bytes(run/'native-context.json',canonical(result));return result


def seal_ready(run_dir,*,prepared,guardian_identity,probe_process,live_clients,now):
    """Seal exactly the first two committed, active baseline frames.

    This pure evidence kernel has no transport or runtime authority. Its caller
    must anchor the returned file identity in its completed Journal action.
    """
    from pgcat_native_ingress import (_identity,validate_plan,_baseline,_runtime,_ready_pair,_process)
    from pgcat_native_evidence import validate_context,_replay
    from segmented_capture import decode_private,canonical,sha,read_progress,open_directory
    from worker_capture import native_owner_evidence
    from pgcat_rehearsal_contract import fresh_provider_source
    run=Path(run_dir);check(run.is_absolute() and run.name=='run')
    paths=dict(plan=run/'native-plan.json',baseline=run/'native-baseline.json',context=run/'native-context.json',
        feedBinding=run/'native-feed-binding.json',providerRunContext=run.parent/'contract'/'provider-source'/'run-context.json',
        progressBinding=run/'native-capture'/'progress-binding.json',gatewayRuntime=run/'native-gateway-runtime.json')
    files={k:_identity(p) for k,p in paths.items()};values={k:decode_private(raw) for k,(raw,_) in files.items()}
    plan=validate_plan(values['plan']);baseline=values['baseline'];_baseline(baseline,plan)
    verify_prepared(run,plan,prepared)
    source_inventory=verify_source_inventory(run,plan['sourceHashes'])
    context=validate_context(values['context']);at=now()
    check(files['plan'][0]==canonical(plan) and plan['baselineSha256']==sha(files['baseline'][0]))
    check(context['planSha256']==sha(files['plan'][0]) and context['baselineSha256']==plan['baselineSha256'])
    check(all(context[k]==plan[k] for k in ('runId','mode','executionKind')))
    check(context['deployment']==baseline['deployment'] and context['initialRoster']==baseline['roster'])
    check(context['sourceHashes']=={k:plan['sourceHashes'][k] for k in collector_sources()})
    provider=values['providerRunContext'];rc=provider['runContext']
    check(set(provider)=={'schemaVersion','runContext','feedSession'} and provider['schemaVersion']==1)
    check(set(rc)=={'schemaVersion','runId','supervisorId','targetSha256','bundleSha256','nonce'} and rc['schemaVersion']==1)
    check(rc['runId']==plan['runId'] and rc['targetSha256']==baseline['targetSha256'])
    check(context['providerRunContextSha256']==sha(files['providerRunContext'][0]))
    check(context['feedSession']==provider['feedSession'] and context['bundleSha256']==rc['bundleSha256']==sha(canonical(plan['sourceHashes'])))
    feed=values['feedBinding'];check(set(feed)=={'runId','feedId','pid','bindingSha256','argvSha256'} and feed['runId']==plan['runId'])
    from pgcat_native_ingress import identifier,integer,hashed,number
    identifier(feed['feedId']);check(integer(feed['pid'],1) and hashed(feed['bindingSha256']) and hashed(feed['argvSha256']))
    check(number(at) and baseline['observedAt']<=at)
    _runtime(values['gatewayRuntime'],plan,at)
    _process(guardian_identity);_process(context['collectorIdentity'])
    check(guardian_identity['pid']!=context['collectorIdentity']['pid'])
    for identity in (guardian_identity,context['collectorIdentity']):check(probe_process(identity['pid'])==identity)
    progress,previous,latest=read_progress(run/'native-capture',context)
    check(progress['committedFrames']==2 and not any(progress['flags'].values()) and progress['maxGapSeconds']<=3)
    _ready_pair([previous,latest],baseline,context,at)
    from capture_window import seconds
    check(0<seconds(latest['at'])-seconds(previous['at'])<=3)
    fd=open_directory(run/'native-capture')
    try:
        info=os.fstat(fd)
        check(values['progressBinding']==dict(context,captureDirectoryIdentity=dict(device=info.st_dev,inode=info.st_ino)))
        snapshot,count,_=_replay(fd,context,progress['latest'])
    finally:os.close(fd)
    check(snapshot==progress['nativeCohort'] and count==2)
    live=live_clients();check(set(live)=={'deployment','replicaSets','pods','roster'})
    owners=native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context)
    check(sorted(live['roster'],key=lambda r:r['uid'])==baseline['roster'])
    check(owners=={p['uid']:p['ownership'] for p in latest['pods']})
    source=fresh_provider_source(run.parent/'contract',plan['runId'],run/'provider-inventory.json',require_fresh=False,mode=MODE)
    check(source['feedSession']==context['feedSession'] and source['providerRunContextSha256']==context['providerRunContextSha256'] and 0<=at-source['observedAt']<30)
    check(source['privateAddresses']=={r['id']:r['address'] for r in baseline['originals']})
    check(all(_identity(path)==files[key] for key,path in paths.items()))
    current,_,_=read_progress(run/'native-capture',context);check(current==progress)
    for identity in (guardian_identity,context['collectorIdentity']):check(probe_process(identity['pid'])==identity)
    ready=dict(schemaVersion=1,mode=MODE,executionKind=plan['executionKind'],runId=plan['runId'],
        planSha256=context['planSha256'],baselineSha256=plan['baselineSha256'],contextSha256=sha(files['context'][0]),
        progressBindingSha256=sha(files['progressBinding'][0]),feedBindingSha256=sha(files['feedBinding'][0]),
        feedSession=context['feedSession'],collectorIdentity=context['collectorIdentity'],guardianIdentity=guardian_identity,
        sources={k:identity for k,(_,identity) in files.items()},frames={k:progress[k] for k in ('previous','latest')},readyAt=at)
    check(verify_source_inventory(run,plan['sourceHashes'])==source_inventory)
    verify_prepared(run,plan,prepared)
    _write_bytes(run/'native-ready.json',canonical(ready))
    return ready


def mirror_operation_binding(run_dir):
    from pgcat_native_ingress import _identity
    run=Path(run_dir);source=run/'native-operation-binding.json'
    raw,identity=_identity(source)
    mirror=_write_bytes(run/'native-capture'/'native-operation-binding.json',raw)
    check(_identity(source)==(raw,identity))
    return mirror


def collect_ingress(run_dir,selector,*,gate,kube_read,http_get,now):
    """A1 process-private collection → seal → conditional final binding."""
    from pgcat_native_ingress import read_ingress,seal_ingress,seal_operation_binding,_identity
    run=Path(run_dir)
    projection=read_ingress(run,selector,gate=gate,kube_read=kube_read,http_get=http_get,now=now)
    sealed=seal_ingress(run,projection)
    record=sealed['record'];execution=record['execution']
    result=dict(status='OBSERVED',ingressSha256=sealed['ingressSha256'],ingressFile=sealed['ingressFile'])
    final=(record['executionKind']=='disabled-proof' or
        (execution is not None and execution['status']=='completed' and execution.get('result') is not None and execution['recoveryNeeded'] is False))
    if final:
        binding=seal_operation_binding(run,run/sealed['ingressFile']['basename'])
        _,identity=_identity(run/'native-operation-binding.json')
        mirror=mirror_operation_binding(run)
        result.update(status='BOUND',operationBinding=binding,operationFile=dict(basename='native-operation-binding.json',identity=identity),operationMirror=mirror)
    return result


class FixedHTTP:
    """Numeric service origins, GET-only, no proxies/redirects, bounded I/O.

    Credentials exist only in this worker's stack and Authorization header.
    Neither response error bodies nor transport exception chains escape.
    """
    def __init__(self,plan):
        from pgcat_native_ingress import validate_plan
        plan=validate_plan(plan)
        self.origins={'http://%s:%d'%(row['clusterIP'],row['port']):label
            for label,row in plan['serviceBindings'].items() if label!='namespace'}
        self.deadline=time.monotonic()+30.

    def __call__(self,origin,path,credentials):
        try:return self._get(origin,path,credentials)
        except Exception:pass
        raise ValueError('native HTTP')

    def _get(self,origin,path,credentials):
        import base64,http.client,re,signal,threading,urllib.parse
        from pgcat_native_ingress import identifier,MAX_RESPONSE
        check(origin in self.origins and type(path) is str)
        label=self.origins[origin]
        request=re.fullmatch(r'/v1/requests/([0-9a-f-]{36})(/context|/control|/events\?after=(?:0|[1-9][0-9]{0,15}))?',path)
        approval=re.fullmatch(r'/v1/approvals/([0-9a-f-]{36})',path)
        if request:
            identifier(request.group(1));suffix=request.group(2)
            check((label=='gateway' and (suffix is None or suffix.startswith('/events?'))) or
                  (label=='openapi' and suffix in ('/context','/control')))
        elif approval:
            identifier(approval.group(1));check(label=='openapi')
        else:raise ValueError('native HTTP')
        check(type(credentials) is tuple and len(credentials)==2 and all(type(v) is str and 0<len(v)<=4096 and '\r' not in v and '\n' not in v and '\x00' not in v for v in credentials) and ':' not in credentials[0])
        check(threading.current_thread() is threading.main_thread())
        started=time.monotonic();remaining=min(5.,self.deadline-started);check(remaining>0)
        old_handler=signal.getsignal(signal.SIGALRM);old_timer=signal.getitimer(signal.ITIMER_REAL)
        check(old_timer[1]==0.)
        def expired(signum,frame):raise TimeoutError('native HTTP')
        signal.signal(signal.SIGALRM,expired)
        signal.setitimer(signal.ITIMER_REAL,min(remaining,old_timer[0]) if old_timer[0]>0 else remaining)
        connection=None
        try:
            parsed=urllib.parse.urlsplit(origin)
            connection=http.client.HTTPConnection(parsed.hostname,parsed.port,timeout=remaining)
            auth=base64.b64encode((credentials[0]+':'+credentials[1]).encode('utf-8')).decode('ascii')
            connection.request('GET',path,headers={'Authorization':'Basic '+auth,'Accept':'application/json','Connection':'close'})
            response=connection.getresponse();check(response.status==200)
            # Reject compression and ambiguous framing before reading any body.
            headers=response.getheaders()
            lengths=[v for k,v in headers if k.lower()=='content-length']
            check(len(lengths)<=1 and not any(k.lower()=='content-encoding' and v.lower()!='identity' for k,v in headers))
            if lengths:check(re.fullmatch(r'0|[1-9][0-9]{0,8}',lengths[0]) and int(lengths[0])<=MAX_RESPONSE)
            chunks=[];size=0
            while True:
                budget=min(5.,self.deadline-time.monotonic(),remaining-(time.monotonic()-started));check(budget>0)
                if connection.sock is not None:connection.sock.settimeout(budget)
                chunk=response.read1(min(65536,MAX_RESPONSE+1-size))
                if not chunk:break
                size+=len(chunk);check(size<=MAX_RESPONSE);chunks.append(chunk)
            check(time.monotonic()-started<remaining)
            return b''.join(chunks)
        finally:
            if connection is not None:connection.close()
            signal.setitimer(signal.ITIMER_REAL,0);signal.signal(signal.SIGALRM,old_handler)
            if old_timer[0]>0:signal.setitimer(signal.ITIMER_REAL,max(.000001,old_timer[0]-(time.monotonic()-started)))


class FixedWorkerReads:
    """Fixed worker-only Kubernetes/read adapters; Secret bytes stay in memory."""
    def __init__(self,plan,kubeconfig,*,run_id=None,credential=None):
        from pgcat_native_ingress import validate_plan,identifier
        self.run_id=plan.get('runId') if run_id is None else run_id
        identifier(self.run_id);check('runId' not in plan or plan['runId']==self.run_id)
        self.plan=validate_plan(plan) if 'runId' in plan else validate_template(plan)
        if 'runId' in plan:
            check(credential is not None and credential['path']==str(kubeconfig))
            verify_credential(credential);self.credential=dict(credential)
        else:
            check(credential is None);self.credential=credential_identity(kubeconfig)
        self.deadline=time.monotonic()+30.;self.secret_names={'gateway-live-config'}

    def read(self,kind,namespace,name=None):
        try:return self._read(kind,namespace,name)
        except Exception:pass
        raise ValueError('native worker read')

    def _read(self,kind,namespace,name):
        from segmented_capture import decode_private
        from pgcat_native_ingress import token
        fixed={('Deployment','raptor-test','rdev-test-client-client'),('ConfigMap','raptor-test','pgcat-provider-inventory'),
            ('Service','raptor-test','rdev-test-metrics-metrics'),('Pod','raptor-test',None),('ReplicaSet','raptor-test',None)}
        for label in ('gateway','openapi'):
            row=self.plan['serviceBindings'][label]
            fixed.add(('Deployment','raptor-system',row['deploymentName']));fixed.add(('Service','raptor-system',row['serviceName']))
        check((kind,namespace,name) in fixed or (kind=='Secret' and namespace=='raptor-system' and name in self.secret_names))
        verify_credential(self.credential);remaining=min(3.,self.deadline-time.monotonic());check(remaining>0)
        args=['kubectl','--kubeconfig',self.credential['path'],'-n',namespace,'get',kind.lower()]
        if name is not None:check(token(name));args.append(name)
        else:args+=['-l','app.kubernetes.io/name=rdev-test-client-client']
        args+=['-o','json','--request-timeout=2s']
        result=subprocess.run(args,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=remaining,
            env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'},close_fds=True)
        check(result.returncode==0 and len(result.stdout)<=2*1024*1024 and time.monotonic()<self.deadline)
        value=decode_private(result.stdout);verify_credential(self.credential)
        if kind=='Deployment' and namespace=='raptor-system':
            rows=[r for label,r in self.plan['serviceBindings'].items() if label!='namespace' and r['deploymentName']==name]
            check(len(rows)==1 and value['metadata']['uid']==rows[0]['deploymentUid'])
            containers=[c for c in value['spec']['template']['spec']['containers'] if c['name']==rows[0]['containerName']];check(len(containers)==1)
            for item in containers[0].get('envFrom',[]):
                secret=item.get('secretRef',{}).get('name')
                if secret is not None:check(token(secret));self.secret_names.add(secret)
            for item in containers[0].get('env',[]):
                if item.get('name') in ('SERVICE_USERNAME','SERVICE_PASSWORD'):
                    secret=item.get('valueFrom',{}).get('secretKeyRef',{}).get('name')
                    if secret is not None:check(token(secret));self.secret_names.add(secret)
        return value

    def live_clients(self):
        from worker_capture import read_pod
        from finish_clients import validate_roster
        dep=self.read('Deployment','raptor-test','rdev-test-client-client')
        rss=self.read('ReplicaSet','raptor-test')['items'];pods=self.read('Pod','raptor-test')['items']
        rows=[]
        from worker_capture import _native_owner_records_for_deployment,_pod_ready
        owners,starting=_native_owner_records_for_deployment(dep,rss,pods,self.plan['application'],allow_starting=True)
        pending={r['uid'] for r in starting}
        for p in pods:
            uid=p['metadata']['uid']
            if uid in pending:continue
            try:rows.append(read_pod(p,{},deadline=self.deadline))
            except (ConnectionError,TimeoutError):
                check(owners[uid]['replicaSetUid']!=self.plan['application']['initialReplicaSetUid'] and not owners[uid]['terminating'] and not _pod_ready(p))
        roster=[dict(name=r['name'],uid=r['uid'],containerId=r['containerId'],processId=r['snapshot']['sample']['processId']) for r in rows]
        return dict(deployment=dep,replicaSets=rss,pods=pods,roster=roster)

    def restart_patch(self,namespace,name,patch):
        _closed('native-disabled-restart');_require_verified()
        from pgcat_native_ingress import exact,instant
        from segmented_capture import canonical,decode_private
        check(self.plan['executionKind']=='disabled-proof' and namespace=='raptor-test' and name=='rdev-test-client-client')
        exact(patch,{'metadata','spec'});exact(patch['metadata'],{'uid','resourceVersion'})
        exact(patch['spec'],{'template'});exact(patch['spec']['template'],{'metadata'});exact(patch['spec']['template']['metadata'],{'annotations'})
        annotations=patch['spec']['template']['metadata']['annotations'];exact(annotations,{'kubectl.kubernetes.io/restartedAt'});instant(annotations['kubectl.kubernetes.io/restartedAt'])
        current=self.read('Deployment',namespace,name)
        check(patch['metadata']=={k:current['metadata'][k] for k in ('uid','resourceVersion')} and patch['metadata']['uid']==self.plan['application']['uid'])
        self.gateway_disabled();verify_credential(self.credential)
        remaining=min(3.,self.deadline-time.monotonic());check(remaining>0)
        result=subprocess.run(['kubectl','--kubeconfig',self.credential['path'],'-n',namespace,'patch','deployment',name,'--type=merge','--patch-file=/dev/stdin','-o','json','--request-timeout=2s'],
            input=canonical(patch),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=remaining,close_fds=True,env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'})
        check(result.returncode==0 and len(result.stdout)<=2*1024*1024);return decode_private(result.stdout)

    def gateway_disabled(self):
        # C must qualify a real retained guarded receipt or source-pinned inspect.
        # A Secret value, caller boolean or invented transition time is no proof.
        raise ValueError('native runtime proof unavailable')


def validate_selector(plan,action,selector):
    from pgcat_native_ingress import exact,identifier
    from pgcat_lane_protocol import native_action
    native_action(action,plan['executionKind'])
    if action=='native-ingress' and plan['executionKind']=='native-replacement':
        exact(selector,{'requestId','approvalIds'});identifier(selector['requestId'])
        ids=selector['approvalIds'];check(type(ids) is list and len(ids)<=100 and len(set(ids))==len(ids))
        for value in ids:identifier(value)
    else:exact(selector,set())


def validate_action(plan,envelope,selector):
    from pgcat_native_ingress import exact,identifier,number,hashed
    from pgcat_lane_protocol import digest
    action=envelope['action'];validate_selector(plan,action,selector)
    keys={'schemaVersion','action','localRequestId','runId','executionKind','planSha256','baselineSha256','feedBinding','deadlineMonotonic','prepared'}
    if action in ('native-ingress','native-disabled-restart'):keys|={'readySha256','feedBindingSha256'}
    exact(envelope,keys);check(type(envelope['schemaVersion']) is int and envelope['schemaVersion']==1)
    identifier(envelope['localRequestId']);check(envelope['runId']==plan['runId'] and envelope['executionKind']==plan['executionKind'])
    check(envelope['planSha256']==digest(plan) and envelope['baselineSha256']==plan['baselineSha256'] and number(envelope['deadlineMonotonic']))
    validate_prepared(plan,envelope['prepared'])
    feed=envelope['feedBinding'];exact(feed,{'runId','feedId','pid','bindingSha256','argvSha256'})
    identifier(feed['feedId']);check(feed['runId']==plan['runId'] and type(feed['pid']) is int and feed['pid']>0 and hashed(feed['bindingSha256']) and hashed(feed['argvSha256']))
    if 'readySha256' in envelope:check(hashed(envelope['readySha256']) and hashed(envelope['feedBindingSha256']))


def stage_action(run,plan,envelope,selector):
    """Only two typed, request-keyed files; never an arbitrary write service."""
    from segmented_capture import canonical
    validate_action(plan,envelope,selector)
    request_id=envelope['localRequestId'];run=Path(run)
    return dict(admission=_write_bytes(run/('native-admission-'+request_id+'.json'),canonical(envelope)),
        selector=_write_bytes(run/('native-selector-'+request_id+'.json'),canonical(selector)))


def file_identity(value):
    from pgcat_native_ingress import integer,hashed,exact
    exact(value,{'device','inode','uid','mode','nlink','size','sha256'})
    check(all(integer(value[k]) for k in set(value)-{'sha256'}) and value['mode']==0o600 and value['nlink']==1 and hashed(value['sha256']))


def validate_prepared(plan,prepared):
    """Exact original preparation anchors, separate from source/content hashes."""
    from pgcat_native_ingress import exact,integer,hashed,detached
    from segmented_capture import canonical,sha
    exact(prepared,{'planFile','baselineFile','gatewayRuntimeFile','workerCredential'})
    for key in ('planFile','baselineFile','gatewayRuntimeFile'):file_identity(prepared[key])
    check(prepared['planFile']['sha256']==sha(canonical(plan)) and prepared['baselineFile']['sha256']==plan['baselineSha256'])
    credential=prepared['workerCredential'];exact(credential,{'path','device','inode','uid','mode','size','sha256'})
    check(type(credential['path']) is str and Path(credential['path']).is_absolute() and '..' not in Path(credential['path']).parts and Path(credential['path']).name=='kubeconfig')
    check(all(integer(credential[k]) for k in ('device','inode','uid','mode','size')) and credential['mode']==0o600 and hashed(credential['sha256']))
    return detached(prepared)


def verify_prepared(run,plan,prepared):
    from pgcat_native_ingress import _identity
    run=Path(run);prepared=validate_prepared(plan,prepared)
    for key,name in (('planFile','native-plan.json'),('baselineFile','native-baseline.json'),('gatewayRuntimeFile','native-gateway-runtime.json')):
        check(_identity(run/name)[1]==prepared[key])
    check(prepared['workerCredential']['path']==str(run.parent/'kubeconfig'))
    verify_credential(prepared['workerCredential'])
    return prepared


def prepared_source_prefix(run,plan,prepared):
    """Fixed verifier prepended to the feed's existing worker scripts only."""
    from pgcat_native_ingress import validate_plan
    from pgcat_source_bootstrap import ROOT
    from segmented_capture import canonical
    run=Path(run);check(run.is_absolute() and run.name=='run' and '..' not in run.parts)
    plan=validate_plan(plan);prepared=validate_prepared(plan,prepared)
    check(prepared['workerCredential']['path']==str(run.parent/'kubeconfig'))
    setup="import json\n_native_plan=json.loads(%r)\n_native_prepared=json.loads(%r)\nm.install(_native_plan['sourceHashes'])\nfrom pgcat_native_controller import verify_prepared as _verify_native_prepared\n_verify_native_prepared(%r,_native_plan,_native_prepared)\n"%(canonical(plan).decode('ascii'),canonical(prepared).decode('ascii'),str(run))
    return ROOT.replace('p,h=sys.argv[1:3]', 'p,h='+repr((str(run/'pgcat_source_bootstrap.py'),plan['sourceHashes']['sourceBootstrap']))).replace('sys.argv=[p]+sys.argv[3:]\nsys.exit(m.main())\n',setup)


def validate_action_result(value,plan,action,local_request):
    from pgcat_native_ingress import exact
    from pgcat_lane_protocol import digest
    exact(value,{'schemaVersion','action','runId','executionKind','planSha256','requestId','staged','outputs'})
    check(type(value['schemaVersion']) is int and value['schemaVersion']==1 and value['action']==action and value['runId']==plan['runId'] and value['executionKind']==plan['executionKind'] and value['planSha256']==digest(plan) and value['requestId']==local_request)
    exact(value['staged'],{'admission','selector'})
    for identity in value['staged'].values():file_identity(identity)
    output=value['outputs'];check(type(output) is dict)
    if action=='native-ingress':
        check(output.get('status') in ('OBSERVED','BOUND'))
        expected={'status','ingressSha256','ingressFile'}
        if output['status']=='BOUND':expected|={'operationBinding','operationFile','operationMirror'}
        exact(output,expected);file_identity(output['ingressFile']['identity'])
        check(output['ingressSha256']==output['ingressFile']['identity']['sha256'])
        if output['status']=='BOUND':
            file_identity(output['operationFile']['identity']);file_identity(output['operationMirror'])
            check(output['operationFile']['identity']['sha256']==output['operationMirror']['sha256']==digest(output['operationBinding']))
    elif action=='native-collector-launch':
        exact(output,{'ready','readyFile'})
        file_identity(output['readyFile']);check(output['readyFile']['sha256']==digest(output['ready']))
        from pgcat_native_ingress import READY_KEYS
        exact(output['ready'],READY_KEYS);check(output['ready']['planSha256']==digest(plan))
        check(all(output['ready'][k]==plan[k] for k in ('mode','executionKind','runId','baselineSha256')))
        exact(output['ready']['sources'],{'plan','baseline','context','feedBinding','providerRunContext','progressBinding','gatewayRuntime'})
        for identity in output['ready']['sources'].values():file_identity(identity)
    elif action=='native-collector-status':
        exact(output,{'status','identity','guardianIdentity'});check(output['status']=='ALIVE')
        from pgcat_collector_control import valid_identity
        valid_identity(output['identity']);valid_identity(output['guardianIdentity'])
    elif action=='native-baseline':
        exact(output,{'status','baselineSha256','feedBindingFile'})
        check(output['status']=='VERIFIED' and output['baselineSha256']==plan['baselineSha256']);file_identity(output['feedBindingFile'])
    elif action=='native-disabled-restart':
        exact(output,{'receipt','receiptFile'});file_identity(output['receiptFile']);check(output['receiptFile']['sha256']==digest(output['receipt']))
    else:raise ValueError('native action')
    return value


class WorkerGate:
    def __init__(self,run,plan,envelope,reads):
        _closed(envelope['action']);_require_verified()
        self.run=run;self.plan=plan;self.envelope=envelope;self.reads=reads
        self.deadline=time.monotonic()+5.;self.reads.deadline=self.deadline
    def admission(self,action):
        check(action==self.envelope['action'] and time.monotonic()<self.deadline)
        return {k:self.envelope[k] for k in ('localRequestId','runId','planSha256','readySha256','feedBindingSha256','deadlineMonotonic')}
    def probe_process(self,pid):
        from pgcat_collector_control import process_identity
        return process_identity(pid)
    def live_clients(self):return self.reads.live_clients()
    def gateway_disabled(self):return self.reads.gateway_disabled()


def validate_template(template):
    from pgcat_native_ingress import PLAN_KEYS,DEPLOYMENT_KEYS,SERVICE_KEYS,exact,deployment,private_ip,token,hashed,integer,instant
    from pgcat_lane_actions import NATIVE_SOURCE_NAMES
    from pgcat_lane_protocol import target_valid
    native=template.get('executionKind')=='native-replacement'
    exact(template,(PLAN_KEYS-{'runId','baselineSha256'})|({'plannedInput','controllerScope'} if native else set()))
    check(template['schemaVersion']==1 and type(template['schemaVersion']) is int and template['mode']==MODE and template['executionKind'] in ('native-replacement','disabled-proof'))
    target_valid(template['target']);deployment(template['application'])
    exact(template['sourceHashes'],NATIVE_SOURCE_NAMES);check(all(hashed(v) for v in template['sourceHashes'].values()))
    exact(template['serviceBindings'],{'namespace','gateway','openapi'});check(template['serviceBindings']['namespace']=='raptor-system')
    for label,port in [('gateway',8874),('openapi',8872)]:
        row=template['serviceBindings'][label];exact(row,SERVICE_KEYS);private_ip(row['clusterIP'])
        check(type(row['port']) is int and row['port']==port and all(token(row[k]) for k in SERVICE_KEYS-{'clusterIP','port'}))
    check(template['serviceBindings']['gateway']['clusterIP']!=template['serviceBindings']['openapi']['clusterIP'])
    limits=template['deadline'];exact(limits,{'mutationCutoff','closureDeadline','captureMaxSeconds'})
    check(integer(limits['captureMaxSeconds'],75) and limits['captureMaxSeconds']<=5400 and instant(limits['mutationCutoff'])<instant(limits['closureDeadline']))
    return template


def validate_preflight(value,binding_sha,template,at):
    import datetime
    from pgcat_native_ingress import exact,number,instant
    exact(value,{'schemaVersion','bindingSha256','checkedAt','expiresAt','singleOperator','producersQuiesced','cost'})
    check(type(value['schemaVersion']) is int and value['schemaVersion']==1 and value['bindingSha256']==binding_sha)
    check(value['singleOperator'] is True and value['producersQuiesced'] is True)
    check(all(number(v) for v in (at,value['checkedAt'],value['expiresAt'])))
    check(0<=at-value['checkedAt']<300 and value['checkedAt']<value['expiresAt']<=value['checkedAt']+300 and at<value['expiresAt'])
    cost=value['cost'];exact(cost,{'day','net','reserve','allowance','ceiling'})
    check(all(number(cost[k]) and cost[k]>=0 for k in ('net','reserve','allowance','ceiling')) and cost['ceiling']==1800 and cost['net']+cost['reserve']+cost['allowance']<=1800)
    china=datetime.datetime.fromtimestamp(at,datetime.timezone(datetime.timedelta(hours=8)))
    check(cost['day']==china.date().isoformat() and china.hour*60+china.minute<21*60+30)
    limits=template['deadline'];check(at<instant(limits['mutationCutoff']) and instant(limits['closureDeadline'])-at>=375)
    check(all(datetime.datetime.fromtimestamp(instant(limits[k]),china.tzinfo).date()==china.date() for k in ('mutationCutoff','closureDeadline')))


def prepare_worker(run,run_id,template,provider):
    """Fixed read-only pre-feed operation, before the ordinary feed envelope."""
    _closed('native-baseline');_require_verified()
    from segmented_capture import canonical,sha
    from pgcat_native_ingress import _runtime,validate_plan
    from pgcat_source_bootstrap import current_hashes
    from worker_capture import read_pod,query_metrics,utc
    run=Path(run);check(run==Path(__file__).absolute().parent and run.name=='run')
    template=validate_template(template);check(template['sourceHashes']==current_hashes());verify_source_inventory(run,template['sourceHashes'])
    reads=FixedWorkerReads(template,run.parent/'kubeconfig',run_id=run_id)
    runtime=reads.gateway_disabled()  # unavailable until C's reviewed runtime verifier
    _runtime(runtime,dict(template,runId=run_id),time.time())
    prepared=prepare_records(run,template,run_id,provider_read=lambda:provider,kube_read=reads.read,
        read_pod=lambda p:read_pod(p,{},deadline=reads.deadline),read_metrics=lambda origin:query_metrics(origin,utc()),now=time.time)
    runtime_file=_write_bytes(run/'native-gateway-runtime.json',canonical(runtime))
    (run/'native-boundary').mkdir(mode=0o700)
    verify_credential(reads.credential)
    return dict(prepared,gatewayRuntimeFile=runtime_file,workerCredential=reads.credential)


class NativeController:
    def __init__(self,binding_path):
        _require_verified()
        from pgcat_lane_protocol import private_json,private
        from pgcat_lane_actions import validate_bindings
        from pgcat_source_bootstrap import current_hashes
        self.binding_path=Path(binding_path);self.bindings,raw=private_json(self.binding_path)
        check(set(self.bindings)=={'laneBindings','laneRoot','captureRoot','workerInventory','workerKubeconfig','localKubeconfig','sourceHashes'})
        b=self.bindings['laneBindings'];validate_bindings(b,MODE);check(b['sourceHashes']==current_hashes())
        capture=Path(self.bindings['captureRoot']);private(capture.parent,True)
        check(capture.is_absolute() and not os.path.lexists(capture) and Path(self.bindings['laneRoot']).parent==capture)
        local=[Path(self.bindings['laneRoot'])]+[Path(b[key]) for key in ('receiptPath','laneLockPath','laneIntentDir','scaleoutIntentPath','scaleinIntentPath','nlbIntentPath','preScaleProofPath')]
        check(len(set(local))==len(local) and all(p.parent==capture and not os.path.lexists(p) for p in local))
        check(self.bindings['sourceHashes']=={k:b['sourceHashes'][k] for k in ('nativeController','client','runner')})
        check(self.bindings['workerInventory']==str(Path(b['remoteRun'])/'provider-inventory.json') and self.bindings['workerKubeconfig']==b['workerKubeconfig']==str(Path(b['remoteRun']).parent/'kubeconfig'))
        self.binding_sha=hashlib.sha256(raw).hexdigest();self.client=None;self.feed=None;self.prepared=None;self.receipt=None

    def _provider_snapshot(self):
        from types import SimpleNamespace
        import provider_feed as provider
        b=self.bindings['laneBindings'];operator=provider.Operator(SimpleNamespace(mode=MODE,status_file=None,listener_id=provider.LISTENER_ID))
        deadline=time.monotonic()+30.
        def command(argv):
            check(self.feed is None and argv[0]=='aliyun' and time.monotonic()<deadline)
            result=subprocess.run([b['aliyunCli']]+argv[1:],stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,
                timeout=min(10.,deadline-time.monotonic()),env={'PATH':os.defpath,'HOME':os.path.expanduser('~'),'LANG':'C'},close_fds=True)
            check(result.returncode==0 and len(result.stdout)<=2*1024*1024);return result.stdout.decode('ascii')
        operator.command=command
        check(operator.aliyun('sts','GetCallerIdentity')['AccountId']==b['target']['accountId'])
        activity=operator.aliyun('ess','DescribeScalingActivities',ScalingGroupId=b['target']['scalingGroupId'],StatusCode='InProgress',PageNumber=1,PageSize=50)
        check(type(activity['TotalCount']) is int and activity['TotalCount']==0 and activity['ScalingActivities']['ScalingActivity']==[])
        group=operator.fetch_group();check(all(type(group[k]) is int and group[k]==v for k,v in [('DesiredCapacity',2),('MinSize',2),('MaxSize',4)]))
        return operator.read()

    def prepare(self,plan_path,preflight_path):
        _closed('native-baseline');_require_verified()
        import uuid,shlex
        from pgcat_lane_protocol import private_json,encode
        from pgcat_source_bootstrap import ROOT
        from pgcat_native_ingress import validate_plan
        from pgcat_lane_actions import PROFILE,REGION,WORKER
        check(self.prepared is None and self.client is None and self.feed is None)
        template,_=private_json(Path(plan_path));template=validate_template(template)
        b=self.bindings['laneBindings'];check(template['target']==b['target'] and template['sourceHashes']==b['sourceHashes'] and template['executionKind']==b['executionKind'])
        preflight,_=private_json(Path(preflight_path));validate_preflight(preflight,self.binding_sha,template,time.time())
        local_credential=credential_identity(self.bindings['localKubeconfig']);run_id=str(uuid.uuid4());provider=self._provider_snapshot()
        parts=['python3','-I','-S','-c',ROOT,str(Path(b['remoteRun'])/'pgcat_source_bootstrap.py'),b['sourceHashes']['sourceBootstrap'],
            'nativeController',encode(b['sourceHashes']).decode(),'prepare',b['remoteRun'],run_id,encode(template).decode(),encode(provider).decode()]
        result=subprocess.run([b['workbenchCli'],'exec','-P',PROFILE,'-r',REGION,'-i',WORKER,'-c',' '.join(shlex.quote(p) for p in parts)],
            stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=30,close_fds=True,
            env={'PATH':os.defpath,'HOME':os.path.expanduser('~'),'LANG':'C'})
        check(result.returncode==0 and len(result.stdout)<=65536)
        from segmented_capture import decode_private,canonical,sha
        prepared=decode_private(result.stdout);check(set(prepared)=={'plan','baseline','planFile','baselineFile','gatewayRuntimeFile','workerCredential'})
        plan=validate_plan(prepared['plan']);check(plan['runId']==run_id and {k:v for k,v in plan.items() if k not in ('runId','baselineSha256')}==template)
        validate_prepared(plan,{k:prepared[k] for k in ('planFile','baselineFile','gatewayRuntimeFile','workerCredential')})
        check(prepared['workerCredential']['path']==b['workerKubeconfig'])
        for key in ('planFile','baselineFile','gatewayRuntimeFile'):file_identity(prepared[key])
        check(prepared['planFile']['sha256']==sha(canonical(plan)) and prepared['baselineFile']['sha256']==plan['baselineSha256']==sha(canonical(prepared['baseline'])))
        verify_credential(local_credential);validate_preflight(preflight,self.binding_sha,template,time.time())
        capture=Path(self.bindings['captureRoot']);capture.mkdir(mode=0o700)
        Path(b['laneIntentDir']).mkdir(mode=0o700)
        prepared['macCredential']=local_credential
        _write_bytes(capture/'native-prepared.json',canonical(prepared));_write_bytes(capture/'native-plan.json',canonical(plan))
        self.prepared=prepared;return prepared

    def _receipt(self,minimum=1):
        from pgcat_lane_supervisor import read_json
        import pgcat_workbench_lane as lane
        until=time.monotonic()+35.
        while time.monotonic()<until:
            self.client._active();check(self.feed.poll() is None)
            try:
                candidate=read_json(self.bindings['laneBindings']['receiptPath'])
                _,receipt,_=lane.read_receipt(self.bindings['laneBindings']['receiptPath'],candidate['feedSession'],minimum,expected_mode=MODE)
                check(self.receipt is None or receipt['feedSession']==self.receipt['feedSession']);self.receipt=receipt;return receipt
            except (FileNotFoundError,lane.Refused):time.sleep(.02)
        raise ValueError('native feed timeout')

    def _call(self,action,selector=None):
        from pgcat_lane_protocol import monotonic
        try:
            receipt=self._receipt(1 if self.receipt is None else self.receipt['sequence']+1)
            result=self.client.call(action,receipt,float(monotonic()+5.),native_selector=selector)
            return result['payload']
        except BaseException:
            self.close();raise

    def close(self):
        if self.client is not None:self.client.close()

    def start_capture(self):
        _closed('native-collector-launch');_require_verified()
        from pgcat_lane_client import LaneClient
        from pgcat_lane_protocol import private_json,encode
        from pgcat_rehearsal_controller import _feed_path
        check(self.prepared is not None and self.client is None)
        prepared,_=private_json(Path(self.bindings['captureRoot'])/'native-prepared.json');check(prepared==self.prepared)
        plan=prepared['plan'];verify_credential(prepared['macCredential']);b=self.bindings['laneBindings']
        try:
            anchors=validate_prepared(plan,{k:prepared[k] for k in ('planFile','baselineFile','gatewayRuntimeFile','workerCredential')})
            self.client=LaneClient.start(b,MODE,Path(self.bindings['laneRoot']),native_plan=plan,native_prepared=anchors)
            _write_bytes(self.client.root/'native-prepared.json',encode(prepared))
            cap=capture_budget(plan,time.time())
            argv=[sys.executable,'-u',b['providerFeedPath'],'--mode',MODE,'--interval','15','--max-seconds',str(cap),
                '--workbench',b['laneWrapper'],'--worker-inventory',self.bindings['workerInventory'],'--kubeconfig',b['workerKubeconfig'],
                '--status-file',b['receiptPath'],'--run-context',str(self.client.root/'run-context.json'),'--contract-dir',b['contractDir']]
            env={'PATH':_feed_path(b),'HOME':os.path.expanduser('~'),'LANG':'C','LC_ALL':'C','PGCAT_LANE_ROLE':'provider',
                'PGCAT_LANE_REAL_CLI':b['workbenchCli'],'PGCAT_LANE_LOCK':b['laneLockPath']}
            self.feed=self.client.start_feed(argv,env,self.binding_path)
            self._call('native-baseline');return self._call('native-collector-launch')
        except BaseException:
            if self.client is not None:self.client.close()
            raise

    def ingest_request(self,request_id,approval_ids=()):
        _closed('native-ingress');_require_verified()
        if self.prepared['plan']['executionKind']=='disabled-proof':
            check(request_id is None and not approval_ids);selector={}
        else:selector=dict(requestId=request_id,approvalIds=list(approval_ids))
        return self._call('native-ingress',selector)

    def restart_disabled(self):
        _closed('native-disabled-restart');_require_verified()
        from pgcat_lane_protocol import native_action
        native_action('native-disabled-restart',self.prepared['plan']['executionKind'])
        return self._call('native-disabled-restart',{})


def worker_entry(action,run_dir,envelope=None,selector=None):
    _closed(action);_require_verified()
    from pgcat_native_ingress import _identity,validate_plan,restart_disabled
    from segmented_capture import decode_private,canonical,sha
    from pgcat_source_bootstrap import current_hashes
    run=Path(run_dir);plan=validate_plan(decode_private(_identity(run/'native-plan.json')[0]))
    check(plan['sourceHashes']==current_hashes());verify_source_inventory(run,plan['sourceHashes'])
    prepared=verify_prepared(run,plan,envelope['prepared'])
    check(envelope['action']==action);staged=stage_action(run,plan,envelope,selector)
    request_id=envelope['localRequestId']
    for name,expected in [('admission',envelope),('selector',selector)]:
        raw,identity=_identity(run/('native-'+name+'-'+request_id+'.json'))
        check(raw==canonical(expected) and identity==staged[name])
    reads=FixedWorkerReads(plan,run.parent/'kubeconfig',credential=prepared['workerCredential'])
    gate=WorkerGate(run,plan,envelope,reads)
    if action=='native-baseline':
        raw=canonical(envelope['feedBinding']);feed_path=run/'native-feed-binding.json'
        mirror=_write_bytes(feed_path,raw)
        from pgcat_rehearsal_contract import fresh_provider_source
        source=fresh_provider_source(run.parent/'contract',plan['runId'],run/'provider-inventory.json',mode=MODE)
        baseline=decode_private(_identity(run/'native-baseline.json')[0])
        check(source['sequence']==1 and source['privateAddresses']=={r['id']:r['address'] for r in baseline['originals']})
        from pgcat_native_ingress import _fleet
        inventory_raw,_=_identity(run/'provider-inventory.json');check(sha(inventory_raw)==source['inventoryRawSha256'])
        inventory=decode_private(inventory_raw);_fleet({k:inventory[k] for k in baseline['fleet']},plan['target']['originalInstanceIds'])
        output=dict(status='VERIFIED',baselineSha256=plan['baselineSha256'],feedBindingFile=mirror)
    elif action=='native-ingress':
        http=FixedHTTP(plan);http.deadline=gate.deadline
        output=collect_ingress(run,selector,gate=gate,kube_read=reads.read,http_get=http,now=time.time)
    elif action=='native-disabled-restart':
        # C must supply its qualified runtime proof before this can reach a patch.
        reads.gateway_disabled()
        receipt=restart_disabled(run,gate=gate,kube_read=reads.read,kube_patch=reads.restart_patch,now=time.time)
        output=dict(receipt=receipt,receiptFile=_identity(run/'native-restart-receipt.json')[1])
    elif action=='native-collector-status':
        from pgcat_collector_control import status
        output=status(run.parent/'contract',plan['runId'],plan['sourceHashes'],profile=MODE,prepared=prepared)
    elif action=='native-collector-launch':
        from pgcat_collector_control import launch,status,process_identity
        runtime=reads.gateway_disabled()
        from pgcat_native_ingress import _runtime,_gateway_config,_Calls
        from types import SimpleNamespace
        _runtime(runtime,plan,time.time());sealed_runtime=decode_private(_identity(run/'native-gateway-runtime.json')[0])
        check({k:v for k,v in runtime.items() if k!='observedAt'}=={k:v for k,v in sealed_runtime.items() if k!='observedAt'})
        check(_gateway_config(SimpleNamespace(plan=plan),_Calls(),reads.read)==dict(secretUid=runtime['secretUid'],secretResourceVersion=runtime['secretResourceVersion'],replacementEnabled=False))
        launched=launch(run.parent/'contract',plan['runId'],run.parent/'kubeconfig',plan['sourceHashes'],profile=MODE,prepared=prepared)
        until=time.monotonic()+5.
        while True:
            check(time.monotonic()<until)
            observed=status(run.parent/'contract',plan['runId'],plan['sourceHashes'],profile=MODE,prepared=prepared)
            if observed.get('status')=='ALIVE' and (run/'native-context.json').exists():
                try:
                    from segmented_capture import read_progress
                    context=decode_private(_identity(run/'native-context.json')[0])
                    progress,_,_=read_progress(run/'native-capture',context)
                    if progress['committedFrames']<2:time.sleep(.01);continue
                    ready=seal_ready(run,prepared=prepared,guardian_identity=launched['guardianIdentity'],probe_process=process_identity,live_clients=reads.live_clients,now=time.time)
                    break
                except FileNotFoundError:pass
            time.sleep(.01)
        output=dict(ready=ready,readyFile=_identity(run/'native-ready.json')[1])
    else:raise ValueError('native action')
    verify_prepared(run,plan,prepared)
    value=dict(schemaVersion=1,action=action,runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),requestId=request_id,staged=staged,outputs=output)
    return validate_action_result(value,plan,action,request_id)


def main():
    try:
        _closed();_require_verified()
        from segmented_capture import decode_private,canonical,sha
        if len(sys.argv)==6 and sys.argv[1]=='prepare':
            _,_,run,run_id,template,provider=sys.argv
            check(len(template)<=16384 and len(provider)<=16384)
            value=prepare_worker(run,run_id,decode_private(template.encode('ascii')),decode_private(provider.encode('ascii')))
            print(canonical(value).decode('ascii'));return 0
        check(len(sys.argv)==8)
        action,run,request_id,admission_sha,selector_sha,admission_raw,selector_raw=sys.argv[1:]
        admission_raw=admission_raw.encode('ascii');selector_raw=selector_raw.encode('ascii')
        check(len(admission_raw)<=16384 and len(selector_raw)<=16384 and sha(admission_raw)==admission_sha and sha(selector_raw)==selector_sha)
        envelope=decode_private(admission_raw);selector=decode_private(selector_raw)
        check(envelope['localRequestId']==request_id)
        value=worker_entry(action,run,envelope,selector)
        print(canonical(value).decode('ascii'));return 0
    except Exception:return 1


if __name__=='__main__':sys.exit(main())
