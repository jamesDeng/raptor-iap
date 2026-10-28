"""Authorized runtime client finish; never retries an uncertain mutation."""
import argparse
import concurrent.futures
import json
import os
import stat
import subprocess
import time
import uuid
from pathlib import Path
from worker_capture import kubectl, read_pod, remaining

_UNBOUND_ROSTER=object()


def atomic_json(path, value):
    temporary=path.with_name('.'+path.name+'.'+uuid.uuid4().hex)
    fd=os.open(str(temporary),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    try:
        with os.fdopen(fd,'w') as stream:
            json.dump(value,stream,allow_nan=False);stream.write('\n');stream.flush();os.fsync(stream.fileno())
        # Never replace a request or acknowledgement from another action.
        os.link(temporary,path)
        directory=os.open(str(path.parent),os.O_RDONLY)
        try:os.fsync(directory)
        finally:os.close(directory)
    finally:
        try:temporary.unlink()
        except FileNotFoundError:pass


def private_directory(path):
    path.mkdir(mode=0o700,exist_ok=True)
    if path.is_symlink() or not path.is_dir() or stat.S_IMODE(path.stat().st_mode)!=0o700:raise ValueError('private boundary directory required')


def read_json(path):
    if path.is_symlink() or not stat.S_ISREG(path.stat().st_mode) or stat.S_IMODE(path.stat().st_mode)!=0o600:raise ValueError('private boundary file required')
    with path.open() as stream:raw=stream.read(65537)
    if len(raw)>65536:raise ValueError('boundary bound exceeded')
    def unique_fields(pairs):
        value={}
        for key,item in pairs:
            if key in value:raise ValueError('duplicate boundary field')
            value[key]=item
        return value
    return json.loads(raw,object_pairs_hook=unique_fields)


def validate_roster(rows, expected=2):
    fields={'name','uid','containerId','processId'}
    if not isinstance(rows,list) or len(rows)!=expected or expected!=2:raise ValueError('exact two-client roster required')
    if any(not isinstance(r,dict) or set(r)!=fields or any(not isinstance(r[k],str) or not r[k] or len(r[k])>256 for k in fields) for r in rows):raise ValueError('invalid client identity')
    if any(len({r[k] for r in rows})!=expected for k in fields):raise ValueError('duplicate client identity')
    return sorted((dict(r) for r in rows),key=lambda r:r['uid'])


def live_roster(*, deadline=None, kubeconfig=None):
    remaining(deadline,3)
    options={}
    if deadline is not None:options['deadline']=deadline
    if kubeconfig is not None:options['kubeconfig']=kubeconfig
    pods=kubectl('get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json',**options)['items']
    remaining(deadline,3)
    rows=[]
    for pod in pods:
        remaining(deadline,2)
        if pod['metadata'].get('deletionTimestamp') or pod['status'].get('phase')!='Running':raise ValueError('client roster not stable')
        row=read_pod(pod,{}) if deadline is None else read_pod(pod,{},deadline=deadline)
        remaining(deadline,2)
        rows.append(dict(name=pod['metadata']['name'],uid=row['uid'],containerId=row['containerId'],processId=row['snapshot']['sample']['processId']))
    return validate_roster(rows)


def wait_ack(directory, request, *, deadline=None):
    end=time.monotonic()+5 if deadline is None else deadline
    while time.monotonic()<end:
        if (directory/'ack.json').exists():
            if read_json(directory/'ack.json')!=request:raise ValueError('boundary acknowledgement mismatch')
            remaining(deadline,5)
            return
        time.sleep(1 if deadline is None else min(1,remaining(end,1)))
    raise ValueError('boundary acknowledgement unavailable')


def exec_finish(row, *, deadline=None, kubeconfig=None):
    options=dict(stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=remaining(deadline,3))
    if kubeconfig is not None:options['env']=dict(os.environ,KUBECONFIG=str(kubeconfig))
    result=subprocess.run(['kubectl','-n','raptor-test','exec',row['name'],'--','/client','finish','--expected-pod-uid',row['uid'],'--expected-process-id',row['processId']],**options)
    remaining(deadline,3)
    if result.returncode:raise RuntimeError('finish response uncertain')
    return 'acknowledged'


def finish(boundary_dir: Path, expected_replicas: int = 2, expected_roster=_UNBOUND_ROSTER, *, deadline=None, kubeconfig=None) -> dict:
    # Legacy callers omit private keywords. A deadline never resets between steps.
    remaining(deadline,4)
    options={}
    if deadline is not None:options['deadline']=deadline
    if kubeconfig is not None:options['kubeconfig']=str(kubeconfig)
    bound=validate_roster(expected_roster,expected_replicas) if expected_roster is not _UNBOUND_ROSTER else None
    directory=Path(boundary_dir);private_directory(directory)
    remaining(deadline,4)
    roster=validate_roster(live_roster(**options),expected_replicas)
    remaining(deadline,4)
    if bound is not None:
        if roster!=bound:raise ValueError('client roster changed')
        roster=bound
    request={'requestId':uuid.uuid4().hex,'roster':roster}
    atomic_json(directory/'request.json',request)
    remaining(deadline,4)
    wait_ack(directory,request,**({'deadline':deadline} if deadline is not None else {}))
    remaining(deadline,4)
    if validate_roster(live_roster(**options),expected_replicas)!=roster:raise ValueError('client roster changed')
    remaining(deadline,4)
    def execute(row):
        status='acknowledged'
        try:
            remaining(deadline,3);exec_finish(row,**options);remaining(deadline,3)
        except Exception:status='unknown'
        return row['uid']+'/'+row['processId'],{'status':status}
    if deadline is None:
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:clients=dict(pool.map(execute,roster))
    else:
        # Outstanding remote work is uncertain, not cancelled. Never wait for a
        # transport thread after the shared budget or retry its mutation.
        pool=concurrent.futures.ThreadPoolExecutor(max_workers=2)
        futures=[]
        try:
            for row in roster:
                if time.monotonic()>=deadline:break
                futures.append((row,pool.submit(execute,row)))
            wait=max(0,deadline-time.monotonic())
            if wait:concurrent.futures.wait([f for _,f in futures],timeout=wait)
            clients={row['uid']+'/'+row['processId']:{'status':'unknown'} for row in roster}
            for row,future in futures:
                if future.done():
                    key,value=future.result();clients[key]=value
        finally:pool.shutdown(wait=False)
    receipt={'requestId':request['requestId'],'status':'INCONCLUSIVE' if any(r['status']=='unknown' for r in clients.values()) else 'ACKNOWLEDGED','clients':clients}
    if receipt['status']=='INCONCLUSIVE':
        receipt['rosterUnchanged']=False
        if deadline is None:
            try:receipt['rosterUnchanged']=validate_roster(live_roster(**options))==roster
            except Exception:pass
    atomic_json(directory/'receipt.json',receipt)
    if receipt['status']=='ACKNOWLEDGED':remaining(deadline,4)
    return receipt


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--boundary-dir',required=True);parser.add_argument('--expected-replicas',type=int,default=2);parser.add_argument('--expected-roster-file');args=parser.parse_args()
    try:
        bound=read_json(Path(args.expected_roster_file)) if args.expected_roster_file is not None else _UNBOUND_ROSTER
        print(json.dumps(finish(Path(args.boundary_dir),args.expected_replicas,expected_roster=bound)))
    except Exception:raise SystemExit('finish refused; preserve evidence and reconcile exact identities')
if __name__=='__main__':main()
