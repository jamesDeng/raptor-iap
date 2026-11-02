"""Fixed Gateway proof/support kernels; native production dispatch stays closed.

C4 owns full manifest/admission validation and the unavailable enable-evidence
hook. No executable, argv, credential value, or settled boolean is input here.
"""
import base64
import hashlib
import os
from pathlib import Path
import re
import stat
import subprocess
import time
from types import SimpleNamespace

from pgcat_native_ingress import (exact,require,integer,number,token,hashed,identifier,
    instant,sanitized,_identity,_runtime,_Calls,_credentials,_context,_execution)
from segmented_capture import canonical,decode_private,open_directory,check_directory

ASSET_KEYS={'schemaVersion','sourceBundleSha256','infraApiRevision','gatewayImage','axImage','clientImage',
    'gateBinarySha256','skillsTag','skillsCommit','scope','application'}
_GATE_IMAGE=r'ghcr\.io/jamesdeng/raptor-iap-gateway@(sha256:[0-9a-f]{64})'
# Genuine provenance is held only by this invocation after its actual set/verify.
# Files (including legacy *.set.json) cannot issue it. Each entry also pins
# its issuing PID: a fork inherits objects and bytes, not issuance authority.
import weakref
_guarded=weakref.WeakKeyDictionary()
_sealed=weakref.WeakKeyDictionary()
_closure_contexts=weakref.WeakKeyDictionary()



_RUNTIME_SCOPE={'accountId','regionId','envCode','proxyCode','scalingGroupId','nlbServerGroupId','targetDbCode','clusterId'}
_RUNTIME_APP={'namespace','name','uid','imageDigest'}
_manifest_anchors={}


def runtime_admission(plan):
    """Known native proof gap is refused before starting capture or enabling."""
    require(type(plan) is dict and plan.get('executionKind') in ('disabled-proof','native-replacement'))
    if plan['executionKind']=='native-replacement':raise ValueError('ClockAgreementUnavailable')


def validate_runtime_assets(value,plan=None,source_hashes=None):
    """Immutable release pins; mutable rollout and process facts stay per run."""
    from pgcat_source_bootstrap import NATIVE_SOURCES
    exact(value,ASSET_KEYS);require(type(value['schemaVersion']) is int and value['schemaVersion']==1)
    require(hashed(value['sourceBundleSha256']) and hashed(value['gateBinarySha256']))
    require(type(value['infraApiRevision']) is str and re.fullmatch(r'[a-f0-9]{40}',value['infraApiRevision']))
    require(type(value['gatewayImage']) is str and re.fullmatch(_GATE_IMAGE,value['gatewayImage']))
    for key in ('axImage','clientImage'):
        require(type(value[key]) is str and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._/:-]*@sha256:[a-f0-9]{64}',value[key]))
    require(type(value['skillsTag']) is str and re.fullmatch(r'skills-v[0-9]+\.[0-9]+\.[0-9]+',value['skillsTag']) and value['skillsTag']!='skills-v0.4.0')
    require(type(value['skillsCommit']) is str and re.fullmatch(r'[a-f0-9]{40}',value['skillsCommit']))
    exact(value['scope'],_RUNTIME_SCOPE);require(all(token(v) for v in value['scope'].values()))
    exact(value['application'],_RUNTIME_APP)
    require(all(token(value['application'][k]) for k in ('namespace','name','uid')) and re.fullmatch(r'sha256:[a-f0-9]{64}',value['application']['imageDigest']))
    require(value['clientImage'].split('@')[1]==value['application']['imageDigest'])
    if plan is not None:
        source_hashes=plan['sourceHashes'];target=plan['target'];scope=value['scope']
        require(all(scope[k]==target[k] for k in ('accountId','regionId','scalingGroupId','nlbServerGroupId')))
        require(value['application']=={k:plan['application'][k] for k in _RUNTIME_APP})
        if plan['executionKind']=='native-replacement':
            original=plan['controllerScope']
            require(scope==dict(accountId=original['accountId'],regionId=original['region'],envCode=original['envCode'],proxyCode=original['proxyCode'],
                scalingGroupId=original['groupId'],nlbServerGroupId=original['serverGroupId'],targetDbCode=original['targetDbCode'],clusterId=original['application']['clusterId']))
            require(value['skillsTag']==plan['plannedInput']['skills']['tag'] and value['skillsCommit']==plan['plannedInput']['skills']['commitSha'])
    if source_hashes is not None:
        exact(source_hashes,set(NATIVE_SOURCES));require(all(hashed(v) for v in source_hashes.values()))
        require(value['sourceBundleSha256']==hashlib.sha256(canonical(source_hashes)).hexdigest())
    return decode_private(canonical(value))


def read_runtime_assets(root,plan=None,*,original=None,source_hashes=None):
    root=Path(root);require(root.is_absolute() and '..' not in root.parts)
    raw,identity=_identity(root/'native-runtime-assets.json',32768)
    require(original is None or identity==original)
    value=decode_private(raw);require(raw==canonical(value))
    return validate_runtime_assets(value,plan,source_hashes),identity


def release_assets():
    """Fixed source staging root only; every process retains its own first inode."""
    from pgcat_source_bootstrap import require_verified,current_hashes
    require_verified(__name__)
    root=Path(__file__).absolute().parent;key=(os.getpid(),str(root))
    require(key in _manifest_anchors or len(_manifest_anchors)<128)
    value,identity=read_runtime_assets(root,original=_manifest_anchors.get(key),source_hashes=current_hashes())
    _manifest_anchors[key]=identity
    return value,identity


def _asset(path):
    fd=open_directory(path.parent)
    try:
        item=os.open(path.name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
        try:
            before=os.fstat(item)
            require(stat.S_ISREG(before.st_mode) and before.st_uid==os.geteuid() and
                stat.S_IMODE(before.st_mode) in (0o500,0o700) and before.st_nlink==1 and 0<before.st_size<=64*1024*1024)
            digest=hashlib.sha256();size=0
            while True:
                chunk=os.read(item,65536)
                if not chunk:break
                digest.update(chunk);size+=len(chunk);require(size<=64*1024*1024)
            def identity(s):return (s.st_dev,s.st_ino,s.st_uid,s.st_mode,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
            require(size==before.st_size and identity(before)==identity(os.fstat(item))==identity(os.stat(path.name,dir_fd=fd,follow_symlinks=False)))
            check_directory(fd,path.parent)
            return (identity(before),digest.hexdigest())
        finally:os.close(item)
    finally:os.close(fd)


def _source(plan,run):
    from pgcat_source_bootstrap import require_verified,current_hashes
    from pgcat_native_controller import verify_source_inventory
    require_verified(__name__)
    require(current_hashes()==plan['sourceHashes'])
    return verify_source_inventory(run,plan['sourceHashes'])


class FixedGatewayAdapters:
    """Narrow worker context; C4 supplies authoritative manifest/action bindings.

    reads must be the original FixedWorkerReads, including its original private
    credential identity. Full-plan calls require all four native_prepared anchors.
    Runtime assets are never inferred from source hashes or the current binary.
    """
    def __init__(self,run_dir,reads,*,runtime_assets,prepared=None,action_id=None,raptor_request_id=None):
        from pgcat_native_controller import FixedWorkerReads,verify_prepared
        require(type(reads) is FixedWorkerReads)
        self.run=Path(run_dir);require(self.run.is_absolute() and self.run.name=='run' and '..' not in self.run.parts)
        self.reads=reads;self.plan=decode_private(canonical(reads.plan));self.run_id=reads.run_id;identifier(self.run_id)
        self.credential=decode_private(canonical(reads.credential))
        require(reads.credential['path']==str(self.run.parent/'kubeconfig'))
        validate_runtime_assets(runtime_assets,self.plan)
        actual,self.runtime_asset_identity=read_runtime_assets(self.run,self.plan,original=None if prepared is None else prepared['runtimeAssetsFile'])
        require(actual==runtime_assets)
        require(type(runtime_assets['schemaVersion']) is int and runtime_assets['schemaVersion']==1 and hashed(runtime_assets['gateBinarySha256']))
        require(type(runtime_assets['gatewayImage']) is str and re.fullmatch(_GATE_IMAGE,runtime_assets['gatewayImage']))
        # C4 checks every manifest field and its immutable provenance. These are
        # the only asset fields the C1 command boundary consumes.
        self.digest=runtime_assets['gateBinarySha256'];self.image=runtime_assets['gatewayImage']
        self.prepared=None if prepared is None else decode_private(canonical(prepared))
        if 'runId' in self.plan:
            require(prepared is not None and prepared['workerCredential']==reads.credential)
            verify_prepared(self.run,self.plan,prepared)
        else:require(prepared is None)
        row=self.plan['serviceBindings']['gateway']
        require(row['deploymentName']=='agent-gateway' and row['containerName']=='agent-gateway' and self.plan['serviceBindings']['namespace']=='raptor-system')
        self.gate=self.run/'gateway-live-gate';self.asset=_asset(self.gate);require(self.asset[1]==self.digest)
        self.sources=_source(self.plan,self.run)
        self.action_id=action_id;self.raptor_request_id=raptor_request_id
        if action_id is not None:identifier(action_id)
        if raptor_request_id is not None:identifier(raptor_request_id)
        self.deadline=reads.deadline

    def _check(self):
        from pgcat_native_controller import verify_credential,verify_prepared
        require(_asset(self.gate)==self.asset and _source(self.plan,self.run)==self.sources)
        read_runtime_assets(self.run,self.plan,original=self.runtime_asset_identity)
        require(self.reads.plan==self.plan and self.reads.run_id==self.run_id and self.reads.credential==self.credential)
        verify_credential(self.credential)
        if 'runId' in self.plan:verify_prepared(self.run,self.plan,self.prepared)
        require(time.monotonic()<self.deadline)

    def _fixed_command(self,args):
        require(type(args) is list and all(type(v) is str for v in args))
        if args and args[0]==str(self.gate):
            tail=args[1:]
            if tail in (['inspect','--expected','false'],['inspect','--expected','true'],
                        ['set','--expected','false','--set','true'],['set','--expected','true','--set','false']):return
            require(len(tail)==7 and tail[0] in ('rollout','verify') and tail[1]=='--expected' and tail[2] in ('true','false') and
                tail[3]=='--resource-version' and token(tail[4]) and tail[5]=='--changed-after')
            instant(tail[6]);return
        prefix=['kubectl','--kubeconfig',self.reads.credential['path'],'-n','raptor-system']
        require(args[:5]==prefix)
        require(args[5:] in (
            ['get','secret','gateway-live-config','-o','json','--request-timeout=2s'],
            ['get','deployment','agent-gateway','-o','json','--request-timeout=2s'],
            ['get','pods','-o','json','--request-timeout=2s'],
            ['exec','deployment/agent-gateway','-c','agent-gateway','--','/usr/local/bin/gateway','--idle-check'],
            ['rollout','status','deployment/agent-gateway','--timeout=120s']))

    def _command(self,args,limit=8192):
        self._fixed_command(args)
        self._check();remaining=min(135.,self.deadline-time.monotonic());require(remaining>0)
        result=subprocess.run(args,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,
            timeout=remaining,close_fds=True,env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8','KUBECONFIG':self.reads.credential['path']})
        self._check();require(result.returncode==0 and type(result.stdout) is bytes and len(result.stdout)<=limit)
        return result.stdout

    def _kube(self,kind):
        require(kind in ('secret','deployment','pods'))
        args=['kubectl','--kubeconfig',self.reads.credential['path'],'-n','raptor-system','get',kind]
        if kind!='pods':args.append('gateway-live-config' if kind=='secret' else 'agent-gateway')
        return decode_private(self._command(args+['-o','json','--request-timeout=2s'],2*1024*1024))

    def _metadata(self):
        secret=self._kube('secret');dep=self._kube('deployment');pods=self._kube('pods')['items']
        sm=secret['metadata'];dm=dep['metadata'];require(all(token(m[k]) for m in (sm,dm) for k in ('uid','resourceVersion')))
        require(dm['uid']==self.plan['serviceBindings']['gateway']['deploymentUid'] and integer(dm['generation'],1))
        data=secret['data'];exact(data,{'config.json'});require(type(data['config.json']) is str)
        config=decode_private(base64.b64decode(data['config.json'],validate=True));require(type(config.get('replacementEnabled')) is bool)
        containers=dep['spec']['template']['spec']['containers'];selected=[c for c in containers if c['name']=='agent-gateway']
        require(len(selected)==1 and selected[0]['image']==self.image)
        ready=[]
        for pod in pods:
            if pod.get('metadata',{}).get('deletionTimestamp') is not None:continue
            spec=pod.get('spec',{});cs=[c for c in spec.get('containers',[]) if c.get('name')=='agent-gateway']
            if not cs:continue
            require(len(cs)==1 and cs[0]['image']==self.image)
            meta=pod['metadata'];status=pod['status'];require(token(meta['uid']) and token(meta['name']))
            require(meta.get('namespace')=='raptor-system' and status['phase']=='Running' and any(c.get('type')=='Ready' and c.get('status')=='True' for c in status['conditions']))
            runtime=[c for c in status['containerStatuses'] if c['name']=='agent-gateway']
            digest=self.image.split('@')[1]
            require(len(runtime)==1 and runtime[0]['ready'] is True and runtime[0]['imageID'] in (self.image,'docker-pullable://'+self.image,digest,'containerd://'+digest))
            ready.append(meta['uid'])
        require(len(ready)==1)
        return dict(secretUid=sm['uid'],secretResourceVersion=sm['resourceVersion'],deploymentUid=dm['uid'],
            deploymentResourceVersion=dm['resourceVersion'],podUid=ready[0],generation=dm['generation'],imageDigest=self.image.split('@')[1],replacementEnabled=config['replacementEnabled'])

    def _idle(self):
        require(self._command(['kubectl','--kubeconfig',self.reads.credential['path'],'-n','raptor-system','exec',
            'deployment/agent-gateway','-c','agent-gateway','--','/usr/local/bin/gateway','--idle-check'])==b'idle=true\n')

    def _enable_evidence(self):
        # C4 must replace this with its actual retained qualification + current
        # ready/source identity + quiesced owned-producer reads. No dict/boolean
        # or historical receipt by shape can grant this authority.
        raise ValueError('native enable qualification unavailable')


def _observe(adapters,want,now,*,set_receipt=None):
    require(type(adapters) is FixedGatewayAdapters and type(want) is bool)
    retained=set_receipt
    if retained is None:
        issued=_guarded.get(adapters)
        if issued is not None:
            require(issued[0]==os.getpid());retained=issued[1]
    started=now();require(number(started));adapters._check();before=adapters._metadata()
    require(before['replacementEnabled'] is want)
    if retained is not None and retained['resourceVersion']==before['secretResourceVersion'] and retained['replacementEnabled'] is want:
        _set_receipt(retained,want,started)
        args=['verify','--expected',str(want).lower(),'--resource-version',retained['resourceVersion'],'--changed-after',retained['changedAfter']]
        provenance=dict(kind='guarded-verify',resourceVersion=retained['resourceVersion'],changedAfter=retained['changedAfter'])
    else:
        args=['inspect','--expected',str(want).lower()];provenance=dict(kind='read-only-inspect')
    verification=decode_private(adapters._command([str(adapters.gate)]+args))
    exact(verification,{'replacementEnabled','generation','ready','podUID'})
    require(type(verification['replacementEnabled']) is bool and type(verification['ready']) is bool and type(verification['generation']) is int)
    require(verification==dict(replacementEnabled=want,generation=before['generation'],ready=True,podUID=before['podUid']))
    require(adapters._metadata()==before);adapters._check()
    observed=now();require(number(observed) and 0<=observed-started<30)
    result=dict(schemaVersion=1,runId=adapters.run_id,observedAt=observed,provenance=provenance,verification=verification,
        **{k:before[k] for k in ('secretUid','secretResourceVersion','deploymentUid','podUid','generation','imageDigest')})
    return result


@sanitized
def runtime_proof(run_dir: Path,*,adapters,now) -> dict:
    """Actual false proof, including pre-feed UUID/template context.

    Returns fresh evidence; never rewrites initial runtime/ready files. Later
    diagnostic callers must put this fresh observation in their new action
    receipt. Current enabled observations use the separate support runtime type.
    """
    require(type(adapters) is FixedGatewayAdapters and Path(run_dir)==adapters.run)
    result=_observe(adapters,False,now)
    _runtime(result,dict(adapters.plan,runId=adapters.run_id),result['observedAt'])
    if 'runId' in adapters.plan:
        raw,_=_identity(adapters.run/'native-gateway-runtime.json');initial=decode_private(raw)
        # Freshness belongs to initial ready admission; aged historical proof is
        # still immutable evidence. Current inspection must bind the same load.
        require({k:result[k] for k in ('runId','secretUid','secretResourceVersion','deploymentUid','podUid','generation','imageDigest','verification')}==
            {k:initial[k] for k in ('runId','secretUid','secretResourceVersion','deploymentUid','podUid','generation','imageDigest','verification')})
    return result


def _set_receipt(value,want,at):
    exact(value,{'resourceVersion','changedAfter','replacementEnabled'})
    require(token(value['resourceVersion']) and type(value['replacementEnabled']) is bool and value['replacementEnabled'] is want)
    require(instant(value['changedAfter'])<=at)


def _transition(adapters,want,now):
    """The caller has durably fenced this action before the one set attempt."""
    before=adapters._metadata();require(before['replacementEnabled'] is not want)
    receipt=decode_private(adapters._command([str(adapters.gate),'set','--expected',str(not want).lower(),'--set',str(want).lower()]))
    _set_receipt(receipt,want,now());require(receipt['resourceVersion']!=before['secretResourceVersion'])
    # Retain actual response before rollout/verify: uncertainty never replays set.
    from pgcat_native_controller import _write_bytes
    _write_bytes(adapters.run/('native-gateway-'+('enable' if want else 'disable')+'.set.json'),canonical(receipt))
    common=['--expected',str(want).lower(),'--resource-version',receipt['resourceVersion'],'--changed-after',receipt['changedAfter']]
    require(adapters._command([str(adapters.gate),'rollout']+common)==b'rollout=requested\n')
    adapters._command(['kubectl','--kubeconfig',adapters.reads.credential['path'],'-n','raptor-system','rollout','status','deployment/agent-gateway','--timeout=120s'])
    runtime=_observe(adapters,want,now,set_receipt=receipt)
    require(all(runtime[k]==before[k] for k in ('secretUid','deploymentUid','imageDigest')))
    require(adapters in _guarded or len(_guarded)<128);_guarded[adapters]=(os.getpid(),dict(receipt))
    return receipt,runtime


def _settlement(adapters):
    from pgcat_native_controller import FixedHTTP
    require(adapters.plan['executionKind']=='native-replacement' and adapters.raptor_request_id is not None)
    request=adapters.raptor_request_id;identifier(request);chain=SimpleNamespace(plan=adapters.plan);calls=_Calls()
    http=FixedHTTP(adapters.plan);http.deadline=adapters.deadline
    auth={};bindings={}
    for label in ('gateway','openapi'):auth[label],bindings[label]=_credentials(chain,label,calls,adapters.reads.read)
    def get(label,suffix):
        body=decode_private(http(bindings[label]['origin'],'/v1/requests/'+request+suffix,auth[label]));exact(body,{'data'});return body['data']
    context=_context(get('openapi','/context'),adapters.plan,request)
    def state():
        execution=_execution(get('gateway',''),adapters.plan,context)
        control=get('openapi','/control');exact(control,{'requestId','status','controlState'})
        # Known terminal states with explicit confirmed cleanup. Blocked and
        # interrupted are resumable; they do not qualify as settled here.
        require(execution['runtimeMode']=='live' and execution['stage']=='finished' and execution['status'] in ('completed','failed','cancelled') and
            execution['cleanup']=='confirmed' and execution['recoveryNeeded'] is False)
        require(control==dict(requestId=request,status=execution['status'],controlState=''))
        return dict(raptorRequestId=request,attemptId=execution['attemptId'],status=execution['status'],cleanup='confirmed',recoveryNeeded=False,controlState='')
    result=state();require(state()==result)
    for label in ('gateway','openapi'):
        current,binding=_credentials(chain,label,calls,adapters.reads.read);require(current==auth[label] and binding==bindings[label])
    adapters._check();return result


def _action(adapters,run_dir,action,inputs):
    from pgcat_native_controller import _write_bytes
    require(type(adapters) in (FixedGatewayAdapters,FixedClosureAdapters) and Path(run_dir)==adapters.run and 'runId' in adapters.plan and adapters.action_id is not None)
    adapters._check();identifier(adapters.action_id)
    result=dict(schemaVersion=1,action=action,runId=adapters.run_id,executionKind=adapters.plan['executionKind'],
        planSha256=hashlib.sha256(canonical(adapters.plan)).hexdigest(),requestId=adapters.action_id,inputs=inputs)
    # O_EXCL survives an unknown mutation response or worker death. Same action
    # and different local IDs are both denied once claimed in this run.
    _write_bytes(adapters.run/(action+'.intent'),canonical(result))
    return result


def _seal_action(adapters,record,outputs):
    from pgcat_native_controller import _write_bytes
    adapters._check();record=dict(record,outputs=outputs)
    validate_support(record,adapters.plan,record['action'])
    basename=record['action']+'.json';identity=_write_bytes(adapters.run/basename,canonical(record))
    require(adapters in _sealed or len(_sealed)<128)
    _sealed[adapters]=(os.getpid(),record['action'],dict(basename=basename,identity=identity),canonical(record))
    return record


def _current(runtime):
    # Deliberately distinct from A's historical false-ready record schema.
    return dict(runtime,kind='gateway-current-runtime')


@sanitized
def gateway_disable(run_dir,*,adapters,now):
    require(type(adapters) is FixedGatewayAdapters)
    settled=_settlement(adapters);adapters._idle()
    before=adapters._metadata()
    record=_action(adapters,run_dir,'native-gateway-disable',dict(raptorRequestId=adapters.raptor_request_id))
    if before['replacementEnabled']:
        receipt,runtime=_transition(adapters,False,now)
    else:
        receipt=None;runtime=_observe(adapters,False,now)
    require(_settlement(adapters)==settled);adapters._idle()
    return _seal_action(adapters,record,dict(status='VERIFIED',observedAt=runtime['observedAt'],settlement=settled,setReceipt=receipt,runtime=_current(runtime)))


@sanitized
def gateway_enable(run_dir,*,adapters,now):
    require(type(adapters) is FixedGatewayAdapters and adapters.plan['executionKind']=='native-replacement')
    # There is no public evidence argument or override. C4 must implement this
    # fixed reader using actual bound ready, qualification and producer owners.
    evidence=adapters._enable_evidence()
    exact(evidence,{'readySha256','qualificationSha256'})
    require(all(hashed(v) for v in evidence.values()))
    record=_action(adapters,run_dir,'native-gateway-enable',evidence)
    receipt,runtime=_transition(adapters,True,now)
    return _seal_action(adapters,record,dict(status='VERIFIED',observedAt=runtime['observedAt'],setReceipt=receipt,runtime=_current(runtime)))


SUPPORT_ACTIONS={'native-gateway-enable','native-gateway-disable'}
RUNTIME_KEYS={'schemaVersion','kind','runId','observedAt','secretUid','secretResourceVersion','deploymentUid',
    'podUid','generation','imageDigest','provenance','verification'}


def validate_support(record,plan,action):
    """Pure exact schema validation; never issues admission or runtime authority."""
    require(action in SUPPORT_ACTIONS)
    exact(record,{'schemaVersion','action','runId','executionKind','planSha256','requestId','inputs','outputs'})
    require(type(record['schemaVersion']) is int and record['schemaVersion']==1 and record['action']==action and
        record['runId']==plan['runId'] and record['executionKind']==plan['executionKind'] and
        record['planSha256']==hashlib.sha256(canonical(plan)).hexdigest())
    identifier(record['requestId']);inputs=record['inputs'];out=record['outputs']
    if action in CLOSURE_ACTIONS:return _closure_support(record,plan,action)
    require(plan['executionKind']=='native-replacement')
    if action=='native-gateway-enable':
        exact(inputs,{'readySha256','qualificationSha256'});require(all(hashed(v) for v in inputs.values()))
    else:
        exact(inputs,{'raptorRequestId'});identifier(inputs['raptorRequestId'])
    exact(out,{'status','observedAt','setReceipt','runtime'}|({'settlement'} if action=='native-gateway-disable' else set()))
    require(out['status']=='VERIFIED' and number(out['observedAt']))
    runtime=out['runtime'];exact(runtime,RUNTIME_KEYS)
    require(type(runtime['schemaVersion']) is int and runtime['schemaVersion']==1 and runtime['kind']=='gateway-current-runtime' and
        runtime['runId']==plan['runId'] and number(runtime['observedAt']) and runtime['observedAt']==out['observedAt'] and runtime['deploymentUid']==plan['serviceBindings']['gateway']['deploymentUid'])
    require(all(token(runtime[k]) for k in ('secretUid','secretResourceVersion','deploymentUid','podUid')) and integer(runtime['generation'],1))
    require(type(runtime['imageDigest']) is str and re.fullmatch(r'sha256:[0-9a-f]{64}',runtime['imageDigest']))
    want=action=='native-gateway-enable';v=runtime['verification'];exact(v,{'replacementEnabled','generation','ready','podUID'})
    require(type(v['replacementEnabled']) is bool and type(v['ready']) is bool and type(v['generation']) is int and
        v==dict(replacementEnabled=want,generation=runtime['generation'],ready=True,podUID=runtime['podUid']))
    proof=runtime['provenance']
    if out['setReceipt'] is not None:
        _set_receipt(out['setReceipt'],want,out['observedAt'])
        require(out['setReceipt']['resourceVersion']==runtime['secretResourceVersion'] and
            proof==dict(kind='guarded-verify',resourceVersion=out['setReceipt']['resourceVersion'],changedAfter=out['setReceipt']['changedAfter']))
    else:
        require(not want and proof==dict(kind='read-only-inspect'))
    if not want:
        settled=out['settlement'];exact(settled,{'raptorRequestId','attemptId','status','cleanup','recoveryNeeded','controlState'})
        require(settled['raptorRequestId']==inputs['raptorRequestId'] and token(settled['attemptId']) and
            settled['status'] in ('completed','failed','cancelled') and settled['cleanup']=='confirmed' and
            settled['recoveryNeeded'] is False and settled['controlState']=='')
    return decode_private(canonical(record))


@sanitized
def support_artifact(run_dir,action,*,adapters):
    """Actual just-sealed file handle for C2 transport/Journal anchoring.

    Available only in the same worker invocation that performed the action.
    A caller record, filename, or a prior process's files cannot issue it.
    """
    require(type(adapters) in (FixedGatewayAdapters,FixedClosureAdapters) and Path(run_dir)==adapters.run)
    retained=_sealed.get(adapters);require(retained is not None and retained[0]==os.getpid() and retained[1]==action)
    artifact=retained[2];raw,identity=_identity(adapters.run/artifact['basename'])
    require(raw==retained[3] and identity==artifact['identity']);adapters._check()
    validate_support(decode_private(raw),adapters.plan,action)
    return decode_private(canonical(artifact))


@sanitized
def read_support(run_dir,action,*,plan,original):
    """Read historical support by its ORIGINAL authoritative Journal identity.

    This does not grant admission, guarded provenance, or fresh settled state.
    C2/C4 must pass the actual original artifact returned by support_artifact.
    """
    require(action in SUPPORT_ACTIONS);exact(original,{'basename','identity'})
    require(original['basename']==action+'.json')
    from pgcat_native_controller import file_identity
    file_identity(original['identity'])
    raw,identity=_identity(Path(run_dir)/(action+'.json'))
    require(identity==original['identity'])
    return validate_support(decode_private(raw),plan,action)


# Final closure kernels are usable offline; C4 still owns production admission.
def _coverage_prefix(chain,progress):
    from pgcat_native_evidence import _replay
    from pgcat_native_ingress import _replacement_pair,_fleet
    from capture_window import TransitionEvidence,seconds,clean
    from segmented_capture import sha
    originals=set(chain.plan['target']['originalInstanceIds']);pair=None;refs=[]
    groups={(r['node'],r['pool'],r['user']) for r in chain.baseline['metricGroups']}
    transition=TransitionEvidence();last=None
    def visit(frame,ref):
        nonlocal pair,last
        inv=frame['inventory'];met=frame['metrics'];at=seconds(frame['at'])
        require(clean(frame)==frame)
        if chain.plan['executionKind']=='disabled-proof':
            _fleet({k:inv[k] for k in chain.baseline['fleet']},sorted(originals))
        else:
            if pair is None:pair=_replacement_pair(inv,originals)
            if pair is not None:require(set(inv['members'])<=originals|set(pair))
        observed={(r['node'],r['pool'],r['user']) for r in met['raw'] if r['node'] in originals}
        groups.update(observed)
        # Every historical pool/user remains an obligation while measurable.
        # Missing series cannot provide a zero or the shared <=1 budget.
        measured=originals & set(met['nodes'])
        require(all(group in observed for group in groups if group[0] in measured))
        issues=[];transition.assess(inv,met,at,issues.append);require(not issues)
        refs.append(dict(ref));last=inv
    fd=open_directory(chain.run/'native-capture')
    try:
        snapshot,count,_=_replay(fd,chain.context,progress['latest'],visit=visit)
        require(snapshot==progress['nativeCohort'] and count==progress['committedFrames'] and not any(progress['flags'].values()))
    finally:os.close(fd)
    if chain.plan['executionKind']=='disabled-proof':pair=sorted(originals)
    else:
        require(pair is not None and originals.issubset(transition.retired) and not originals.intersection(transition.obligations))
        _fleet({k:last[k] for k in chain.baseline['fleet']},pair)
    return pair,refs


def _current_terminal(capture,context,operation,terminal,live_read,*,now):
    """Validate current committed frames independently of the historical seal."""
    from pgcat_native_evidence import _replay,_verify_refs,_terminal_roster
    from segmented_capture import read_progress
    from capture_window import seconds
    from worker_capture import native_owner_evidence
    from finish_clients import validate_roster
    progress,_,_=read_progress(capture,context)
    require(progress['previous'] is not None and not any(progress['flags'].values()))
    fd=open_directory(capture)
    try:
        snapshot,count,_=_replay(fd,context,progress['latest'])
        require(snapshot==progress['nativeCohort'] and count==progress['committedFrames'])
        previous,latest,_=_verify_refs(fd,context,snapshot,progress['previous'],progress['latest'])
        for frame in (previous,latest):
            require(0<=seconds(now())-seconds(frame['at'])<=3)
            require(_terminal_roster(frame,context,operation,snapshot)==terminal['roster'])
        live=live_read();exact(live,{'deployment','replicaSets','pods','roster'})
        owners=native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context)
        require(validate_roster(live['roster'])==terminal['roster'] and set(owners)=={r['uid'] for r in terminal['roster']})
        dep=live['deployment'];require(type(dep['metadata']['generation']) is int and type(dep['status']['observedGeneration']) is int and dep['metadata']['generation']==dep['status']['observedGeneration']==operation['restartGeneration'])
        require(type(dep['spec']['replicas']) is int and type(dep['status']['readyReplicas']) is int and dep['spec']['replicas']==dep['status']['readyReplicas']==2)
        for pod in live['pods']:
            row=next(r for r in terminal['roster'] if r['uid']==pod['metadata']['uid'])
            require(pod['metadata']['name']==row['name'] and pod['status']['containerStatuses'][0]['containerID']==row['containerId'])
            require(any(c.get('type')=='Ready' and c.get('status')=='True' for c in pod['status'].get('conditions',[])))
            retained=next(p for p in latest['pods'] if p['uid']==row['uid']);require(owners[row['uid']]==retained['ownership'])
        fresh,_,_=read_progress(capture,context);require(fresh==progress)
        require(all(0<=seconds(now())-seconds(f['at'])<=3 for f in (previous,latest)))
        return progress
    finally:os.close(fd)


CLOSURE_ACTIONS={'native-terminal-seal','native-reconcile','native-feed-stop','native-finish'}
SUPPORT_ACTIONS |= CLOSURE_ACTIONS


class FixedClosureAdapters:
    """Fixed worker reads with original prepared, operation and Journal anchors.

    C4 obtains operation/support artifacts from claimed Journal admission. No
    caller pair, target list, command, credential value or finish executor exists.
    """
    def __init__(self,run_dir,reads,*,prepared,action_id,operation,supports,stopped=None,stop_artifact=None,hold_artifact=None,ingresses=None,gateway=None):
        from pgcat_native_controller import FixedWorkerReads,verify_prepared
        from pgcat_native_ingress import _Chain
        require(type(reads) is FixedWorkerReads)
        self.run=Path(run_dir);require(self.run.is_absolute() and self.run.name=='run' and '..' not in self.run.parts)
        self.reads=reads;self.plan=decode_private(canonical(reads.plan));self.run_id=self.plan['runId']
        self.prepared=decode_private(canonical(prepared));verify_prepared(self.run,self.plan,self.prepared)
        require(self.prepared['workerCredential']==reads.credential)
        self.credential=decode_private(canonical(reads.credential));self.deadline=reads.deadline
        self.action_id=action_id;identifier(action_id)
        self.stopped=None;self.stop_mirror=None;self.gateway=gateway;self.hold_artifact=hold_artifact;self.ingresses=[] if ingresses is None else decode_private(canonical(ingresses))
        self.operation=decode_private(canonical(operation));self.supports=decode_private(canonical(supports))
        exact(self.operation,{'basename','identity'});require(self.operation['basename']=='native-operation-binding.json')
        self.sources=_source(self.plan,self.run);self.chain=_Chain(self.run)
        require(self.chain.plan==self.plan)
        self.mirror=_identity(self.run/'native-capture'/'native-operation-binding.json')
        require(len(_closure_contexts)<128);_closure_contexts[self]=os.getpid()
        self._check()
        if stopped is not None:
            from pgcat_native_controller import _write_bytes
            self.stopped=decode_private(canonical(stopped))
            historical='native-finish' in self.supports
            _stopped_provider(self,time.time,require_fresh=not historical)
            if historical:
                exact(stop_artifact,{'basename','identity'});require(stop_artifact['basename']=='native-feed-stop.json')
                raw,self.stop_mirror=_identity(self.run/'native-feed-stop.json');require(self.stop_mirror==stop_artifact['identity'] and raw==canonical(self.stopped['support']))
            else:self.stop_mirror=_write_bytes(self.run/'native-feed-stop.json',canonical(self.stopped['support']))
            self._check()

    def _check(self):
        require(_closure_contexts.get(self)==os.getpid())
        from pgcat_native_controller import verify_prepared,verify_credential
        require(time.monotonic()<self.deadline and _source(self.plan,self.run)==self.sources)
        require(self.reads.plan==self.plan and self.reads.credential==self.credential)
        verify_credential(self.credential);verify_prepared(self.run,self.plan,self.prepared);self.chain.unchanged()
        raw,identity=_identity(self.run/self.operation['basename']);require(identity==self.operation['identity'])
        require(_identity(self.run/'native-capture'/'native-operation-binding.json')==self.mirror and self.mirror[0]==raw)
        from pgcat_native_evidence import validate_operation_binding
        validate_operation_binding(decode_private(raw),self.chain.context)
        require(type(self.supports) is dict and set(self.supports)<=CLOSURE_ACTIONS|{'native-gateway-disable'})
        if self.stopped is not None:require(_identity(self.run/'native-feed-stop.json')==(canonical(self.stopped['support']),self.stop_mirror))
        for action,original in self.supports.items():read_support(self.run,action,plan=self.plan,original=original)

    def _operation(self):
        self._check();raw,_=_identity(self.run/'native-operation-binding.json');return decode_private(raw),hashlib.sha256(raw).hexdigest()

    def _provider(self,now):
        from pgcat_rehearsal_contract import fresh_provider_source
        source=fresh_provider_source(self.run.parent/'contract',self.run_id,self.run/'provider-inventory.json',require_fresh=False,mode='full-replacement-direct-v2')
        require(source['providerRunContextSha256']==self.chain.context['providerRunContextSha256'] and source['feedSession']==self.chain.context['feedSession'])
        require(0<=now()-source['observedAt']<30)
        raw,_=_identity(self.run/'provider-inventory.json');require(hashlib.sha256(raw).hexdigest()==source['inventoryRawSha256'])
        return source,decode_private(raw)

    def _support(self,action):
        require(action in self.supports)
        return read_support(self.run,action,plan=self.plan,original=self.supports[action])

    def _patch_targets(self,before,new):
        from pgcat_native_controller import verify_credential
        # RFC6902 tests make UID, current RV and exact prior bytes atomic.
        patch=[dict(op='test',path='/metadata/uid',value=before['metadata']['uid']),
               dict(op='test',path='/metadata/resourceVersion',value=before['metadata']['resourceVersion']),
               dict(op='test',path='/data/targets.json',value=before['data']['targets.json']),
               dict(op='replace',path='/data/targets.json',value=new.decode('ascii'))]
        self._check();remaining=min(3.,self.deadline-time.monotonic());require(remaining>0)
        result=subprocess.run(['kubectl','--kubeconfig',self.credential['path'],'-n','raptor-test','patch','configmap',
            'pgcat-provider-inventory','--type=json','--patch-file=/dev/stdin','-o','json','--request-timeout=2s'],
            input=canonical(patch),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=remaining,close_fds=True,
            env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'})
        self._check();require(result.returncode==0 and len(result.stdout)<=65536);return decode_private(result.stdout)


def _closure_support(record,plan,action):
    inputs=record['inputs'];out=record['outputs']
    fields={'native-terminal-seal':({'operationBindingSha256'},{'terminalSha256'}),
        'native-reconcile':({'operationBindingSha256','terminalSha256','coverageSha256'},
            {'pair','configMapUid','previousResourceVersion','resourceVersion','targetsSha256','feedSession','sequence','observedAt'}),
        'native-feed-stop':({'reconcileSha256'},{'intentSha256','ackSha256','finalReceiptSha256','feedExit'}),
        'native-finish':({'terminalSha256','operationBindingSha256','stopSha256'},{'boundaryRequestId','receiptSha256','boundaryCompletedSha256'}),
        'native-cleanup':({'assessmentSha256','restoreSha256'},{'workerAbsent','localAbsent','workerCleanupSha256','localCleanupSha256'}),
        'native-verdict':({'stopSha256','finishSha256'},{'assessmentSha256','status'}),
        'native-restore':({'assessmentSha256','finishSha256','gatewayProofSha256'},{'roster','framesSha256','restoredAt'})}
    ik,ok=fields[action];exact(inputs,ik);exact(out,ok);require(all(hashed(v) for v in inputs.values()))
    for key,value in out.items():
        if key.endswith('Sha256'):require(hashed(value))
    if action=='native-reconcile':
        pair=out['pair'];require(type(pair) is list and len(pair)==2 and pair==sorted(set(pair)) and all(token(n) for n in pair))
        require(token(out['configMapUid']) and all(type(out[k]) is str and re.fullmatch(r'[1-9][0-9]{0,31}',out[k]) for k in ('previousResourceVersion','resourceVersion')))
        identifier(out['feedSession']);require(integer(out['sequence'],1) and number(out['observedAt']))
        require(int(out['resourceVersion'])>=int(out['previousResourceVersion']))
        originals=plan['target']['originalInstanceIds'];require(pair==originals if plan['executionKind']=='disabled-proof' else not set(pair)&set(originals))
    elif action=='native-feed-stop':
        exit=out['feedExit'];exact(exit,{'runId','feedId','pid','exitCode'});identifier(exit['feedId'])
        require(exit['runId']==plan['runId'] and integer(exit['pid'],1) and type(exit['exitCode']) is int and exit['exitCode']==0)
    elif action=='native-cleanup':require(type(out['workerAbsent']) is bool and out['workerAbsent'] is True and type(out['localAbsent']) is bool and out['localAbsent'] is True)
    elif action=='native-verdict':require(out['status'] in ('PASS','FAIL','INCONCLUSIVE'))
    elif action=='native-restore':
        from finish_clients import validate_roster
        validate_roster(out['roster']);require(number(out['restoredAt']))
    elif action=='native-finish':require(type(out['boundaryRequestId']) is str and re.fullmatch('[0-9a-f]{32}',out['boundaryRequestId']))
    return decode_private(canonical(record))


def _targets(addresses):
    require(type(addresses) is dict and 2<=len(addresses)<=4)
    from pgcat_native_ingress import private_ip
    for node,address in addresses.items():require(token(node));private_ip(address)
    return canonical([dict(targets=[addresses[node]+':9930'],labels=dict(env='rdev.ali',db_proxy_code='DP00001',target_db_code='D00002',ecs_instance_id=node)) for node in sorted(addresses)])


def _cm(cm,baseline,expected,*,exact_bytes=False):
    meta=cm['metadata'];require(not meta.get('deletionTimestamp'))
    require(all(meta.get(k)==baseline['configMap'][k] for k in ('uid','name','namespace')))
    require(type(meta['resourceVersion']) is str and re.fullmatch(r'[1-9][0-9]{0,31}',meta['resourceVersion']))
    require(int(meta['resourceVersion'])>=int(baseline['configMap']['resourceVersion']))
    exact(cm['data'],{'targets.json'});raw=cm['data']['targets.json'].encode('ascii')
    rows=decode_private(raw);wanted=decode_private(expected)
    require(type(rows) is list and len(rows)==len(wanted))
    require(raw==expected if exact_bytes else sorted(rows,key=lambda row:row['labels']['ecs_instance_id'])==wanted)
    return meta['resourceVersion']


@sanitized
def seal_terminal(run_dir,*,adapters,now):
    require(type(adapters) is FixedClosureAdapters)
    from pgcat_native_evidence import seal_terminal_clients
    operation,op_sha=adapters._operation()
    record=_action(adapters,run_dir,'native-terminal-seal',dict(operationBindingSha256=op_sha))
    terminal=seal_terminal_clients(adapters.run/'native-capture',adapters.chain.context,operation,adapters.reads.live_clients,now=now)
    return _seal_action(adapters,record,dict(terminalSha256=terminal['terminalSha256']))


@sanitized
def reconcile(run_dir,*,adapters,now):
    require(type(adapters) is FixedClosureAdapters)
    from pgcat_native_evidence import read_terminal_clients
    from pgcat_native_ingress import _result,_fleet
    from pgcat_native_controller import _write_bytes
    from segmented_capture import read_progress,sha
    operation,op_sha=adapters._operation();chain=adapters.chain
    terminal=read_terminal_clients(adapters.run/'native-capture',chain.context,operation)
    require(adapters._support('native-terminal-seal')['outputs']['terminalSha256']==terminal['terminalSha256'])
    progress,_,_=read_progress(adapters.run/'native-capture',chain.context)
    pair,refs=_coverage_prefix(chain,progress)
    if chain.plan['executionKind']=='native-replacement':
        # Binding points at the authenticated immutable result, not caller IDs.
        matching=[]
        for path in adapters.run.glob('native-ingress-[0-9][0-9][0-9][0-9].json'):
            raw,_=_identity(path,32*1024*1024)
            if sha(raw)==operation['ingressSha256']:matching.append(decode_private(raw))
        require(len(matching)==1);execution=matching[0]['execution']
        result=_result(execution['result'],chain.plan,operation['requestId'],execution['attemptId'])
        require(sha(canonical(result))==operation['resultSha256'] and sorted(result['replacement']['newInstanceIds'])==pair)
    coverage=dict(schemaVersion=1,runId=adapters.run_id,executionKind=chain.plan['executionKind'],planSha256=chain.context['planSha256'],
        operationBindingSha256=op_sha,oldIds=[] if chain.plan['executionKind']=='disabled-proof' else chain.plan['target']['originalInstanceIds'],pair=pair,frames=refs)
    # Fenced before target mutation; a crash after coverage never retries patch.
    coverage_raw=canonical(coverage);_write_bytes(adapters.run/'native-coverage.json',coverage_raw)
    record=_action(adapters,run_dir,'native-reconcile',dict(operationBindingSha256=op_sha,terminalSha256=terminal['terminalSha256'],coverageSha256=sha(coverage_raw)))
    source,inventory=adapters._provider(now);_fleet({k:inventory[k] for k in chain.baseline['fleet']},pair)
    addresses=source['privateAddresses'];new=_targets(addresses)
    expected=dict((r['id'],r['address']) for r in chain.baseline['originals']);expected.update(addresses)
    old=_targets(expected)
    current=adapters.reads.read('ConfigMap','raptor-test','pgcat-provider-inventory');rv=_cm(current,chain.baseline,old)
    if chain.plan['executionKind']=='native-replacement':
        response=adapters._patch_targets(current,new);new_rv=_cm(response,chain.baseline,new,exact_bytes=True);require(int(new_rv)>int(rv))
    else:new_rv=rv;new=current['data']['targets.json'].encode('ascii')
    before_source,before_inventory=adapters._provider(now)
    readback=adapters.reads.read('ConfigMap','raptor-test','pgcat-provider-inventory');observed=now()
    require(_cm(readback,chain.baseline,new,exact_bytes=True)==new_rv)
    # Sequence is the successful worker publication actually current at readback.
    source,inventory=adapters._provider(now);require(source==before_source and inventory==before_inventory)
    _fleet({k:inventory[k] for k in chain.baseline['fleet']},pair)
    require(decode_private(_targets(source['privateAddresses']))==decode_private(new))
    require(number(observed) and source['observedAt']<=observed)
    return _seal_action(adapters,record,dict(pair=pair,configMapUid=current['metadata']['uid'],previousResourceVersion=rv,resourceVersion=new_rv,
        targetsSha256=sha(new),feedSession=source['feedSession'],sequence=source['sequence'],observedAt=observed))


def _stopped_provider(adapters,now,*,require_fresh=True):
    from pgcat_lane_protocol import native_stop_ack,roster_sha,digest
    from pgcat_native_ingress import _fleet
    stopped=adapters.stopped;exact(stopped,{'support','intent','ack','receipt','feedBinding','feedExit'})
    record=validate_support(stopped['support'],adapters.plan,'native-feed-stop')
    reconcile=adapters._support('native-reconcile')
    require(stopped['ack']==native_stop_ack(stopped['intent'],stopped['receipt'],reconcile,adapters.plan,now=now() if require_fresh else stopped['receipt']['observedAt']))
    require(record['inputs']==dict(reconcileSha256=digest(reconcile)))
    require(record['outputs']==dict(intentSha256=digest(stopped['intent']),ackSha256=digest(stopped['ack']),finalReceiptSha256=digest(stopped['receipt']),feedExit=stopped['feedExit']))
    require(stopped['feedBinding']==adapters.chain.values['feedBinding'] and all(stopped['feedExit'][k]==stopped['feedBinding'][k] for k in ('runId','feedId','pid')))
    source,inventory=adapters._provider(now if require_fresh else lambda:stopped['receipt']['observedAt']);receipt=stopped['receipt']
    require(all(source[k]==receipt[k] for k in ('feedSession','sequence','observedAt')) and roster_sha(inventory)==receipt['rosterSha256'])
    _fleet({k:inventory[k] for k in adapters.chain.baseline['fleet']},reconcile['outputs']['pair'])
    cm=adapters.reads.read('ConfigMap','raptor-test','pgcat-provider-inventory')
    require(_cm(cm,adapters.chain.baseline,_targets(source['privateAddresses']))==reconcile['outputs']['resourceVersion'])
    require(sha_bytes(cm['data']['targets.json'].encode('ascii'))==reconcile['outputs']['targetsSha256'])
    return record,inventory


def sha_bytes(raw):return hashlib.sha256(raw).hexdigest()


@sanitized
def finish_terminal(run_dir,*,adapters,now):
    require(type(adapters) is FixedClosureAdapters and adapters.stopped is not None)
    from pgcat_native_evidence import read_terminal_clients
    from pgcat_collector_control import native_boundary_completion
    from segmented_capture import read_progress
    import finish_clients
    operation,op_sha=adapters._operation();chain=adapters.chain
    terminal=read_terminal_clients(adapters.run/'native-capture',chain.context,operation)
    require(adapters._support('native-terminal-seal')['outputs']['terminalSha256']==terminal['terminalSha256'])
    stop,inventory=_stopped_provider(adapters,now)
    progress=_current_terminal(adapters.run/'native-capture',chain.context,operation,terminal,adapters.reads.live_clients,now=now)
    require(read_progress(adapters.run/'native-capture',chain.context)[2]['inventory']==inventory)
    record=_action(adapters,run_dir,'native-finish',dict(terminalSha256=terminal['terminalSha256'],operationBindingSha256=op_sha,stopSha256=sha_bytes(canonical(stop))))
    adapters._check();started=time.monotonic();deadline=min(adapters.deadline,started+4.)
    # Exactly one bound finish. Uncertain mutation and exhausted budget cannot retry.
    result=finish_clients.finish(adapters.run/'native-boundary',expected_roster=terminal['roster'],deadline=deadline,kubeconfig=adapters.credential['path'])
    require(result['status']=='ACKNOWLEDGED' and time.monotonic()<adapters.deadline)
    completed=native_boundary_completion(adapters.run,chain.context,operation,terminal,inventory,progress,started=started,deadline=adapters.deadline)
    require(result['requestId']==completed['boundaryRequestId'])
    again,current=_stopped_provider(adapters,now);require(again==stop and current==inventory)
    completed_raw,_=_identity(adapters.run/'native-boundary'/'boundary-completed.json');bound=decode_private(completed_raw)
    acknowledged=time.monotonic();boot=_worker_boot()
    hold=dict(schemaVersion=1,runId=adapters.run_id,bootId=boot,acknowledgedMonotonic=acknowledged,finalDurableMonotonic=bound['finalDurableMonotonic'],notBeforeMonotonic=max(acknowledged,bound['finalDurableMonotonic'])+120.)
    from pgcat_native_controller import _write_bytes
    adapters.hold_artifact=dict(basename='native-finish-hold.json',identity=_write_bytes(adapters.run/'native-finish-hold.json',canonical(hold)))
    return _seal_action(adapters,record,completed)


@sanitized
def gateway_false_idle(run_dir,*,adapters,now):
    """Actual read-only false/idle observation; no fictional Raptor settlement."""
    require(type(adapters) is FixedGatewayAdapters and Path(run_dir)==adapters.run)
    adapters._check();before=adapters._metadata();adapters._idle()
    result=runtime_proof(run_dir,adapters=adapters,now=now)
    adapters._idle();require(adapters._metadata()==before);adapters._check()
    return result


@sanitized
def cleanup_bound_credential(path,expected_identity):
    """Remove one original authorized private copy, with same-descriptor checks."""
    from pgcat_native_controller import credential_identity,_write_bytes
    from segmented_capture import open_directory,check_directory,sha
    path=Path(path);require(type(expected_identity) is dict and expected_identity.get('path')==str(path))
    exact(expected_identity,{'path','device','inode','uid','mode','size','sha256'})
    require(all(type(expected_identity[k]) is int and expected_identity[k]>=0 for k in ('device','inode','uid','mode','size')) and hashed(expected_identity['sha256']))
    require(path.is_absolute() and path.name=='kubeconfig' and '..' not in path.parts)
    folder=open_directory(path.parent)
    try:
        fd=os.open(path.name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=folder)
        try:
            before=os.fstat(fd)
            def fingerprint(s):return (s.st_dev,s.st_ino,s.st_uid,s.st_mode,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
            require(stat.S_ISREG(before.st_mode) and before.st_uid==os.geteuid() and stat.S_IMODE(before.st_mode)==0o600 and before.st_nlink==1 and before.st_size<=65536)
            raw=os.read(fd,65537);require(len(raw)==before.st_size)
            actual=dict(path=str(path),device=before.st_dev,inode=before.st_ino,uid=before.st_uid,mode=stat.S_IMODE(before.st_mode),size=before.st_size,sha256=sha(raw))
            require(actual==expected_identity and fingerprint(os.fstat(fd))==fingerprint(before))
            claim=dict(schemaVersion=1,credential=actual)
            claim_identity=_write_bytes(path.parent/(path.name+'.cleanup.claim'),canonical(claim))
            require(fingerprint(os.fstat(fd))==fingerprint(before)==fingerprint(os.stat(path.name,dir_fd=folder,follow_symlinks=False)))
            check_directory(folder,path.parent);os.unlink(path.name,dir_fd=folder);os.fsync(folder)
            try:os.stat(path.name,dir_fd=folder,follow_symlinks=False)
            except FileNotFoundError:pass
            else:raise ValueError('credential absence')
            check_directory(folder,path.parent)
            receipt=dict(schemaVersion=1,state='deleted',credentialSha256=actual['sha256'],claimSha256=claim_identity['sha256'],absent=True)
            identity=_write_bytes(path.parent/(path.name+'.cleanup.json'),canonical(receipt))
            return dict(state='deleted',claimSha256=claim_identity['sha256'],receiptSha256=identity['sha256'])
        finally:os.close(fd)
    finally:os.close(folder)


POST_ACTIONS={'native-verdict','native-restore','native-cleanup'}
CLOSURE_ACTIONS |= POST_ACTIONS
SUPPORT_ACTIONS |= POST_ACTIONS


def hold_ready(hold,run_id,boot,at):
    exact(hold,{'schemaVersion','runId','bootId','acknowledgedMonotonic','finalDurableMonotonic','notBeforeMonotonic'})
    require(type(hold['schemaVersion']) is int and hold['schemaVersion']==1 and hold['runId']==run_id and hold['bootId']==boot)
    require(all(number(hold[k]) for k in ('acknowledgedMonotonic','finalDurableMonotonic','notBeforeMonotonic')) and number(at))
    require(hold['notBeforeMonotonic']==max(hold['acknowledgedMonotonic'],hold['finalDurableMonotonic'])+120.)
    return at>=hold['notBeforeMonotonic']


@sanitized
def verdict(run_dir,*,adapters,now):
    require(type(adapters) is FixedClosureAdapters)
    from pgcat_native_controller import _write_bytes
    from pgcat_native_assessment import assess_native
    stop=adapters.stopped['support'];finished=adapters._support('native-finish')
    record=_action(adapters,run_dir,'native-verdict',dict(stopSha256=sha_bytes(canonical(stop)),finishSha256=sha_bytes(canonical(finished))))
    inputs=dict(operation=adapters.operation,supports=adapters.supports,ingresses=adapters.ingresses)
    _write_bytes(adapters.run/'native-assessment-input.json',canonical(inputs))
    result=assess_native(adapters.run);identity=_write_bytes(adapters.run/'native-assessment.json',canonical(result))
    return _seal_action(adapters,record,dict(assessmentSha256=identity['sha256'],status=result['status']))


def _restoration_frame(adapters,now):
    from worker_capture import _native_owner_records_for_deployment,read_pod
    from capture_window import clean_client_pods,seconds
    from finish_clients import validate_roster
    live=adapters.reads.live_clients();dep=live['deployment'];bound=adapters.plan['application'];meta=dep['metadata']
    require(meta['uid']==bound['uid'] and meta['generation']==bound['generation']+1 and dep['status']['observedGeneration']==meta['generation'])
    require(dep['spec']['replicas']==2 and all(type(dep['status'][k]) is int and dep['status'][k]==2 for k in ('replicas','readyReplicas','updatedReplicas','availableReplicas')))
    owners,starting=_native_owner_records_for_deployment(dep,live['replicaSets'],live['pods'],bound)
    require(not starting and len(owners)==2 and all(not owner['terminating'] for owner in owners.values()))
    readings=[read_pod(p,{},deadline=adapters.deadline) for p in live['pods']]
    readings,valid=clean_client_pods(readings);require(valid and len(readings)==2)
    from pgcat_native_assessment import complete_restored_history
    for reading in readings:complete_restored_history(reading['snapshot'])
    old=decode_private(_identity(adapters.run/'native-capture'/'terminal-clients.json')[0])['roster']
    retired={(r['uid'],r['containerId'],r['processId']) for r in old}
    for row in readings:
        snap=row['snapshot'];sample=snap['sample']
        require(row['ready'] is True and snap['final'] is False and snap['connected']>0 and not snap['truncated'])
        require((row['uid'],row['containerId'],sample['processId']) not in retired and 0<=now()-seconds(sample['at'])<=3)
        require(sample['Attempts']>0 and sample['Success']>0 and all(sample[k]==0 for k in ('Failure','Timeout','Ambiguous','Skipped')))
    roster=validate_roster([dict(name=r['name'],uid=r['uid'],containerId=r['containerId'],processId=r['snapshot']['sample']['processId']) for r in readings])
    require(sorted(live['roster'],key=lambda r:r['uid'])==roster)
    return dict(observedAt=now(),deployment=dict(uid=meta['uid'],generation=meta['generation'],observedGeneration=dep['status']['observedGeneration'],imageDigest=bound['imageDigest']),ownership=owners,readings=readings,roster=roster)


@sanitized
def restore(run_dir,*,adapters,now):
    require(type(adapters) is FixedClosureAdapters and type(adapters.gateway) is FixedGatewayAdapters)
    from pgcat_native_assessment import _original
    from pgcat_native_controller import _write_bytes
    hold,_=_original(adapters.run,adapters.hold_artifact)
    boot=_worker_boot()
    require(hold_ready(hold,adapters.run_id,boot,time.monotonic()))
    finished=adapters._support('native-finish');assessment=adapters._support('native-verdict')
    g=adapters.gateway;require(g.plan==adapters.plan and g.prepared==adapters.prepared and g.credential==adapters.credential and g.action_id==adapters.action_id);g._idle()
    if adapters.plan['executionKind']=='disabled-proof':runtime=gateway_false_idle(run_dir,adapters=g,now=now)
    else:
        disabled=adapters._support('native-gateway-disable');require(_settlement(g)==disabled['outputs']['settlement'])
        runtime=_observe(g,False,now)
        expected=disabled['outputs']['runtime'];require(all(runtime[k]==expected[k] for k in ('secretUid','secretResourceVersion','deploymentUid','podUid','generation','imageDigest','verification')))
    g._idle();proof_identity=_write_bytes(adapters.run/'native-restore-gateway.json',canonical(runtime))
    record=_action(adapters,run_dir,'native-restore',dict(assessmentSha256=assessment['outputs']['assessmentSha256'],finishSha256=sha_bytes(canonical(finished)),gatewayProofSha256=proof_identity['sha256']))
    _stopped_provider(adapters,now,require_fresh=False)
    first=_restoration_frame(adapters,now)
    while True:
        adapters._check();require(time.monotonic()<adapters.deadline)
        second=_restoration_frame(adapters,now)
        require(second['roster']==first['roster'])
        one={r['uid']:r['snapshot']['sample'] for r in first['readings']};two={r['uid']:r['snapshot']['sample'] for r in second['readings']}
        if all(two[k]['sequence']>one[k]['sequence'] and two[k]['Success']>one[k]['Success'] and two[k]['Attempts']>one[k]['Attempts'] for k in one):break
        time.sleep(min(.02,max(0,adapters.deadline-time.monotonic())))
    _stopped_provider(adapters,now,require_fresh=False);g._idle()
    frames=_write_bytes(adapters.run/'native-restoration-frames.json',canonical([first,second]))
    return _seal_action(adapters,record,dict(roster=second['roster'],framesSha256=frames['sha256'],restoredAt=second['observedAt']))


def _worker_boot():return Path('/proc/sys/kernel/random/boot_id').read_text().strip()


def stop_bound_process(identity,*,deadline):
    """Signal only the exact worker tuple and re-probe within a finite budget."""
    import signal
    from pgcat_collector_control import process_identity,valid_identity
    valid_identity(identity);current=process_identity(identity['pid'])
    if current is None:return 'absent'
    if current!=identity:return 'unknown'
    os.kill(identity['pid'],signal.SIGTERM)
    while time.monotonic()<deadline:
        current=process_identity(identity['pid'])
        if current is None:return 'stopped'
        if current!=identity:return 'unknown'
        time.sleep(min(.02,max(0,deadline-time.monotonic())))
    if process_identity(identity['pid'])==identity:os.kill(identity['pid'],signal.SIGKILL)
    return 'stopped' if process_identity(identity['pid']) is None else 'unknown'


@sanitized
def abort_owned(run_dir,*,adapters,now):
    """Fixed Mac owner abort: independent cleanup attempts even before finish."""
    from pgcat_native_controller import NativeController,_write_bytes
    from pgcat_lane_protocol import Refused
    import sqlite3
    require(type(adapters) is NativeController)
    original=adapters._abort_originals();require(Path(run_dir)==Path(original['bindings']['captureRoot']))
    run=Path(run_dir);run.mkdir(mode=0o700,exist_ok=True)
    prepared=original['prepared'];plan=None if prepared is None else prepared['plan']
    status='INCONCLUSIVE'
    try:
        if adapters._retained_ordinary_status()=='FAIL':status='FAIL'
    except (OSError,ValueError,KeyError,TypeError,AttributeError,sqlite3.Error,TimeoutError,subprocess.SubprocessError,Refused):pass
    try:
        if adapters._worker_ordinary_status()=='FAIL':status='FAIL'
    except (OSError,ValueError,KeyError,TimeoutError,subprocess.SubprocessError,Refused):pass
    processes=[];faults=[]
    if getattr(adapters,'collector_attempted',False) and any(role not in original['workerProcesses'] for role in ('collector','guardian')):
        faults.append(dict(phase='collector-launch',operation='original-worker-identities',state='unavailable'))
    child=original['feed']
    if child is None:processes.append(dict(role='feed',identity=None,state='unknown' if original['feedAttempted'] else 'never-started'))
    else:
        identity=original['feedIdentity']
        # A retained Popen is necessary; a dict or a PID cannot grant kill authority.
        if type(child) is not subprocess.Popen:processes.append(dict(role='feed',identity=identity,state='unknown'))
        else:
            state='absent' if child.poll() is not None else 'unknown'
            try:
                if child.poll() is None:
                    try:child.terminate();child.wait(timeout=1);state='stopped'
                    except subprocess.TimeoutExpired:
                        child.kill()
                        try:child.wait(timeout=1);state='stopped'
                        except subprocess.TimeoutExpired:pass
            except OSError:state='unknown'
            if identity is None:
                state='unknown';faults.append(dict(phase='feed-start',operation='original-feed-binding',state='unavailable'))
            processes.append(dict(role='feed',identity=identity,state=state))
    for role in ('collector','guardian'):
        identity=original['workerProcesses'].get(role)
        if identity is None:processes.append(dict(role=role,identity=None,state='never-started' if not getattr(adapters,'collector_attempted',False) else 'unknown'));continue
        try:state=adapters._worker_process_stop(identity)
        except (OSError,ValueError,TimeoutError,subprocess.SubprocessError):state='unknown'
        processes.append(dict(role=role,identity=identity,state=state))
    gateway='unknown'
    try:
        proof=adapters._abort_gateway_false()
        if proof is not None:gateway='verified-disabled'
    except (OSError,ValueError,KeyError,TimeoutError,subprocess.SubprocessError):pass
    unknown=dict(state='unknown',claimSha256=None,receiptSha256=None)
    worker=dict(unknown);local=dict(unknown)
    try:worker=adapters._cleanup_worker()
    except (OSError,ValueError,KeyError,TimeoutError,subprocess.SubprocessError):pass
    try:
        identity=original['mac']
        if getattr(adapters,'local_cleanup',None) is not None:
            require(not os.path.lexists(identity['path']));local=dict(adapters.local_cleanup,state='absent')
        elif identity is not None:local=cleanup_bound_credential(identity['path'],identity)
    except (OSError,ValueError,KeyError):local=dict(state='refused',claimSha256=None,receiptSha256=None)
    try:
        if original['client'] is not None:
            original['client'].journal.cancel('incomplete')
            original['client'].close()
            if original['client'].process is not None:
                try:original['client'].process.wait(timeout=2)
                except subprocess.TimeoutExpired:pass
    finally:
        _write_bytes(run/'native-abort-faults.json',canonical(dict(schemaVersion=1,failedPhase=getattr(adapters,'phase','startup'),faults=faults)))
        record=dict(schemaVersion=1,runId=original['runId'] if plan is None else plan['runId'],executionKind=original['bindings']['laneBindings']['executionKind'],
            planSha256=None if plan is None else sha_bytes(canonical(plan)),failedPhase=getattr(adapters,'phase','startup'),status=status,ownedProcesses=processes,
            gatewayState=gateway,clientState='unknown',workerCleanup=worker,localCleanup=local,reservationState='unresolved')
        _write_bytes(run/'native-abort.json',canonical(record))
    return record


def audit_global_completion(controller):
    """Independently verify finished owner, then release only its reservation."""
    import fcntl
    from pgcat_lane_supervisor import GLOBAL_ROOT
    from pgcat_native_controller import NativeController,_write_bytes
    from segmented_capture import open_directory,check_directory,sha
    require(type(controller) is NativeController);controller._require_owner()
    client=controller.client;state=client.journal.run_snapshot();require(state['runState']=='finished')
    require(isinstance(client.process,subprocess.Popen) and client.process.poll()==0)
    stopped=client.journal.native_stop();plan=controller.prepared['plan']
    root=open_directory(GLOBAL_ROOT)
    try:
        lock=os.open('owner.lock',os.O_RDWR|os.O_NOFOLLOW,dir_fd=root)
        try:
            info=os.fstat(lock);require(stat.S_ISREG(info.st_mode) and info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1)
            fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            require((os.fstat(lock).st_dev,os.fstat(lock).st_ino)==(os.stat('owner.lock',dir_fd=root,follow_symlinks=False).st_dev,os.stat('owner.lock',dir_fd=root,follow_symlinks=False).st_ino))
            try:raw,identity=_identity(GLOBAL_ROOT/'active-run')
            except FileNotFoundError:pass
            else:
                expected=dict(runId=plan['runId'],supervisorId=client.supervisor_id,laneRoot=str(client.root))
                require(decode_private(raw)==expected)
                require(_identity(GLOBAL_ROOT/'active-run')==(raw,identity));os.unlink('active-run',dir_fd=root);os.fsync(root)
            require(not os.path.lexists(GLOBAL_ROOT/'active-run'));check_directory(root,GLOBAL_ROOT)
            audit=dict(schemaVersion=1,runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),journalState='finished',
                feedExitSha256=sha(canonical(stopped['feedExit'])),supervisorAbsent=True,reservationAbsent=True)
            identity=_write_bytes(Path(controller.bindings['captureRoot'])/'native-global-audit.json',canonical(audit))
            return audit,identity
        finally:os.close(lock)
    finally:os.close(root)



def verify_credential_absence(identity):
    """After cleanup, original claim/receipt plus absence replace live opening."""
    path=Path(identity['path']);require(not os.path.lexists(path))
    raw,claim_identity=_identity(path.parent/(path.name+'.cleanup.claim'))
    require(decode_private(raw)==dict(schemaVersion=1,credential=identity))
    raw,receipt_identity=_identity(path.parent/(path.name+'.cleanup.json'))
    require(decode_private(raw)==dict(schemaVersion=1,state='deleted',credentialSha256=identity['sha256'],claimSha256=claim_identity['sha256'],absent=True))
    require(not os.path.lexists(path));return receipt_identity['sha256']


def diagnostic_worker_proof(run,payload):
    """Re-read originals on their issuing host after credential removal."""
    from pgcat_native_controller import verify_prepared_files,verify_source_inventory
    from pgcat_native_assessment import assess_native,complete_restored_history,_original
    from pgcat_source_bootstrap import require_verified,current_hashes
    require_verified(__name__);run=Path(run)
    exact(payload,{'plan','prepared','supports','workerCleanupSha256','stopped','stopArtifact','holdArtifact','operation','ingresses'})
    plan=payload['plan'];runtime_admission(plan)
    require(plan['sourceHashes']==current_hashes());verify_source_inventory(run,plan['sourceHashes'])
    prepared=verify_prepared_files(run,plan,payload['prepared'])
    require(verify_credential_absence(prepared['workerCredential'])==payload['workerCleanupSha256'])
    exact(payload['supports'],{'native-terminal-seal','native-reconcile','native-finish','native-verdict','native-restore'})
    supports={k:read_support(run,k,plan=plan,original=a) for k,a in payload['supports'].items()}
    stopped=payload['stopped'];stop,stop_sha=_original(run,payload['stopArtifact'])
    require(stop==stopped['support'] and stop_sha==supports['native-finish']['inputs']['stopSha256']==supports['native-verdict']['inputs']['stopSha256'])
    require(decode_private(_identity(run/'native-feed-binding.json')[0])==stopped['feedBinding'])
    hold,_=_original(run,payload['holdArtifact'])
    require(payload['stopArtifact']['basename']=='native-feed-stop.json' and payload['holdArtifact']['basename']=='native-finish-hold.json')
    require(hold_ready(hold,plan['runId'],hold['bootId'],hold['notBeforeMonotonic']))
    boundary=decode_private(_identity(run/'native-boundary'/'boundary-completed.json')[0])
    require(hold['finalDurableMonotonic']==boundary['finalDurableMonotonic'] and sha_bytes(canonical(boundary))==supports['native-finish']['outputs']['boundaryCompletedSha256'])
    expected=dict(operation=payload['operation'],ingresses=payload['ingresses'],supports={k:payload['supports'][k] for k in ('native-terminal-seal','native-reconcile','native-finish')})
    require(decode_private(_identity(run/'native-assessment-input.json')[0])==expected)
    raw,_=_identity(run/'native-assessment.json');assessment=decode_private(raw)
    require(sha_bytes(raw)==supports['native-verdict']['outputs']['assessmentSha256'] and assessment==assess_native(run))
    require(assessment['status']==assessment['ordinaryStatus']==assessment['operationStatus']=='PASS' and assessment['collectorAbsent'] is True and assessment['guardianAbsent'] is True)
    raw,_=_identity(run/'native-restoration-frames.json');frames=decode_private(raw)
    restored=supports['native-restore'];require(sha_bytes(raw)==restored['outputs']['framesSha256'] and type(frames) is list and len(frames)==2)
    require(_identity(run/'native-restore-gateway.json')[1]['sha256']==restored['inputs']['gatewayProofSha256'])
    require(frames[0]['roster']==frames[1]['roster']==restored['outputs']['roster'] and frames[1]['observedAt']==restored['outputs']['restoredAt'])
    for frame in frames:
        require(frame['deployment']['uid']==plan['application']['uid'] and frame['deployment']['imageDigest']==plan['application']['imageDigest'])
        for reading in frame['readings']:complete_restored_history(reading['snapshot'])
    first={r['uid']:r['snapshot']['sample'] for r in frames[0]['readings']};second={r['uid']:r['snapshot']['sample'] for r in frames[1]['readings']}
    require(set(first)==set(second) and len(first)==2 and all(second[k]['sequence']>first[k]['sequence'] and second[k]['Success']>first[k]['Success'] and second[k]['Attempts']>first[k]['Attempts'] for k in first))
    verify_prepared_files(run,plan,prepared);verify_credential_absence(prepared['workerCredential'])
    return dict(schemaVersion=1,runId=plan['runId'],planSha256=sha_bytes(canonical(plan)),runtimeAssetsSha256=prepared['runtimeAssetsFile']['sha256'],
        assessmentSha256=supports['native-verdict']['outputs']['assessmentSha256'],restoreSha256=sha_bytes(canonical(restored)),workerCleanupSha256=payload['workerCleanupSha256'])


def _retained_diagnostic(root,assets):
    """Mac originals and source-bound worker originals form one closed proof."""
    from pgcat_lane_protocol import Journal,private_json,digest
    from pgcat_native_controller import validate_prepared,verify_source_inventory
    from pgcat_source_bootstrap import require_verified,current_hashes,ROOT
    from pgcat_lane_actions import PROFILE,REGION,WORKER
    import shlex
    root=Path(root);require(root.is_absolute() and '..' not in root.parts)
    require(not any(os.path.lexists(root/name) for name in ('native-abort.json','native-abort-faults.json')))
    prepared,_=private_json(root/'native-prepared.json');plan=prepared['plan'];runtime_admission(plan)
    validate_runtime_assets(assets,plan);actual,_=read_runtime_assets(root,plan,original=prepared['macRuntimeAssetsFile']);require(actual==assets)
    lanes=[p for p in root.iterdir() if p.is_dir() and not p.is_symlink() and (p/'journal.sqlite3').exists()]
    require(len(lanes)==1);journal=Journal.read_only(lanes[0])
    _,journal_identity=_identity(journal.path,32*1024*1024)
    state=journal.run_snapshot();require(state['runState']=='finished' and state['runId']==plan['runId'])
    require(journal.native_plan()==plan)
    with journal._connection() as db:
        outer=decode_private(bytes(db.execute('SELECT raw FROM feed_manifest').fetchone()['raw']))
        require(outer['captureRoot']==str(root) and outer['laneRoot']==str(journal.root))
        require(all(r['state']=='complete' for r in db.execute('SELECT state FROM requests')))
        cleanup=journal._native_cleanup_in_tx(db)
        records={name:journal._native_record_in_tx(db,name) for name in ('native-terminal-seal','native-reconcile','native-finish','native-verdict','native-restore')}
        rows=[decode_private(r['proof'].encode()) for r in db.execute('SELECT proof FROM native_support')]
        artifacts={name:next(r['outputs']['artifact'] for r in rows if r['action']==name) for name in records}
        finish=next(r['outputs'] for r in rows if r['action']=='native-finish')
        ingresses=[r['outputs']['ingressFile'] for r in rows if r['action']=='native-ingress']
        operation=next(r['outputs']['operationFile'] for r in reversed(rows) if r['action']=='native-ingress' and r['outputs'].get('status')=='BOUND')
    stopped=journal.native_stop();b=outer['laneBindings']
    anchors=validate_prepared(plan,{k:prepared[k] for k in ('planFile','baselineFile','gatewayRuntimeFile','runtimeAssetsFile','workerCredential')})
    require(journal.native_prepared()==anchors and anchors['runtimeAssetsFile']['sha256']==sha_bytes(canonical(assets)))
    require(verify_credential_absence(prepared['macCredential'])==cleanup['outputs']['localCleanupSha256'])
    raw,close_identity=_identity(root/'native-close.json');close=decode_private(raw)
    raw,audit_identity=_identity(root/'native-global-audit.json');audit=decode_private(raw)
    exact(audit,{'schemaVersion','runId','executionKind','planSha256','journalState','feedExitSha256','supervisorAbsent','reservationAbsent'})
    require(audit==dict(schemaVersion=1,runId=plan['runId'],executionKind='disabled-proof',planSha256=digest(plan),journalState='finished',feedExitSha256=digest(stopped['feedExit']),supervisorAbsent=True,reservationAbsent=True))
    require(close==dict(schemaVersion=1,runId=plan['runId'],executionKind='disabled-proof',planSha256=digest(plan),assessmentSha256=records['native-verdict']['outputs']['assessmentSha256'],
        restoreSha256=digest(records['native-restore']),cleanupSha256=digest(cleanup),globalAuditSha256=audit_identity['sha256'],status='PASS',closed=True))
    require(records['native-verdict']['outputs']['status']=='PASS' and cleanup['outputs']['workerAbsent'] is True and cleanup['outputs']['localAbsent'] is True)
    require_verified(__name__);require(current_hashes()==plan['sourceHashes']);verify_source_inventory(Path(__file__).absolute().parent,plan['sourceHashes'])
    payload=dict(plan=plan,prepared=anchors,supports=artifacts,workerCleanupSha256=cleanup['outputs']['workerCleanupSha256'],
        stopped=stopped,stopArtifact=finish['stopArtifact'],holdArtifact=finish['holdArtifact'],operation=operation,ingresses=ingresses)
    run=b['remoteRun'];parts=['python3','-I','-S','-c',ROOT,str(Path(run)/'pgcat_source_bootstrap.py'),b['sourceHashes']['sourceBootstrap'],
        'nativeController',canonical(b['sourceHashes']).decode(),'owned-control','diagnostic-proof',run,canonical(payload).decode()]
    result=subprocess.run([b['workbenchCli'],'exec','-P',PROFILE,'-r',REGION,'-i',WORKER,'-c',' '.join(shlex.quote(p) for p in parts)],
        stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=4,close_fds=True,env={'PATH':os.defpath,'HOME':os.path.expanduser('~'),'LANG':'C'})
    require(result.returncode==0 and len(result.stdout)<=32768)
    require(decode_private(result.stdout)==dict(schemaVersion=1,runId=plan['runId'],planSha256=digest(plan),runtimeAssetsSha256=sha_bytes(canonical(assets)),assessmentSha256=close['assessmentSha256'],restoreSha256=close['restoreSha256'],workerCleanupSha256=cleanup['outputs']['workerCleanupSha256']))
    require(_identity(root/'native-close.json')[1]==close_identity and _identity(journal.path,32*1024*1024)[1]==journal_identity)
    return dict(proofRunId=plan['runId'],closeSha256=close_identity['sha256'],sourceBundleSha256=assets['sourceBundleSha256'],runtimeAssetsSha256=sha_bytes(canonical(assets)))


def validate_native_qualification(root,assets):
    raw,identity=_identity(Path(root)/'native-qualification.json');value=decode_private(raw)
    require(raw==canonical(value))
    exact(value,{'schemaVersion','proofRunId','closeSha256','sourceBundleSha256','runtimeAssetsSha256','qualifiedAt'})
    require(type(value['schemaVersion']) is int and value['schemaVersion']==1 and number(value['qualifiedAt']) and value['qualifiedAt']<=time.time())
    require({k:value[k] for k in ('proofRunId','closeSha256','sourceBundleSha256','runtimeAssetsSha256')}==_retained_diagnostic(root,assets))
    require(_identity(Path(root)/'native-qualification.json')==(raw,identity));return value


def seal_native_qualification(controller):
    from pgcat_native_controller import NativeController,_write_bytes
    require(type(controller) is NativeController);controller._require_owner()
    root=Path(controller.bindings['captureRoot'])
    proof=_retained_diagnostic(root,controller.runtime_assets)
    value=dict(schemaVersion=1,qualifiedAt=time.time(),**proof)
    _write_bytes(root/'native-qualification.json',canonical(value));return value
