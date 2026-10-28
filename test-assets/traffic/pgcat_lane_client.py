"""Sole private local submitter; completion is observed only in the journal."""
import copy
import hashlib
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


class LaneClient:
    @classmethod
    def start(cls, bindings: dict, mode: str, root: Path):
        bindings = copy.deepcopy(bindings)
        validate_bindings(bindings)
        root = Path(root).absolute()
        try: root.mkdir(mode=0o700)
        except OSError: raise Refused('storage') from None
        private(root, True)
        self = cls()
        self.root = root; self.bindings = bindings; self.mode = mode
        self.run_id = str(uuid.uuid4()); self.supervisor_id = None
        self.journal = Journal(root)
        self.bundle = hashlib.sha256(encode(bindings['sourceHashes'])).hexdigest()
        self.journal.create_run(self.run_id, mode, bindings['target'],
                                dict(bindings['sourceHashes'], bundleSha256=self.bundle))
        save_once(root, 'bindings.json', bindings)
        reader, self.liveness_fd = os.pipe()
        os.set_inheritable(reader, False); os.set_inheritable(self.liveness_fd, False)
        self.process = None
        try:
            env = {'PATH':os.defpath, 'HOME':os.path.expanduser('~'), 'LANG':'C', 'LC_ALL':'C'}
            for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
                        'http_proxy','https_proxy','all_proxy','no_proxy'):
                if key in os.environ: env[key] = os.environ[key]
            self.process = subprocess.Popen([sys.executable,
                str(Path(__file__).resolve().with_name('pgcat_lane_runner.py')), str(root), str(reader)],
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
        """No-scale only: bind the live exact child before allowing provider exec."""
        self._active()
        require(self.mode=='no-scale' and getattr(self,'feed_process',None) is None,'child')
        outer,raw=private_json(Path(outer_binding_path))
        b=self.bindings
        require(outer['laneBindings']==b and Path(outer['laneRoot'])==self.root,'source')
        expected=[sys.executable,'-u',b['providerFeedPath'],
            '--mode','disabled-rehearsal','--interval','15','--max-seconds','180',
            '--workbench',b['laneWrapper'],'--worker-inventory',outer['workerInventory'],
            '--kubeconfig',outer['workerKubeconfig'],'--status-file',b['receiptPath'],
            '--run-context',str(self.root/'run-context.json'),'--contract-dir',b['contractDir'],
            '--no-scale-target',encode(b['target']).decode('ascii')]
        require(argv==expected,'source')
        require(hashlib.sha256(Path(b['providerFeedPath']).read_bytes()).hexdigest()==b['sourceHashes']['providerFeed'],'source')
        here=Path(__file__).resolve().parent
        for key,name in dict(client='pgcat_lane_client.py',controller='pgcat_rehearsal_controller.py',runner='pgcat_lane_runner.py').items():
            require(hashlib.sha256((here/name).read_bytes()).hexdigest()==outer['sourceHashes'][key],'source')
        reader,writer=os.pipe()
        # The isolated bootstrap imports only stdlib and exec preserves this PID.
        # EOF and timeout exit without importing or executing provider code.
        gate=('import os,select,sys; fd=int(sys.argv[1]); '
              'ready=select.select([fd],[],[],5.0)[0]; '
              'token=os.read(fd,1) if ready else b""; os.close(fd); '
              'sys.exit(125) if token!=b"1" else None; '
              'os.execv(sys.argv[2],sys.argv[2:])')
        try:
            self.feed_process=subprocess.Popen([sys.executable,'-I','-S','-c',gate,str(reader)]+argv,
                stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
                start_new_session=True,close_fds=True,pass_fds=(reader,),env=env)
            os.close(reader);reader=None
            require(self.feed_process.poll() is None,'child')
            _,current=private_json(Path(outer_binding_path));require(current==raw,'source')
            self.feed_binding=dict(runId=self.run_id,feedId=str(uuid.uuid4()),pid=self.feed_process.pid,
                bindingSha256=hashlib.sha256(raw).hexdigest(),argvSha256=digest(argv))
            self.journal.bind_feed(self.feed_binding)
            self._active();require(self.feed_process.poll() is None,'child')
            require(os.write(writer,b'1')==1,'child')
            return self.feed_process
        finally:
            if reader is not None:os.close(reader)
            os.close(writer)

    def confirm_feed_exit(self, feed_process):
        require(self.mode=='no-scale' and feed_process is getattr(self,'feed_process',None) and
                isinstance(feed_process,subprocess.Popen),'child')
        code=feed_process.poll();require(type(code) is int,'child')
        bound=self.feed_binding
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

    def _prepared_result_ready(self, action, request_id, result):
        if action != 'pending-status' or result['payload'].get('status') != 'PREPARED':
            return True
        with self.journal._connection() as db:
            row = db.execute('SELECT 1 FROM prepared_marker WHERE request_id=?',
                             (request_id,)).fetchone()
        return row is not None

    def call(self, action: str, receipt: dict, deadline: float) -> dict:
        try:
            self._active()
            require(type(receipt) is dict, 'receipt')
            identity, current, _ = lane.read_receipt(self.bindings['receiptPath'],
                                                   receipt['feedSession'], receipt['sequence'])
            require(current == receipt, 'receipt')
            request = dict(requestId=str(uuid.uuid4()), runId=self.run_id,
                supervisorId=self.supervisor_id, action=action, feedSession=receipt['feedSession'],
                minimumSequence=receipt['sequence'], receiptIdentity=receipt_identity(identity),
                target=copy.deepcopy(self.bindings['target']), bundleSha256=self.bundle,
                nonce=str(uuid.uuid4()), deadlineMonotonic=deadline)
            require(type(deadline) is float and monotonic() < deadline, 'timeout')
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
