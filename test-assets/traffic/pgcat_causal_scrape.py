"""Fixed-source causal scrape evidence; no native action admission.

Policy fingerprints bind the independently parsed fixed config/rules reviewed on
2026-11-02 to Prometheus 3.15.0 revision 5241a27. They are release material,
not a caller's assertion about a newly captured hash. Raw config stays in memory.

At this revision scrape-pool sync is serialized and removed loops finish before
replacement. One accepted label/address hash per pool bounds pre-watermark
in-flight work to old and incoming loops. THREE complete samples are necessary;
numeric timestamp order cannot identify the causal sample after clock reversal.
"""
import copy
import hashlib
import ipaddress
import json
import math
import os
import re
import time
import urllib.parse
import weakref

SOURCE_POLICY = {
    'image':'prom/prometheus@sha256:efd719c99d83b060d9daefdcf00360461adf279f45ef5391f8d111892118753e',
    'version':'3.15.0', 'revision':'5241a27fe3c6983549fccc32f6e65917408c63cd',
    'configSha256':'fabe5666d3e056fa9a48ef39d3b75b478976ddbe1f9125952916e79d9c276361',
    'configMapConfigSha256':'90301cdcb4fe7d26f3dd99b042454b6ae79b6e3749b9e512e800dd65e40f8dc6',
    'rulesSha256':'540629f75ae272f3d4c43cfcae905ee11f520b9527abc528f561d9a759d17638'}
METRICS_NAME = 'rdev-test-metrics-metrics'
ARGS = ['--config.file=/config/prometheus.yml', '--storage.tsdb.path=/data',
        '--storage.tsdb.retention.time=2h', '--storage.tsdb.retention.size=128MB']
SCOPE = dict(job='pgcat',env='rdev.ali',db_proxy_code='DP00001',target_db_code='D00002')
FAMILIES = ('pgcat_pools_cl_active','pgcat_pools_cl_idle','pgcat_pools_cl_waiting')
ENDPOINTS = frozenset('/api/v1/status/'+p for p in ('runtimeinfo','buildinfo','config','flags','tsdb')) | frozenset(['/api/v1/targets?state=active','/api/v1/rules'])
# Actual /api/v1/rules definitions independently compared with the reviewed
# ConfigMap on 2026-11-02. Current file bytes alone do not prove loaded rules.
_ACTIVE_RULES = [{'name': 'infra-pgcat-client-observation',
  'file': '/config/recording-rules.yaml',
  'interval': 5,
  'limit': 0,
  'rules': [{'type': 'recording',
             'name': 'infra_pgcat_connected_clients',
             'query': 'sum by (env, db_proxy_code, target_db_code, ecs_instance_id) '
                      '(pgcat_pools_cl_active + pgcat_pools_cl_idle + pgcat_pools_cl_waiting) '
                      'unless on (env, db_proxy_code, target_db_code, ecs_instance_id) count by '
                      '(env, db_proxy_code, target_db_code, ecs_instance_id) (count by (env, '
                      'db_proxy_code, target_db_code, ecs_instance_id, pool, user) '
                      '({__name__=~"pgcat_pools_cl_(active|idle|waiting)"}) != 3)',
             'labels': {}},
            {'type': 'recording',
             'name': 'infra_pgcat_client_observation_timestamp_seconds',
             'query': 'min by (env, db_proxy_code, target_db_code, ecs_instance_id) '
                      '(label_replace(timestamp(pgcat_pools_cl_active), "source_metric", "active", '
                      '"", "") or label_replace(timestamp(pgcat_pools_cl_idle), "source_metric", '
                      '"idle", "", "") or label_replace(timestamp(pgcat_pools_cl_waiting), '
                      '"source_metric", "waiting", "", "")) and on (env, db_proxy_code, '
                      'target_db_code, ecs_instance_id) infra_pgcat_connected_clients',
             'labels': {}}]}]
_pending = weakref.WeakKeyDictionary()
_issued = weakref.WeakKeyDictionary()


def _check(ok):
    if not ok:raise ValueError('causal scrape')


def _token(value):
    return type(value) is str and re.fullmatch(r'[A-Za-z0-9_.:-]{1,256}',value) is not None


def _number(value):
    return type(value) in (int,float) and math.isfinite(value)


def _sha(value):
    return hashlib.sha256(value).hexdigest()


def _canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')


def private_ipv4(value):
    _check(type(value) is str)
    address=ipaddress.IPv4Address(value)
    _check(str(address)==value and any(address in ipaddress.IPv4Network(n) for n in ('10.0.0.0/8','172.16.0.0/12','192.168.0.0/16')))
    return value


def _nodes(nodes):
    _check(type(nodes) is list and len(nodes)==2)
    result={}
    for node in nodes:
        _check(type(node) is dict and set(node)=={'id','address'} and _token(node['id']) and node['id'] not in result)
        result[node['id']]=private_ipv4(node['address'])
    _check(len(set(result.values()))==2)
    return result


def query_path(nodes):
    """One instant range selector: original sample pairs, never query_range."""
    ids=sorted(_nodes(nodes))
    _check(all(re.fullmatch(r'[A-Za-z0-9_-]{1,128}',n) for n in ids))
    expression='{__name__=~"up|pgcat_pools_cl_(active|idle|waiting)",job="pgcat",env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002",ecs_instance_id=~"'+('|'.join(ids))+'"}[15s]'
    return '/api/v1/query?'+urllib.parse.urlencode({'query':expression})


def permitted_path(path):
    if path in ENDPOINTS:return True
    if type(path) is not str or len(path)>1024:return False
    parsed=urllib.parse.urlsplit(path)
    if parsed.path!='/api/v1/query' or parsed.scheme or parsed.netloc or parsed.fragment:return False
    query=urllib.parse.parse_qs(parsed.query)
    if set(query)!={'query'} or len(query['query'])!=1:return False
    match=re.search(r'ecs_instance_id=~"([A-Za-z0-9_-]{1,128})\|([A-Za-z0-9_-]{1,128})"',query['query'][0])
    if not match:return False
    try:return path==query_path([dict(id=match.group(1),address='10.0.0.1'),dict(id=match.group(2),address='10.0.0.2')])
    except Exception:return False


def _metadata(value,name=None):
    _check(type(value) is dict and value.get('namespace')=='raptor-test' and not value.get('deletionTimestamp') and _token(value.get('uid')))
    if name is not None:_check(value.get('name')==name)


def _owner(value,kind,uid):
    rows=[r for r in value.get('ownerReferences',[]) if r.get('controller') is True]
    _check(len(rows)==1 and rows[0].get('kind')==kind and rows[0].get('uid')==uid)


def kubernetes_identity(kube):
    """Validate exact deployment/owner chain before assigning a direct Pod origin."""
    _check(type(kube) is dict and set(kube)=={'deployment','replicaSets','pods','configMap'})
    dep=kube['deployment'];meta=dep['metadata'];_metadata(meta,METRICS_NAME)
    _check(type(meta['generation']) is int and meta['generation']>=1 and type(dep['spec']['replicas']) is int and dep['spec']['replicas']==1)
    status=dep['status'];_check(status['observedGeneration']==meta['generation'])
    _check(all(type(status[k]) is int and status[k]==1 for k in ('replicas','updatedReplicas','readyReplicas','availableReplicas')))
    pods=kube['pods']['items'];_check(type(pods) is list and len(pods)==1)
    pod=pods[0];pm=pod['metadata'];_metadata(pm)
    _check(_token(pm['name']) and pm['labels'].get('app.kubernetes.io/name')==METRICS_NAME)
    owners=[r for r in pm.get('ownerReferences',[]) if r.get('controller') is True]
    _check(len(owners)==1 and owners[0].get('kind')=='ReplicaSet' and _token(owners[0].get('uid')))
    rss=kube['replicaSets']['items'];_check(type(rss) is list and 1<=len(rss)<=16)
    matching=[r for r in rss if r['metadata']['uid']==owners[0]['uid']];_check(len(matching)==1)
    _metadata(matching[0]['metadata']);_owner(matching[0]['metadata'],'Deployment',meta['uid'])
    for spec in (dep['spec']['template']['spec'],pod['spec']):
        _check(not spec.get('initContainers') and not spec.get('ephemeralContainers') and not spec.get('hostNetwork',False))
        containers=spec['containers'];_check(len(containers)==1)
        c=containers[0];_check(c['name']=='prometheus' and c['image']==SOURCE_POLICY['image'] and c['args']==ARGS and not c.get('command') and not c.get('env') and not c.get('envFrom'))
        mounts=c['volumeMounts'];_check(mounts==[dict(name='config',mountPath='/config',readOnly=True),dict(name='inventory',mountPath='/inventory',readOnly=True),dict(name='data',mountPath='/data')])
        volumes=copy.deepcopy(spec['volumes'])
        _check(type(volumes) is list and len(volumes)==3)
        for volume in volumes[:2]:
            cm_ref=volume['configMap']
            if 'defaultMode' in cm_ref:_check(type(cm_ref['defaultMode']) is int and cm_ref.pop('defaultMode')==420)
        _check(volumes==[dict(name='config',configMap=dict(name=METRICS_NAME)),dict(name='inventory',configMap=dict(name='pgcat-provider-inventory')),dict(name='data',emptyDir=dict(sizeLimit='256Mi'))])
    ps=pod['status'];_check(ps['phase']=='Running')
    ready=[r for r in ps['conditions'] if r.get('type')=='Ready'];_check(len(ready)==1 and ready[0]['status']=='True')
    statuses=ps['containerStatuses'];_check(len(statuses)==1)
    c=statuses[0];_check(c['name']=='prometheus' and c['ready'] is True and type(c['restartCount']) is int and c['restartCount']>=0)
    _check(type(c['containerID']) is str and re.fullmatch(r'containerd://[0-9a-f]{64}',c['containerID']))
    _check(type(c['imageID']) is str and c['imageID'].endswith('@'+SOURCE_POLICY['image'].split('@')[1]))
    _check(set(c['state'])=={'running'} and _token(c['state']['running']['startedAt']))
    cm=kube['configMap'];_metadata(cm['metadata'],METRICS_NAME);_check(_token(cm['metadata']['resourceVersion']))
    _check(set(cm['data'])=={'prometheus.yml','recording-rules.yaml'} and not cm.get('binaryData'))
    for key,policy_key in [('prometheus.yml','configMapConfigSha256'),('recording-rules.yaml','rulesSha256')]:
        raw=cm['data'][key];_check(type(raw) is str and len(raw)<=131072 and _sha(raw.encode('utf-8'))==SOURCE_POLICY[policy_key])
    return dict(deploymentUid=meta['uid'],deploymentGeneration=meta['generation'],replicaSetUid=owners[0]['uid'],podName=pm['name'],podUid=pm['uid'],containerId=c['containerID'],containerStart=c['state']['running']['startedAt'],restartCount=c['restartCount'],image=SOURCE_POLICY['image'],imageId=c['imageID'],origin='http://'+private_ipv4(ps['podIP'])+':9090',configMapUid=cm['metadata']['uid'],configMapResourceVersion=cm['metadata']['resourceVersion'])


def _active_rules(value):
    """Full unfiltered loaded manager projection, excluding evaluation progress."""
    _check(type(value) is dict and set(value)=={'groups'})
    groups=value['groups'];_check(type(groups) is list and len(groups)==1)
    group=groups[0];required={'name','file','interval','limit','rules'}
    progress={'evaluationTime','lastEvaluation'}
    _check(type(group) is dict and required<=set(group) and set(group)<=required|progress)
    _check(_number(group['interval']) and group['interval']==5 and type(group['limit']) is int and group['limit']==0)
    rules=group['rules'];_check(type(rules) is list and len(rules)==2)
    projected=[]
    for rule in rules:
        needed={'name','query','type','health'}
        _check(type(rule) is dict and needed<=set(rule) and set(rule)<=needed|progress|{'labels','lastError'})
        _check(rule['type']=='recording' and rule['health']=='ok' and not rule.get('lastError'))
        _check(type(rule['query']) is str and len(rule['query'])<=4096 and type(rule.get('labels',{})) is dict)
        projected.append(dict(type=rule['type'],name=rule['name'],query=rule['query'],labels=copy.deepcopy(rule.get('labels',{}))))
    result=[dict(name=group['name'],file=group['file'],interval=5,limit=0,rules=projected)]
    _check(result==_ACTIVE_RULES)
    return result


def _source(source,policy):
    _check(type(policy) is dict and policy==SOURCE_POLICY)
    _check(type(source) is dict and set(source)=={'kubernetes','runtime','build','config','flags','targets','rules'})
    identity=kubernetes_identity(source['kubernetes'])
    active_rules=_active_rules(source['rules'])
    identity.update(activeRules=active_rules,activeRulesSha256=_sha(_canonical(active_rules)))
    raw=source['config'];_check(type(raw) is str and len(raw)<=131072 and _sha(raw.encode('utf-8'))==policy['configSha256'])
    build=source['build'];_check(build['version']==policy['version'] and build['revision']==policy['revision'])
    runtime=source['runtime'];_check(runtime['reloadConfigSuccess'] is True and type(runtime['corruptionCount']) is int and runtime['corruptionCount']==0)
    _check(_token(runtime['startTime']) and _token(runtime['lastConfigTime']))
    flags=source['flags'];_check(type(flags) is dict and len(flags)<=256 and all(type(k) is str and type(v) is str for k,v in flags.items()))
    for key,value in [('web.enable-remote-write-receiver','false'),('web.enable-otlp-receiver','false'),('enable-feature',''),('scrape.discovery-reload-interval','5s')]:_check(flags.get(key)==value)
    _check(flags.get('storage.tsdb.allow-overlapping-blocks','false')=='false')
    _check(flags.get('scrape-on-shutdown','false')=='false')
    active=source['targets']['activeTargets'];_check(type(active) is list and 2<=len(active)<=8)
    accepted=[];seen_nodes=set();seen_addresses=set();seen_hashes=set()
    for target in active:
        labels=target['labels'];pool=target['scrapePool']
        if pool=='test-client':
            _check(labels.get('job')=='test-client');continue
        _check(pool=='pgcat' and type(labels) is dict and set(labels)==set(SCOPE)|{'instance','ecs_instance_id'} and all(labels.get(k)==v for k,v in SCOPE.items()))
        node=labels['ecs_instance_id'];_check(_token(node) and node not in seen_nodes)
        address=labels['instance'];_check(type(address) is str and address.endswith(':9930'));private_ipv4(address[:-5])
        _check(address not in seen_addresses and target['scrapeUrl']=='http://'+address+'/metrics' and target['discoveredLabels']['__address__']==address)
        _check(target['health']=='up' and target['scrapeInterval']=='5s' and target['scrapeTimeout']=='3s')
        # API exposes labels + scrape URL, the complete accepted loop identity;
        # this is a stable digest of that identity, not an invented API hash.
        loop_hash=_sha(_canonical(dict(pool=pool,labels=labels,url=target['scrapeUrl'])))
        _check(loop_hash not in seen_hashes);seen_hashes.add(loop_hash);seen_nodes.add(node);seen_addresses.add(address)
        accepted.append(dict(node=node,address=address[:-5],labels=copy.deepcopy(labels),loopIdentitySha256=loop_hash))
    _check(2<=len(accepted)<=4)
    identity.update(version=build['version'],revision=build['revision'],processStart=runtime['startTime'],lastConfigTime=runtime['lastConfigTime'],configSha256=policy['configSha256'],configMapConfigSha256=policy['configMapConfigSha256'],rulesSha256=policy['rulesSha256'],flagsSha256=_sha(_canonical(flags)),targets=sorted(accepted,key=lambda r:r['node']))
    return identity


def validate_source(source,policy):
    try:return _source(source,policy)
    except Exception:pass
    raise ValueError('causal scrape')


class _Insufficient(ValueError):
    def __init__(self,groups):
        super().__init__('causal scrape pending')
        self.groups=groups


def _samples(watermark_ms,matrix,*,nodes,metric_groups,now,max_combined):
    addresses=_nodes(nodes)
    _check(type(watermark_ms) is int and 0<watermark_ms<2**53 and _number(now))
    _check(max_combined is None or type(max_combined) is int and max_combined==1)
    _check(type(metric_groups) is list and 2<=len(metric_groups)<=256)
    baseline=set()
    for row in metric_groups:
        _check(type(row) is dict and set(row)=={'node','pool','user'} and row['node'] in addresses and _token(row['pool']) and _token(row['user']))
        group=(row['node'],row['pool'],row['user']);_check(group not in baseline);baseline.add(group)
    _check(all((n,'test','poc_app') in baseline for n in addresses))
    _check(type(matrix) is dict and matrix.get('status')=='success' and not matrix.get('warnings') and not matrix.get('infos'))
    data=matrix['data'];_check(type(data) is dict and set(data)=={'resultType','result'} and data['resultType']=='matrix')
    rows=data['result'];_check(type(rows) is list and len(rows)<=1538)
    series={};groups=set(baseline);times={n:set() for n in addresses}
    for row in rows:
        _check(type(row) is dict and set(row)=={'metric','values'})
        labels=row['metric'];_check(type(labels) is dict)
        family=labels['__name__'];_check(family in FAMILIES+('up',))
        needed=set(SCOPE)|{'__name__','ecs_instance_id','instance'}|({'pool','user'} if family!='up' else set())
        _check(set(labels)==needed and all(labels[k]==v for k,v in SCOPE.items()))
        node=labels['ecs_instance_id'];_check(node in addresses and labels['instance']==addresses[node]+':9930')
        pool,user=(labels['pool'],labels['user']) if family!='up' else ('','')
        if family!='up':_check(_token(pool) and _token(user));groups.add((node,pool,user))
        key=(node,pool,user,family);_check(key not in series)
        pairs=row['values'];_check(type(pairs) is list and len(pairs)<=16)
        points={}
        for pair in pairs:
            _check(type(pair) is list and len(pair)==2 and _number(pair[0]) and type(pair[1]) is str and len(pair[1])<=64)
            stamp=pair[0];value=float(pair[1])
            _check(math.isfinite(value) and 0<=value<=2**53 and value.is_integer())
            _check(stamp*1000>watermark_ms and 0<=now-stamp<=15 and stamp not in points)
            _check(family!='up' or value==1)
            points[stamp]=int(value);times[node].add(stamp)
        series[key]=points
    _check(len(groups)<=256)
    result=[];insufficient=False
    for node in sorted(addresses):
        required=[(node,'','','up')]+[(n,p,u,f) for n,p,u in sorted(groups) if n==node for f in FAMILIES]
        if not times[node]:
            # Empty post-watermark history is pending, never evidence.
            insufficient=True;continue
        _check(all(key in series and set(series[key])==times[node] for key in required))
        stamps=sorted(times[node])
        if len(stamps)<3:insufficient=True;continue
        # Taking maxima across ALL fresh post-watermark observations protects
        # against hiding an earlier high value among more than three samples.
        totals=[dict(timestamp=stamp,total=sum(series[k][stamp] for k in required if k[-1]!='up')) for stamp in stamps]
        maximum=max(s['total'] for s in totals)
        selected=sorted(totals,key=lambda s:(-s['total'],s['timestamp']))[:3]
        result.append(dict(node=node,address=addresses[node],maximum=maximum,samples=sorted(selected,key=lambda s:s['timestamp'])))
    if insufficient:raise _Insufficient([dict(node=n,pool=p,user=u) for n,p,u in sorted(groups)])
    combined=sum(r['maximum'] for r in result)
    _check(max_combined is None or combined<=max_combined)
    return dict(watermarkMs=watermark_ms,nodes=result,metricGroups=[dict(node=n,pool=p,user=u) for n,p,u in sorted(groups)],combinedMaximum=combined,observedAt=now)


def validate_causal_samples(watermark_ms,matrix,*,nodes,metric_groups,now,max_combined):
    try:return _samples(watermark_ms,matrix,nodes=nodes,metric_groups=metric_groups,now=now,max_combined=max_combined)
    except Exception:pass
    raise ValueError('causal scrape')


class _Ticket:
    __slots__=('__weakref__',)


class _Receipt:
    __slots__=('__weakref__',)


def issued_evidence(receipt):
    """Only the original live-read object can be projected into a sealed witness."""
    try:
        _check(type(receipt) is _Receipt and receipt in _issued)
        return copy.deepcopy(_issued[receipt])
    except Exception:pass
    raise ValueError('causal scrape')


def _response(response):
    _check(type(response) is dict and response.get('status')=='success' and not response.get('warnings') and not response.get('infos'))
    return response['data']


def _after_watermark(matrix,watermark):
    # A range selector necessarily includes history. Remove it by each ORIGINAL
    # paired timestamp; never attach a query evaluation timestamp to a value.
    result=copy.deepcopy(matrix);_response(result)
    _check(result['data']['resultType']=='matrix' and type(result['data']['result']) is list and len(result['data']['result'])<=1538)
    for row in result['data']['result']:
        _check(type(row['values']) is list and len(row['values'])<=16)
        kept=[]
        for pair in row['values']:
            _check(type(pair) is list and len(pair)==2 and _number(pair[0]))
            if pair[0]*1000>watermark:kept.append(pair)
        row['values']=kept
    return result


def _collect(*,source_read,prometheus_get,nodes,metric_groups,max_combined,now,deadline,pending):
    from pgcat_native_controller import FixedWorkerReads
    from pgcat_source_bootstrap import require_verified
    require_verified(__name__);require_verified('pgcat_native_controller')
    owner=getattr(source_read,'__self__',None)
    _check(type(owner) is FixedWorkerReads and getattr(source_read,'__func__',None) is FixedWorkerReads.metrics_source and getattr(prometheus_get,'__self__',None) is owner and getattr(prometheus_get,'__func__',None) is FixedWorkerReads.prometheus_get)
    started=time.monotonic();_check(_number(deadline) and 0<deadline-started<=30 and _number(owner.deadline))
    # Tighten the adapter itself before any source I/O; post-read deadline
    # checks alone cannot bound a slow HTTP header or Kubernetes request.
    owner.deadline=min(owner.deadline,deadline)
    _check(started<owner.deadline)
    binding=_canonical(dict(nodes=nodes,metricGroups=metric_groups,maxCombined=max_combined))
    _nodes(nodes)
    if pending is None:
        state=dict(owner=owner,pid=os.getpid(),binding=binding,deadline=deadline,started=started,passes=0,groups=copy.deepcopy(metric_groups))
        state['source']=validate_source(source_read(),SOURCE_POLICY)
        _check(time.monotonic()<deadline)
        watermark=_response(prometheus_get('/api/v1/status/tsdb'))['headStats']['maxTime']
        _check(type(watermark) is int and 0<watermark<2**53);state['watermark']=watermark
        pending=_Ticket();_pending[pending]=state
    else:
        _check(type(pending) is _Ticket and pending in _pending);state=_pending[pending]
        _check(state['owner'] is owner and state['pid']==os.getpid() and state['binding']==binding and state['deadline']==deadline)
        _check(validate_source(source_read(),SOURCE_POLICY)==state['source'])
    _check(time.monotonic()<deadline and state['passes']<32);state['passes']+=1
    accepted={r['node']:r['address'] for r in state['source']['targets']}
    _check(all(accepted.get(n['id'])==n['address'] for n in nodes))
    response=prometheus_get(query_path(nodes));matrix=_after_watermark(response,state['watermark'])
    current=validate_source(source_read(),SOURCE_POLICY);_check(current==state['source'] and time.monotonic()<deadline)
    observed=now()
    try:
        samples=_samples(state['watermark'],matrix,nodes=nodes,metric_groups=state['groups'],now=observed,max_combined=max_combined)
    except _Insufficient as error:
        state['groups']=error.groups
        state['evidence']=_read_evidence(state,matrix,None,nodes,max_combined,observed)
        return dict(status='PENDING',pending=pending)
    _check(time.monotonic()<deadline)
    state['groups']=copy.deepcopy(samples['metricGroups'])
    receipt=_Receipt();_issued[receipt]=_read_evidence(state,matrix,samples,nodes,max_combined,observed)
    del _pending[pending]
    return dict(status='VERIFIED',receipt=receipt)


def collect_causal_scrapes(*,source_read,prometheus_get,nodes,metric_groups,max_combined,now,deadline,pending=None):
    """One bounded pass, after actual rollout observation. Never sleeps.

    The original total collection deadline is <=30s. Each pass also keeps the
    reader's shorter action I/O deadline. Cross-process resume requires the
    existing Journal-anchored original ingress, never a serialized ticket input.
    Neither a caller watermark nor a caller response can issue a live receipt.
    """
    try:return _collect(source_read=source_read,prometheus_get=prometheus_get,nodes=nodes,metric_groups=metric_groups,max_combined=max_combined,now=now,deadline=deadline,pending=pending)
    except Exception:
        if type(pending) is _Ticket:_pending.pop(pending,None)
    raise ValueError('causal scrape')


def _read_evidence(state,matrix,samples,nodes,maximum,observed):
    return dict(schemaVersion=2,source=copy.deepcopy(state['source']),watermarkMs=state['watermark'],
        nodes=copy.deepcopy(nodes),metricGroups=copy.deepcopy(state['groups']),maxCombined=maximum,
        samples=samples,matrix=matrix,observedAt=observed,passes=state['passes'],
        startedMonotonic=state['started'],completedMonotonic=time.monotonic(),deadlineMonotonic=state['deadline'])


def pending_evidence(ticket):
    """Export only an actual pending read, for the existing ingress seal."""
    try:
        _check(type(ticket) is _Ticket and ticket in _pending)
        state=_pending[ticket];_check(state['pid']==os.getpid() and 'evidence' in state)
        return copy.deepcopy(state['evidence'])
    except Exception:pass
    raise ValueError('causal scrape')


def _validate_read_evidence(value):
    """Historical shape/policy/sample validation; never issues read authority."""
    _check(type(value) is dict and set(value)=={'schemaVersion','source','watermarkMs','nodes','metricGroups','maxCombined','samples','matrix','observedAt','passes','startedMonotonic','completedMonotonic','deadlineMonotonic'})
    _check(type(value['schemaVersion']) is int and value['schemaVersion']==2)
    _check(type(value['passes']) is int and 1<=value['passes']<=32)
    a,b,d=(value[k] for k in ('startedMonotonic','completedMonotonic','deadlineMonotonic'))
    _check(all(_number(t) for t in (a,b,d)) and a<=b<d<=a+30)
    source=value['source'];_check(type(source) is dict)
    fields={'deploymentUid','deploymentGeneration','replicaSetUid','podName','podUid','containerId','containerStart','restartCount','image','imageId','origin','configMapUid','configMapResourceVersion','activeRules','activeRulesSha256','version','revision','processStart','lastConfigTime','configSha256','configMapConfigSha256','rulesSha256','flagsSha256','targets'}
    _check(set(source)==fields)
    _check(all(source[k]==SOURCE_POLICY[k] for k in SOURCE_POLICY))
    _check(source['activeRules']==_ACTIVE_RULES and source['activeRulesSha256']==_sha(_canonical(_ACTIVE_RULES)))
    _check(all(type(source[k]) is str and re.fullmatch('[0-9a-f]{64}',source[k]) for k in ('configSha256','configMapConfigSha256','rulesSha256','flagsSha256','activeRulesSha256')))
    _check(all(_token(source[k]) for k in ('deploymentUid','replicaSetUid','podName','podUid','containerStart','configMapUid','configMapResourceVersion','processStart','lastConfigTime')))
    _check(type(source['deploymentGeneration']) is int and source['deploymentGeneration']>=1 and type(source['restartCount']) is int and source['restartCount']>=0)
    _check(type(source['containerId']) is str and re.fullmatch('containerd://[0-9a-f]{64}',source['containerId']))
    _check(source['imageId'].endswith('@'+SOURCE_POLICY['image'].split('@')[1]))
    origin=source['origin'];_check(type(origin) is str and origin.startswith('http://') and origin.endswith(':9090'));private_ipv4(origin[7:-5])
    targets=source['targets'];_check(type(targets) is list and 2<=len(targets)<=4)
    nodes=set();addresses=set()
    for row in targets:
        _check(type(row) is dict and set(row)=={'node','address','labels','loopIdentitySha256'})
        node=row['node'];address=private_ipv4(row['address']);_check(_token(node) and node not in nodes and address not in addresses)
        nodes.add(node);addresses.add(address)
        labels=dict(SCOPE,ecs_instance_id=node,instance=address+':9930');_check(row['labels']==labels)
        _check(row['loopIdentitySha256']==_sha(_canonical(dict(pool='pgcat',labels=labels,url='http://'+address+':9930/metrics'))))
    _check(targets==sorted(targets,key=lambda r:r['node']))
    accepted={r['node']:r['address'] for r in targets}
    _check(all(accepted.get(n['id'])==n['address'] for n in value['nodes']))
    try:computed=_samples(value['watermarkMs'],value['matrix'],nodes=value['nodes'],metric_groups=value['metricGroups'],now=value['observedAt'],max_combined=value['maxCombined'])
    except _Insufficient as error:
        _check(value['samples'] is None and error.groups==value['metricGroups']);return copy.deepcopy(value)
    _check(value['samples']==computed)
    return copy.deepcopy(value)


def restore_ingress_pending(*,gate,run,binding):
    """Restore only the latest original Journal-linked ingress on this worker."""
    try:
        from pgcat_native_ingress import _causal_continuation
        from pgcat_source_bootstrap import require_verified
        require_verified(__name__);require_verified('pgcat_native_ingress')
        previous=_causal_continuation(gate,run,binding)
        if previous is None:return None
        evidence=validate_read_evidence(previous['evidence'])
        _check(time.monotonic()<evidence['deadlineMonotonic'])
        owner=gate.reads
        state=dict(owner=owner,pid=os.getpid(),binding=_canonical(dict(nodes=evidence['nodes'],metricGroups=binding['metricGroups'],maxCombined=evidence['maxCombined'])),
            deadline=evidence['deadlineMonotonic'],started=evidence['startedMonotonic'],passes=evidence['passes'],
            groups=evidence['metricGroups'],source=evidence['source'],watermark=evidence['watermarkMs'],evidence=evidence)
        ticket=_Ticket();_pending[ticket]=state;return ticket
    except Exception:pass
    raise ValueError('causal scrape')


def validate_read_evidence(value):
    try:return _validate_read_evidence(value)
    except Exception:pass
    raise ValueError('causal scrape')
