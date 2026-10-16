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
from worker_capture import kubectl, read_pod


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
    finally:temporary.unlink(missing_ok=True)


def private_directory(path):
    path.mkdir(mode=0o700,exist_ok=True)
    if path.is_symlink() or not path.is_dir() or stat.S_IMODE(path.stat().st_mode)!=0o700:raise ValueError('private boundary directory required')


def read_json(path):
    if path.is_symlink() or not stat.S_ISREG(path.stat().st_mode) or stat.S_IMODE(path.stat().st_mode)!=0o600:raise ValueError('private boundary file required')
    with path.open() as stream:raw=stream.read(65537)
    if len(raw)>65536:raise ValueError('boundary bound exceeded')
    return json.loads(raw)


def validate_roster(rows, expected=2):
    fields={'name','uid','containerId','processId'}
    if not isinstance(rows,list) or len(rows)!=expected or expected!=2:raise ValueError('exact two-client roster required')
    if any(set(r)!=fields or any(not isinstance(r[k],str) or not r[k] or len(r[k])>256 for k in fields) for r in rows):raise ValueError('invalid client identity')
    if any(len({r[k] for r in rows})!=expected for k in fields):raise ValueError('duplicate client identity')
    return sorted(rows,key=lambda r:r['uid'])


def live_roster():
    pods=kubectl('get','pods','-l','app.kubernetes.io/name=rdev-test-client-client','-o','json')['items']
    rows=[]
    for pod in pods:
        if pod['metadata'].get('deletionTimestamp') or pod['status'].get('phase')!='Running':raise ValueError('client roster not stable')
        row=read_pod(pod,{})
        rows.append(dict(name=pod['metadata']['name'],uid=row['uid'],containerId=row['containerId'],processId=row['snapshot']['sample']['processId']))
    return validate_roster(rows)


def wait_ack(directory, request):
    end=time.monotonic()+5
    while time.monotonic()<end:
        if (directory/'ack.json').exists():
            if read_json(directory/'ack.json')!=request:raise ValueError('boundary acknowledgement mismatch')
            return
        time.sleep(1)
    raise ValueError('boundary acknowledgement unavailable')


def exec_finish(row):
    result=subprocess.run(['kubectl','-n','raptor-test','exec',row['name'],'--','/client','finish','--expected-pod-uid',row['uid'],'--expected-process-id',row['processId']],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=3)
    if result.returncode:raise RuntimeError('finish response uncertain')
    return 'acknowledged'


def finish(boundary_dir: Path, expected_replicas: int = 2) -> dict:
    directory=Path(boundary_dir);private_directory(directory)
    roster=validate_roster(live_roster(),expected_replicas)
    request={'requestId':uuid.uuid4().hex,'roster':roster}
    atomic_json(directory/'request.json',request);wait_ack(directory,request)
    if validate_roster(live_roster(),expected_replicas)!=roster:raise ValueError('client roster changed')
    def execute(row):
        status='acknowledged'
        try:exec_finish(row)
        except Exception:status='unknown'
        return row['uid']+'/'+row['processId'],{'status':status}
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:clients=dict(pool.map(execute,roster))
    receipt={'requestId':request['requestId'],'status':'INCONCLUSIVE' if any(r['status']=='unknown' for r in clients.values()) else 'ACKNOWLEDGED','clients':clients}
    # Read-only exact identity reconciliation is diagnostic; it cannot repair an uncertain response.
    if receipt['status']=='INCONCLUSIVE':
        try:receipt['rosterUnchanged']=validate_roster(live_roster())==roster
        except Exception:receipt['rosterUnchanged']=False
    atomic_json(directory/'receipt.json',receipt)
    return receipt


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--boundary-dir',required=True);parser.add_argument('--expected-replicas',type=int,default=2);args=parser.parse_args()
    try:print(json.dumps(finish(Path(args.boundary_dir),args.expected_replicas)))
    except Exception:raise SystemExit('finish refused; preserve evidence and reconcile exact identities')
if __name__=='__main__':main()
