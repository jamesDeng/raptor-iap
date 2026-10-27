"""Single-thread Mac-local child owner. No restart/replay or remote cancellation.

Global active-run records intentionally survive close/crash. Releasing one needs
an independent exact feed/process audit; this module never infers feed exit.
"""
from contextlib import contextmanager
import copy
from datetime import datetime, timezone, timedelta
import fcntl
import hashlib
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import threading
import time
import uuid

from pgcat_lane_protocol import (Journal, Refused, require, encode, private,
    read_private, read_request, write_all, monotonic, NO_SCALE_ACTIONS)
from pgcat_lane_actions import ActionCatalog, validate_bindings
import pgcat_workbench_lane as lane

GLOBAL_ROOT = Path('/private/tmp/pgcat-lane-'+str(os.getuid()))
GRACEFUL = {signal.SIGINT,signal.SIGTERM,signal.SIGHUP}


def receipt_identity(identity):
    stat, raw = identity
    return hashlib.sha256(encode({'stat':list(stat),'receipt':raw.decode('ascii')})).hexdigest()


def before_cutoff():
    now=datetime.now(timezone(timedelta(hours=8)))
    return (now.hour,now.minute) < (21,30)


def save_once(root, name, value, atomic=False):
    folder=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    staging='.'+str(uuid.uuid4())+'.startup' if atomic else name
    try:
        fd=os.open(staging,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=folder)
        try:
            os.fchmod(fd,0o600); write_all(fd,encode(value)); os.fsync(fd)
        finally: os.close(fd)
        if atomic:
            os.link(staging,name,src_dir_fd=folder,dst_dir_fd=folder,follow_symlinks=False)
            os.unlink(staging,dir_fd=folder)
        os.fsync(folder)
    finally:
        if atomic:
            try: os.unlink(staging,dir_fd=folder)
            except FileNotFoundError: pass
        os.close(folder)


def read_json(path, publication_marker=False):
    path=Path(path); private(path.parent,True)
    folder=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        if publication_marker: os.fsync(folder)
        return json.loads(read_private(folder,path.name,8192,publication_marker=publication_marker).decode('ascii'))
    finally: os.close(folder)


class LaneSupervisor:
    def __init__(self, root: Path, bindings: dict, journal: Journal):
        require(threading.current_thread() is threading.main_thread())
        self.root=Path(root); private(self.root,True)
        require(journal.root == self.root)
        self.bindings=copy.deepcopy(bindings); validate_bindings(self.bindings)
        self.journal=journal; self.child=None; self.cancelled=None; self.owner_fd=None
        self.liveness_fd=None; self.supervisor_id=str(uuid.uuid4())
        self._verify_sources()
        try:
            GLOBAL_ROOT.mkdir(mode=0o700,exist_ok=True); private(GLOBAL_ROOT,True)
            fd=os.open(GLOBAL_ROOT/'owner.lock',os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW,0o600)
            self.owner_fd=fd
            private(GLOBAL_ROOT/'owner.lock')
            fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
            opened=os.fstat(fd); current=os.lstat(GLOBAL_ROOT/'owner.lock')
            require((opened.st_dev,opened.st_ino) == (current.st_dev,current.st_ino))
            with journal._connection() as db:
                run=db.execute('SELECT * FROM run').fetchone()
                require(run is not None and run['state'] == 'active')
                self.run_id=run['run_id']
                require(json.loads(run['target']) == self.bindings['target'],'target')
                require(json.loads(run['hashes']) == dict(self.bindings['sourceHashes'],bundleSha256=self.bundle),'source')
                require(db.execute('SELECT 1 FROM requests').fetchone() is None,'incomplete')
            save_once(GLOBAL_ROOT,'active-run',dict(runId=self.run_id,supervisorId=self.supervisor_id))
            save_once(self.root,'supervisor-startup.json',dict(runId=self.run_id,supervisorId=self.supervisor_id),atomic=True)
        except (OSError,Refused):
            self.close()
            raise Refused('incomplete') from None

    def _verify_sources(self):
        b=self.bindings; validate_bindings(b)
        here=Path(__file__).resolve().parent
        paths=dict(laneWrapper=Path(b['laneWrapper']),providerFeed=Path(b['providerFeedPath']),
            laneProtocol=here/'pgcat_lane_protocol.py',laneSupervisor=here/'pgcat_lane_supervisor.py',
            laneActions=here/'pgcat_lane_actions.py',
            pendingProofWorker=here/'pgcat_pending_proof_worker.py')
        for name,path in paths.items():
            require(not path.is_symlink() and hashlib.sha256(path.read_bytes()).hexdigest() == b['sourceHashes'][name],'source')
        self.bundle=hashlib.sha256(encode(b['sourceHashes'])).hexdigest()

    def close(self):
        if self.owner_fd is not None:
            os.close(self.owner_fd); self.owner_fd=None

    @contextmanager
    def _handlers(self):
        previous={sig:signal.getsignal(sig) for sig in GRACEFUL}
        for sig in GRACEFUL: signal.signal(sig,lambda signum,frame:self.cancel('cancelled'))
        try: yield
        finally:
            for sig,handler in previous.items(): signal.signal(sig,handler)

    @contextmanager
    def _masked(self):
        previous=signal.pthread_sigmask(signal.SIG_BLOCK,GRACEFUL)
        try: yield previous
        finally: signal.pthread_sigmask(signal.SIG_SETMASK,previous)

    def cancel(self, category):
        require(type(category) is str and category in ('cancelled','incomplete','timeout','child','storage','receipt','source','target','parse','configuration','remote'))
        self.cancelled=category
        self.journal.cancel(category)

    def _alive(self):
        require(self.cancelled is None,'cancelled')
        if self.liveness_fd is not None and select.select([self.liveness_fd],[],[],0)[0]:
            # The pipe is liveness only; any data is a protocol violation.
            os.read(self.liveness_fd,1)
            self.cancel('incomplete')
            raise Refused('incomplete')

    def _cleanup(self):
        with self._masked():
            if self.child is not None:
                child=self.child
                if child.poll() is None: child.kill()
                child.wait()
                for stream in (child.stdout,child.stderr):
                    if stream is not None: stream.close()
                self.child=None

    def _receipt(self, req):
        try: identity,value,_=lane.read_receipt(self.bindings['receiptPath'],req['feedSession'],req['minimumSequence'])
        except lane.Refused: raise Refused('receipt') from None
        require(receipt_identity(identity) == req['receiptIdentity'],'receipt')
        return identity,value

    def _unused(self, req, receipt):
        directory=Path(self.bindings['laneIntentDir']); private(directory,True)
        for entry in directory.iterdir():
            if entry.name.startswith(req['feedSession']+'.') and entry.name.endswith('.intent'):
                middle=entry.name[len(req['feedSession'])+1:-7]
                require(middle.isdigit() and int(middle) < receipt['sequence'],'receipt')

    def _ess(self, req, receipt):
        require(receipt['desired'] == 2 and time.time()-receipt['observedAt'] < 3.5 and before_cutoff(),'receipt')
        intent=read_json(self.bindings['scaleoutIntentPath'])
        require(type(intent) is dict,'configuration')
        expected=dict(action='ModifyScalingGroup',account=req['target']['accountId'],group=req['target']['scalingGroupId'],
            fromDesired=2,toDesired=3,feedSession=req['feedSession'],sequence=receipt['sequence'],at=intent.get('at'))
        require(intent == expected and type(intent['at']) is str,'configuration')
        try: require(datetime.fromisoformat(intent['at']).tzinfo is not None)
        except ValueError: raise Refused() from None
        proof=read_json(self.bindings['preScaleProofPath'])
        require(type(proof) is dict and set(proof) == {'feedSession','sequence'} and proof['feedSession'] == req['feedSession']
                and type(proof['sequence']) is int and 0 < proof['sequence'] <= receipt['sequence'],'receipt')
        with self.journal._connection() as db:
            require(db.execute("SELECT 1 FROM requests WHERE json_extract(request,'$.action')='scaleout-two-to-three'").fetchone() is None)

    def _communicate(self, deadline):
        child=self.child
        streams={child.stdout:b'',child.stderr:b''}
        for stream in streams: os.set_blocking(stream.fileno(),False)
        active=list(streams)
        while active or child.poll() is None:
            self._alive()
            remaining=deadline-monotonic(); require(remaining > 0,'timeout')
            watch=[x.fileno() for x in active]
            if self.liveness_fd is not None: watch.append(self.liveness_fd)
            ready,_,_=select.select(watch,[],[],min(.02,remaining))
            self._alive()
            for stream in list(active):
                if stream.fileno() in ready:
                    part=os.read(stream.fileno(),8193-len(streams[stream]))
                    streams[stream]+=part
                    require(len(streams[stream]) <= 8192,'parse')
                    if not part: active.remove(stream)
        child.wait()
        require(child.returncode == 0 and not streams[child.stderr],'child')
        return streams[child.stdout]

    def run_once(self, request_id):
        require(self.owner_fd is not None,'incomplete')
        with self._handlers():
            admitted=False
            try:
                self._alive(); req=read_request(self.root,request_id)
                require(req['supervisorId'] == self.supervisor_id and req['runId'] == self.run_id)
                self._verify_sources(); require(req['bundleSha256'] == self.bundle,'source')
                require(req['target'] == self.bindings['target'],'target')
                with self.journal._connection() as db:
                    run=db.execute('SELECT * FROM run').fetchone()
                    require(run['state'] == 'active')
                    require(run['mode'] != 'no-scale' or req['action'] in NO_SCALE_ACTIONS)
                    require(req['action'] not in ('scaleout-two-to-three','pending-proof') or
                            run['mode'] == 'disabled-rehearsal')
                identity,receipt=self._receipt(req); self._unused(req,receipt)
                if req['action'] == 'pending-proof':
                    require(receipt['desired'] == 3 and time.time()-receipt['observedAt'] < 3.5,'receipt')
                argv,env,budget=ActionCatalog.command(req['action'],self.bindings,
                    req['runId'] if req['action'] == 'pending-proof' else None)
                deadline=req['deadlineMonotonic']
                require(0 < deadline-monotonic() <= budget,'timeout')
                if req['action'] == 'scaleout-two-to-three': self._ess(req,receipt)
                else: deadline=min(deadline,monotonic()+receipt['observedAt']+12-time.time())
                env.update(PGCAT_LANE_SESSION=req['feedSession'],PGCAT_LANE_MIN_SEQUENCE=str(req['minimumSequence']))
                self._alive()
                with self._masked() as old_mask:
                    self._alive()
                    self._receipt(req)
                    if req['action'] == 'scaleout-two-to-three':
                        require(before_cutoff() and time.time()-receipt['observedAt'] < 3.5,'receipt')
                    self.journal.admit(req); admitted=True
                    if req['action'] == 'scaleout-two-to-three':
                        lane.consume_intent(self.bindings['laneIntentDir'],req['feedSession'],receipt['sequence'])
                        self._receipt(req)
                        require(before_cutoff() and time.time()-receipt['observedAt'] < 3.5,'receipt')
                    # Python's restore_signals does not restore a pthread mask.
                    child_mask=set(old_mask)-GRACEFUL
                    require(monotonic() < deadline,'timeout')
                    self.child=subprocess.Popen(argv,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE,start_new_session=True,close_fds=True,
                        preexec_fn=lambda:signal.pthread_sigmask(signal.SIG_SETMASK,child_mask))
                self._alive(); require(monotonic() < deadline,'timeout')
                raw=self._communicate(deadline)
                if req['action'] == 'pending-proof':
                    payload=ActionCatalog.parse(req['action'],raw,req,receipt,
                        source_sha=self.bindings['sourceHashes']['pendingProofWorker'])
                else:
                    payload=ActionCatalog.parse(req['action'],raw,req,receipt)
                self._alive(); require(monotonic() < deadline,'timeout')
                if req['action'] != 'scaleout-two-to-three':
                    require(time.time() < receipt['observedAt']+12,'receipt')
                self._cleanup()
                with self._masked():
                    self._alive()
                    require(monotonic() < deadline,'timeout')
                    result=dict(state='complete',category='ok',payload=payload)
                    if req['action'] == 'pending-proof':
                        self.journal.seal(request_id,result,
                            pending_source_sha=self.bindings['sourceHashes']['pendingProofWorker'])
                    else:
                        self.journal.seal(request_id,result)
                if monotonic() >= deadline:
                    self.cancel('timeout')
                return self.journal.snapshot(request_id)
            except (Refused,lane.Refused,OSError,ValueError,KeyError,TypeError) as error:
                category=error.category if isinstance(error,Refused) else 'configuration'
                self._cleanup()
                self.cancel(category)
                if not admitted: raise Refused(category) from None
                with self._masked():
                    self.journal.seal(request_id,dict(state='uncertain',category=category,payload={}))
                return self.journal.snapshot(request_id)

    def finish(self):
        require(self.owner_fd is not None and self.child is None, 'incomplete')
        # This callback runs inside the finish transaction immediately before
        # COMMIT. It must not attempt a nested journal write on detecting EOF.
        def before_commit():
            require(self.cancelled is None, 'cancelled')
            if self.liveness_fd is not None:
                require(not select.select([self.liveness_fd],[],[],0)[0], 'incomplete')
        with self._masked():
            try:
                self._alive()
                return self.journal.finish(before_commit=before_commit)
            except Refused as error:
                # The finish transaction has rolled back before cancellation.
                self.cancel(error.category)
                raise

    def serve(self, liveness_fd):
        require(self.owner_fd is not None,'incomplete')
        self.liveness_fd=liveness_fd
        try:
            with self._handlers():
                while True:
                    self._alive()
                    if os.path.lexists(self.root/'finish.ready'):
                        self.finish()
                        return
                    folder=self.root/'requests'
                    if folder.exists():
                        private(folder,True)
                        for marker in sorted(folder.glob('*.ready')):
                            request_id=marker.name[:-6]
                            try: self.journal.snapshot(request_id)
                            except Refused: self.run_once(request_id)
                            self._alive()
                    select.select([liveness_fd],[],[],.02)
        except (Refused, OSError, ValueError, TypeError):
            self._cleanup()
            self.cancel(self.cancelled or 'incomplete')
        finally:
            self.liveness_fd=None
