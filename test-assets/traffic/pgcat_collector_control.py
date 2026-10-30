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


def launch(contract,run_id,kubeconfig,expected,worker_path=None,spawn=None,*,profile='no-scale',prepared=None):
    if profile==NATIVE_MODE:
        from pgcat_lane_protocol import _release_admission
        _release_admission(profile,'native-collector-launch')
    profile_values(profile)
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
            plan=decode(_identity(run/'native-plan.json')[0]);started['prepared']=verify_prepared(run,plan,prepared)
            check(prepared['workerCredential']['path']==kubeconfig)
            started['captureMaxSeconds']=capture_budget(plan,time.time())
        write(fd,'intent.json',started)
        args=entry_command(run/'pgcat_collector_control.py',expected['collectorControl'],
            ['_native-run' if profile==NATIVE_MODE else '_run' if profile=='no-scale' else '_rehearsal-run',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':'))])
        if profile==NATIVE_MODE:
            from segmented_capture import canonical,sha
            args=native_entry_command(run,expected,['_native-run',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':')),sha(canonical(prepared))])
        env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'}
        try:
            guardian=(spawn or subprocess.Popen)(args,cwd=str(run),env=env,stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True,close_fds=True)
        except BaseException:
            raise ValueError('collector') from None
        if profile==NATIVE_MODE:
            from pgcat_native_controller import owned_process
            identity=owned_process(guardian,process_identity)
            verify_prepared(run,plan,prepared)
            write(fd,'guardian.json',identity)
            return dict(status='LAUNCHED',guardianIdentity=identity)
        return {'status':'LAUNCHED'}
    finally:
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
    if profile==NATIVE_MODE:raise ValueError('native closure not implemented')
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


def run_child(contract,run_id,kubeconfig,expected,worker_path=None,spawn=None,probe=None,*,profile='no-scale',prepared_sha=None):
    if profile==NATIVE_MODE:
        from pgcat_lane_protocol import _release_admission
        _release_admission(profile,'native-collector-launch')
    fd=None;child=None
    try:
        _,_,wait_seconds,_=profile_values(profile)
        run,fd,_=context(contract,run_id,expected,worker_path,profile=profile)
        kubeconfig=kube_binding(contract,kubeconfig);started=intent(fd,run_id,profile=profile)
        if profile==NATIVE_MODE:
            native_prepared(run,started,prepared_sha)
            _,_,wait_seconds,_=profile_values(profile,started['captureMaxSeconds'])
        write(fd,'claim.json',dict(runId=run_id))
        args=entry_command(run/'pgcat_collector_control.py',expected['collectorControl'],
            ['_native-capture' if profile==NATIVE_MODE else '_capture' if profile=='no-scale' else '_rehearsal-capture',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':'))]
            +capture_arguments(run,profile=profile))
        if profile==NATIVE_MODE:args=native_entry_command(run,expected,['_native-capture',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':')),prepared_sha]+capture_arguments(run,profile=profile))
        child=(spawn or subprocess.Popen)(args,cwd=str(run),
            env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8','KUBECONFIG':kubeconfig},
            stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,close_fds=True)
        if profile==NATIVE_MODE:
            from pgcat_native_controller import owned_process
            identity=owned_process(child,probe or process_identity)
            native_prepared(run,started,prepared_sha)
        else:identity=(probe or process_identity)(child.pid);valid_identity(identity)
        write(fd,'identity.json',identity)
        category='incomplete';digest=None
        try:
            code=child.wait(timeout=wait_seconds)
            if type(code) is int and code==0:
                digest=completion(run,profile=profile);category='completed'
        except subprocess.TimeoutExpired:
            child.kill();code=child.wait(timeout=5);category='timeout'
        write(fd,'exit.json',dict(identity=identity,exitCode=code,category=category,
                                  completionSha256=digest))
        return 0 if category=='completed' else 1
    except BaseException:
        # The owning guardian never leaves an unknown child running on failure.
        if child is not None:
            try:
                child.kill();child.wait(timeout=5)
            except BaseException:
                pass
        return 1
    finally:
        if fd is not None:
            os.close(fd)


def status(contract,run_id,expected,probe=None,now=None,*,profile='no-scale',prepared=None):
    if profile==NATIVE_MODE:
        from pgcat_lane_protocol import _release_admission
        _release_admission(profile,'native-collector-status')
    fd=None
    try:
        _,_,_,status_seconds=profile_values(profile)
        run,fd,_=context(contract,run_id,expected,profile=profile)
        started=intent(fd,run_id,profile=profile)
        if profile==NATIVE_MODE:
            from segmented_capture import canonical,sha
            check(started['prepared']==prepared);native_prepared(run,started,sha(canonical(prepared)))
            _,_,_,status_seconds=profile_values(profile,started['captureMaxSeconds'])
            guardian=decode(read(fd,'guardian.json'));valid_identity(guardian)
            check((probe or process_identity)(guardian['pid'])==guardian)
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
                check(len(args)==6)
                return run_child(contract,run_id,kubeconfig,expected,worker_path=Path(__file__),profile=NATIVE_MODE,prepared_sha=args[5])
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
                plan=decode(_identity(run/'native-plan.json')[0]);reads=FixedWorkerReads(plan,kubeconfig,credential=prepared['workerCredential'])
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
