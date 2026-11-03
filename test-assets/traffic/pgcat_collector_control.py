#!/usr/bin/env python3
"""One-use finite collector. Private bindings only; no runtime command input."""
import hashlib
import importlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import uuid

# This loader executes the opened reviewed bytes and never consults pyc. It is
# used at every process boundary, including the initial Workbench entry.
BOOTSTRAP = '''import hashlib,os,stat,sys
try:
 p,h=sys.argv[1:3]
 f=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
 try:
  s=os.fstat(f);b=os.read(f,262145)
  ok=(stat.S_ISREG(s.st_mode) and s.st_uid==os.geteuid() and stat.S_IMODE(s.st_mode)==384 and s.st_nlink==1 and len(b)==s.st_size and len(b)<=262144 and hashlib.sha256(b).hexdigest()==h)
 finally: os.close(f)
 if not ok: raise ValueError()
 sys.argv=[p]+sys.argv[3:]
 exec(compile(b,p,'exec',dont_inherit=True),{'__name__':'__main__','__file__':p})
except BaseException as e:
 if isinstance(e,SystemExit): raise
 print('{"status":"INCONCLUSIVE"}')
 sys.exit(1)
'''
SOURCES = {
    'collectorControl': 'pgcat_collector_control.py',
    'laneTimingAssessor': 'pgcat_lane_timing_assessor.py',
    'rehearsalWorker': 'pgcat_rehearsal_worker.py',
    'rehearsalContract': 'pgcat_rehearsal_contract.py',
    'pendingProofWorker': 'pgcat_pending_proof_worker.py',
    'preDrainProofWorker': 'pgcat_pre_drain_proof_worker.py',
    'zeroProofWorker': 'pgcat_zero_proof_worker.py',
    'captureWindow': 'capture_window.py', 'finishClients': 'finish_clients.py',
    'workerCapture': 'worker_capture.py', 'segmentedCapture': 'segmented_capture.py'}

NATIVE_MODE='full-replacement-direct-v2'
NATIVE_SOURCES=dict(SOURCES,nativeEvidence='pgcat_native_evidence.py')


def check(value):
    if not value:
        raise ValueError('collector')


def directory(path):
    path = Path(path)
    check(path.is_absolute() and '..' not in path.parts)
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in path.parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd); fd = child
        s = os.fstat(fd)
        check(s.st_uid == os.geteuid() and stat.S_IMODE(s.st_mode) == 0o700)
        return fd
    except BaseException:
        os.close(fd)
        raise


def read(fd, name, limit=262144):
    item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
    try:
        before = os.fstat(item)
        check(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and
              stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1 and before.st_size <= limit)
        raw = os.read(item, limit+1)
        after = os.fstat(item); current = os.stat(name, dir_fd=fd, follow_symlinks=False)
        def identity(s):
            return (s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
        check(len(raw) == before.st_size and identity(before) == identity(after) == identity(current))
        return raw
    finally:
        os.close(item)


def decode(raw):
    def unique(rows):
        value = {}
        for k,v in rows:
            check(k not in value); value[k] = v
        return value
    def invalid(value):
        raise ValueError('collector')
    return json.loads(raw.decode('ascii'),object_pairs_hook=unique,parse_constant=invalid)


def write(fd, name, value):
    raw = json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')
    item = os.open(name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=fd)
    try:
        os.fchmod(item,0o600)
        while raw:
            count=os.write(item,raw);check(count>0);raw=raw[count:]
        os.fsync(item)
    finally:
        os.close(item)
    os.fsync(fd)


def _failure_category(error):
    # Never inspect exception text, dynamic names, traceback paths or locals.
    for kind,category in ((subprocess.TimeoutExpired,'timeout'),(OSError,'os-error'),
            (ValueError,'value-error'),(TypeError,'type-error'),(KeyError,'key-error'),
            (RuntimeError,'runtime-error'),(KeyboardInterrupt,'interrupted'),(SystemExit,'system-exit')):
        if isinstance(error,kind):return category
    return 'other-error'


def _failure_locations(error):
    # Compare exact source filenames in this loaded bundle directory. Never
    # export traceback filenames, dynamic function names, messages or locals.
    parent=os.path.dirname(os.path.abspath(__file__))
    names={'pgcat_native_controller.py':'nativeController','pgcat_native_ingress.py':'nativeIngress',
        'pgcat_collector_control.py':'collectorControl','pgcat_lane_protocol.py':'laneProtocol',
        'pgcat_lane_client.py':'laneClient','pgcat_native_closure.py':'nativeClosure',
        'pgcat_native_evidence.py':'nativeEvidence','segmented_capture.py':'segmentedCapture',
        'capture_window.py':'captureWindow','pgcat_rehearsal_contract.py':'rehearsalContract'}
    allowed={os.path.join(parent,name):module for name,module in names.items()}
    trace=error.__traceback__;locations=[];examined=0
    while trace is not None and examined<64:
        module=allowed.get(trace.tb_frame.f_code.co_filename)
        if module is not None and type(trace.tb_lineno) is int and trace.tb_lineno>0:
            locations.append(dict(module=module,line=trace.tb_lineno));locations=locations[-8:]
        trace=trace.tb_next;examined+=1
    return locations


def _private_startup_failure(fd,name,value):
    """Failure-only, bounded private sibling; never a startup authority."""
    check(name in ('native-owner-failure.json','native-guardian-failure.json'))
    info=os.fstat(fd)
    check(stat.S_ISDIR(info.st_mode) and info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o700)
    raw=json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')
    check(len(raw)<=4096)
    write(fd,name,value)


def entry_command(worker, digest, arguments):
    return [sys.executable,'-I','-S','-c',BOOTSTRAP,str(worker),digest]+arguments


def native_entry_command(run,expected,args):
    from pgcat_source_bootstrap import ROOT,NATIVE_SOURCES
    check(set(expected)==set(NATIVE_SOURCES))
    return ['python3','-I','-S','-c',ROOT,str(run/'pgcat_source_bootstrap.py'),expected['sourceBootstrap'],
        'collectorControl',json.dumps(expected,sort_keys=True,separators=(',',':'))]+args


def context(contract, run_id, expected, worker_path=None, *, profile='no-scale'):
    check(type(run_id) is str and str(uuid.UUID(run_id)) == run_id)
    contract=Path(contract);run=contract.parent/'run'
    check(contract.name == 'contract' and contract.is_absolute())
    check(Path(worker_path) == run/'pgcat_collector_control.py' if worker_path is not None else True)
    sources=SOURCES
    if profile==NATIVE_MODE:
        from pgcat_source_bootstrap import NATIVE_SOURCES as sources,require_verified
        require_verified(__name__)
    check(type(expected) is dict and set(expected) == set(sources))
    fd=directory(contract);os.close(fd)
    fd=directory(run)
    verified={}
    try:
        for key,name in sources.items():
            check(type(expected[key]) is str and re.fullmatch(r'[0-9a-f]{64}',expected[key]) is not None)
            raw=read(fd,name)
            check(hashlib.sha256(raw).hexdigest() == expected[key]);verified[name[:-3]]=raw
        try:
            os.mkdir('collector-job',0o700,dir_fd=fd);os.fsync(fd)
        except FileExistsError:
            pass
    finally:
        os.close(fd)
    return run,directory(run/'collector-job'),verified


def kube_binding(contract,kubeconfig):
    # This is the private binding supplied by the fixed lane template.
    path=Path(kubeconfig)
    check(path.is_absolute() and '..' not in path.parts)
    fd=directory(path.parent)
    try:
        read(fd,path.name,65536)
    finally:
        os.close(fd)
    return str(path)


def process_identity(pid):
    """Linux exact process tuple, including the entire NUL-separated argv hash."""
    try:
        check(type(pid) is int and pid>0)
        base=Path('/proc')/str(pid)
        raw=(base/'stat').read_text();fields=raw[raw.rfind(')')+2:].split()
        if fields[0] == 'Z':
            return None
        boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        command=(base/'cmdline').read_bytes()
        check(command and re.fullmatch(r'[A-Za-z0-9_-]{1,256}',boot) is not None)
        return dict(pid=pid,bootId=boot,startTicks=int(fields[19]),
                    commandSha256=hashlib.sha256(command).hexdigest())
    except FileNotFoundError:
        return None


def valid_identity(value):
    check(type(value) is dict and set(value)=={'pid','bootId','startTicks','commandSha256'})
    check(type(value['pid']) is int and value['pid']>0 and
          type(value['startTicks']) is int and value['startTicks']>=0 and
          type(value['bootId']) is str and re.fullmatch(r'[A-Za-z0-9_-]{1,256}',value['bootId']) is not None and
          type(value['commandSha256']) is str and re.fullmatch(r'[0-9a-f]{64}',value['commandSha256']) is not None)


def capture_context(contract,run_id,expected,verified,job_fd):
    """Private disabled-capture provenance, under the verified import guard."""
    from segmented_capture import validate_context
    source=importlib.import_module('pgcat_rehearsal_contract')
    contract=Path(contract);run=contract.parent/'run'
    check(set(expected)==set(SOURCES) and set(verified)=={name[:-3] for name in SOURCES.values()})
    for key,name in SOURCES.items():check(hashlib.sha256(verified[name[:-3]]).hexdigest()==expected[key])
    identity=process_identity(os.getpid());valid_identity(identity)
    deadline=time.monotonic()+1;recorded=None
    for attempt in range(101):
        try:recorded=decode(read(job_fd,'identity.json'));break
        except FileNotFoundError:
            remaining=deadline-time.monotonic()
            if remaining<=0 or attempt==100:break
            time.sleep(min(.01,remaining))
    check(recorded==identity and time.monotonic()<=deadline and process_identity(os.getpid())==identity)
    fd=source._directory(contract)
    try:
        baseline=source._sealed_read(fd,'baseline.json')
        raw=source._read_at(fd,'baseline.json',65536)
        check(source._decode(raw)==baseline)
        seal=source._decode(source._read_at(fd,'baseline.json.sha256',256))
        check(seal==dict(sha256=hashlib.sha256(raw).hexdigest()))
    finally:os.close(fd)
    source_fd=directory(contract/'provider-source')
    try:context_raw=read(source_fd,'run-context.json',2048);record=decode(context_raw)
    finally:os.close(source_fd)
    provider=source.fresh_provider_source(contract,run_id,run/'provider-inventory.json',require_fresh=False)
    check(type(baseline['version']) is int and baseline['version']==1 and baseline['mode']=='disabled-rehearsal' and baseline['purpose']=='disabled-rehearsal-post-stop')
    check(baseline['paths']==dict(captureDir=str(run/'rehearsal-capture'),boundaryDir=str(run/'disabled-boundary'),kubeconfig=str(contract.parent/'kubeconfig')) and baseline['inventoryPath']==str(run/'provider-inventory.json'))
    check(record['runContext']['runId']==run_id and provider['providerRunContextSha256']==hashlib.sha256(context_raw).hexdigest()==baseline['providerRunContextSha256'])
    check(provider['targetSha256']==baseline['targetSha256']==record['runContext']['targetSha256'] and provider['feedSession']==baseline['receipt']['feedSession']==record['feedSession'])
    check(type(baseline['originals']) is list and len(baseline['originals'])==2 and len({row['id'] for row in baseline['originals']})==2)
    check(all(provider['privateAddresses'].get(row['id'])==row['address'] for row in baseline['originals']))
    return validate_context(dict(schemaVersion=1,runId=run_id,feedSession=record['feedSession'],
        providerRunContextSha256=hashlib.sha256(context_raw).hexdigest(),bundleSha256=record['runContext']['bundleSha256'],
        sourceHashes=dict(expected),baselineSha256=hashlib.sha256(raw).hexdigest(),originalRoster=baseline['roster'],
        deployment=baseline['deployment'],collectorIdentity=identity))


def profile_values(profile,cap=5400):
    check(type(profile) is str and profile in ('no-scale','disabled-rehearsal',NATIVE_MODE))
    if profile==NATIVE_MODE:
        check(type(cap) is int and 75<=cap<=5400)
        return 'native-capture',cap,cap+15,cap+30
    if profile=='no-scale':
        return 'collector-capture',65,80,90
    return 'rehearsal-capture',5400,5415,5430


def intent_profile(value,profile):
    profile_values(profile)
    keys={'runId','startedAt'}
    if profile!='no-scale':keys.add('profile')
    if profile==NATIVE_MODE:keys.update(('captureMaxSeconds','prepared'))
    check(type(value) is dict and set(value)==keys and type(value['startedAt']) is int)
    check(profile=='no-scale' or value['profile']==profile)
    if profile==NATIVE_MODE:profile_values(profile,value['captureMaxSeconds'])
    return value


def intent(fd,run_id,*,profile='no-scale'):
    value=intent_profile(decode(read(fd,'intent.json')),profile)
    check(value['runId']==run_id)
    return value


def native_prepared(run,started,expected_sha):
    """Descendants inherit the launch-bound digest, never adopt new anchors."""
    from pgcat_native_controller import verify_prepared
    from pgcat_native_ingress import _identity,hashed
    from segmented_capture import canonical,sha
    check(hashed(expected_sha) and sha(canonical(started['prepared']))==expected_sha)
    return verify_prepared(run,decode(_identity(run/'native-plan.json')[0]),started['prepared'])


def startup_slot(info):
    check(stat.S_ISREG(info.st_mode) and info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1)
    return dict(device=info.st_dev,inode=info.st_ino,uid=info.st_uid,mode=stat.S_IMODE(info.st_mode),nlink=info.st_nlink)


def receive_startup_ack(reader,guardian,probe,deadline):
    """Read one bounded message from our actual guardian's inherited pipe."""
    import select
    from pgcat_native_controller import owned_process
    actual=owned_process(guardian,probe);raw=b''
    while True:
        remaining=deadline-time.monotonic();check(remaining>0)
        readable,_,_=select.select([reader],[],[],remaining);check(readable)
        part=os.read(reader,4097-len(raw))
        if not part:break
        raw+=part;check(len(raw)<=4096)
    check(time.monotonic()<deadline and owned_process(guardian,probe)==actual)
    value=decode(raw);check(value['guardianIdentity']==actual)
    check(probe(value['collectorIdentity']['pid'])==value['collectorIdentity'])
    return value


def guardian_startup(run,child,identity,started,intent_file,startup,spawned,probe,*,diagnostic=None):
    """One proof, owned by the existing guardian and its retained collector."""
    from pgcat_native_controller import owned_process,seal_ready,FixedWorkerReads,validate_launch
    from pgcat_native_ingress import _identity,validate_plan,exact
    from segmented_capture import canonical,sha,read_progress
    exact(startup,{'ackFD','resultFD','keyFD'})
    check(all(type(v) is int and v>=3 for v in startup.values()) and len(set(startup.values()))==3)
    for name in ('ackFD','keyFD'):check(stat.S_ISFIFO(os.fstat(startup[name]).st_mode))
    key=os.read(startup['keyFD'],33);os.close(startup['keyFD']);startup.pop('keyFD');check(len(key)==32)
    fd=startup['resultFD'];slot=startup_slot(os.fstat(fd));check(os.fstat(fd).st_size==0)
    plan=validate_plan(decode(_identity(run/'native-plan.json')[0]))
    guardian=probe(os.getpid());valid_identity(guardian)
    check(guardian['pid']==os.getpid() and owned_process(child,probe)==identity)
    deadline=spawned+4.
    binding=dict(runId=plan['runId'],planSha256=sha(canonical(plan)),collectorIntentFile=intent_file,
        guardianIdentity=guardian,collectorIdentity=identity,resultPath=str(run/'collector-job'/'startup-result.json'),resultSlot=slot,deadlineMonotonic=deadline)
    validate_launch(dict(status='LAUNCHED',**binding),plan,keyed=False)
    check(startup_slot(os.stat(binding['resultPath'],follow_symlinks=False))==slot)
    check(_identity(run/'collector-job'/'intent.json')[1]==intent_file and time.monotonic()<deadline)
    message=canonical(binding);check(len(message)<=4096 and os.write(startup['ackFD'],message)==len(message))
    os.close(startup['ackFD']);startup.pop('ackFD')
    # Poll only for the initial context and two committed frames. Once proof
    # begins it runs once; refusal never refreshes or retries its deadline.
    if diagnostic is not None:diagnostic['phase']='waiting-frames'
    while True:
        check(time.monotonic()<deadline and owned_process(child,probe)==identity)
        try:
            context=decode(_identity(run/'native-context.json')[0])
            progress,_,_=read_progress(run/'native-capture',context)
            if progress['committedFrames']>=2:break
        except FileNotFoundError:pass
        time.sleep(min(.01,max(0,deadline-time.monotonic())))
    if diagnostic is not None:diagnostic['phase']='seal-ready'
    reads=FixedWorkerReads(plan,run.parent/'kubeconfig',credential=started['prepared']['workerCredential'],prepared=started['prepared']);reads.deadline=deadline
    ready=seal_ready(run,prepared=started['prepared'],guardian_identity=guardian,probe_process=probe,live_clients=reads.live_clients,now=time.time,deadline=deadline)
    check(owned_process(child,probe)==identity and _identity(run/'collector-job'/'intent.json')[1]==intent_file and time.monotonic()<deadline)
    if diagnostic is not None:diagnostic['phase']='publish-result'
    publish_startup_result(run,fd,key,binding,ready)


def publish_startup_result(run,fd,key,binding,ready):
    import fcntl
    fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
    # Failure retains this lock until the owning guardian exits. In particular,
    # failed truncation must never expose a signed late result to status.
    result=_publish_startup_result(run,fd,key,binding,ready)
    fcntl.flock(fd,fcntl.LOCK_UN)
    return result


def _publish_startup_result(run,fd,key,binding,ready):
    """Only the original guardian holds this inherited slot and signing key."""
    import hmac
    from pgcat_native_ingress import _identity
    from segmented_capture import canonical
    check(type(key) is bytes and len(key)==32 and binding['resultPath']==str(Path(run)/'collector-job'/'startup-result.json'))
    check(startup_slot(os.fstat(fd))==binding['resultSlot'] and os.fstat(fd).st_size==0)
    check(startup_slot(os.stat(binding['resultPath'],follow_symlinks=False))==binding['resultSlot'])
    ready_raw,ready_file=_identity(Path(run)/'native-ready.json');check(ready_raw==canonical(ready))
    value=dict(binding=binding,ready=ready,readyFile=ready_file)
    # The large body remains incomplete JSON until it is durable and the
    # original deadline is checked. Only then can the small authenticated seal
    # make this one inherited slot a complete result. No retry or second file.
    prefix=canonical(value)[:-1]+b',"seal":'
    check(len(prefix)<=65000 and time.monotonic()<binding['deadlineMonotonic'])
    try:
        remaining=prefix
        while remaining:
            check(time.monotonic()<binding['deadlineMonotonic'])
            count=os.write(fd,remaining);check(count>0);remaining=remaining[count:]
        os.fsync(fd)
        completed=time.monotonic();check(completed<binding['deadlineMonotonic'])
        signed=dict(value,completedMonotonic=completed)
        seal=dict(completedMonotonic=completed,authenticator=hmac.new(key,canonical(signed),hashlib.sha256).hexdigest())
        suffix=canonical(seal)+b'}';result=dict(value,seal=seal)
        check(time.monotonic()<binding['deadlineMonotonic'] and os.write(fd,suffix)==len(suffix))
        os.fsync(fd)
        check(time.monotonic()<binding['deadlineMonotonic'])
        raw,identity=_identity(Path(binding['resultPath']),65536)
        check(raw==canonical(result) and {k:identity[k] for k in binding['resultSlot']}==binding['resultSlot']==startup_slot(os.fstat(fd)))
        check(time.monotonic()<binding['deadlineMonotonic'])
    except BaseException:
        os.ftruncate(fd,0);os.fsync(fd);raise
    return result


def read_startup_result(run,launch,prepared,probe):
    import fcntl
    path=Path(run)/'collector-job'/'startup-result.json';check(launch['resultPath']==str(path))
    fd=os.open(str(path),os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        check(startup_slot(os.fstat(fd))==launch['resultSlot'])
        fcntl.flock(fd,fcntl.LOCK_SH|fcntl.LOCK_NB)
        return _read_startup_result(run,launch,prepared,probe)
    finally:os.close(fd)


def _read_startup_result(run,launch,prepared,probe):
    from pgcat_native_controller import validate_launch,validate_startup_result,verify_prepared,verify_source_inventory
    from pgcat_native_ingress import _identity,_Chain
    from segmented_capture import canonical
    run=Path(run);plan=decode(_identity(run/'native-plan.json')[0]);validate_launch(launch,plan,keyed=False)
    verify_prepared(run,plan,prepared);sources=verify_source_inventory(run,plan['sourceHashes'])
    check(launch['resultPath']==str(run/'collector-job'/'startup-result.json'))
    intent_raw,intent_file=_identity(run/'collector-job'/'intent.json');check(intent_file==launch['collectorIntentFile'])
    started=intent_profile(decode(intent_raw),NATIVE_MODE);check(started['prepared']==prepared and started['runId']==plan['runId'])
    result_raw,result_file=_identity(Path(launch['resultPath']),65536)
    check({k:result_file[k] for k in launch['resultSlot']}==launch['resultSlot'])
    result=validate_startup_result(decode(result_raw),plan)
    check(result['binding']=={k:v for k,v in launch.items() if k!='status'})
    for name in ('guardianIdentity','collectorIdentity'):check(probe(launch[name]['pid'])==launch[name])
    chain=_Chain(run,retain=True)
    try:
        check(chain.ready==result['ready'] and chain.files['ready']==(canonical(result['ready']),result['readyFile']))
        chain.unchanged()
        verify_prepared(run,plan,prepared);check(verify_source_inventory(run,plan['sourceHashes'])==sources)
        check(_identity(run/'collector-job'/'intent.json')==(intent_raw,intent_file) and _identity(Path(launch['resultPath']),65536)==(result_raw,result_file))
        chain.unchanged()
        for name in ('guardianIdentity','collectorIdentity'):check(probe(launch[name]['pid'])==launch[name])
    finally:chain.close()
    return dict(status='READY',ready=result['ready'],readyFile=result['readyFile'],result=result,resultFile=result_file)


def launch(contract,run_id,kubeconfig,expected,worker_path=None,spawn=None,*,profile='no-scale',prepared=None,deadline=None):
    if profile==NATIVE_MODE:
        from pgcat_lane_protocol import _release_admission
        _release_admission(profile,'native-collector-launch')
    profile_values(profile)
    stop=min(deadline,time.monotonic()+4.) if deadline is not None else time.monotonic()+4.
    startup_fds=[]
    run,fd,_=context(contract,run_id,expected,worker_path,profile=profile)
    try:
        kubeconfig=kube_binding(contract,kubeconfig)
        if profile in ('disabled-rehearsal',NATIVE_MODE):
            boundary=directory(run/('native-boundary' if profile==NATIVE_MODE else 'disabled-boundary'))
            try:check(not os.listdir(boundary))
            finally:os.close(boundary)
        check(not os.listdir(fd))
        started=dict(runId=run_id,startedAt=int(time.time()))
        if profile!='no-scale':started['profile']=profile
        if profile==NATIVE_MODE:
            from pgcat_native_controller import capture_budget,verify_prepared
            from pgcat_native_ingress import _identity
            plan=decode(_identity(run/'native-plan.json')[0]);_release_admission(profile,'native-collector-launch',plan=plan,prepared=prepared);started['prepared']=verify_prepared(run,plan,prepared)
            check(prepared['workerCredential']['path']==kubeconfig)
            started['captureMaxSeconds']=capture_budget(plan,time.time())
        write(fd,'intent.json',started)
        args=entry_command(run/'pgcat_collector_control.py',expected['collectorControl'],
            ['_native-run' if profile==NATIVE_MODE else '_run' if profile=='no-scale' else '_rehearsal-run',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':'))])
        if profile==NATIVE_MODE:
            from segmented_capture import canonical,sha
            args=native_entry_command(run,expected,['_native-run',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':')),sha(canonical(prepared))])
        env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'}
        options={}
        if profile==NATIVE_MODE:
            from pgcat_native_ingress import _identity
            intent_file=_identity(run/'collector-job'/'intent.json')[1]
            result_fd=os.open('startup-result.json',os.O_RDWR|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=fd);startup_fds.append(result_fd)
            slot=startup_slot(os.fstat(result_fd))
            ack_reader,ack_writer=os.pipe();startup_fds.extend((ack_reader,ack_writer))
            key_reader,key_writer=os.pipe();startup_fds.extend((key_reader,key_writer))
            key=os.urandom(32);check(os.write(key_writer,key)==32);os.close(key_writer);startup_fds.remove(key_writer)
            payload=dict(ackFD=ack_writer,resultFD=result_fd,keyFD=key_reader)
            args.append(json.dumps(payload,sort_keys=True,separators=(',',':')))
            options['pass_fds']=(result_fd,ack_writer,key_reader)
        try:
            guardian=(spawn or subprocess.Popen)(args,cwd=str(run),env=env,stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True,close_fds=True,**options)
        except BaseException:
            raise ValueError('collector') from None
        if profile==NATIVE_MODE:
            from pgcat_native_controller import validate_launch
            for item in (ack_writer,key_reader):os.close(item);startup_fds.remove(item)
            binding=receive_startup_ack(ack_reader,guardian,process_identity,stop)
            result=dict(status='LAUNCHED',**binding,resultKey=key.hex());validate_launch(result,plan)
            check(binding['resultSlot']==slot and binding['resultPath']==str(run/'collector-job'/'startup-result.json') and binding['collectorIntentFile']==intent_file)
            check(_identity(run/'collector-job'/'intent.json')[1]==intent_file)
            verify_prepared(run,plan,prepared)
            write(fd,'guardian.json',binding['guardianIdentity'])
            return result
        return {'status':'LAUNCHED'}
    finally:
        for item in startup_fds:os.close(item)
        os.close(fd)


def capture_arguments(run,*,profile='no-scale'):
    cap=5400
    if profile==NATIVE_MODE:
        job=directory(run/'collector-job')
        try:cap=intent_profile(decode(read(job,'intent.json')),profile)['captureMaxSeconds']
        finally:os.close(job)
    output,seconds,_,_=profile_values(profile,cap)
    arguments=['--output',str(run/output),'--inventory',
               str(run/'provider-inventory.json'),'--seconds',str(seconds)]
    if profile in ('disabled-rehearsal',NATIVE_MODE):arguments+=['--boundary-dir',str(run/('native-boundary' if profile==NATIVE_MODE else 'disabled-boundary'))]
    return arguments


def completion(run,*,profile='no-scale'):
    if profile==NATIVE_MODE:return native_completion(run)
    output,seconds,_,_=profile_values(profile)
    job=directory(run/'collector-job')
    try:intent_profile(decode(read(job,'intent.json')),profile)
    finally:os.close(job)
    fd=directory(run/output)
    try:
        raw=read(fd,'completion.json',2*1024*1024);value=decode(raw)
        check(type(value) is dict and value.get('version')==1 and value.get('completed') is True and
              value.get('cancelled') is False and type(value.get('maxTotalSeconds')) is int and
              value['maxTotalSeconds']==seconds and type(value.get('segments')) is list and
              1<=len(value['segments'])<=seconds+6)
        if profile=='disabled-rehearsal':
            check(type(value.get('maxSegmentSeconds')) is int and value['maxSegmentSeconds']==1800 and
                  type(value.get('missingPollCount')) is int and value['missingPollCount']==0 and
                  value.get('errors')==[])
            finals=value.get('clientFinalMarkers')
            check(type(finals) is list and finals)
            identities=set()
            for row in finals:
                check(type(row) is dict and set(row)=={'uid','processId','final'} and row['final'] is True and
                      type(row['uid']) is str and row['uid'] and type(row['processId']) is str and row['processId'])
                identity=(row['uid'],row['processId']);check(identity not in identities);identities.add(identity)
        return hashlib.sha256(raw).hexdigest()
    finally:
        os.close(fd)


def run_child(contract,run_id,kubeconfig,expected,worker_path=None,spawn=None,probe=None,*,profile='no-scale',prepared_sha=None,startup=None):
    if profile==NATIVE_MODE:
        from pgcat_lane_protocol import _release_admission
        _release_admission(profile,'native-collector-launch')
    fd=None;child=None;published=False;diagnostic=None
    try:
        _,_,wait_seconds,_=profile_values(profile)
        run,fd,_=context(contract,run_id,expected,worker_path,profile=profile)
        kubeconfig=kube_binding(contract,kubeconfig);started=intent(fd,run_id,profile=profile)
        if profile==NATIVE_MODE:
            native_prepared(run,started,prepared_sha)
            from pgcat_native_ingress import _identity
            _release_admission(profile,'native-collector-launch',plan=decode(_identity(run/'native-plan.json')[0]),prepared=started['prepared'])
            _,_,wait_seconds,_=profile_values(profile,started['captureMaxSeconds'])
        write(fd,'claim.json',dict(runId=run_id))
        args=entry_command(run/'pgcat_collector_control.py',expected['collectorControl'],
            ['_native-capture' if profile==NATIVE_MODE else '_capture' if profile=='no-scale' else '_rehearsal-capture',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':'))]
            +capture_arguments(run,profile=profile))
        if profile==NATIVE_MODE:args=native_entry_command(run,expected,['_native-capture',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':')),prepared_sha]+capture_arguments(run,profile=profile))
        spawned=time.monotonic();capture_end=spawned+wait_seconds
        child=(spawn or subprocess.Popen)(args,cwd=str(run),
            env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8','KUBECONFIG':kubeconfig},
            stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,close_fds=True)
        if profile==NATIVE_MODE:
            from pgcat_native_controller import owned_process
            identity=owned_process(child,probe or process_identity)
            native_prepared(run,started,prepared_sha)
        else:identity=(probe or process_identity)(child.pid);valid_identity(identity)
        write(fd,'identity.json',identity)
        if profile==NATIVE_MODE:
            diagnostic=dict(phase='initialize')
            guardian_startup(run,child,identity,started,_identity(run/'collector-job'/'intent.json')[1],startup,spawned,probe or process_identity,diagnostic=diagnostic)
            published=True
        category='incomplete';digest=None
        try:
            remaining=capture_end-time.monotonic() if profile==NATIVE_MODE else wait_seconds
            check(remaining>0);code=child.wait(timeout=remaining)
            if type(code) is int and code==0:
                digest=completion(run,profile=profile);category='completed'
        except subprocess.TimeoutExpired:
            child.kill();code=child.wait(timeout=5);category='timeout'
        write(fd,'exit.json',dict(identity=identity,exitCode=code,category=category,
                                  completionSha256=digest))
        return 0 if category=='completed' else 1
    except BaseException as error:
        record=None
        # Capture the actual failure instant and exit before cleanup changes it.
        # The dict carries only fixed boundary identifiers, never inner locals.
        if diagnostic is not None and not published:
            try:
                check(type(run_id) is str and str(uuid.UUID(run_id))==run_id)
                failed=time.monotonic();code=child.poll()
                record=dict(schemaVersion=1,runId=run_id,action='native-collector-launch',phase=diagnostic['phase'],
                    location='collectorControl.guardian_startup',category=_failure_category(error),sourceLocations=_failure_locations(error),
                    childReturnCode=code if type(code) is int else None,deadlineMonotonic=spawned+4.,
                    failedMonotonic=failed,elapsedSeconds=failed-spawned,deadlineExceeded=failed>=spawned+4.)
            except BaseException:pass
        # The owning guardian never leaves an unknown child running on failure.
        if child is not None:
            try:
                child.kill();child.wait(timeout=5)
            except BaseException:
                pass
        if record is not None:
            try:_private_startup_failure(fd,'native-guardian-failure.json',record)
            except BaseException:pass
        return 1
    finally:
        if profile==NATIVE_MODE and type(startup) is dict:
            for name,item in startup.items():
                # A failed publication keeps its exclusive lock until actual
                # guardian process exit, even if invalidation/child stop fails.
                if name=='resultFD' and not published:continue
                try:os.close(item)
                except OSError:pass
        if fd is not None:
            os.close(fd)


def status(contract,run_id,expected,probe=None,now=None,*,profile='no-scale',prepared=None,launch=None):
    if profile==NATIVE_MODE:
        from pgcat_lane_protocol import _release_admission
        _release_admission(profile,'native-collector-status')
    fd=None
    try:
        _,_,_,status_seconds=profile_values(profile)
        run,fd,_=context(contract,run_id,expected,profile=profile)
        started=intent(fd,run_id,profile=profile)
        if profile==NATIVE_MODE:
            check(started['prepared']==prepared and launch['runId']==run_id)
            _,_,_,status_seconds=profile_values(profile,started['captureMaxSeconds'])
            check(0<=(now or time.time)()-started['startedAt']<=status_seconds)
            return read_startup_result(run,launch,prepared,probe or process_identity)
        check(0<=(now or time.time)()-started['startedAt']<=status_seconds)
        check(decode(read(fd,'claim.json')) == dict(runId=run_id))
        identity=decode(read(fd,'identity.json'));valid_identity(identity)
        observed=(probe or process_identity)(identity['pid'])
        if observed is not None:
            check(observed==identity)
            if 'observed.json' not in os.listdir(fd):
                write(fd,'observed.json',identity)
            check(decode(read(fd,'observed.json'))==identity)
            if profile==NATIVE_MODE:
                native_prepared(run,started,sha(canonical(prepared)))
                return dict(status='ALIVE',identity=identity,guardianIdentity=guardian)
            return dict(status='ALIVE',identity=identity)
        check(decode(read(fd,'observed.json'))==identity)
        sealed=decode(read(fd,'exit.json'))
        digest=completion(run,profile=profile)
        check(sealed==dict(identity=identity,exitCode=0,category='completed',completionSha256=digest)
              and type(sealed['exitCode']) is int)
        return dict(status='EXITED',identity=identity,
                    exit=dict(category='completed',exitCode=0,completionSha256=digest))
    except (OSError,ValueError,TypeError,KeyError,IndexError,OverflowError,UnicodeError,AttributeError):
        return {'status':'INCONCLUSIVE'}
    finally:
        if fd is not None:
            os.close(fd)


def close_verdict(contract, run_id, kubeconfig, result):
    """Delete only the bound temporary credential after the sealed assessment."""
    run=Path(contract).parent/'run'
    path=Path(kubeconfig)
    check(path == Path(contract).parent/'kubeconfig')
    job=directory(run/'collector-job');parent=None
    try:
        raw=read(job,'lane-verdict.json');verdict=decode(raw)
        check(hashlib.sha256(raw).hexdigest()==result['verdictSha256'] and
              verdict.get('runId',run_id)==run_id and verdict['status']==result['status'])
        digest=verdict.get('clientIdentitySha256')
        cleanup_sha=None
        try:
            # Claim before deletion; even a failed fsync consumes this attempt.
            write(job,'credential-cleanup-claim.json',dict(runId=run_id,verdictSha256=result['verdictSha256']))
            parent=directory(path.parent)
            before=os.stat(path.name,dir_fd=parent,follow_symlinks=False)
            check(stat.S_ISREG(before.st_mode) and before.st_uid==os.geteuid() and
                  stat.S_IMODE(before.st_mode)==0o600 and before.st_nlink==1)
            os.unlink(path.name,dir_fd=parent);os.fsync(parent)
            try:os.stat(path.name,dir_fd=parent,follow_symlinks=False)
            except FileNotFoundError:pass
            else:raise ValueError('collector')
            cleanup=dict(runId=run_id,verdictSha256=result['verdictSha256'],workerKubeconfigAbsent=True)
            write(job,'credential-cleanup.json',cleanup)
            cleanup_sha=hashlib.sha256(read(job,'credential-cleanup.json')).hexdigest()
        except FileExistsError:
            raise  # Never replay a previously claimed cleanup.
        except (OSError,ValueError):
            # A sealed adverse assessment survives uncertain cleanup. PASS may
            # never be returned without both independent closure hashes.
            if result['status']=='PASS':raise
        if not (verdict.get('runId')==run_id and type(digest) is str and
                re.fullmatch(r'[0-9a-f]{64}',digest) is not None):digest=None
        check(result['status']!='PASS' or (digest is not None and cleanup_sha is not None))
        return dict(result,clientIdentitySha256=digest,workerCredentialCleanupSha256=cleanup_sha)
    finally:
        if parent is not None:os.close(parent)
        os.close(job)


def main():
    os.umask(0o077)
    try:
        args=sys.argv[1:]
        if args and args[0] in ('native-launch','native-status','_native-run','_native-capture'):
            from pgcat_lane_protocol import _release_admission
            _release_admission(NATIVE_MODE)
        check(len(args)>=5)
        verb,contract,run_id,kubeconfig,hashes=args[:5];expected=decode(hashes.encode('ascii'))
        if verb in ('native-launch','native-status','_native-run','_native-capture'):
            from pgcat_source_bootstrap import require_verified
            require_verified(__name__)
            if verb=='_native-run':
                check(len(args)==7)
                return run_child(contract,run_id,kubeconfig,expected,worker_path=Path(__file__),profile=NATIVE_MODE,prepared_sha=args[5],startup=decode(args[6].encode('ascii')))
            if verb=='_native-capture':
                from pgcat_native_controller import seal_capture_context,FixedWorkerReads
                from pgcat_native_ingress import _identity
                run,job,verified=context(contract,run_id,expected,Path(__file__),profile=NATIVE_MODE)
                try:
                    check(len(args)>6);prepared=native_prepared(run,intent(job,run_id,profile=NATIVE_MODE),args[5])
                    until=time.monotonic()+1.
                    while 'identity.json' not in os.listdir(job):
                        check(time.monotonic()<until);time.sleep(.01)
                finally:os.close(job)
                check(args[6:]==capture_arguments(run,profile=NATIVE_MODE))
                plan=decode(_identity(run/'native-plan.json')[0]);_release_admission(NATIVE_MODE,'native-collector-launch',plan=plan,prepared=prepared);reads=FixedWorkerReads(plan,kubeconfig,credential=prepared['workerCredential'])
                progress=seal_capture_context(run,expected,prepared=prepared,probe_process=process_identity,live_clients=reads.live_clients,now=time.time)
                import worker_capture
                sys.argv=['worker_capture.py']+args[6:];worker_capture.main(progress_context=progress);return 0
            check(len(args)==5)
            result=(launch(contract,run_id,kubeconfig,expected,worker_path=Path(__file__),profile=NATIVE_MODE) if verb=='native-launch' else status(contract,run_id,expected,profile=NATIVE_MODE))
            print(json.dumps(result,sort_keys=True,separators=(',',':')));return 0 if result['status'] in ('LAUNCHED','ALIVE') else 1
        profiles={'launch':'no-scale','status':'no-scale','_run':'no-scale','_capture':'no-scale',
                  'verdict':'no-scale','rehearsal-launch':'disabled-rehearsal',
                  'rehearsal-status':'disabled-rehearsal','_rehearsal-run':'disabled-rehearsal',
                  '_rehearsal-capture':'disabled-rehearsal'}
        check(verb in profiles);profile=profiles[verb]
        capture=verb in ('_capture','_rehearsal-capture')
        if capture or verb=='verdict':
            run,fd,verified=context(contract,run_id,expected,Path(__file__))
            try:
                if capture:intent(fd,run_id,profile=profile)
            finally:os.close(fd)
            kube_binding(contract,kubeconfig)
            check(args[5:]==capture_arguments(run,profile=profile) if capture else len(args)==7)
            # Bootstrap the import guard itself from verified source, then ensure
            # every transitive local import executes the same reviewed bytes.
            namespace={'__name__':'_verified_worker','__file__':str(run/'pgcat_rehearsal_worker.py')}
            exec(compile(verified['pgcat_rehearsal_worker'],namespace['__file__'],'exec',dont_inherit=True),namespace)
            check(not any(name in sys.modules for name in verified))
            finder=namespace['_VerifiedSourceFinder'](run,verified);sys.meta_path.insert(0,finder)
            try:
                if capture:
                    module=importlib.import_module('worker_capture');sys.argv=['worker_capture.py']+args[5:]
                    if profile=='disabled-rehearsal':
                        job=directory(run/'collector-job')
                        try:progress_context=capture_context(contract,run_id,expected,verified,job)
                        finally:os.close(job)
                        module.main(progress_context=progress_context)
                    else:module.main()
                else:
                    module=importlib.import_module('pgcat_lane_timing_assessor')
                    result=module.seal_verdict(run,run_id,decode(args[5].encode('ascii')),
                        decode(args[6].encode('ascii')),expected)
                    result=close_verdict(contract,run_id,kubeconfig,result)
                    print(json.dumps(result,sort_keys=True,separators=(',',':')))
            finally:
                sys.meta_path.remove(finder)
            return 0
        check(len(args)==5)
        if verb in ('_run','_rehearsal-run'):
            return run_child(contract,run_id,kubeconfig,expected,worker_path=Path(__file__),profile=profile)
        result=(launch(contract,run_id,kubeconfig,expected,worker_path=Path(__file__),profile=profile)
                if verb in ('launch','rehearsal-launch') else status(contract,run_id,expected,profile=profile))
        print(json.dumps(result,sort_keys=True,separators=(',',':')))
        return 0 if result['status'] in ('LAUNCHED','ALIVE','EXITED') else 1
    except BaseException:
        print('{"status":"INCONCLUSIVE"}')
        return 1


if __name__=='__main__':
    sys.exit(main())


def native_boundary_completion(run,context,operation,terminal,inventory,progress,*,started,deadline):
    """Authenticate B's actual v2 boundary and all four committed final refs."""
    from pgcat_native_ingress import exact,require,number,_identity
    from pgcat_native_evidence import _replay,read_terminal_clients
    from segmented_capture import canonical,sha,decode_private,read_progress,read_frame,open_directory,read_private
    from capture_window import seconds
    boundary=Path(run)/'native-boundary';fd=open_directory(boundary);capture=open_directory(Path(run)/'native-capture')
    try:
        while True:
            require(time.monotonic()<deadline)
            try:completed_raw=read_private(fd,'boundary-completed.json');break
            except FileNotFoundError:time.sleep(min(.01,max(0,deadline-time.monotonic())))
        raws={name:read_private(fd,name+'.json') for name in ('request','ack','receipt','boundary-start')}
        request,ack,receipt,start=[decode_private(raws[n]) for n in ('request','ack','receipt','boundary-start')]
        completed=decode_private(completed_raw)
        exact(request,{'requestId','roster'});require(request['roster']==terminal['roster'] and ack==request)
        require(type(request['requestId']) is str and re.fullmatch('[0-9a-f]{32}',request['requestId']))
        require(receipt==dict(requestId=request['requestId'],status='ACKNOWLEDGED',clients={r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in terminal['roster']}))
        exact(start,{'schemaVersion','bindingSha256','requestSha256','requestId','terminalSha256','executionKind','startedMonotonic','deadlineMonotonic','firstSettlingFrame'})
        require(type(start['schemaVersion']) is int and start['schemaVersion']==2 and start['bindingSha256']==progress['bindingSha256'] and start['requestSha256']==sha(raws['request']) and start['requestId']==request['requestId'])
        require(start['terminalSha256']==terminal['terminalSha256'] and start['executionKind']==context['executionKind'])
        require(all(number(start[k]) for k in ('startedMonotonic','deadlineMonotonic')) and started<=start['startedMonotonic'] and start['deadlineMonotonic']==start['startedMonotonic']+5)
        exact(completed,{'schemaVersion','bindingSha256','requestSha256','requestId','startSha256','receiptSha256','finalDurableMonotonic','finals','lastFrame','terminalSha256','executionKind','predecessorFinals'})
        require(type(completed['schemaVersion']) is int and completed['schemaVersion']==2 and completed['executionKind']==context['executionKind'] and completed['terminalSha256']==terminal['terminalSha256'])
        require(completed['bindingSha256']==progress['bindingSha256'] and completed['requestSha256']==sha(raws['request']) and completed['requestId']==request['requestId'] and completed['startSha256']==sha(raws['boundary-start']) and completed['receiptSha256']==sha(raws['receipt']))
        durable=completed['finalDurableMonotonic'];require(number(durable) and start['startedMonotonic']<=durable<=start['deadlineMonotonic'] and durable<=time.monotonic()<deadline)
        first=read_frame(capture,start['firstSettlingFrame']);require(first['phase']=='settling' and seconds(first['at'])>=seconds(progress['latest']['at']))
        sealed=decode_private(read_private(capture,'terminal-clients.json'))
        require(completed['predecessorFinals']==sealed['predecessorFinals'])
        finals=completed['finals'];require(type(finals) is list and len(finals)==2)
        require(sorted((r['uid'],r['processId']) for r in finals)==sorted((r['uid'],r['processId']) for r in terminal['roster']))
        all_finals=completed['predecessorFinals']+finals
        require(len(all_finals)==4 and len({(r['uid'],r['processId']) for r in all_finals})==4)
        final_samples={}
        for row in all_finals:
            exact(row,{'uid','processId','frame'});frame=read_frame(capture,row['frame'])
            pods=[p for p in frame['pods'] if p['uid']==row['uid'] and p['snapshot']['sample']['processId']==row['processId']];require(len(pods)==1)
            snap=pods[0]['snapshot'];sample=snap['sample']
            require(snap['final'] is True and type(snap['connected']) is int and snap['connected']==0 and sample['Scheduled']==sample['Attempts']+sample['Skipped'] and sample['Skipped']==0)
            if row in finals:
                require(frame['phase']=='settling' and frame['inventory']==inventory and seconds(frame['at'])>=seconds(first['at']))
                require(dict(name=pods[0]['name'],uid=pods[0]['uid'],containerId=pods[0]['containerId'],processId=sample['processId']) in terminal['roster'])
                final_samples[row['uid']]=sample
        after,_,tail=read_progress(Path(run)/'native-capture',context)
        require(not any(after['flags'].values()) and after['latest']==completed['lastFrame'] and tail['phase']=='settling' and tail['inventory']==inventory)
        require(type(tail['expectedReplicas']) is int and tail['expectedReplicas']==2 and len(tail['pods'])==2 and {p['uid'] for p in tail['pods']}==set(final_samples))
        for pod in tail['pods']:
            require(pod['snapshot']['final'] is True and type(pod['snapshot']['connected']) is int and pod['snapshot']['connected']==0 and pod['snapshot']['sample']==final_samples[pod['uid']])
        snapshot,count,_=_replay(capture,context,after['latest']);require(snapshot==after['nativeCohort'] and count==after['committedFrames'] and not snapshot['startingPods'])
        require(len(snapshot['cohort'])==4 and all(r['finalFrame'] is not None for r in snapshot['cohort']))
        require({(r['identity']['uid'],r['identity']['processId'],sha(canonical(r['finalFrame']))) for r in snapshot['cohort']}=={(r['uid'],r['processId'],sha(canonical(r['frame']))) for r in all_finals})
        read_frame(capture,completed['lastFrame'],tail=True)
        require(read_terminal_clients(Path(run)/'native-capture',context,operation)==terminal)
        for name,raw in raws.items():require(read_private(fd,name+'.json')==raw)
        require(read_private(fd,'boundary-completed.json')==completed_raw)
        return dict(boundaryRequestId=request['requestId'],receiptSha256=sha(raws['receipt']),boundaryCompletedSha256=sha(completed_raw))
    finally:os.close(capture);os.close(fd)


def native_completion(run):
    from pgcat_native_ingress import _identity,require
    from segmented_capture import decode_private,sha,read_progress
    from pgcat_native_evidence import validate_context,_replay
    run=Path(run);raw,_=_identity(run/'native-capture'/'completion.json',2*1024*1024);m=decode_private(raw)
    require(type(m['version']) is int and m['version']==1 and m['completed'] is True and m['cancelled'] is False and type(m['missingPollCount']) is int and m['missingPollCount']==0 and m['errors']==[])
    job=directory(run/'collector-job')
    try:started=decode(read(job,'intent.json'));intent_profile(started,NATIVE_MODE)
    finally:os.close(job)
    require(m['maxTotalSeconds']==started['captureMaxSeconds'] and type(m['pollSeconds']) is int and m['pollSeconds']==1 and m['maxSegmentSeconds']==1800)
    context=validate_context(decode_private(_identity(run/'native-context.json')[0]));progress,_,_=read_progress(run/'native-capture',context)
    fd=directory(run/'native-capture')
    try:snapshot,count,_=_replay(fd,context,progress['latest'])
    finally:os.close(fd)
    require(snapshot==progress['nativeCohort'] and count==progress['committedFrames'] and not any(snapshot['flags'].values()) and not snapshot['startingPods'])
    require(len(snapshot['cohort'])==4 and all(r['finalFrame'] is not None for r in snapshot['cohort']))
    return sha(raw)
