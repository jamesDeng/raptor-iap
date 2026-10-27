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
    read_private, read_request, write_all, monotonic, NO_SCALE_ACTIONS,
    roster_sha, expected_roster)
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
            nonce=str(uuid.uuid4())
            context=dict(schemaVersion=1,runId=self.run_id,supervisorId=self.supervisor_id,
                targetSha256=hashlib.sha256(encode(self.bindings['target'])).hexdigest(),
                bundleSha256=self.bundle,nonce=nonce)
            save_once(self.root,'run-context.json',context)
            save_once(GLOBAL_ROOT,'active-run',dict(runId=self.run_id,
                supervisorId=self.supervisor_id,laneRoot=str(self.root)))
            save_once(self.root,'supervisor-startup.json',
                dict(runId=self.run_id,supervisorId=self.supervisor_id,contextNonce=nonce),atomic=True)
        except (OSError,Refused):
            self.close()
            raise Refused('incomplete') from None

    def _verify_sources(self):
        b=self.bindings; validate_bindings(b)
        here=Path(__file__).resolve().parent
        paths=dict(laneWrapper=Path(b['laneWrapper']),providerFeed=Path(b['providerFeedPath']),
            laneProtocol=here/'pgcat_lane_protocol.py',laneSupervisor=here/'pgcat_lane_supervisor.py',
            laneActions=here/'pgcat_lane_actions.py',
            pendingProofWorker=here/'pgcat_pending_proof_worker.py',
            preDrainProofWorker=here/'pgcat_pre_drain_proof_worker.py',
            zeroProofWorker=here/'pgcat_zero_proof_worker.py',
            rehearsalWorker=here/'pgcat_rehearsal_worker.py',
            rehearsalContract=here/'pgcat_rehearsal_contract.py',
            captureWindow=here/'capture_window.py',finishClients=here/'finish_clients.py',
            workerCapture=here/'worker_capture.py',
            segmentedCapture=here/'segmented_capture.py')
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

    def _nlb(self,req,receipt):
        require(before_cutoff() and receipt['desired'] == 3 and
                0 <= time.time()-receipt['observedAt'] < 3.5,'receipt')
        pending=self.journal.pending_binding()
        pre=self.journal.pre_drain_binding()
        require(pre['runId'] == req['runId'] and pre['feedSession'] == req['feedSession'] and
                pre['newId'] == pending['newId'] and
                pre['originalInstanceIds'] == req['target']['originalInstanceIds'] and
                pre['pendingBindingSha256'] == hashlib.sha256(encode(pending)).hexdigest() and
                pre['sequence'] <= receipt['sequence'] and
                0 <= time.time()-pre['sealedAt'] < 30 and
                0 <= time.time()-pre['observedAt'] < 30,'target')
        expected=roster_sha(expected_roster(req['target']['originalInstanceIds'],pending['newId'],registered=True))
        require(receipt['rosterSha256'] == expected,'receipt')
        intent=read_json(self.bindings['nlbIntentPath'])
        require(type(intent) is dict,'configuration')
        wanted=dict(action='RemoveServersFromServerGroup',runId=req['runId'],
            feedSession=req['feedSession'],sequence=receipt['sequence'],
            newId=pending['newId'],serverGroupId=req['target']['nlbServerGroupId'],
            preDrainProofSha256=hashlib.sha256(encode({key:value for key,value in pre.items()
                if key != 'sealedAt'})).hexdigest(),rosterSha256=expected,at=intent.get('at'))
        require(intent == wanted and type(intent['at']) is str,'configuration')
        try:
            at=datetime.fromisoformat(intent['at'])
            require(at.tzinfo is not None and
                    0 <= (datetime.now(timezone.utc)-at).total_seconds() < 30)
        except ValueError: raise Refused() from None
        return pending

    def _scalein(self,req,receipt):
        require(before_cutoff() and receipt['desired'] == 3 and
                0 <= time.time()-receipt['observedAt'] < 3.5,'receipt')
        pending=self.journal.pending_binding()
        zero=self.journal.zero_binding()
        require(zero['runId'] == req['runId'] and zero['feedSession'] == req['feedSession'] and
                zero['newId'] == pending['newId'] and
                zero['originalInstanceIds'] == req['target']['originalInstanceIds'] and
                zero['clientRosterSha256'] == pending['clientRosterSha256'] and
                zero['sequence'] < receipt['sequence'] and
                0 <= time.time()-zero['sealedAt'] < 30 and
                receipt['observedAt'] > zero['sealedAt'],'target')
        expected=roster_sha(expected_roster(req['target']['originalInstanceIds'],
            pending['newId'],registered=False))
        require(receipt['rosterSha256'] == expected,'receipt')
        with self.journal._connection() as db:
            activity=db.execute('SELECT request_id,sealed_at FROM activity_clear').fetchone()
        require(activity is not None and 0 <= time.time()-activity['sealed_at'] < 3.5,
                'receipt')
        intent=read_json(self.bindings['scaleinIntentPath'])
        require(type(intent) is dict,'configuration')
        wanted=dict(action='ModifyScalingGroup',runId=req['runId'],
            feedSession=req['feedSession'],sequence=receipt['sequence'],
            fromDesired=3,toDesired=2,newId=pending['newId'],
            zeroProofSha256=hashlib.sha256(encode({key:value for key,value in zero.items()
                if key != 'sealedAt'})).hexdigest(),
            activityRequestId=activity['request_id'],rosterSha256=expected,at=intent.get('at'))
        require(intent == wanted and type(intent['at']) is str,'configuration')
        try:
            at=datetime.fromisoformat(intent['at'])
            require(at.tzinfo is not None and
                    0 <= (datetime.now(timezone.utc)-at).total_seconds() < 30)
        except ValueError: raise Refused() from None

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
                    require(req['action'] not in ('scaleout-two-to-three','pending-proof',
                        'pre-drain-proof','deregister-new-node','zero-proof',
                        'scaling-activity-check','scalein-three-to-two') or
                            run['mode'] == 'disabled-rehearsal')
                identity,receipt=self._receipt(req); self._unused(req,receipt)
                if req['action'] in ('pending-proof','pre-drain-proof','zero-proof',
                                     'scaling-activity-check','scalein-three-to-two'):
                    require(receipt['desired'] == 3 and time.time()-receipt['observedAt'] < 3.5,'receipt')
                binding=(self.journal.pending_binding() if req['action'] in
                    ('pre-drain-proof','deregister-new-node') else
                    self.journal.pre_drain_binding() if req['action']=='zero-proof' else None)
                argv,env,budget=ActionCatalog.command(req['action'],self.bindings,
                    self.run_id,binding)
                deadline=req['deadlineMonotonic']
                require(0 < deadline-monotonic() <= budget,'timeout')
                if req['action'] == 'scaleout-two-to-three': self._ess(req,receipt)
                if req['action'] == 'deregister-new-node': self._nlb(req,receipt)
                if req['action'] == 'scalein-three-to-two': self._scalein(req,receipt)
                if req['action'] not in ('scaleout-two-to-three','deregister-new-node',
                                         'scalein-three-to-two'):
                    deadline=min(deadline,monotonic()+receipt['observedAt']+12-time.time())
                env.update(PGCAT_LANE_SESSION=req['feedSession'],PGCAT_LANE_MIN_SEQUENCE=str(req['minimumSequence']))
                self._alive()
                with self._masked() as old_mask:
                    self._alive()
                    self._receipt(req)
                    if req['action'] in ('scaleout-two-to-three','deregister-new-node',
                                         'scalein-three-to-two'):
                        require(before_cutoff() and time.time()-receipt['observedAt'] < 3.5,'receipt')
                    if req['action'] in ('pending-proof','pre-drain-proof','deregister-new-node',
                                         'zero-proof','scaling-activity-check','scalein-three-to-two'):
                        self.journal.admit(req,receipt)
                    else:
                        self.journal.admit(req)
                    admitted=True
                    if req['action'] in ('scaleout-two-to-three','deregister-new-node',
                                         'scalein-three-to-two'):
                        lane.consume_intent(self.bindings['laneIntentDir'],req['feedSession'],receipt['sequence'])
                        self._receipt(req)
                        require(before_cutoff() and time.time()-receipt['observedAt'] < 3.5,'receipt')
                        if req['action'] == 'scalein-three-to-two':
                            self._scalein(req,receipt)
                    # Python's restore_signals does not restore a pthread mask.
                    child_mask=set(old_mask)-GRACEFUL
                    require(monotonic() < deadline,'timeout')
                    self.child=subprocess.Popen(argv,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE,start_new_session=True,close_fds=True,
                        preexec_fn=lambda:signal.pthread_sigmask(signal.SIG_SETMASK,child_mask))
                self._alive(); require(monotonic() < deadline,'timeout')
                raw=self._communicate(deadline)
                if req['action'] in ('pending-proof','pre-drain-proof','zero-proof'):
                    payload=ActionCatalog.parse(req['action'],raw,req,receipt,
                        source_sha=self.bindings['sourceHashes'][
                            {'pending-proof':'pendingProofWorker','pre-drain-proof':'preDrainProofWorker',
                             'zero-proof':'zeroProofWorker'}[req['action']]],
                        pending_binding=binding if req['action']=='zero-proof' else None)
                else:
                    payload=ActionCatalog.parse(req['action'],raw,req,receipt)
                self._alive(); require(monotonic() < deadline,'timeout')
                if req['action'] not in ('scaleout-two-to-three','deregister-new-node',
                                         'scalein-three-to-two'):
                    require(time.time() < receipt['observedAt']+12,'receipt')
                self._cleanup()
                with self._masked():
                    self._alive()
                    require(monotonic() < deadline,'timeout')
                    result=dict(state='complete',category='ok',payload=payload)
                    if req['action'] == 'pending-proof':
                        self.journal.seal(request_id,result,
                            pending_source_sha=self.bindings['sourceHashes']['pendingProofWorker'])
                    elif req['action'] == 'pre-drain-proof':
                        self.journal.seal(request_id,result,
                            pre_drain_source_sha=self.bindings['sourceHashes']['preDrainProofWorker'])
                    elif req['action'] == 'zero-proof':
                        self.journal.seal(request_id,result,
                            zero_source_sha=self.bindings['sourceHashes']['zeroProofWorker'])
                    else:
                        self.journal.seal(request_id,result)
                    if req['action'] == 'pending-status' and payload == {'status':'PREPARED'}:
                        self.journal.record_prepared(request_id)
                if monotonic() >= deadline:
                    self.cancel('timeout')
                return self.journal.snapshot(request_id)
            except (Refused,lane.Refused,OSError,ValueError,KeyError,TypeError) as error:
                category=error.category if isinstance(error,Refused) else 'configuration'
                self._cleanup()
                self.cancel(category)
                if not admitted: raise Refused(category) from None
                with self._masked():
                    if self.journal.snapshot(request_id)['state'] == 'dispatching':
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
