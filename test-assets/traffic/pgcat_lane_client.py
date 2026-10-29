"""Sole private local submitter; completion is observed only in the journal."""
import copy
from datetime import datetime, timezone, timedelta
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from pgcat_lane_actions import validate_bindings
from pgcat_lane_protocol import (Journal, Refused, require, encode, private,
    monotonic, identifier, digest, private_json, publish_request, publish_finish, read_finish, validate_finish_receipts)
from pgcat_lane_supervisor import read_json, save_once, receipt_identity
import pgcat_workbench_lane as lane

def _require_verified():
    from pgcat_source_bootstrap import require_verified
    try: require_verified(__name__)
    except (ValueError, KeyError): raise Refused('source') from None



class LaneClient:
    @classmethod
    def start(cls, bindings: dict, mode: str, root: Path, *, native_plan=None):
        from pgcat_lane_protocol import _release_admission
        _release_admission(mode)
        _require_verified()
        bindings = copy.deepcopy(bindings)
        validate_bindings(bindings,mode)
        if mode=='full-replacement-direct-v2':
            from pgcat_native_controller import validate_start_plan
            native_plan=validate_start_plan(bindings,native_plan)
        else:require(native_plan is None)
        root = Path(root).absolute()
        try: root.mkdir(mode=0o700)
        except OSError: raise Refused('storage') from None
        private(root, True)
        self = cls()
        self.root = root; self.bindings = bindings; self.mode = mode
        self.run_id = native_plan['runId'] if native_plan is not None else str(uuid.uuid4()); self.supervisor_id = None
        self.journal = Journal(root)
        self.bundle = hashlib.sha256(encode(bindings['sourceHashes'])).hexdigest()
        if native_plan is not None:save_once(root,'native-plan.json',native_plan)
        self.journal.create_run(self.run_id, mode, bindings['target'],
                                dict(bindings['sourceHashes'], bundleSha256=self.bundle),native_plan=native_plan)
        save_once(root, 'bindings.json', bindings)
        reader, self.liveness_fd = os.pipe()
        os.set_inheritable(reader, False); os.set_inheritable(self.liveness_fd, False)
        self.process = None
        try:
            env = {'PATH':os.defpath, 'HOME':os.path.expanduser('~'), 'LANG':'C', 'LC_ALL':'C'}
            for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
                        'http_proxy','https_proxy','all_proxy','no_proxy'):
                if key in os.environ: env[key] = os.environ[key]
            from pgcat_source_bootstrap import command
            self.process = subprocess.Popen(command(bindings['sourceHashes'],'runner',[str(root),str(reader)]),
                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                close_fds=True, pass_fds=(reader,), start_new_session=True, env=env)
            os.close(reader); reader = None
            deadline = monotonic()+5.
            while not (root/'supervisor-startup.json').exists():
                self._active(); require(monotonic() < deadline, 'timeout'); time.sleep(.01)
            startup = read_json(root/'supervisor-startup.json', publication_marker=True)
            require(type(startup) is dict and set(startup) == {'runId','supervisorId','contextNonce'})
            require(startup['runId'] == self.run_id)
            identifier(startup['supervisorId'])
            identifier(startup['contextNonce'])
            self.supervisor_id = startup['supervisorId']
            self._active()
            return self
        except (Refused, OSError, ValueError):
            self._cancel_and_close('incomplete')
            if self.process is not None:
                try: self.process.wait(timeout=2)
                except subprocess.TimeoutExpired: pass  # Retain record; never infer cleanup.
            raise Refused('incomplete') from None
        finally:
            if reader is not None: os.close(reader)

    def start_feed(self, argv, env, outer_binding_path):
        """Bind this mode's live exact child before allowing provider exec."""
        self._active()
        require(self.mode in ('no-scale','disabled-rehearsal','full-replacement-direct-v2') and getattr(self,'feed_process',None) is None,'child')
        from pgcat_lane_protocol import _release_admission
        _release_admission(self.mode)
        outer,raw=private_json(Path(outer_binding_path))
        b=self.bindings
        require(outer['laneBindings']==b and Path(outer['laneRoot'])==self.root,'source')
        cap='180'
        if self.mode in ('disabled-rehearsal','full-replacement-direct-v2'):
            require(type(argv) is list and len(argv) > 8 and type(argv[8]) is str,'source')
            cap=argv[8]
            require(0 < len(cap) <= 4 and all('0' <= c <= '9' for c in cap) and str(int(cap)) == cap and 75 <= int(cap) <= 5400,'timeout')
        expected=[sys.executable,'-u',b['providerFeedPath'],
            '--mode',('full-replacement-direct-v2' if self.mode=='full-replacement-direct-v2' else 'disabled-rehearsal'),'--interval','15','--max-seconds',cap,
            '--workbench',b['laneWrapper'],'--worker-inventory',outer['workerInventory'],
            '--kubeconfig',outer['workerKubeconfig'],'--status-file',b['receiptPath'],
            '--run-context',str(self.root/'run-context.json'),'--contract-dir',b['contractDir']]
        if self.mode == 'no-scale':expected += ['--no-scale-target',encode(b['target']).decode('ascii')]
        require(argv==expected,'source')
        _require_verified()
        from pgcat_source_bootstrap import command
        verified_argv=command(b['sourceHashes'],'providerFeed',argv[3:])
        require(hashlib.sha256(Path(b['providerFeedPath']).read_bytes()).hexdigest()==b['sourceHashes']['providerFeed'],'source')
        here=Path(__file__).resolve().parent
        local_sources=dict(client='pgcat_lane_client.py',runner='pgcat_lane_runner.py')
        local_sources.update({'nativeController':'pgcat_native_controller.py'} if self.mode=='full-replacement-direct-v2' else {'controller':'pgcat_rehearsal_controller.py'})
        for key,name in local_sources.items():
            require(hashlib.sha256((here/name).read_bytes()).hexdigest()==outer['sourceHashes'][key],'source')
        def check_cap():
            if self.mode == 'full-replacement-direct-v2':
                from pgcat_native_controller import capture_budget
                require(int(cap)<=capture_budget(self.journal.native_plan(),time.time()),'timeout')
            if self.mode in ('disabled-rehearsal','full-replacement-direct-v2'):
                now=datetime.fromtimestamp(time.time(),timezone(timedelta(hours=8)))
                require(now < now.replace(hour=21,minute=30,second=0,microsecond=0) and
                        int(cap) <= int((now.replace(hour=22,minute=30,second=0,microsecond=0)-now).total_seconds())-120,'timeout')
        check_cap()
        reader,writer=os.pipe()
        # The isolated bootstrap imports only stdlib and exec preserves this PID.
        # EOF and timeout exit without importing or executing provider code.
        gate=('import os,select,sys; fd=int(sys.argv[1]); '
              'ready=select.select([fd],[],[],5.0)[0]; '
              'token=os.read(fd,1) if ready else b""; os.close(fd); '
              'sys.exit(125) if token!=b"1" else None; '
              'os.execv(sys.argv[2],sys.argv[2:])')
        try:
            self.feed_process=subprocess.Popen([sys.executable,'-I','-S','-c',gate,str(reader)]+verified_argv,
                stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
                start_new_session=True,close_fds=True,pass_fds=(reader,),env=env)
            os.close(reader);reader=None
            require(self.feed_process.poll() is None,'child')
            _,current=private_json(Path(outer_binding_path));require(current==raw,'source')
            self.feed_binding=dict(runId=self.run_id,feedId=str(uuid.uuid4()),pid=self.feed_process.pid,
                bindingSha256=hashlib.sha256(raw).hexdigest(),argvSha256=digest(verified_argv))
            if self.mode in ('disabled-rehearsal','full-replacement-direct-v2'):self.journal.bind_feed(self.feed_binding,manifest_raw=raw)
            else:self.journal.bind_feed(self.feed_binding)
            self._active();require(self.feed_process.poll() is None,'child')
            check_cap()
            require(os.write(writer,b'1')==1,'child')
            return self.feed_process
        finally:
            if reader is not None:os.close(reader)
            os.close(writer)

    def confirm_feed_exit(self, feed_process):
        require(self.mode in ('no-scale','disabled-rehearsal','full-replacement-direct-v2') and feed_process is getattr(self,'feed_process',None) and
                isinstance(feed_process,subprocess.Popen),'child')
        code=feed_process.poll();require(type(code) is int,'child')
        bound=self.feed_binding
        with self.journal._connection() as db:
            row=db.execute('SELECT proof FROM feed_binding').fetchone()
            require(row is not None,'child')
            require(encode(json.loads(row['proof'])) == encode(bound),'source')
            run=db.execute('SELECT * FROM run').fetchone()
            require(run is not None and run['mode'] == self.mode and run['run_id'] == self.run_id and
                    json.loads(run['target']) == self.bindings['target'] and
                    json.loads(run['hashes']) == dict(self.bindings['sourceHashes'],bundleSha256=self.bundle),'source')
        require(feed_process.pid==bound['pid'],'child')
        self.journal.confirm_feed_exit(dict(runId=self.run_id,feedId=bound['feedId'],
            pid=feed_process.pid,exitCode=code))

    def _cancel_and_close(self, category):
        try:
            self.journal.cancel(category)
        except (Refused, OSError):
            # A failed persistence attempt cannot keep the owner alive. The
            # original refusal is reported; EOF still stops the supervisor.
            pass
        finally:
            self.close()

    def close(self):
        """Disconnect; EOF before committed finish means uncertainty."""
        if self.liveness_fd is not None:
            os.close(self.liveness_fd); self.liveness_fd = None

    def _active(self):
        require(self.liveness_fd is not None, 'incomplete')
        require(self.process is not None and self.process.poll() is None, 'incomplete')
        state = self.journal.run_snapshot()
        require(state['runState'] == 'active' and state['runId'] == self.run_id, 'incomplete')
        require(state['mode'] == self.mode and state['target'] == self.bindings['target'])
        require(state['hashes'] == dict(self.bindings['sourceHashes'], bundleSha256=self.bundle), 'source')
        if self.mode=='full-replacement-direct-v2':
            from pgcat_native_controller import validate_start_plan
            validate_start_plan(self.bindings,self.journal.native_plan())
            if getattr(self,'feed_process',None) is not None:require(self.feed_process.poll() is None,'child')

    def _prepared_result_ready(self, action, request_id, result):
        if action != 'pending-status' or result['payload'].get('status') != 'PREPARED':
            return True
        with self.journal._connection() as db:
            row = db.execute('SELECT 1 FROM prepared_marker WHERE request_id=?',
                             (request_id,)).fetchone()
        return row is not None

    def call(self, action: str, receipt: dict, deadline: float, *, native_selector=None) -> dict:
        # Proven stopped calls refuse before the uncertainty/owner-close path.
        with self.journal._connection() as db:
            stopped=db.execute('SELECT 1 FROM rehearsal_stop').fetchone() is not None
        if stopped:
            self._active();raise Refused('incomplete')
        require(action != 'rehearsal-finish','configuration')
        try:
            self._active()
            require(type(receipt) is dict, 'receipt')
            identity, current, _ = lane.read_receipt(self.bindings['receiptPath'],
                                                   receipt['feedSession'], receipt['sequence'],
                                                   expected_mode='full-replacement-direct-v2' if self.mode=='full-replacement-direct-v2' else 'disabled-rehearsal')
            require(current == receipt, 'receipt')
            request = dict(requestId=str(uuid.uuid4()), runId=self.run_id,
                supervisorId=self.supervisor_id, action=action, feedSession=receipt['feedSession'],
                minimumSequence=receipt['sequence'], receiptIdentity=receipt_identity(identity),
                target=copy.deepcopy(self.bindings['target']), bundleSha256=self.bundle,
                nonce=str(uuid.uuid4()), deadlineMonotonic=deadline)
            require(type(deadline) is float and monotonic() < deadline, 'timeout')
            if self.mode=='full-replacement-direct-v2':
                from pgcat_native_controller import validate_selector
                selector={} if native_selector is None else native_selector
                validate_selector(self.journal.native_plan(),action,selector)
                save_once(self.root,'native-selector-'+request['requestId']+'.json',selector)
            else:require(native_selector is None)
            publish_request(self.root, request)
            while True:
                self._active(); require(monotonic() < deadline, 'timeout')
                with self.journal._connection() as db:
                    exists = db.execute('SELECT 1 FROM requests WHERE request_id=?', (request['requestId'],)).fetchone()
                if exists:
                    snapshot = self.journal.snapshot(request['requestId'])
                    require(snapshot['request'] == request and snapshot['runId'] == self.run_id)
                    require(snapshot['runState'] == 'active' and snapshot['state'] != 'uncertain', 'incomplete')
                    if snapshot['state'] == 'complete':
                        if not self._prepared_result_ready(action,request['requestId'],snapshot['result']):
                            time.sleep(.01)
                            continue
                        self._active(); require(monotonic() < deadline, 'timeout')
                        return snapshot['result']
                time.sleep(.01)
        except (Refused, lane.Refused, OSError, ValueError, KeyError, TypeError) as error:
            category = error.category if isinstance(error, Refused) else 'receipt'
            self._cancel_and_close(category)
            raise Refused(category) from None

    def post_stop_call(self):
        """One fixed journal-derived finish; never refresh a frozen publication."""
        with self.journal._connection() as db:
            duplicate=db.execute('SELECT 1 FROM post_stop_admission').fetchone() is not None
        if duplicate:
            self._active();raise Refused('incomplete')
        deadline=monotonic()+5.
        try:
            self._active();require(self.mode == 'disabled-rehearsal','configuration')
            stopped=self.journal.rehearsal_stop();receipt=stopped['receipt']
            require(0 <= time.time()-receipt['observedAt'] < 24,'receipt')
            request=dict(requestId=str(uuid.uuid4()),runId=self.run_id,supervisorId=self.supervisor_id,
                action='rehearsal-finish',feedSession=receipt['feedSession'],minimumSequence=receipt['sequence'],
                receiptIdentity=stopped['receiptIdentity'],target=copy.deepcopy(self.bindings['target']),
                bundleSha256=self.bundle,nonce=str(uuid.uuid4()),deadlineMonotonic=float(deadline))
            self.journal.post_stop_context(request)
            require(monotonic() < deadline,'timeout');publish_request(self.root,request)
            while True:
                self._active();require(monotonic() < deadline,'timeout')
                with self.journal._connection() as db:
                    row=db.execute('SELECT 1 FROM requests WHERE request_id=?',(request['requestId'],)).fetchone()
                if row is not None:
                    snapshot=self.journal.snapshot(request['requestId'])
                    require(snapshot['request'] == request and snapshot['runState'] == 'active' and snapshot['state'] != 'uncertain','source')
                    if snapshot['state'] == 'complete':
                        binding=self.journal.post_stop_binding()
                        require(binding['requestId'] == request['requestId'] and binding['runId'] == self.run_id and
                            binding['workerResult'] == snapshot['result']['payload'],'source')
                        self._active();require(monotonic() < deadline and 0 <= time.time()-receipt['observedAt'] < 30,'timeout')
                        return snapshot['result']
                time.sleep(.01)
        except (Refused,OSError,ValueError,KeyError,TypeError) as error:
            category=error.category if isinstance(error,Refused) else 'source'
            self._cancel_and_close(category);raise Refused(category) from None

    def finish(self, feed_process: subprocess.Popen, feed_exit: dict, assessment: dict) -> dict:
        try:
            self._active()
        except Refused:
            self._cancel_and_close('incomplete')
            raise
        require(isinstance(feed_process, subprocess.Popen), 'child')
        exit_code = feed_process.poll()
        require(type(exit_code) is int, 'child')
        validate_finish_receipts(feed_exit, assessment, self.mode)
        require(feed_exit['pid'] == feed_process.pid and feed_exit['exitCode'] == exit_code, 'child')
        try:
            if self.mode=='no-scale':self.confirm_feed_exit(feed_process)
            publish_finish(self.root, self.run_id, feed_exit, assessment)
            deadline = monotonic()+5.
            def sealed_final(state):
                require(state['runState'] == 'finished' and state['runId'] == self.run_id)
                require(state['mode'] == self.mode and state['target'] == self.bindings['target'])
                require(state['hashes'] == dict(self.bindings['sourceHashes'], bundleSha256=self.bundle), 'source')
                require(encode(state['assessment']) == encode(assessment) and
                        encode(state['feedExit']) == encode(feed_exit), 'source')
                run_id, sealed_exit, sealed_assessment = read_finish(self.root)
                require(run_id == self.run_id and encode(sealed_exit) == encode(feed_exit) and
                        encode(sealed_assessment) == encode(assessment), 'source')
                self.close()
                return state
            while True:
                state = self.journal.run_snapshot()
                require(state['runId'] == self.run_id)
                if state['runState'] == 'finished':
                    return sealed_final(state)
                try:
                    self._active()
                except Refused:
                    # The owner may have committed and exited since our active
                    # snapshot. Only exact durable receipts can close this race.
                    return sealed_final(self.journal.run_snapshot())
                require(monotonic() < deadline, 'timeout'); time.sleep(.01)
        except (Refused, OSError, ValueError) as error:
            category = error.category if isinstance(error, Refused) else 'incomplete'
            self._cancel_and_close(category)
            raise Refused(category) from None
