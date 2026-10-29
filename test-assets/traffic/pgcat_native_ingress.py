"""Offline native evidence boundary. No production transport or executable entry.

The future source-verified Journal dispatch owns admission. These functions only
consume its bound files and narrow injected adapters; they cannot enable a run.
"""
import datetime
import ipaddress
import json
import math
import re
import uuid

MODE = 'full-replacement-direct-v2'
KINDS = ('native-replacement', 'disabled-proof')
PLAN_KEYS = {'schemaVersion','mode','executionKind','runId','target','application',
    'sourceHashes','baselineSha256','plannedOperation','serviceBindings','deadline'}
DEPLOYMENT_KEYS = {'name','namespace','uid','generation','initialReplicaSetUid','initialTemplateHash','imageDigest'}
SERVICE_KEYS = {'deploymentName','deploymentUid','containerName','serviceName','serviceUid','clusterIP','port'}


def require(condition):
    if not condition:raise ValueError('invalid native ingress')


def exact(value, keys):require(type(value) is dict and set(value)==set(keys))
def integer(value, minimum=0):return type(value) is int and minimum<=value<2**53
def number(value):return type(value) in (int,float) and math.isfinite(value)
def token(value):return type(value) is str and re.fullmatch(r'[A-Za-z0-9_.:/-]{1,256}',value) is not None
def hashed(value):return type(value) is str and re.fullmatch('[0-9a-f]{64}',value) is not None
def detached(value):return json.loads(json.dumps(value,allow_nan=False))


def identifier(value):
    require(type(value) is str)
    require(str(uuid.UUID(value))==value)


def instant(value):
    require(type(value) is str and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z',value))
    value=re.sub(r'(\.\d{6})\d+',r'\1',value)
    form='%Y-%m-%dT%H:%M:%S.%fZ' if '.' in value else '%Y-%m-%dT%H:%M:%SZ'
    return datetime.datetime.strptime(value,form).replace(tzinfo=datetime.timezone.utc).timestamp()


def private_ip(value):
    require(type(value) is str)
    parsed=ipaddress.IPv4Address(value)
    require(str(parsed)==value and parsed.is_private and not parsed.is_loopback and not parsed.is_link_local and not parsed.is_unspecified)


def deployment(value):
    exact(value,DEPLOYMENT_KEYS)
    require(value['name']=='rdev-test-client-client' and value['namespace']=='raptor-test')
    require(integer(value['generation'],1) and all(token(value[k]) for k in ('uid','initialReplicaSetUid','initialTemplateHash')))
    require(type(value['imageDigest']) is str and re.fullmatch('sha256:[0-9a-f]{64}',value['imageDigest']))


def validate_plan(value):
    from pgcat_lane_protocol import target_valid
    from pgcat_lane_actions import SOURCE_NAMES
    require(type(value) is dict and value.get('executionKind') in KINDS)
    native=value['executionKind']=='native-replacement'
    exact(value,PLAN_KEYS|({'plannedInput','controllerScope'} if native else set()))
    require(type(value['schemaVersion']) is int and value['schemaVersion']==1 and value['mode']==MODE)
    identifier(value['runId']);deployment(value['application'])
    try:target_valid(value['target'])
    except Exception:raise ValueError('invalid native ingress') from None
    require(value['target']['originalInstanceIds']==sorted(value['target']['originalInstanceIds']))
    exact(value['sourceHashes'],SOURCE_NAMES|{'nativeController','nativeIngress','nativeEvidence'})
    require(all(hashed(v) for v in value['sourceHashes'].values()) and hashed(value['baselineSha256']))
    limits=value['deadline'];exact(limits,{'mutationCutoff','closureDeadline','captureMaxSeconds'})
    require(integer(limits['captureMaxSeconds'],75) and limits['captureMaxSeconds']<=5400)
    cutoff=instant(limits['mutationCutoff']);closure=instant(limits['closureDeadline'])
    require(cutoff<closure and limits['mutationCutoff'][11:19]<='13:30:00' and limits['closureDeadline'][11:19]<='14:30:00')
    services=value['serviceBindings'];exact(services,{'namespace','gateway','openapi'})
    require(services['namespace']=='raptor-system')
    for name,port in [('gateway',8874),('openapi',8872)]:
        row=services[name];exact(row,SERVICE_KEYS)
        require(all(token(row[k]) for k in SERVICE_KEYS-{'port','clusterIP'}))
        require(type(row['port']) is int and row['port']==port);private_ip(row['clusterIP'])
    require(services['gateway']['clusterIP']!=services['openapi']['clusterIP'])
    require(value['plannedOperation']==('db-proxy.replace-nodes' if native else 'disabled-client-restart'))
    if native:
        selected=value['plannedInput'];exact(selected,{'type','providerId','model','object','envCode','operations','skills'})
        require(selected['type']=='agent' and selected['providerId']=='codex' and token(selected['model']) and token(selected['envCode']))
        exact(selected['object'],{'kind','code'});require(selected['object']['kind']=='db-proxy' and token(selected['object']['code']))
        require(selected['operations']==[dict(name='db-proxy.replace-nodes',parameters=dict(mode='replacement'))])
        skills=selected['skills'];exact(skills,{'tag','commitSha'})
        require(skills['tag']=='skills-v0.4.0' and type(skills['commitSha']) is str and re.fullmatch('[0-9a-f]{40}',skills['commitSha']))
        scope=value['controllerScope'];exact(scope,{'envCode','proxyCode','groupId','serverGroupId','targetDbCode','application'})
        require(all(token(scope[k]) for k in set(scope)-{'application'}))
        require(scope['envCode']==selected['envCode'] and scope['proxyCode']==selected['object']['code'])
        require(scope['groupId']==value['target']['scalingGroupId'] and scope['serverGroupId']==value['target']['nlbServerGroupId'])
        app=scope['application'];exact(app,{'envCode','appCode','clusterId','namespace','name','uid'})
        require(all(token(v) for v in app.values()) and app['envCode']==selected['envCode'])
        require(all(app[k]==value['application'][k] for k in ('namespace','name','uid')))
    return detached(value)

# Imports shared with the reviewed worker closure remain lazy at call boundaries.
import base64
import functools
import os
from pathlib import Path
import signal
import threading
import time

READY_KEYS={'schemaVersion','mode','executionKind','runId','planSha256','baselineSha256','contextSha256',
    'progressBindingSha256','feedBindingSha256','feedSession','collectorIdentity','guardianIdentity','frames','readyAt','sources'}
BASELINE_KEYS={'schemaVersion','mode','executionKind','runId','observedAt','targetSha256','originals','configMap',
    'roster','metricGroups','deployment','fleet','gateway'}
RECORD_KEYS={'schemaVersion','executionKind','runId','planSha256','readySha256','readAt','serviceBindings','context','execution','events','approvals','diagnostic'}
MAX_RESPONSE=2*1024*1024


def sanitized(function):
    @functools.wraps(function)
    def wrapped(*args,**kwargs):
        try:return function(*args,**kwargs)
        except Exception:pass
        # Raise outside the handler so even __context__ cannot expose adapter errors.
        raise ValueError('invalid native ingress')
    return wrapped


class _Calls:
    """One worker-local deadline; never compares a Mac monotonic timestamp."""
    def __init__(self):self.deadline=time.monotonic()+30
    def call(self,function,*args):
        remaining=min(5,self.deadline-time.monotonic());require(remaining>0)
        require(threading.current_thread() is threading.main_thread())
        require(signal.getitimer(signal.ITIMER_REAL)==(0.0,0.0))
        previous=signal.getsignal(signal.SIGALRM)
        def expire(signum,frame):raise TimeoutError('native adapter timeout')
        signal.signal(signal.SIGALRM,expire);signal.setitimer(signal.ITIMER_REAL,remaining)
        try:
            value=function(*args);require(time.monotonic()<self.deadline);return value
        finally:signal.setitimer(signal.ITIMER_REAL,0);signal.signal(signal.SIGALRM,previous)


def _identity(path,limit=MAX_RESPONSE):
    from segmented_capture import open_directory,read_private,sha,check_directory
    fd=open_directory(path.parent)
    try:
        raw=read_private(fd,path.name,limit);s=os.stat(path.name,dir_fd=fd,follow_symlinks=False)
        check_directory(fd,path.parent)
        return raw,dict(device=s.st_dev,inode=s.st_ino,uid=s.st_uid,mode=s.st_mode&0o777,nlink=s.st_nlink,size=s.st_size,sha256=sha(raw))
    finally:os.close(fd)


def _process(value):
    exact(value,{'pid','bootId','startTicks','commandSha256'})
    require(integer(value['pid'],1) and integer(value['startTicks']) and token(value['bootId']) and hashed(value['commandSha256']))


def _fleet(value,ids):
    exact(value,{'desired','members','healthyRegistered','memberStates','backendStates'})
    require(type(value['desired']) is int and value['desired']==len(ids) and value['members']==value['healthyRegistered']==ids)
    require(value['memberStates']==[dict(id=n,lifecycle='Protected',health='Healthy',addressKnown=True) for n in ids])
    require(all(type(row['addressKnown']) is bool for row in value['memberStates']))
    rows=value['backendStates'];require(type(rows) is list and len(rows)==len(ids))
    for row,node in zip(rows,ids):
        exact(row,{'id','status','weight','listenerHealth'})
        require(row['id']==node and row['status']=='Available' and row['listenerHealth']=='Normal' and integer(row['weight'],1) and row['weight']<=100)


def _baseline(value,plan):
    from segmented_capture import sha,canonical
    from finish_clients import validate_roster
    exact(value,BASELINE_KEYS)
    require(type(value['schemaVersion']) is int and value['schemaVersion']==1)
    require(all(value[k]==plan[k] for k in ('mode','executionKind','runId')) and number(value['observedAt']))
    require(value['targetSha256']==sha(canonical(plan['target'])) and value['deployment']==plan['application'])
    deployment(value['deployment']);validate_roster(value['roster'])
    rows=value['originals'];require(type(rows) is list and len(rows)==2)
    for row in rows:exact(row,{'id','address'});private_ip(row['address'])
    require([r['id'] for r in rows]==plan['target']['originalInstanceIds'] and len({r['address'] for r in rows})==2)
    cm=value['configMap'];exact(cm,{'name','namespace','uid','resourceVersion'})
    require(cm['name']=='pgcat-provider-inventory' and cm['namespace']=='raptor-test' and token(cm['uid']) and token(cm['resourceVersion']))
    groups=value['metricGroups'];require(type(groups) is list and 1<=len(groups)<=256)
    for row in groups:exact(row,{'node','pool','user'});require(all(token(v) for v in row.values()))
    tuples=[(r['node'],r['pool'],r['user']) for r in groups]
    require(tuples==sorted(set(tuples)) and {r[0] for r in tuples}==set(plan['target']['originalInstanceIds']))
    require(all((node,'test','poc_app') in tuples for node in plan['target']['originalInstanceIds']))
    _fleet(value['fleet'],plan['target']['originalInstanceIds'])
    require(value['gateway']==dict(deploymentUid=plan['serviceBindings']['gateway']['deploymentUid'],replacementEnabled=False) and type(value['gateway']['replacementEnabled']) is bool)


def _runtime(value,plan,at):
    exact(value,{'schemaVersion','runId','observedAt','secretUid','secretResourceVersion','deploymentUid','podUid','generation','imageDigest','provenance','verification'})
    require(type(value['schemaVersion']) is int and value['schemaVersion']==1 and value['runId']==plan['runId'])
    require(number(value['observedAt']) and 0<=at-value['observedAt']<30)
    require(all(token(value[k]) for k in ('secretUid','secretResourceVersion','podUid')) and integer(value['generation'],1))
    require(value['deploymentUid']==plan['serviceBindings']['gateway']['deploymentUid'])
    require(type(value['imageDigest']) is str and re.fullmatch('sha256:[0-9a-f]{64}',value['imageDigest']))
    proof=value['provenance'];require(type(proof) is dict)
    if proof.get('kind')=='guarded-verify':
        exact(proof,{'kind','resourceVersion','changedAfter'});require(proof['resourceVersion']==value['secretResourceVersion'])
        require(instant(proof['changedAfter'])<=value['observedAt'])
    else:require(proof==dict(kind='read-only-inspect'))  # Offline schema only; C qualifies the actual command.
    require(value['verification']==dict(replacementEnabled=False,generation=value['generation'],ready=True,podUID=value['podUid']))
    require(type(value['verification']['replacementEnabled']) is bool and type(value['verification']['ready']) is bool and type(value['verification']['generation']) is int)


def _baseline_frame(frame,baseline,context,at):
    from pgcat_native_evidence import pod_identity
    from capture_window import seconds,clean_client_pods
    require(0<=at-seconds(frame['at'])<=3 and frame['expectedReplicas']==2 and not frame.get('startingPods'))
    dep=baseline['deployment'];require(frame['deployment']==dict(uid=dep['uid'],generation=dep['generation'],observedGeneration=dep['generation'],desired=2,ready=2))
    require(frame['inventory']['mode']==MODE and frame['inventory']['feedSession']==context['feedSession'])
    require({k:frame['inventory'][k] for k in baseline['fleet']}==baseline['fleet'])
    require(0<=seconds(frame['at'])-seconds(frame['inventory']['observedAt'])<30)
    pods=frame['pods'];require(len(pods)==2 and clean_client_pods(pods)[1])
    require(sorted([pod_identity(p) for p in pods],key=lambda r:r['uid'])==baseline['roster'])
    for pod in pods:
        require(pod['ready'] is True and pod['snapshot']['final'] is False and pod['snapshot']['connected']>0)
        require(pod['ownership']==dict(deploymentUid=dep['uid'],replicaSetUid=dep['initialReplicaSetUid'],templateHash=dep['initialTemplateHash'],imageDigest=dep['imageDigest'],terminating=False))
    metrics=frame['metrics'];require(metrics['available'] is True and 0<=seconds(frame['at'])-seconds(metrics['observedAt'])<=15)
    groups={}
    for row in metrics['raw']:
        require(number(row['value']) and row['value']>=0 and 0<=seconds(frame['at'])-seconds(row['sourceAt'])<=15)
        group=(row['node'],row['pool'],row['user']);require(row['kind'] not in groups.setdefault(group,set()));groups[group].add(row['kind'])
    require(all(kinds=={'active','idle','waiting'} for kinds in groups.values()))
    require([dict(node=n,pool=p,user=u) for n,p,u in sorted(groups)]==baseline['metricGroups'])


class _Chain:
    def __init__(self,run_dir):
        from segmented_capture import open_directory,decode_private,sha,canonical,read_frame,read_progress
        from pgcat_native_evidence import validate_context
        from capture_window import seconds
        self.run=Path(run_dir);require(self.run.is_absolute() and self.run.name=='run' and '..' not in self.run.parts)
        self.parent_fd=open_directory(self.run.parent);os.close(self.parent_fd)
        self.paths=dict(plan=self.run/'native-plan.json',baseline=self.run/'native-baseline.json',context=self.run/'native-context.json',
            feedBinding=self.run/'native-feed-binding.json',providerRunContext=self.run.parent/'contract'/'provider-source'/'run-context.json',
            progressBinding=self.run/'native-capture'/'progress-binding.json',gatewayRuntime=self.run/'native-gateway-runtime.json',ready=self.run/'native-ready.json')
        self.files={k:_identity(p) for k,p in self.paths.items()}
        self.values={k:decode_private(raw) for k,(raw,_) in self.files.items()}
        self.plan=validate_plan(self.values['plan']);self.baseline=self.values['baseline'];_baseline(self.baseline,self.plan)
        self.context=validate_context(self.values['context']);self.ready=self.values['ready']
        require(self.files['plan'][0]==canonical(self.plan));require(self.plan['baselineSha256']==sha(self.files['baseline'][0]))
        require(self.context['deployment']==self.plan['application'] and self.context['initialRoster']==self.baseline['roster'])
        require(self.context['sourceHashes']=={k:self.plan['sourceHashes'][k] for k in self.context['sourceHashes']})
        require(self.context['planSha256']==sha(canonical(self.plan)) and self.context['baselineSha256']==self.plan['baselineSha256'])
        require(all(self.context[k]==self.plan[k] for k in ('mode','runId','executionKind')))
        provider=self.values['providerRunContext'];exact(provider,{'schemaVersion','runContext','feedSession'})
        rc=provider['runContext'];exact(rc,{'schemaVersion','runId','supervisorId','targetSha256','bundleSha256','nonce'})
        require(type(provider['schemaVersion']) is int and provider['schemaVersion']==1 and type(rc['schemaVersion']) is int and rc['schemaVersion']==1)
        for k in ('runId','supervisorId','nonce'):identifier(rc[k])
        identifier(provider['feedSession'])
        require(rc['runId']==self.plan['runId'] and rc['targetSha256']==self.baseline['targetSha256'] and rc['bundleSha256']==sha(canonical(self.plan['sourceHashes'])))
        require(self.context['providerRunContextSha256']==sha(self.files['providerRunContext'][0]) and self.context['bundleSha256']==rc['bundleSha256'] and self.context['feedSession']==provider['feedSession'])
        feed=self.values['feedBinding'];exact(feed,{'runId','feedId','pid','bindingSha256','argvSha256'})
        identifier(feed['feedId']);require(feed['runId']==self.plan['runId'] and integer(feed['pid'],1) and hashed(feed['bindingSha256']) and hashed(feed['argvSha256']))
        r=self.ready;exact(r,READY_KEYS)
        require(type(r['schemaVersion']) is int and r['schemaVersion']==1 and number(r['readyAt']) and self.baseline['observedAt']<=r['readyAt'])
        require(all(r[k]==self.context[k] for k in ('mode','executionKind','runId','planSha256','baselineSha256','feedSession','collectorIdentity')))
        for field,key in [('contextSha256','context'),('progressBindingSha256','progressBinding'),('feedBindingSha256','feedBinding')]:require(r[field]==sha(self.files[key][0]))
        require(r['sources']=={k:ident for k,(_,ident) in self.files.items() if k!='ready'})
        for ident in r['sources'].values():
            exact(ident,{'device','inode','uid','mode','nlink','size','sha256'});require(all(integer(ident[k]) for k in set(ident)-{'sha256'}) and ident['mode']==0o600 and ident['nlink']==1 and hashed(ident['sha256']))
        for key in ('collectorIdentity','guardianIdentity'):_process(r[key])
        require(r['collectorIdentity']['pid']!=r['guardianIdentity']['pid'])
        _runtime(self.values['gatewayRuntime'],self.plan,r['readyAt'])
        exact(r['frames'],{'previous','latest'})
        fd=open_directory(self.run/'native-capture')
        try:
            info=os.fstat(fd);require(self.values['progressBinding']==dict(self.context,captureDirectoryIdentity=dict(device=info.st_dev,inode=info.st_ino)))
            frames=[read_frame(fd,r['frames'][k]) for k in ('previous','latest')]
            _ready_pair(frames,self.baseline,self.context,r['readyAt'])
            require(0<seconds(frames[1]['at'])-seconds(frames[0]['at'])<=3)
            a,b=[r['frames'][k] for k in ('previous','latest')]
            require((a['basename']==b['basename'] and a['offset']+a['length']==b['offset']) or (int(b['basename'][8:12])==int(a['basename'][8:12])+1 and b['offset']==0))
        finally:os.close(fd)
        self.ready_sha=sha(self.files['ready'][0])
    def unchanged(self):
        require(all(_identity(path)==self.files[key] for key,path in self.paths.items()))


def _gateway_config(chain,calls,kube_read):
    from segmented_capture import decode_private
    name=chain.plan['serviceBindings']['gateway']['deploymentName']
    dep=calls.call(kube_read,'Deployment','raptor-system',name)
    require(dep['metadata']['uid']==chain.plan['serviceBindings']['gateway']['deploymentUid'])
    volumes=dep['spec']['template']['spec'].get('volumes',[])
    require(sum(v.get('secret',{}).get('secretName')=='gateway-live-config' for v in volumes)==1)
    secret=calls.call(kube_read,'Secret','raptor-system','gateway-live-config')
    raw=secret['data']['config.json'];require(type(raw) is str and len(raw)<=90000)
    decoded=base64.b64decode(raw,validate=True);require(len(decoded)<=65536)
    config=decode_private(decoded);require(type(config) is dict and type(config.get('replacementEnabled')) is bool)
    meta=secret['metadata'];require(token(meta['uid']) and token(meta['resourceVersion']))
    return dict(secretUid=meta['uid'],secretResourceVersion=meta['resourceVersion'],replacementEnabled=config['replacementEnabled'])


def _action_gate(chain,gate,calls,kube_read,now,action,initial=False):
    from segmented_capture import read_progress,open_directory
    from pgcat_native_evidence import _replay,pod_identity
    from capture_window import seconds
    from worker_capture import _native_owner_records
    from pgcat_rehearsal_contract import fresh_provider_source
    at=now();require(number(at))
    admission=calls.call(gate.admission,action)
    exact(admission,{'localRequestId','runId','planSha256','readySha256','feedBindingSha256','deadlineMonotonic'})
    identifier(admission['localRequestId']);require(number(admission['deadlineMonotonic']))
    require(admission['runId']==chain.plan['runId'] and admission['planSha256']==chain.context['planSha256'] and admission['readySha256']==chain.ready_sha and admission['feedBindingSha256']==chain.ready['feedBindingSha256'])
    for key in ('collectorIdentity','guardianIdentity'):
        identity=chain.ready[key];require(calls.call(gate.probe_process,identity['pid'])==identity)
    capture=chain.run/'native-capture';progress,previous,latest=read_progress(capture,chain.context)
    require(previous is not None and latest is not None and 0<=at-seconds(latest['at'])<=3 and 0<seconds(latest['at'])-seconds(previous['at'])<=3)
    require(progress['maxGapSeconds']<=3 and not any(progress['flags'].values()))
    fd=open_directory(capture)
    try:snapshot,count,_=_replay(fd,chain.context,progress['latest'])
    finally:os.close(fd)
    require(snapshot==progress['nativeCohort'] and count==progress['committedFrames'])
    live=calls.call(gate.live_clients);exact(live,{'deployment','replicaSets','pods','roster'})
    owners,starting=_native_owner_records(live['deployment'],live['replicaSets'],live['pods'],chain.context,allow_starting=True)
    active=[p for p in latest['pods'] if not p['snapshot']['final']]
    retained={p['uid']:p for p in latest['pods']}
    require({p['uid'] for p in active}<=set(owners)<=set(retained))
    expected=sorted([pod_identity(retained[uid]) for uid in owners],key=lambda r:r['uid'])
    require(sorted(live['roster'],key=lambda r:r['uid'])==expected)
    require(sorted(starting,key=lambda r:r['uid'])==latest.get('startingPods',[]))
    require(all(owners[uid]==retained[uid]['ownership'] for uid in owners))
    for pod in live['pods']:
        uid=pod['metadata']['uid']
        if uid in owners:
            require(pod['metadata']['name']==retained[uid]['name'] and pod['status']['containerStatuses'][0]['containerID']==retained[uid]['containerId'])
    require(live['deployment']['metadata']['generation']==latest['deployment']['generation'])
    source=fresh_provider_source(chain.run.parent/'contract',chain.plan['runId'],chain.run/'provider-inventory.json',require_fresh=False,mode=MODE)
    require(source['providerRunContextSha256']==chain.context['providerRunContextSha256'] and source['feedSession']==chain.context['feedSession'] and 0<=at-source['observedAt']<30)
    from segmented_capture import decode_private,sha
    raw,_=_identity(chain.run/'provider-inventory.json');require(sha(raw)==source['inventoryRawSha256'])
    inventory=decode_private(raw)
    config=_gateway_config(chain,calls,kube_read)
    if initial or chain.plan['executionKind']=='disabled-proof':
        _fleet({k:inventory[k] for k in chain.baseline['fleet']},chain.plan['target']['originalInstanceIds'])
        require(source['privateAddresses']=={r['id']:r['address'] for r in chain.baseline['originals']})
        runtime=calls.call(gate.gateway_disabled);_runtime(runtime,chain.plan,at)
        require({k:v for k,v in runtime.items() if k!='observedAt'}=={k:v for k,v in chain.values['gatewayRuntime'].items() if k!='observedAt'})
        require(config==dict(secretUid=runtime['secretUid'],secretResourceVersion=runtime['secretResourceVersion'],replacementEnabled=False))
        if initial:
            from pgcat_rehearsal_contract import _read_configmap
            cm=_read_configmap(lambda *args:calls.call(kube_read,'ConfigMap','raptor-test','pgcat-provider-inventory'),source['privateAddresses'])
            require(cm['uid']==chain.baseline['configMap']['uid'])
            require(at<instant(chain.plan['deadline']['mutationCutoff']))
            _ready_pair([previous,latest],chain.baseline,chain.context,at)
            require(live['deployment']['metadata']['generation']==chain.baseline['deployment']['generation'])
    chain.unchanged()
    return dict(progress=progress,inventory=inventory,config=config,live=live,at=at)


def _credentials(chain,label,calls,kube_read):
    """Resolve only effective auth keys, keeping values outside returned evidence."""
    binding=chain.plan['serviceBindings'][label];ns=chain.plan['serviceBindings']['namespace']
    service=calls.call(kube_read,'Service',ns,binding['serviceName']);meta=service['metadata'];spec=service['spec']
    require(meta['uid']==binding['serviceUid'] and spec.get('type','ClusterIP')=='ClusterIP' and spec['clusterIP']==binding['clusterIP'] and not spec.get('externalName'))
    require(sum(p.get('port')==binding['port'] and p.get('protocol','TCP')=='TCP' for p in spec['ports'])==1)
    dep=calls.call(kube_read,'Deployment',ns,binding['deploymentName']);dm=dep['metadata'];require(dm['uid']==binding['deploymentUid'] and token(dm['resourceVersion']))
    containers=dep['spec']['template']['spec']['containers'];selected=[c for c in containers if c['name']==binding['containerName']];require(len(selected)==1)
    container=selected[0];keys=('SERVICE_USERNAME','SERVICE_PASSWORD');references={};secrets={}
    def secret(name):
        require(token(name))
        if name not in secrets:
            row=calls.call(kube_read,'Secret',ns,name);require(token(row['metadata']['uid']) and token(row['metadata']['resourceVersion']));secrets[name]=row
        return secrets[name]
    imported=set()
    for source in container.get('envFrom',[]):
        require(type(source) is dict and set(source)=={'secretRef'})
        ref=source['secretRef'];require(set(ref)<={'name','optional'} and ref.get('optional',False) is False and ref['name'] not in imported);imported.add(ref['name'])
        row=secret(ref['name'])
        for key in keys:
            if key in row['data']:require(key not in references);references[key]=(ref['name'],key)
    explicit=set()
    for env in container.get('env',[]):
        if env.get('name') not in keys:continue
        key=env['name'];require(key not in explicit);explicit.add(key)
        exact(env,{'name','valueFrom'});exact(env['valueFrom'],{'secretKeyRef'});ref=env['valueFrom']['secretKeyRef']
        require(set(ref)<={'name','key','optional'} and ref.get('optional',False) is False and token(ref['key']))
        row=secret(ref['name']);require(ref['key'] in row['data']);references[key]=(ref['name'],ref['key'])
    require(set(references)==set(keys))
    values=[];retained=[]
    for key in keys:
        name,entry=references[key];row=secrets[name];raw=row['data'][entry]
        require(type(raw) is str and len(raw)<=8192)
        value=base64.b64decode(raw,validate=True).decode('utf-8');require(1<=len(value)<=4096 and '\n' not in value and '\r' not in value)
        if key=='SERVICE_USERNAME':require(':' not in value)
        values.append(value);retained.append(dict(environmentKey=key,name=name,key=entry,uid=row['metadata']['uid'],resourceVersion=row['metadata']['resourceVersion']))
    origin='http://%s:%d'%(binding['clusterIP'],binding['port'])
    metadata=dict(binding,origin=origin,deploymentResourceVersion=dm['resourceVersion'],secretReferences=retained,
        imports=[dict(name=name,uid=row['metadata']['uid'],resourceVersion=row['metadata']['resourceVersion'],selectedKeys=[k for k in keys if k in row['data']]) for name,row in sorted(secrets.items())])
    return tuple(values),metadata


def _definition(value,plan):
    exact(value,set(plan['plannedInput'])|{'connectionVersion'})
    require({k:v for k,v in value.items() if k!='connectionVersion'}==plan['plannedInput'])
    require(integer(value['connectionVersion'],1));return detached(value)


def _context(value,plan,request_id):
    require(type(value) is dict and set(value)=={'requestId','definition','skills','definitionSha256','environment'})
    require(value['requestId']==request_id and value['skills']==plan['plannedInput']['skills'] and hashed(value['definitionSha256']))
    return dict(requestId=request_id,definition=_definition(value['definition'],plan),skills=detached(value['skills']),definitionSha256=value['definitionSha256'])


def _execution(value,plan,context):
    required={'requestId','attemptId','input','appliedSkills','status','runtimeMode','stage','cleanup','recoveryNeeded'}
    optional={'result','checkpointStatus','cleanupStatus','failureCode','checkpoint','updatedAt'}
    require(type(value) is dict and required<=set(value)<=required|optional)
    require(value['requestId']==context['requestId'] and token(value['attemptId']) and value['appliedSkills']==context['skills'])
    require(type(value['recoveryNeeded']) is bool and all(token(value[k]) for k in ('status','runtimeMode','stage','cleanup')))
    selected=_context(value['input'],plan,context['requestId']);require(selected==context)
    result={k:detached(value[k]) for k in required-{'input'}};result['input']=selected
    if 'result' in value and value['result'] is not None:result['result']=_result(value['result'],plan,context['requestId'],value['attemptId'])
    return result


def _result(value,plan,request_id,attempt):
    required={'requestId','attemptId','selectedModel','actualModel','selectedProvider','actualProvider','answer','evidence','generatedAt','replacement'}
    require(type(value) is dict and required<=set(value)<=required|{'usage'})
    require(value['requestId']==request_id and value['attemptId']==attempt)
    require(value['selectedModel']==value['actualModel']==plan['plannedInput']['model'])
    require(value['selectedProvider']==plan['plannedInput']['providerId']=='codex' and value['actualProvider']=='openai-codex')
    require(value['answer']=='Replacement converged; retained traffic and approval assessment is pending.' and value['evidence']==[])
    instant(value['generatedAt']);replacement=value['replacement']
    exact(replacement,{'version','converged','acceptance','scope','oldInstanceIds','newInstanceIds','desiredCapacity','restartGeneration','actions','observedAt'})
    require(type(replacement['version']) is int and replacement['version']==2 and replacement['converged'] is True and replacement['acceptance']=='pending')
    require(replacement['scope']==plan['controllerScope'] and replacement['oldInstanceIds']==plan['target']['originalInstanceIds'])
    ids=replacement['newInstanceIds'];require(type(ids) is list and len(ids)==2 and all(token(n) for n in ids) and len(set(ids+replacement['oldInstanceIds']))==4)
    require(type(replacement['desiredCapacity']) is int and replacement['desiredCapacity']==2)
    require(type(replacement['restartGeneration']) is int and replacement['restartGeneration']==plan['application']['generation']+1)
    instant(replacement['observedAt']);actions=replacement['actions'];require(type(actions) is list and len(actions)==2)
    for action,pair in zip(actions,[(2,4),(4,2)]):
        exact(action,{'actionId','previousDesiredCapacity','desiredCapacity','outcome'})
        require(token(action['actionId']) and action['outcome'] in ('submitted','unknown'))
        require(type(action['previousDesiredCapacity']) is int and type(action['desiredCapacity']) is int and (action['previousDesiredCapacity'],action['desiredCapacity'])==pair)
    require(actions[0]['actionId']!=actions[1]['actionId'])
    for row in value.get('usage',[]):
        exact(row,{'input','output','totalTokens'});require(all(integer(v) for v in row.values()))
    return detached(value)


class _Projection(dict):
    pass


@sanitized
def read_ingress(run_dir,selector,*,gate,kube_read,http_get,now):
    from segmented_capture import decode_private,canonical
    chain=_Chain(run_dir);calls=_Calls();native=chain.plan['executionKind']=='native-replacement'
    state=_action_gate(chain,gate,calls,kube_read,now,'native-ingress')
    capture_prefix=_capture_prefix(chain,state['progress']['latest'])
    record=dict(schemaVersion=1,executionKind=chain.plan['executionKind'],runId=chain.plan['runId'],planSha256=chain.context['planSha256'],
        readySha256=chain.ready_sha,readAt=state['at'],serviceBindings=None,context=None,execution=None,events=None,approvals=None,diagnostic=None)
    if not native:
        exact(selector,set());record['diagnostic']=_diagnostic(chain,state)
    else:
        exact(selector,{'requestId','approvalIds'});request_id=selector['requestId'];identifier(request_id)
        ids=selector['approvalIds'];require(type(ids) is list and len(ids)<=100 and len(set(ids))==len(ids))
        for value in ids:identifier(value)
        auth={};bindings={}
        for label in ('gateway','openapi'):auth[label],bindings[label]=_credentials(chain,label,calls,kube_read)
        secrets=[v for pair in auth.values() for v in pair]
        def get(label,path):
            raw=calls.call(http_get,bindings[label]['origin'],path,auth[label])
            require(type(raw) is bytes and len(raw)<=MAX_RESPONSE)
            return decode_private(raw)
        prefix='/v1/requests/'+request_id
        response=get('openapi',prefix+'/context');exact(response,{'data'})
        ctx=_context(response['data'],chain.plan,request_id)
        response=get('gateway',prefix);exact(response,{'data'});first=_execution(response['data'],chain.plan,ctx)
        events=[];cursor=0;seen=set();pages=0
        def exhaust():
            nonlocal cursor,pages
            while True:
                pages+=1;require(pages<=100)
                page=get('gateway',prefix+'/events?after='+str(cursor));exact(page,{'data','nextAfter','hasMore'})
                require(type(page['data']) is list and len(page['data'])<=100 and type(page['hasMore']) is bool and integer(page['nextAfter']))
                for row in page['data']:
                    exact(row,{'eventId','requestId','attemptId','sequence','occurredAt','kind','summary','details','evidenceMode'})
                    require(row['requestId']==request_id and token(row['eventId']) and row['eventId'] not in seen and token(row['attemptId']))
                    require(integer(row['sequence'],1) and row['sequence']>cursor and token(row['kind']) and token(row['evidenceMode']))
                    instant(row['occurredAt']);require(type(row['summary']) is str and len(row['summary'])<=1024)
                    details=row['details'];require(type(details) is dict)
                    retained={k:details[k] for k in ('tool','status','runtimeSequence') if k in details}
                    if row['kind'] in ('tool_start','tool_end','tool_result'):require(bool(retained))
                    if retained:
                        require(set(retained)=={'tool','status','runtimeSequence'} and token(retained['tool']) and token(retained['status']) and integer(retained['runtimeSequence'],1))
                    # Free-text summaries and arbitrary runtime detail fields are not evidence.
                    event={k:row[k] for k in set(row)-{'summary','details'}};event['details']=retained
                    events.append(event);seen.add(row['eventId']);cursor=row['sequence'];require(len(events)<=10000)
                require(page['nextAfter']==cursor and (not page['hasMore'] or len(page['data'])>0))
                if not page['hasMore']:break
        exhaust();response=get('gateway',prefix);exact(response,{'data'});last=_execution(response['data'],chain.plan,ctx)
        require((first['attemptId'],first.get('result'))==(last['attemptId'],last.get('result')))
        exhaust();response=get('gateway',prefix);exact(response,{'data'});require(_execution(response['data'],chain.plan,ctx)==last)
        approvals=[]
        for approval_id in ids:
            response=get('openapi','/v1/approvals/'+approval_id);exact(response,{'data'})
            approvals.append(_approval(response['data'],approval_id,chain.plan,request_id,last.get('result')))
        for label in ('gateway','openapi'):
            current,current_binding=_credentials(chain,label,calls,kube_read);require(current==auth[label] and current_binding==bindings[label])
        require(_gateway_config(chain,calls,kube_read)==state['config'])
        record.update(serviceBindings=dict(namespace='raptor-system',gateway=bindings['gateway'],openapi=bindings['openapi'],gatewayConfiguration=state['config']),
            context=ctx,execution=last,events=events,approvals=approvals)
        encoded=canonical(record).decode('ascii')
        require(all(json.dumps(secret)[1:-1] not in encoded for secret in secrets))
    _action_gate(chain,gate,calls,kube_read,now,'native-ingress')
    chain.unchanged()
    require(_capture_prefix(chain,state['progress']['latest'])==capture_prefix)
    result=_Projection(record);result._chain=chain;result._record=canonical(record)
    result._capture=(state['progress']['latest'],capture_prefix)
    return result


@sanitized
def seal_ingress(run_dir,projection):
    from segmented_capture import canonical,sha,open_directory,check_directory
    require(type(projection) is _Projection and projection._chain.run==Path(run_dir))
    require(canonical(dict(projection))==projection._record)
    projection._chain.unchanged();chain=_Chain(run_dir)
    require(chain.files==projection._chain.files)
    require(_capture_prefix(chain,projection._capture[0])==projection._capture[1])
    fd=open_directory(chain.run)
    try:
        for n in range(1,10000):
            name='native-ingress-%04d.json'%n
            try:raw=_publish(fd,name,dict(projection));break
            except FileExistsError:continue
        else:raise ValueError('native ingress limit')
        check_directory(fd,chain.run)
        return dict(ingressSha256=sha(raw),record=dict(projection))
    finally:os.close(fd)


def _publish(fd,name,value):
    from segmented_capture import canonical
    raw=canonical(value);require(len(raw)<=32*1024*1024)
    item=os.open(name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=fd)
    try:
        os.fchmod(item,0o600)
        remaining=raw
        while remaining:
            count=os.write(item,remaining);require(count>0);remaining=remaining[count:]
        os.fsync(item)
    finally:os.close(item)
    os.fsync(fd);return raw


def _approval(value,approval_id,plan,request_id,result):
    exact(value,{'approvalId','requestId','actionId','binding','state','guidance'})
    require(result is not None and value['approvalId']==approval_id and value['requestId']==request_id and value['state']=='approved')
    action=result['replacement']['actions'][1];require(value['actionId']==action['actionId'])
    expected=dict(requestId=request_id,actionId=action['actionId'],interface='ess.scale-in',envCode=plan['plannedInput']['envCode'],
        target=dict(proxyCode=plan['controllerScope']['proxyCode'],groupId=plan['controllerScope']['groupId']),parameters=dict(desiredCapacity=2))
    require(value['binding']==expected)
    return {k:detached(value[k]) for k in ('approvalId','requestId','actionId','binding','state')}

RECEIPT_KEYS={'schemaVersion','kind','runId','planSha256','readySha256','intentSha256','deploymentUid','previousGeneration',
    'restartGeneration','previousResourceVersion','responseResourceVersion','startedAt','completedAt','responseSha256','readbackSha256'}


def _deployment_projection(dep,chain):
    """Never retain Deployment environment or other potential credential fields."""
    meta=dep['metadata'];bound=chain.plan['application']
    require(all(meta[k]==bound[k] for k in ('uid','name','namespace')) and integer(meta['generation'],1) and token(meta['resourceVersion']))
    strategy=dep['spec']['strategy'];require(strategy==dict(type='RollingUpdate',rollingUpdate=dict(maxSurge=1,maxUnavailable=0)))
    require(type(strategy['rollingUpdate']['maxSurge']) is int and type(strategy['rollingUpdate']['maxUnavailable']) is int)
    containers=dep['spec']['template']['spec']['containers'];require(len(containers)==1 and containers[0]['name']=='client')
    require(containers[0]['image'].endswith('@'+bound['imageDigest']))
    annotation=dep['spec']['template'].get('metadata',{}).get('annotations',{}).get('kubectl.kubernetes.io/restartedAt')
    require(annotation is None or type(annotation) is str)
    return dict(uid=meta['uid'],generation=meta['generation'],resourceVersion=meta['resourceVersion'],imageDigest=bound['imageDigest'],strategy=detached(strategy),restartedAt=annotation)


@sanitized
def restart_disabled(run_dir,*,gate,kube_read,kube_patch,now):
    from segmented_capture import open_directory,sha,canonical,check_directory
    chain=_Chain(run_dir);require(chain.plan['executionKind']=='disabled-proof');calls=_Calls()
    state=_action_gate(chain,gate,calls,kube_read,now,'native-disabled-restart',initial=True)
    current=calls.call(kube_read,'Deployment','raptor-test','rdev-test-client-client')
    initial=state['live']['deployment'];before=_deployment_projection(current,chain)
    require(before['generation']==chain.plan['application']['generation'])
    require(current['metadata']['uid']==initial['metadata']['uid'] and current['metadata']['resourceVersion']==initial['metadata']['resourceVersion'] and current['spec']==initial['spec'])
    fresh=_action_gate(chain,gate,calls,kube_read,now,'native-disabled-restart',initial=True)
    require(fresh['live']['deployment']['spec']==current['spec'] and fresh['live']['deployment']['metadata']['resourceVersion']==before['resourceVersion'])
    started=now();require(number(started) and started<instant(chain.plan['deadline']['mutationCutoff']))
    restarted=datetime.datetime.fromtimestamp(started,datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    require(before['restartedAt']!=restarted)
    patch=dict(metadata=dict(uid=before['uid'],resourceVersion=before['resourceVersion']),spec=dict(template=dict(metadata=dict(annotations={'kubectl.kubernetes.io/restartedAt':restarted}))))
    expected=detached(current['spec']);expected['template'].setdefault('metadata',{}).setdefault('annotations',{})['kubectl.kubernetes.io/restartedAt']=restarted
    intent=dict(schemaVersion=1,kind='disabled-proof',runId=chain.plan['runId'],planSha256=chain.context['planSha256'],readySha256=chain.ready_sha,
        deployment=before,startedAt=started,restartedAt=restarted)
    # The intent is the durable one-use fence. Never retry any I/O after it exists.
    chain.unchanged();fd=open_directory(chain.run)
    try:
        intent_raw=_publish(fd,'native-restart.intent',intent);intent_sha=sha(intent_raw)
        try:
            response=calls.call(kube_patch,'raptor-test','rdev-test-client-client',patch)
            after=_deployment_projection(response,chain)
            require(after['generation']==before['generation']+1 and after['resourceVersion']!=before['resourceVersion'] and response['spec']==expected)
            readback=calls.call(kube_read,'Deployment','raptor-test','rdev-test-client-client');read_projection=_deployment_projection(readback,chain)
            require(read_projection==after and readback['spec']==expected)
            completed=now();require(number(completed) and started<=completed<instant(chain.plan['deadline']['closureDeadline']))
            chain.unchanged();check_directory(fd,chain.run)
            receipt=dict(schemaVersion=1,kind='disabled-proof',runId=chain.plan['runId'],planSha256=chain.context['planSha256'],readySha256=chain.ready_sha,
                intentSha256=intent_sha,deploymentUid=before['uid'],previousGeneration=before['generation'],restartGeneration=after['generation'],
                previousResourceVersion=before['resourceVersion'],responseResourceVersion=after['resourceVersion'],startedAt=started,completedAt=completed,
                responseSha256=sha(canonical(after)),readbackSha256=sha(canonical(read_projection)))
            _publish(fd,'native-restart-receipt.json',receipt);return receipt
        except Exception:
            _publish(fd,'native-restart-uncertain.json',dict(schemaVersion=1,kind='disabled-proof',runId=chain.plan['runId'],intentSha256=intent_sha,
                startedAt=started,outcome='uncertain'))
            raise ValueError('uncertain native restart') from None
    finally:os.close(fd)


def _diagnostic(chain,state):
    from segmented_capture import decode_private,sha
    raw,_=_identity(chain.run/'native-restart-receipt.json');receipt=decode_private(raw);exact(receipt,RECEIPT_KEYS)
    intent_raw,_=_identity(chain.run/'native-restart.intent');intent=decode_private(intent_raw)
    require(type(receipt['schemaVersion']) is int and receipt['schemaVersion']==1 and receipt['kind']=='disabled-proof')
    require(receipt['runId']==chain.plan['runId'] and receipt['planSha256']==chain.context['planSha256'] and receipt['readySha256']==chain.ready_sha)
    require(receipt['intentSha256']==sha(intent_raw) and receipt['deploymentUid']==chain.plan['application']['uid'])
    require(type(receipt['previousGeneration']) is int and receipt['previousGeneration']==chain.plan['application']['generation'])
    require(type(receipt['restartGeneration']) is int and receipt['restartGeneration']==receipt['previousGeneration']+1)
    require(intent['runId']==receipt['runId'] and intent['planSha256']==receipt['planSha256'] and intent['readySha256']==receipt['readySha256'])
    require(receipt['previousResourceVersion']==intent['deployment']['resourceVersion'] and token(receipt['responseResourceVersion']) and receipt['responseResourceVersion']!=receipt['previousResourceVersion'])
    require(number(receipt['startedAt']) and number(receipt['completedAt']) and receipt['startedAt']==intent['startedAt'] and chain.ready['readyAt']<=receipt['startedAt']<=receipt['completedAt'])
    require(hashed(receipt['responseSha256']) and receipt['responseSha256']==receipt['readbackSha256'])
    return dict(restartReceiptSha256=sha(raw),receipt=receipt,gatewayConfiguration=state['config'],inventory=state['inventory'])


def _history(chain,latest):
    """Read the committed prefix only, bounding every row and retaining its identity."""
    from segmented_capture import open_directory,private_info,read_frame,decode_private,sha,FRAME_LIMIT
    fd=open_directory(chain.run/'native-capture');count=0
    try:
        for n in range(1,int(latest['basename'][8:12])+1):
            name='segment-%04d.jsonl'%n
            item=os.open(name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
            try:
                info=os.fstat(item);private_info(info);end=latest['offset']+latest['length'] if name==latest['basename'] else info.st_size
                with os.fdopen(os.dup(item),'rb') as stream:
                    offset=0
                    while offset<end:
                        raw=stream.readline(FRAME_LIMIT+1);require(0<len(raw)<=FRAME_LIMIT and raw.endswith(b'\n') and offset+len(raw)<=end)
                        value=decode_private(raw);ref=dict(basename=name,device=info.st_dev,inode=info.st_ino,offset=offset,length=len(raw),sha256=sha(raw),at=value['at'])
                        value=read_frame(fd,ref);count+=1;require(count<=5406)
                        yield value
                        offset+=len(raw)
                    require(offset==end)
            finally:os.close(item)
    finally:os.close(fd)


@sanitized
def seal_operation_binding(run_dir,ingress_path):
    from segmented_capture import sha,canonical,decode_private,read_progress,open_directory
    from pgcat_native_evidence import validate_operation_binding,_replay
    chain=_Chain(run_dir);path=Path(ingress_path)
    require(path.parent==chain.run and re.fullmatch(r'native-ingress-[0-9]{4}\.json',path.name))
    raw,identity=_identity(path,32*1024*1024);record=decode_private(raw);exact(record,RECORD_KEYS);require(canonical(record)==raw)
    require(record['runId']==chain.plan['runId'] and record['executionKind']==chain.plan['executionKind'] and record['planSha256']==chain.context['planSha256'] and record['readySha256']==chain.ready_sha)
    progress,_,latest=read_progress(chain.run/'native-capture',chain.context)
    require(latest is not None and not any(progress['flags'].values()))
    fd=open_directory(chain.run/'native-capture')
    try:snapshot,count,_=_replay(fd,chain.context,progress['latest'])
    finally:os.close(fd)
    require(snapshot==progress['nativeCohort'] and count==progress['committedFrames'])
    binding=dict(schemaVersion=1,kind=chain.plan['executionKind'],planSha256=chain.context['planSha256'],restartGeneration=chain.plan['application']['generation']+1,ingressSha256=sha(raw))
    if chain.plan['executionKind']=='native-replacement':
        require(record['diagnostic'] is None and record['context'] is not None)
        ctx=dict(record['context'],environment={});ctx=_context(ctx,chain.plan,record['context']['requestId'])
        execution=record['execution']
        verified=_execution(dict(execution,input=dict(execution['input'],environment={})),chain.plan,ctx)
        require(verified==execution and execution['input']==ctx and execution['status']=='completed' and execution['recoveryNeeded'] is False)
        result=_result(execution['result'],chain.plan,ctx['requestId'],execution['attemptId'])
        require(ctx['definitionSha256']==execution['input']['definitionSha256'] and ctx['definitionSha256']!=chain.context['planSha256'])
        replacement=result['replacement'];originals=set(chain.plan['target']['originalInstanceIds']);replacement_pair=None;final=None
        for frame in _history(chain,progress['latest']):
            inv=frame['inventory'];members=inv['members']
            if replacement_pair is None and inv['desired']==4 and len(members)==4:
                replacement_pair=_replacement_pair(inv,originals)
            if replacement_pair is not None:
                require(set(members)<=originals|set(replacement_pair));final=inv
        require(replacement_pair is not None and sorted(replacement['newInstanceIds'])==replacement_pair and final is not None)
        _fleet({k:final[k] for k in chain.baseline['fleet']},replacement_pair)
        require(snapshot['observedGeneration']==binding['restartGeneration'])
        mutations={'mcp__infra__db_proxy_scale','mcp__infra__db_proxy_node_protection_set','mcp__infra__db_proxy_nodes_deregister','mcp__infra__deployment_restart'}
        scales=[];previous_time=None
        for event in record['events']:
            if event['kind']=='tool_start' and event['details'].get('tool') in mutations:
                require(event['attemptId']==execution['attemptId'] and event['details'].get('status')=='started' and instant(event['occurredAt'])>chain.ready['readyAt'])
                when=instant(event['occurredAt']);require(previous_time is None or previous_time<=when);previous_time=when
                if event['details']['tool']=='mcp__infra__db_proxy_scale':scales.append(event)
        require(len(scales)==2)
        require(instant(scales[-1]['occurredAt'])<=instant(result['replacement']['observedAt']))
        binding.update(requestId=ctx['requestId'],definitionSha256=ctx['definitionSha256'],skillsCommit=ctx['skills']['commitSha'],model=chain.plan['plannedInput']['model'],
            resultSha256=sha(canonical(result)),eventsSha256=sha(canonical(record['events'])))
    else:
        require(all(record[k] is None for k in ('context','execution','events','approvals')))
        diagnostic=_diagnostic(chain,dict(config=record['diagnostic']['gatewayConfiguration'],inventory=record['diagnostic']['inventory']))
        require(diagnostic==record['diagnostic'])
        for frame in _history(chain,progress['latest']):_fleet({k:frame['inventory'][k] for k in chain.baseline['fleet']},chain.plan['target']['originalInstanceIds'])
        binding['restartReceiptSha256']=diagnostic['restartReceiptSha256']
    validate_operation_binding(binding,chain.context);chain.unchanged();require(_identity(path,32*1024*1024)==(raw,identity))
    fd=open_directory(chain.run)
    try:_publish(fd,'native-operation-binding.json',binding)
    finally:os.close(fd)
    return binding


def _replacement_pair(inventory,originals):
    from capture_window import clean_inventory
    require(clean_inventory(inventory)==inventory)
    members=inventory['members']
    if inventory['desired']!=4 or len(members)!=4 or not originals.issubset(members):return None
    pair=sorted(set(members)-originals)
    if len(pair)!=2 or not set(pair).issubset(inventory['healthyRegistered']):return None
    states={r['id']:r for r in inventory['memberStates']};backends={r['id']:r for r in inventory['backendStates']}
    for node in pair:
        if states.get(node)!=dict(id=node,lifecycle='Protected',health='Healthy',addressKnown=True):return None
        row=backends.get(node)
        if row is None or row['status']!='Available' or row['weight']<=0 or row['listenerHealth']!='Normal':return None
    return pair


def _capture_prefix(chain,latest):
    import hashlib
    from segmented_capture import open_directory,private_info,check_directory
    directory=chain.run/'native-capture';fd=open_directory(directory);retained=[]
    try:
        for n in range(1,int(latest['basename'][8:12])+1):
            name='segment-%04d.jsonl'%n
            item=os.open(name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
            try:
                before=os.fstat(item);private_info(before)
                end=latest['offset']+latest['length'] if name==latest['basename'] else before.st_size
                require(0<end<=before.st_size);offset=0;digest=hashlib.sha256()
                while offset<end:
                    raw=os.pread(item,min(128*1024,end-offset),offset);require(bool(raw));digest.update(raw);offset+=len(raw)
                after=os.stat(name,dir_fd=fd,follow_symlinks=False);private_info(after)
                require((before.st_dev,before.st_ino)==(after.st_dev,after.st_ino) and after.st_size>=end)
                retained.append((name,before.st_dev,before.st_ino,end,digest.hexdigest()))
            finally:os.close(item)
        check_directory(fd,directory);return retained
    finally:os.close(fd)


def _ready_pair(frames,baseline,context,at):
    require(type(frames) is list and len(frames)==2)
    for frame in frames:_baseline_frame(frame,baseline,context,at)
    before={p['uid']:p['snapshot']['sample'] for p in frames[0]['pods']}
    for pod in frames[1]['pods']:
        old=before[pod['uid']];new=pod['snapshot']['sample']
        require(new['sequence']>old['sequence'] and new['Attempts']>old['Attempts'] and new['Success']>old['Success'])
        require(all(new[k]>=old[k] for k in ('Scheduled','Attempts','Success','Failure','Timeout','Ambiguous','Skipped')))
        require(all(new[k]==0 for k in ('Failure','Timeout','Ambiguous','Skipped')))
