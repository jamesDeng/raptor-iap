"""One-use worker-local controls for the bounded PgCat live test."""
import hashlib
import json
import os
import stat
import subprocess
import time
import argparse
import fcntl
import uuid
from pathlib import Path

SCOPE = {
    'cluster': 'c92787e953503492ea141a744c81498f1',
    'group': 'asg-t4necb4dfmu6o3z2rn0g',
    'proxy': 'DP00001',
    'region': 'ap-southeast-1',
}


def private_directory(path):
    if path.is_symlink() or not path.is_dir() or stat.S_IMODE(path.stat().st_mode) != 0o700:
        raise ValueError('private worker directory required')


def unique_pairs(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError('duplicate trigger key')
        value[key] = item
    return value


def write_receipt(path, value):
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, allow_nan=False, separators=(',', ':'))
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(str(path.parent), os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def write_heartbeat(run_dir, run_id, action):
    directory = run_dir / 'control'
    directory.mkdir(mode=0o700, exist_ok=True)
    private_directory(directory)
    destination = directory / (action + '.heartbeat.json')
    temporary = directory / ('.' + action + '.heartbeat.' + uuid.uuid4().hex)
    try:
        write_receipt(temporary, {'runId': run_id, 'action': action, 'observedAt': time.time()})
        os.replace(str(temporary), str(destination))
        fd = os.open(str(directory), os.O_RDONLY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if temporary.exists():
            temporary.unlink()


def command(argv, timeout, env):
    result = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            timeout=timeout, env=env, check=True)
    if len(result.stdout) > 8192:
        raise ValueError('control response too large')
    return result.stdout


def checked_json(raw):
    if len(raw) > 8192:
        raise ValueError('control response too large')
    value = json.loads(raw.decode('utf-8'), object_pairs_hook=unique_pairs)
    if not isinstance(value, dict):
        raise ValueError('control response must be an object')
    return value


class WorkerControl:
    def __init__(self, run_dir, run_id, action, expected_digest, kubeconfig=None, run_command=command):
        self.run_dir = Path(run_dir)
        self.run_id = run_id
        self.action = action
        self.expected_digest = expected_digest
        self.kubeconfig = Path(kubeconfig) if kubeconfig else None
        self.run_command = run_command

    def _environment(self):
        path = self.kubeconfig
        if path is None or path.is_symlink() or not path.is_file() or stat.S_IMODE(path.stat().st_mode) != 0o600:
            raise ValueError('private worker credential required')
        env = os.environ.copy()
        env['KUBECONFIG'] = str(path)
        return env

    def _gate(self):
        gate = str(self.run_dir / 'gateway-live-gate')
        want = self.action == 'enable'
        expected = 'false' if want else 'true'
        target = 'true' if want else 'false'
        stage = 'set'
        try:
            env = self._environment()
            set_result = checked_json(self.run_command(
                [gate, '--namespace', 'raptor-system', '--secret', 'gateway-live-config',
                 '--expected', expected, '--set', target], 90, env))
            rv = set_result.get('resourceVersion')
            changed = set_result.get('changedAfter')
            if (not isinstance(rv, str) or not rv.isdigit() or not isinstance(changed, str)
                    or not changed.endswith('Z') or set_result.get('replacementEnabled') is not want):
                raise ValueError('invalid guarded set receipt')
            write_receipt(self.run_dir / 'control' / (self.action + '.set.json'),
                          {'resourceVersion': rv, 'changedAfter': changed, 'replacementEnabled': want})
            stage = 'rollout'
            common = ['--expected', target, '--resource-version', rv, '--changed-after', changed]
            rollout = self.run_command([gate, 'rollout'] + common, 120, env)
            if rollout.strip() != b'rollout=requested':
                raise ValueError('rollout request uncertain')
            self.run_command(['kubectl', '-n', 'raptor-system', 'rollout', 'status',
                              'deployment/agent-gateway', '--timeout=120s'], 135, env)
            stage = 'verify'
            verification = checked_json(self.run_command([gate, 'verify'] + common, 90, env))
            generation = verification.get('generation')
            pod_uid = verification.get('podUID')
            if (verification.get('replacementEnabled') is not want or verification.get('ready') is not True
                    or not isinstance(generation, int) or generation <= 0
                    or not isinstance(pod_uid, str) or not pod_uid):
                raise ValueError('guard verification failed')
            return {'action': self.action, 'status': 'VERIFIED', 'resourceVersion': rv,
                    'podUID': pod_uid, 'generation': generation, 'replacementEnabled': want}
        except Exception:
            return {'action': self.action, 'status': 'INCONCLUSIVE', 'stage': stage}

    def _finish(self):
        try:
            env = self._environment()
            result = checked_json(self.run_command(
                ['python3', str(self.run_dir / 'finish_clients.py'), '--boundary-dir',
                 str(self.run_dir / 'full-boundary'), '--expected-replicas', '2'], 30, env))
            clients = result.get('clients')
            boundary_id = result.get('requestId')
            if (result.get('status') != 'ACKNOWLEDGED' or not isinstance(clients, dict)
                    or len(clients) != 2 or not all(isinstance(row, dict) and row.get('status') == 'acknowledged'
                                                    for row in clients.values())
                    or not isinstance(boundary_id, str) or not boundary_id):
                raise ValueError('client finish uncertain')
            return {'action': 'finish', 'status': 'ACKNOWLEDGED', 'boundaryRequestId': boundary_id}
        except Exception:
            return {'action': 'finish', 'status': 'INCONCLUSIVE', 'stage': 'finish'}

    def _controlled_action(self):
        lock_path = self.run_dir / 'control' / 'action.lock'
        try:
            fd = os.open(str(lock_path), os.O_CREAT | os.O_RDWR | getattr(os, 'O_NOFOLLOW', 0), 0o600)
            if not stat.S_ISREG(os.fstat(fd).st_mode) or stat.S_IMODE(os.fstat(fd).st_mode) != 0o600:
                os.close(fd)
                raise ValueError('private control lock required')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except Exception:
                os.close(fd)
                raise
        except Exception:
            return {'action': self.action, 'status': 'INCONCLUSIVE', 'stage': 'lock'}
        try:
            if self.action in ('enable', 'disable'):
                return self._gate()
            return self._finish()
        finally:
            fcntl.flock(fd, fcntl.LOCK_UN)
            os.close(fd)

    def run_once(self):
        if self.action not in ('probe', 'enable', 'disable', 'finish'):
            raise ValueError('unsupported control action')
        if (not isinstance(self.expected_digest, str) or len(self.expected_digest) != 64
                or any(char not in '0123456789abcdef' for char in self.expected_digest)):
            raise ValueError('invalid trigger digest')
        commands = self.run_dir / 'commands'
        trigger = commands / (self.action + '.json')
        claimed = commands / (self.action + '.claimed')
        private_directory(self.run_dir)
        private_directory(commands)
        if claimed.exists():
            if trigger.exists() or trigger.is_symlink():
                raise ValueError('action already claimed')
            return None
        if not trigger.exists() and not trigger.is_symlink():
            return None
        fd = os.open(str(claimed), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        os.close(fd)
        try:
            flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0)
            fd = os.open(str(trigger), flags)
            try:
                info = os.fstat(fd)
                if (not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) not in (0o600, 0o644)
                        or info.st_uid != os.geteuid() or info.st_nlink != 1 or info.st_size > 4096):
                    raise ValueError('invalid trigger file')
                raw = os.read(fd, 4097)
            finally:
                os.close(fd)
            if len(raw) > 4096 or hashlib.sha256(raw).hexdigest() != self.expected_digest:
                raise ValueError('trigger checksum mismatch')
            body = json.loads(raw.decode('utf-8'), object_pairs_hook=unique_pairs)
            if not isinstance(body, dict) or set(body) != {'runId', 'action', 'scope'}:
                raise ValueError('trigger schema mismatch')
            if body['runId'] != self.run_id or body['action'] != self.action or body['scope'] != SCOPE:
                raise ValueError('trigger target mismatch')
            directory = self.run_dir / 'control'
            directory.mkdir(mode=0o700, exist_ok=True)
            private_directory(directory)
            if self.action in ('enable', 'disable', 'finish'):
                result = self._controlled_action()
            else:
                result = {'action': self.action, 'status': 'ACKNOWLEDGED'}
        except Exception:
            result = {'action': self.action, 'status': 'REJECTED', 'stage': 'trigger'}
        directory = self.run_dir / 'control'
        directory.mkdir(mode=0o700, exist_ok=True)
        private_directory(directory)
        receipt = directory / (self.action + '.status.json')
        write_receipt(receipt, result)
        trigger.unlink()
        return result


def native_transition(adapters,want,now):
    """Reuse the fixed legacy sequence, with native source/deadline/provenance gates."""
    from pgcat_native_closure import FixedGatewayAdapters,_set_receipt,_observe,_guarded
    from pgcat_native_ingress import require,_identity
    from pgcat_native_controller import _write_bytes
    from segmented_capture import canonical,decode_private
    require(type(adapters) is FixedGatewayAdapters and type(want) is bool)
    action='enable' if want else 'disable';run=adapters.run
    (run/'control').mkdir(mode=0o700,exist_ok=True);private_directory(run/'control')
    before=adapters._metadata();require(before['replacementEnabled'] is not want)
    job_end=getattr(adapters,'job_deadline',adapters.deadline)
    actual=[];observed=[]
    def group(seconds):
        adapters.deadline=min(job_end,time.monotonic()+seconds)
        adapters.reads.deadline=adapters.deadline
    def invoke(args,timeout,env):
        # The helper's old argv form is converted into the existing exact native allowlist.
        if args[0]==str(adapters.gate) and args[1]=='--namespace':
            args=[str(adapters.gate),'set','--expected',str(not want).lower(),'--set',str(want).lower()]
            group(4.)
        elif args[:2]==[str(adapters.gate),'rollout']:
            group(4.)
        elif args[0]=='kubectl':
            args=['kubectl','--kubeconfig',adapters.credential['path']]+args[1:]
            group(135.)
        elif args[:2]==[str(adapters.gate),'verify']:
            group(4.)
            require(len(actual)==1)
            observed.append(_observe(adapters,want,now,set_receipt=actual[0]))
            return canonical(observed[0]['verification'])
        raw=adapters._command(args)
        if args[1]=='set':
            receipt=decode_private(raw);_set_receipt(receipt,want,now());require(receipt['resourceVersion']!=before['secretResourceVersion'])
            _write_bytes(run/('native-gateway-'+action+'.set.json'),canonical(receipt));actual.append(receipt)
        return raw
    helper=WorkerControl(run,adapters.run_id,action,'0'*64,kubeconfig=adapters.credential['path'],run_command=invoke)
    result=helper._gate();require(result['status']=='VERIFIED' and len(actual)==len(observed)==1)
    # _gate performed verify within this invocation. The same final 4s group
    # includes the independent metadata/runtime reread; it does not renew here.
    runtime=observed[0]
    require(all(runtime[k]==before[k] for k in ('secretUid','deploymentUid','imageDigest')))
    _guarded[adapters]=(os.getpid(),dict(actual[0]))
    return actual[0],runtime


_native_jobs={}
_launched_children=[]


def verify_job_result(result,key):
    """Pure integrity check. Only the controller's original launch owns the key."""
    import hmac
    from pgcat_native_ingress import exact,require,hashed
    from segmented_capture import canonical,decode_private
    exact(result,{'claimSha256','process','status','support','artifact','authenticator'})
    require(hashed(key) and hashed(result['authenticator']))
    value={k:v for k,v in result.items() if k!='authenticator'}
    require(hmac.compare_digest(result['authenticator'],hmac.new(bytes.fromhex(key),canonical(value),hashlib.sha256).hexdigest()))
    return decode_private(canonical(value))


def issued_gateway_job(adapter):
    from pgcat_native_ingress import require
    from segmented_capture import canonical,decode_private
    row=_native_jobs.get(id(adapter))
    require(row is not None and row[0] is adapter and row[1]==os.getpid())
    return decode_private(row[2])


def _native_claim(run,claim):
    from pgcat_native_ingress import exact,require,identifier,_identity,validate_plan,hashed
    from pgcat_native_controller import verify_prepared,verify_source_inventory
    from pgcat_native_closure import read_runtime_assets
    from pgcat_source_bootstrap import current_hashes,require_verified
    from segmented_capture import decode_private,canonical
    require_verified(__name__)
    exact(claim,{'schemaVersion','action','requestId','runId','planSha256','prepared','readyFile','qualification','settlement','owner','reservation','budget'})
    require(claim['schemaVersion']==1 and claim['action'] in ('native-gateway-enable','native-gateway-disable'))
    identifier(claim['requestId']);identifier(claim['runId'])
    plan=validate_plan(decode_private(_identity(run/'native-plan.json')[0]))
    require(plan['executionKind']=='native-replacement' and plan['runId']==claim['runId'] and hashlib.sha256(canonical(plan)).hexdigest()==claim['planSha256'])
    require(plan['sourceHashes']==current_hashes());verify_source_inventory(run,plan['sourceHashes'])
    verify_prepared(run,plan,claim['prepared']);assets,_=read_runtime_assets(run,plan,original=claim['prepared']['runtimeAssetsFile'])
    owner=claim['owner'];exact(owner,{'pid','runId','planSha256','feedBindingSha256','supervisorId','humanPause'})
    require(type(owner['pid']) is int and owner['pid']>0 and owner['runId']==plan['runId'] and owner['planSha256']==claim['planSha256'] and hashed(owner['feedBindingSha256']))
    identifier(owner['supervisorId']);require(owner['humanPause']=='operator-confirmed-external-pause')
    reservation=claim['reservation'];exact(reservation,{'runId','planSha256','reservationId'})
    require(reservation['runId']==plan['runId'] and reservation['planSha256']==claim['planSha256']);identifier(reservation['reservationId'])
    if claim['action']=='native-gateway-enable':
        require(claim['settlement'] is None and type(claim['qualification']) is dict)
        require(_identity(run/'native-ready.json')[1]==claim['readyFile'])
    else:
        require(claim['qualification'] is None)
        settled=claim['settlement'];require(type(settled) is dict)
        if settled.get('kind')=='pre-create-reserved':
            from pgcat_native_closure import validate_settlement
            validate_settlement(settled,request_id=None)
            require(settled['runId']==plan['runId'] and settled['planSha256']==claim['planSha256'] and
                settled['ownerSha256']==hashlib.sha256(canonical(owner)).hexdigest() and settled['reservationSha256']==hashlib.sha256(canonical(reservation)).hexdigest())
        else:
            exact(settled,{'kind','raptorRequestId'});require(settled['kind']=='bound-request');identifier(settled['raptorRequestId'])
    _native_budget_end(run,claim,plan)
    return plan,assets


def _native_budget_end(run,claim,plan):
    from pgcat_native_controller import validate_gateway_budget,capture_budget
    from pgcat_native_ingress import _identity,require
    from pgcat_collector_control import intent_profile,NATIVE_MODE
    from segmented_capture import decode_private
    end=validate_gateway_budget(plan,claim['budget'])
    raw,identity=_identity(run/'collector-job'/'intent.json')
    require(identity==claim['budget']['collectorIntentFile'])
    started=intent_profile(decode_private(raw),NATIVE_MODE)
    require(started['runId']==plan['runId'] and started['prepared']==claim['prepared'])
    require(started['captureMaxSeconds']<=capture_budget(plan,started['startedAt']))
    require(started['startedAt']>=claim['budget']['captureStartedAt']-1.)
    return min(end,started['startedAt']+started['captureMaxSeconds'])


def _slot_identity(info):
    return dict(device=info.st_dev,inode=info.st_ino,uid=info.st_uid,mode=stat.S_IMODE(info.st_mode),nlink=info.st_nlink)


def _native_anchor(run,payload):
    from pgcat_native_ingress import exact,require,_identity
    from segmented_capture import canonical,decode_private
    exact(payload,{'claim','launch'})
    claim=payload['claim'];plan,assets=_native_claim(run,claim)
    launch=payload['launch'];exact(launch,{'record','file'})
    folder=run/(claim['action']+'-job')
    raw,identity=_identity(folder/'started.json')
    require(raw==canonical(launch['record']) and identity==launch['file'])
    record=launch['record'];exact(record,{'claimSha256','intentFile','readyFile','process','resultSlot','deadlineMonotonic'})
    raw,intent_identity=_identity(folder/'intent.json');intent=decode_private(raw)
    require(intent_identity==record['intentFile'] and intent['claim']==claim and record['claimSha256']==hashlib.sha256(canonical(claim)).hexdigest())
    require(record['deadlineMonotonic']==intent['deadlineMonotonic'])
    ready,ready_identity=_identity(folder/'ready.json')
    require(ready_identity==record['readyFile'] and decode_private(ready)==dict(claimSha256=record['claimSha256'],process=record['process']))
    from pgcat_collector_control import valid_identity
    valid_identity(record['process'])
    return folder,record,plan,assets


def native_job_control(verb,run,payload):
    """Short launch/trigger/status calls; only the detached child waits on rollout."""
    from pgcat_native_ingress import exact,require,_identity
    from pgcat_native_controller import _write_bytes,owned_process
    from pgcat_collector_control import process_identity
    from pgcat_source_bootstrap import command as source_command,require_verified
    from segmented_capture import canonical,decode_private
    require_verified(__name__);run=Path(run)
    require(run==Path(__file__).absolute().parent and run.name=='run')
    private_directory(run)
    if verb=='launch':
        exact(payload,{'claim'});claim=payload['claim'];plan,assets=_native_claim(run,claim)
        remaining=_native_budget_end(run,claim,plan)-time.time()
        require(remaining>0 and (claim['action']!='native-gateway-enable' or remaining>=407.))
        folder=run/(claim['action']+'-job');folder.mkdir(mode=0o700)
        deadline=time.monotonic()+min(151.,remaining)
        intent=dict(claim=claim,deadlineMonotonic=deadline)
        intent_file=_write_bytes(folder/'intent.json',canonical(intent))
        fd=os.open(str(folder/'result.json'),os.O_RDWR|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        slot=_slot_identity(os.fstat(fd));child=None
        key=os.urandom(32);key_reader,key_writer=os.pipe()
        try:
            os.write(key_writer,key);os.close(key_writer);key_writer=None
            child_payload=dict(action=claim['action'],intentSha256=intent_file['sha256'],resultFD=fd,resultKeyFD=key_reader)
            args=source_command(plan['sourceHashes'],'nativeController',['owned-control','gateway-job-run',str(run),canonical(child_payload).decode()])
            child=subprocess.Popen(args,cwd=str(run),env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'},stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,close_fds=True,pass_fds=(fd,key_reader),start_new_session=True)
            stop=time.monotonic()+3.
            while not (folder/'ready.json').exists():
                require(child.poll() is None and time.monotonic()<stop);time.sleep(.01)
            identity=owned_process(child,process_identity)
            require(identity['commandSha256']==hashlib.sha256(b'\0'.join(os.fsencode(v) for v in args)+b'\0').hexdigest())
            raw,ready_file=_identity(folder/'ready.json');ready=decode_private(raw)
            require(ready==dict(claimSha256=hashlib.sha256(canonical(claim)).hexdigest(),process=identity))
            record=dict(claimSha256=ready['claimSha256'],intentFile=intent_file,readyFile=ready_file,process=identity,resultSlot=slot,deadlineMonotonic=deadline)
            # Retain real Popen until this short launcher exits; do not invent a
            # long Workbench transport merely to retain a remote Python object.
            _launched_children[:]=[c for c in _launched_children if c.poll() is None]
            _launched_children.append(child)
            return dict(record=record,file=_write_bytes(folder/'started.json',canonical(record)),resultKey=key.hex())
        except BaseException:
            # No trigger exists, so stopping this actual launch cannot interrupt a mutation.
            if child is not None and child.poll() is None:
                child.terminate()
                try:child.wait(timeout=.2)
                except subprocess.TimeoutExpired:child.kill()
            raise
        finally:
            os.close(fd);os.close(key_reader)
            if key_writer is not None:os.close(key_writer)
    require(verb in ('trigger','status','stop','ready','false'))
    folder,record,plan,assets=_native_anchor(run,payload)
    current=process_identity(record['process']['pid'])
    if verb in ('ready','false'):
        from pgcat_native_controller import FixedWorkerReads
        from pgcat_native_closure import FixedGatewayAdapters,_observe,_settlement,gateway_ready
        # This re-read never dispatches a job or issues a support artifact.
        status=native_job_control('status',run,payload);require(status['status']=='VERIFIED')
        claim=payload['claim'];require(claim['action']==('native-gateway-enable' if verb=='ready' else 'native-gateway-disable'))
        reads=FixedWorkerReads(plan,run.parent/'kubeconfig',credential=claim['prepared']['workerCredential'],prepared=claim['prepared'])
        settled=claim['settlement'];request=None if settled is None or settled['kind']=='pre-create-reserved' else settled['raptorRequestId']
        adapter=FixedGatewayAdapters(run,reads,runtime_assets=assets,prepared=claim['prepared'],action_id=claim['requestId'],raptor_request_id=request)
        adapter.deadline=reads.deadline=time.monotonic()+3.5
        _native_jobs[id(adapter)]=(adapter,os.getpid(),canonical(claim))
        try:
            adapter._idle()
            if verb=='ready':gateway_ready(adapter,claim['readyFile'],now=time.time,initial=False)
            else:require(_settlement(adapter)==status['support']['outputs']['settlement'])
            observed=_observe(adapter,verb=='ready',time.time);adapter._idle()
            return observed
        finally:_native_jobs.pop(id(adapter),None)
    if verb=='trigger':
        require(current==record['process'] and time.monotonic()<record['deadlineMonotonic'] and time.time()<_native_budget_end(run,payload['claim'],plan))
        # No retry, including after a lost response. Child verifies original launch.
        identity=_write_bytes(folder/'trigger.json',canonical(payload['launch']))
        return dict(status='TRIGGERED',triggerFile=identity)
    if verb=='stop':
        from pgcat_native_closure import stop_bound_process
        state=stop_bound_process(record['process'],deadline=time.monotonic()+2.)
        return dict(status='UNKNOWN',processState=state)
    flags=os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK
    fd=os.open(str(folder/'result.json'),flags)
    try:
        before=os.fstat(fd);require(_slot_identity(before)==record['resultSlot'] and before.st_size<=65536)
        raw=os.read(fd,65537);after=os.fstat(fd)
        require(_slot_identity(after)==record['resultSlot'] and (before.st_size,before.st_mtime_ns,before.st_ctime_ns)==(after.st_size,after.st_mtime_ns,after.st_ctime_ns))
    finally:os.close(fd)
    if not raw:
        return dict(status='PENDING' if current==record['process'] and time.monotonic()<record['deadlineMonotonic'] else 'UNKNOWN')
    # Child writes result only after issuing actual support in its own process.
    # The original preopened output inode, source/claim/launch chain and exact
    # terminal identity are all required; an arbitrary status file is irrelevant.
    result=decode_private(raw);exact(result,{'claimSha256','process','status','support','artifact','authenticator'})
    require(result['claimSha256']==record['claimSha256'] and result['process']==record['process'])
    require(current in (None,record['process']) and result['status'] in ('VERIFIED','UNKNOWN'))
    if result['status']=='VERIFIED':
        from pgcat_native_closure import read_support
        require(read_support(run,payload['claim']['action'],plan=plan,original=result['artifact'])==result['support'])
    else:require(result['support'] is None and result['artifact'] is None)
    return result


def native_job_run(run,payload):
    """Single detached source-owned process; waits for one exact trigger, then exits."""
    from pgcat_native_ingress import exact,require,_identity
    from pgcat_native_controller import _write_bytes,FixedWorkerReads
    from pgcat_native_closure import FixedGatewayAdapters,gateway_enable,gateway_disable,support_artifact
    from pgcat_collector_control import process_identity
    from pgcat_source_bootstrap import require_verified
    from segmented_capture import canonical,decode_private
    require_verified(__name__);run=Path(run)
    require(run==Path(__file__).absolute().parent)
    exact(payload,{'action','intentSha256','resultFD','resultKeyFD'})
    require(payload['action'] in ('native-gateway-enable','native-gateway-disable') and type(payload['resultFD']) is int and payload['resultFD']>=3)
    require(type(payload['resultKeyFD']) is int and payload['resultKeyFD']>=3 and payload['resultKeyFD']!=payload['resultFD'])
    key=os.read(payload['resultKeyFD'],33);os.close(payload['resultKeyFD']);require(len(key)==32)
    folder=run/(payload['action']+'-job');raw,intent_file=_identity(folder/'intent.json')
    require(intent_file['sha256']==payload['intentSha256']);intent=decode_private(raw)
    exact(intent,{'claim','deadlineMonotonic'});claim=intent['claim'];plan,assets=_native_claim(run,claim)
    deadline=intent['deadlineMonotonic'];require(0<deadline-time.monotonic()<=151.)
    proc=process_identity(os.getpid());require(proc is not None)
    fd=payload['resultFD'];slot=_slot_identity(os.fstat(fd));require(slot['uid']==os.geteuid() and slot['mode']==0o600 and slot['nlink']==1 and os.fstat(fd).st_size==0)
    claim_sha=hashlib.sha256(canonical(claim)).hexdigest()
    _write_bytes(folder/'ready.json',canonical(dict(claimSha256=claim_sha,process=proc)))
    result=dict(claimSha256=claim_sha,process=proc,status='UNKNOWN',support=None,artifact=None)
    adapter=None
    try:
        while not (folder/'trigger.json').exists():
            require(time.monotonic()<deadline);time.sleep(.02)
        launch=decode_private(_identity(folder/'trigger.json')[0])
        _,record,_,_=_native_anchor(run,dict(claim=claim,launch=launch))
        require(record['process']==proc and record['resultSlot']==slot and record['deadlineMonotonic']==deadline)
        # All wait time consumes the same absolute ceiling.
        reads=FixedWorkerReads(plan,run.parent/'kubeconfig',credential=claim['prepared']['workerCredential'],prepared=claim['prepared'])
        settled=claim['settlement'];request=None if settled is None or settled['kind']=='pre-create-reserved' else settled['raptorRequestId']
        adapter=FixedGatewayAdapters(run,reads,runtime_assets=assets,prepared=claim['prepared'],action_id=claim['requestId'],raptor_request_id=request)
        remaining=_native_budget_end(run,claim,plan)-time.time();require(remaining>0)
        adapter.job_deadline=min(deadline,time.monotonic()+remaining);adapter.deadline=reads.deadline=min(adapter.job_deadline,time.monotonic()+4.)
        require(len(_native_jobs)<16);_native_jobs[id(adapter)]=(adapter,os.getpid(),canonical(claim))
        short='enable' if claim['action']=='native-gateway-enable' else 'disable'
        commands=run/'commands';commands.mkdir(mode=0o700,exist_ok=True);private_directory(commands)
        body=canonical(dict(runId=plan['runId'],action=short,scope=SCOPE))
        _write_bytes(commands/(short+'.json'),body)
        class NativeControl(WorkerControl):
            def _gate(self):
                support=(gateway_enable if short=='enable' else gateway_disable)(run,adapters=adapter,now=time.time)
                artifact=support_artifact(run,claim['action'],adapters=adapter)
                result.update(status='VERIFIED',support=support,artifact=artifact)
                return dict(action=short,status='VERIFIED')
        control=NativeControl(run,plan['runId'],short,hashlib.sha256(body).hexdigest(),kubeconfig=run.parent/'kubeconfig')
        status=control.run_once();require(status is not None and status['status']=='VERIFIED' and time.monotonic()<deadline)
    except Exception:result.update(status='UNKNOWN',support=None,artifact=None)
    finally:
        if adapter is not None:_native_jobs.pop(id(adapter),None)
        require(_slot_identity(os.fstat(fd))==slot and os.fstat(fd).st_size==0)
        import hmac
        signed=dict(result,authenticator=hmac.new(key,canonical(result),hashlib.sha256).hexdigest())
        raw=canonical(signed);require(len(raw)<=65536);require(os.write(fd,raw)==len(raw));os.fsync(fd);os.close(fd)
    return dict(status=result['status'])


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--run-dir', required=True)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--action', choices=('probe', 'enable', 'disable', 'finish'), required=True)
    parser.add_argument('--trigger-sha256', required=True)
    parser.add_argument('--kubeconfig')
    parser.add_argument('--max-seconds', type=int, default=7200)
    args = parser.parse_args()
    if args.max_seconds < 1 or args.max_seconds > 7200:
        raise SystemExit('invalid control wait bound')
    control = WorkerControl(args.run_dir, args.run_id, args.action, args.trigger_sha256,
                            kubeconfig=args.kubeconfig)
    deadline = time.monotonic() + args.max_seconds
    next_heartbeat = 0
    while time.monotonic() < deadline:
        now = time.monotonic()
        if now >= next_heartbeat:
            write_heartbeat(Path(args.run_dir), args.run_id, args.action)
            next_heartbeat = now + 5
        result = control.run_once()
        if result is not None:
            return 0 if result['status'] in ('ACKNOWLEDGED', 'VERIFIED') else 1
        time.sleep(0.2)
    return 2


if __name__ == '__main__':
    raise SystemExit(main())
