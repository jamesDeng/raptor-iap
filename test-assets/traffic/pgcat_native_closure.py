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
        exact(runtime_assets,ASSET_KEYS)
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
    require(type(adapters) is FixedGatewayAdapters and Path(run_dir)==adapters.run and 'runId' in adapters.plan and adapters.action_id is not None)
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
        record['runId']==plan['runId'] and record['executionKind']==plan['executionKind']=='native-replacement' and
        record['planSha256']==hashlib.sha256(canonical(plan)).hexdigest())
    identifier(record['requestId']);inputs=record['inputs'];out=record['outputs']
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
    require(type(adapters) is FixedGatewayAdapters and Path(run_dir)==adapters.run)
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
