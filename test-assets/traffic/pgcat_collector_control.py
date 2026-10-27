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


def context(contract, run_id, expected, worker_path=None):
    check(type(run_id) is str and str(uuid.UUID(run_id)) == run_id)
    contract=Path(contract);run=contract.parent/'run'
    check(contract.name == 'contract' and contract.is_absolute())
    check(Path(worker_path) == run/'pgcat_collector_control.py' if worker_path is not None else True)
    check(type(expected) is dict and set(expected) == set(SOURCES))
    fd=directory(contract);os.close(fd)
    fd=directory(run)
    verified={}
    try:
        for key,name in SOURCES.items():
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


def intent(fd,run_id):
    value=decode(read(fd,'intent.json'))
    check(type(value) is dict and set(value)=={'runId','startedAt'} and value['runId']==run_id and
          type(value['startedAt']) is int)
    return value


def launch(contract,run_id,kubeconfig,expected,worker_path=None,spawn=None):
    run,fd,_=context(contract,run_id,expected,worker_path)
    try:
        kubeconfig=kube_binding(contract,kubeconfig)
        check(not os.listdir(fd))
        write(fd,'intent.json',dict(runId=run_id,startedAt=int(time.time())))
        args=entry_command(run/'pgcat_collector_control.py',expected['collectorControl'],
            ['_run',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':'))])
        env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8'}
        try:
            (spawn or subprocess.Popen)(args,cwd=str(run),env=env,stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True,close_fds=True)
        except BaseException:
            raise ValueError('collector') from None
        return {'status':'LAUNCHED'}
    finally:
        os.close(fd)


def capture_arguments(run):
    return ['--output',str(run/'collector-capture'),'--inventory',
            str(run/'provider-inventory.json'),'--seconds','65']


def completion(run):
    fd=directory(run/'collector-capture')
    try:
        raw=read(fd,'completion.json',2*1024*1024);value=decode(raw)
        check(type(value) is dict and value.get('version')==1 and value.get('completed') is True and
              value.get('cancelled') is False and type(value.get('maxTotalSeconds')) is int and
              value['maxTotalSeconds']==65 and type(value.get('segments')) is list and
              1<=len(value['segments'])<=71)
        return hashlib.sha256(raw).hexdigest()
    finally:
        os.close(fd)


def run_child(contract,run_id,kubeconfig,expected,worker_path=None,spawn=None,probe=None):
    fd=None;child=None
    try:
        run,fd,_=context(contract,run_id,expected,worker_path)
        kubeconfig=kube_binding(contract,kubeconfig);intent(fd,run_id)
        write(fd,'claim.json',dict(runId=run_id))
        args=entry_command(run/'pgcat_collector_control.py',expected['collectorControl'],
            ['_capture',str(contract),run_id,kubeconfig,json.dumps(expected,sort_keys=True,separators=(',',':'))]
            +capture_arguments(run))
        child=(spawn or subprocess.Popen)(args,cwd=str(run),
            env={'PATH':os.defpath,'HOME':'/root','LANG':'C.UTF-8','KUBECONFIG':kubeconfig},
            stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,close_fds=True)
        identity=(probe or process_identity)(child.pid);valid_identity(identity)
        write(fd,'identity.json',identity)
        category='incomplete';digest=None
        try:
            code=child.wait(timeout=80)
            if type(code) is int and code==0:
                digest=completion(run);category='completed'
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


def status(contract,run_id,expected,probe=None,now=None):
    fd=None
    try:
        run,fd,_=context(contract,run_id,expected)
        started=intent(fd,run_id)
        check(0<=(now or time.time)()-started['startedAt']<=90)
        check(decode(read(fd,'claim.json')) == dict(runId=run_id))
        identity=decode(read(fd,'identity.json'));valid_identity(identity)
        observed=(probe or process_identity)(identity['pid'])
        if observed is not None:
            check(observed==identity)
            if 'observed.json' not in os.listdir(fd):
                write(fd,'observed.json',identity)
            check(decode(read(fd,'observed.json'))==identity)
            return dict(status='ALIVE',identity=identity)
        check(decode(read(fd,'observed.json'))==identity)
        sealed=decode(read(fd,'exit.json'))
        digest=completion(run)
        check(sealed==dict(identity=identity,exitCode=0,category='completed',completionSha256=digest)
              and type(sealed['exitCode']) is int)
        return dict(status='EXITED',identity=identity,
                    exit=dict(category='completed',exitCode=0,completionSha256=digest))
    except (OSError,ValueError,TypeError,KeyError,IndexError,OverflowError,UnicodeError,AttributeError):
        return {'status':'INCONCLUSIVE'}
    finally:
        if fd is not None:
            os.close(fd)


def main():
    os.umask(0o077)
    try:
        args=sys.argv[1:]
        check(len(args)>=5)
        verb,contract,run_id,kubeconfig,hashes=args[:5];expected=decode(hashes.encode('ascii'))
        check(verb in ('launch','status','_run','_capture','verdict'))
        if verb in ('_capture','verdict'):
            run,fd,verified=context(contract,run_id,expected,Path(__file__));os.close(fd)
            kube_binding(contract,kubeconfig)
            check(args[5:]==capture_arguments(run) if verb=='_capture' else len(args)==7)
            # Bootstrap the import guard itself from verified source, then ensure
            # every transitive local import executes the same reviewed bytes.
            namespace={'__name__':'_verified_worker','__file__':str(run/'pgcat_rehearsal_worker.py')}
            exec(compile(verified['pgcat_rehearsal_worker'],namespace['__file__'],'exec',dont_inherit=True),namespace)
            check(not any(name in sys.modules for name in verified))
            finder=namespace['_VerifiedSourceFinder'](run,verified);sys.meta_path.insert(0,finder)
            try:
                if verb=='_capture':
                    module=importlib.import_module('worker_capture');sys.argv=['worker_capture.py']+args[5:]
                    module.main()
                else:
                    module=importlib.import_module('pgcat_lane_timing_assessor')
                    result=module.seal_verdict(run,run_id,decode(args[5].encode('ascii')),
                        decode(args[6].encode('ascii')),expected)
                    print(json.dumps(result,sort_keys=True,separators=(',',':')))
            finally:
                sys.meta_path.remove(finder)
            return 0
        check(len(args)==5)
        if verb=='_run':
            return run_child(contract,run_id,kubeconfig,expected,worker_path=Path(__file__))
        result=(launch(contract,run_id,kubeconfig,expected,worker_path=Path(__file__)) if verb=='launch'
                else status(contract,run_id,expected))
        print(json.dumps(result,sort_keys=True,separators=(',',':')))
        return 0 if result['status'] in ('LAUNCHED','ALIVE','EXITED') else 1
    except BaseException:
        print('{"status":"INCONCLUSIVE"}')
        return 1


if __name__=='__main__':
    sys.exit(main())
