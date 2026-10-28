"""Tracked guarded rehearsal orchestration. Terminal text is never completion evidence.

No executable CLI entry point: a reviewed private binding and an independent
assessment are required by the caller. The historical local helper is unused.
"""
import copy
from datetime import datetime, timezone, timedelta
from types import SimpleNamespace
import hashlib
import json
import os
import shutil
from pathlib import Path
import subprocess
from subprocess import Popen
import sys
import time

from pgcat_lane_actions import validate_bindings, unique
from pgcat_lane_client import LaneClient
from pgcat_lane_protocol import (Refused, require, private, read_private, identifier,
    sha, token, monotonic, validate_result, validate_finish_receipts,
    encode, roster_sha, expected_roster, removing_roster_shas, original_roster, Journal,
    validate_no_scale_stop, no_scale_stop_ack, NO_SCALE_STOP_INTENT, NO_SCALE_STOP_ACK, NO_SCALE_PROOF,
    no_scale_assessment, private_json, digest, read_finish)
from pgcat_lane_supervisor import read_json, save_once
import provider_feed as provider
import pgcat_workbench_lane as lane
from provider_feed import GROUP_ID, SERVER_GROUP_ID

def _require_verified():
    from pgcat_source_bootstrap import require_verified
    try: require_verified(__name__)
    except (ValueError, KeyError): raise Refused('source') from None


BINDING_KEYS = frozenset(('laneBindings','laneRoot','captureRoot','workerInventory',
                          'workerKubeconfig','sourceHashes'))
NO_SCALE_ACTIONS = frozenset(('baseline-start','baseline-status','collector-launch','collector-status','lane-verdict'))


def _absolute(value):
    require(type(value) is str and value.startswith('/') and
            all(ord(c) >= 32 for c in value) and '..' not in Path(value).parts)
    return Path(value)


def _hashes(expected, paths):
    require(type(expected) is dict and set(expected) == set(paths), 'source')
    for name,path in paths.items():
        require(sha(expected[name]) and not path.is_symlink() and path.is_file(), 'source')
        require(hashlib.sha256(path.read_bytes()).hexdigest() == expected[name], 'source')


def _feed_path(bindings):
    executable=Path(bindings['aliyunCli'])
    require(executable.name == 'aliyun' and not executable.is_symlink() and
            executable.is_file() and os.access(executable,os.X_OK))
    path=str(executable.parent)+os.pathsep+os.defpath
    resolved=shutil.which('aliyun',path=path)
    require(resolved is not None and Path(resolved) == executable and
            os.path.samefile(resolved,executable))
    return path


def load_bindings(path):
    """Read exact owner-only manifest; refuse reused local roots and stale code.

    Worker paths are explicit reviewed bindings, not proof of remote freshness;
    the action-day preflight still verifies their contents and identities.
    """
    try:
        path=_absolute(str(path)); private(path.parent, True)
        folder=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        try:
            value=json.loads(read_private(folder,path.name,8192).decode('ascii'),object_pairs_hook=unique)
        finally: os.close(folder)
        require(type(value) is dict and set(value) in (BINDING_KEYS,BINDING_KEYS | {'localKubeconfig'}))
        b=value['laneBindings']; validate_bindings(b)
        # provider_feed resolves this executable by its fixed name on PATH.
        _feed_path(b)
        require(b['target']['scalingGroupId'] == GROUP_ID and
                b['target']['nlbServerGroupId'] == SERVER_GROUP_ID,'target')
        capture=_absolute(value['captureRoot']); private(capture.parent,True)
        require(not os.path.lexists(capture))
        # All mutable local paths belong to this one new private capture root.
        local=[_absolute(value['laneRoot'])]+[_absolute(b[key]) for key in
            ('receiptPath','laneLockPath','laneIntentDir','scaleoutIntentPath','scaleinIntentPath',
             'nlbIntentPath','preScaleProofPath')]
        require(len(set(local)) == len(local))
        require(all(p.parent == capture and not os.path.lexists(p) for p in local))
        inventory=_absolute(value['workerInventory']); kubeconfig=_absolute(value['workerKubeconfig'])
        require(inventory != kubeconfig and
                b['workerKubeconfig'] == str(kubeconfig))
        remote_run=_absolute(b['remoteRun']); contract=_absolute(b['contractDir'])
        require(remote_run.name == 'run' and contract.name == 'contract' and
                remote_run.parent == contract.parent and
                inventory.parent == remote_run and inventory.name == 'provider-inventory.json')
        here=Path(__file__).resolve().parent
        _hashes(b['sourceHashes'],dict(laneWrapper=Path(b['laneWrapper']),
            providerFeed=Path(b['providerFeedPath']),laneProtocol=here/'pgcat_lane_protocol.py',
            laneSupervisor=here/'pgcat_lane_supervisor.py',laneActions=here/'pgcat_lane_actions.py',
            sourceBootstrap=here/'pgcat_source_bootstrap.py',controller=here/'pgcat_rehearsal_controller.py',
            client=here/'pgcat_lane_client.py',runner=here/'pgcat_lane_runner.py',
            pendingProofWorker=here/'pgcat_pending_proof_worker.py',
            preDrainProofWorker=here/'pgcat_pre_drain_proof_worker.py',
            zeroProofWorker=here/'pgcat_zero_proof_worker.py',
            rehearsalWorker=here/'pgcat_rehearsal_worker.py',
            rehearsalContract=here/'pgcat_rehearsal_contract.py',
            captureWindow=here/'capture_window.py',finishClients=here/'finish_clients.py',
            workerCapture=here/'worker_capture.py',
            segmentedCapture=here/'segmented_capture.py',
            collectorControl=here/'pgcat_collector_control.py',
            laneTimingAssessor=here/'pgcat_lane_timing_assessor.py'))
        _hashes(value['sourceHashes'],dict(controller=here/'pgcat_rehearsal_controller.py',
            client=here/'pgcat_lane_client.py',runner=here/'pgcat_lane_runner.py'))
        return value
    except (OSError,ValueError,UnicodeError,TypeError,KeyError):
        raise Refused('configuration') from None


class Controller:
    def __init__(self, binding_path):
        _require_verified()
        self.binding_path=Path(binding_path)
        self.bindings=load_bindings(self.binding_path)
        self.binding_sha=hashlib.sha256(self.binding_path.read_bytes()).hexdigest()
        self.client=None; self.feed=None; self.receipt=None
        self.state='new'; self.collector_identity=None; self.mode=None
        self.pending_proof=None; self.pre_drain_proof=None; self.zero_proof=None; self.reconcile_proof=None
        self.collector_exit=None; self.verdict_result=None; self.verdict_proof=None
        self.no_scale_stop=None; self.stop_attempted=False
        self.feed_deadline=None; self.local_cleanup_verified=False

    def close(self):
        """Disconnect the owner on interruption; retain the bounded feed handle.

        EOF before durable finish makes the supervisor uncertain. Do not signal
        numeric PIDs, clear its global record, retry, or claim remote cleanup.
        """
        if self.state != 'finished': self.state='uncertain'
        if self.client is not None: self.client.close()
        if self.mode=='no-scale':
            # start_feed retains its actual child even when binding fails.
            if self.feed is None and self.client is not None:
                retained=getattr(self.client,'feed_process',None)
                if isinstance(retained,Popen):self.feed=retained
            if isinstance(self.feed,Popen):
                deadline=self.feed_deadline
                remaining=max(0.,deadline-monotonic()) if deadline is not None else 0.
                while self.feed.poll() is None and remaining>0:
                    interval=min(30.,remaining)
                    try:
                        self.feed.wait(timeout=interval)
                        break
                    except subprocess.TimeoutExpired:
                        remaining=min(remaining-interval,max(0.,deadline-monotonic()))
                        continue
                    except (OSError,ValueError):break
                cleaned=type(self.feed.poll()) is int and self._local_cleanup()
                if not cleaned:
                    pending=dict(status='cleanup-pending',runId=self.client.run_id,
                        pid=self.feed.pid,credentialPath=self.bindings['localKubeconfig'],
                        action='Confirm the retained exact feed has exited, then remove only the unchanged bound local credential and verify absence.')
                    root=Path(self.bindings['laneRoot'])
                    if not (root/'cleanup-pending.json').exists():
                        save_once(root,'cleanup-pending.json',pending)
            elif hasattr(self,'local_credential_identity'):
                self._local_cleanup()

    def _live(self):
        require(self.state in ('active','scaled-three','pending-bound','pre-drain-bound',
                               'nlb-absent','zero-bound','scaled-two','reconciled-two') and
                self.feed is not None and self.feed.poll() is None,'child')
        require(self.client is not None,'incomplete')
        self.client._active()

    def _next_receipt(self, minimum, observed_after=None, desired=2, timeout=45.):
        deadline=monotonic()+timeout
        b=self.bindings['laneBindings']; session=self.receipt['feedSession'] if self.receipt else None
        while monotonic() < deadline:
            self._live()
            try:
                current=read_json(b['receiptPath'])
            except FileNotFoundError:
                time.sleep(.05); continue
            require(type(current) is dict and set(current) == lane.RECEIPT_FIELDS,'receipt')
            identifier(current['feedSession'])
            require(session is None or current['feedSession'] == session,'receipt')
            require(current['mode'] == 'disabled-rehearsal' and type(current['desired']) is int
                    and current['desired'] in ((2,3) if desired in (3,None) else (2,)),'receipt')
            require(type(current['sequence']) is int and current['sequence'] > 0,'receipt')
            require(lane.finite_number(current['observedAt']),'receipt')
            require(sha(current['rosterSha256']),'receipt')
            # A higher sequence can have been observed during the worker call.
            # Only a later observation establishes post-call provider progress.
            if ((desired is None or current['desired'] == desired) and
                    current['sequence'] >= minimum and
                    (observed_after is None or current['observedAt'] > observed_after)):
                try:
                    _,fresh,_=lane.read_receipt(b['receiptPath'],current['feedSession'],minimum)
                except lane.Refused:
                    time.sleep(.05); continue
                require(fresh == current,'receipt'); self._live()
                self.receipt=copy.deepcopy(fresh)
                return self.receipt
            time.sleep(.05)
        raise Refused('timeout')

    def call_no_scale(self, action):
        """Only harmless fixed actions; each waits for a successful successor."""
        try:
            require(self.mode == 'no-scale' and type(action) is str and action in NO_SCALE_ACTIONS)
            return self._worker_call(action)
        except BaseException:
            self.close(); raise

    def _worker_call(self, action):
        try:
            require((self.mode == 'no-scale' and action in NO_SCALE_ACTIONS) or
                    (self.mode == 'disabled-rehearsal' and action in ('baseline-start',
                    'baseline-status','pre-scale-start','pre-scale-status',
                    'rehearsal-collector-launch','rehearsal-collector-status','reconcile-close')))
            if action == 'reconcile-close':
                require(self.state == 'scaled-two' and self.reconcile_proof is None,'incomplete')
            if action == 'lane-verdict':
                require(self.mode == 'no-scale' and self.collector_exit is not None and
                        self.verdict_result is None,'incomplete')
            self._live(); require(self.receipt is not None,'receipt')
            receipt=copy.deepcopy(self.receipt)
            # The same host-wide clock as protocol/runner; never time.monotonic.
            started=monotonic()
            deadline=min(started+5.,started+receipt['observedAt']+12.-time.time())
            require(deadline > started,'timeout')
            result=self.client.call(action,receipt,float(deadline))
            completed_at=time.time()
            self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok','incomplete')
            payload=result['payload']
            if action.startswith('pre-scale-'):
                expected={'status','phase'}
                statuses=('STARTED',) if action.endswith('start') else ('RUNNING','PREPARED')
                require(payload.get('status') in statuses and payload.get('phase') == 'pre-scale','parse')
                if payload['status'] == 'PREPARED':
                    expected |= {'feedSession','sequence'}
                    require(payload.get('feedSession') == receipt['feedSession'] and
                        type(payload.get('sequence')) is int and
                        self.pre_scale_minimum <= payload['sequence'] <= receipt['sequence'],'receipt')
                require(set(payload) == expected,'parse')
            elif action == 'baseline-start':
                require(payload == dict(status='STARTED',phase='baseline'),'parse')
            elif action == 'baseline-status':
                require(set(payload) == {'status','phase'} and payload['phase'] == 'baseline'
                        and payload['status'] in ('RUNNING','PREPARED'),'parse')
            elif action in ('collector-launch','rehearsal-collector-launch'):
                require(payload == dict(status='LAUNCHED'),'parse')
            elif action == 'reconcile-close':
                proof=Journal(Path(self.bindings['laneRoot'])).reconcile_binding()
                require(type(payload) is dict and set(payload) == {'feedSession','sequence','resourceVersion'} and
                        all(payload[key] == proof.get(key) for key in payload) and
                        proof.get('runId') == self.client.run_id and proof.get('feedSession') == receipt['feedSession'] and
                        proof.get('sequence') == receipt['sequence'] and
                        proof.get('sourceSha256') == self.bindings['laneBindings']['sourceHashes']['rehearsalContract'] and
                        self.pending_proof is not None and proof.get('pendingBindingSha256') ==
                            hashlib.sha256(encode(self.pending_proof)).hexdigest() and
                        lane.finite_number(proof.get('sealedAt')) and
                        receipt['observedAt'] <= proof['sealedAt'] <= completed_at,'source')
                identifier(proof.get('requestId'));identifier(proof.get('scaleinRequestId'))
            elif action == 'lane-verdict':
                require(self.mode == 'no-scale' and self.collector_exit is not None and
                        self.verdict_result is None,'incomplete')
                proof=Journal(Path(self.bindings['laneRoot'])).lane_verdict()
                require(proof['payload'] == payload and proof['runId'] == self.client.run_id and
                        proof['feedSession'] == receipt['feedSession'] and
                        proof['sequence'] == receipt['sequence'] and
                        proof['collectorExit']['payload'] == self.collector_exit,'source')
            else:
                status=payload.get('status')
                require(status in ('ALIVE','EXITED') and set(payload) ==
                        ({'status','identity'} if status == 'ALIVE' else {'status','identity','exit'}),'parse')
                require(self.mode == 'no-scale' or status == 'ALIVE','incomplete')
                if status == 'EXITED':
                    require(self.collector_identity is not None and payload['exit']['category'] == 'completed'
                            and type(payload['exit']['exitCode']) is int and payload['exit']['exitCode'] == 0
                            and sha(payload['exit']['completionSha256']),'child')
                if self.collector_identity is not None:
                    require(payload['identity'] == self.collector_identity,'child')
                self.collector_identity=copy.deepcopy(payload['identity'])
            later=self._next_receipt(receipt['sequence']+1,observed_after=completed_at,desired=receipt['desired'])
            if action == 'reconcile-close':
                require(later['rosterSha256'] == roster_sha(original_roster(
                    self.bindings['laneBindings']['target']['originalInstanceIds'])),'receipt')
                self.reconcile_proof=copy.deepcopy(proof)
            if action == 'collector-status' and payload['status'] == 'EXITED':
                self.collector_exit=copy.deepcopy(payload)
            if action == 'lane-verdict':
                self.verdict_result=copy.deepcopy(payload); self.verdict_proof=copy.deepcopy(proof)
            return copy.deepcopy(payload)
        except BaseException:
            self.close(); raise

    def _start(self, mode, max_seconds):
        self.mode=mode
        self.state='starting'
        value=self.bindings; b=value['laneBindings']
        Path(value['captureRoot']).mkdir(mode=0o700)
        Path(b['laneIntentDir']).mkdir(mode=0o700)
        self.client=LaneClient.start(copy.deepcopy(b),mode,Path(value['laneRoot']))
        here=Path(__file__).resolve().parent
        _hashes(b['sourceHashes'],dict(laneWrapper=Path(b['laneWrapper']),
            providerFeed=Path(b['providerFeedPath']),laneProtocol=here/'pgcat_lane_protocol.py',
            laneSupervisor=here/'pgcat_lane_supervisor.py',laneActions=here/'pgcat_lane_actions.py',
            sourceBootstrap=here/'pgcat_source_bootstrap.py',controller=here/'pgcat_rehearsal_controller.py',
            client=here/'pgcat_lane_client.py',runner=here/'pgcat_lane_runner.py',
            pendingProofWorker=here/'pgcat_pending_proof_worker.py',
            preDrainProofWorker=here/'pgcat_pre_drain_proof_worker.py',
            zeroProofWorker=here/'pgcat_zero_proof_worker.py',
            rehearsalWorker=here/'pgcat_rehearsal_worker.py',
            rehearsalContract=here/'pgcat_rehearsal_contract.py',
            captureWindow=here/'capture_window.py',
            finishClients=here/'finish_clients.py',
            workerCapture=here/'worker_capture.py',
            segmentedCapture=here/'segmented_capture.py',
            collectorControl=here/'pgcat_collector_control.py',
            laneTimingAssessor=here/'pgcat_lane_timing_assessor.py'))
        _hashes(value['sourceHashes'],dict(controller=here/'pgcat_rehearsal_controller.py',
            client=here/'pgcat_lane_client.py',runner=here/'pgcat_lane_runner.py'))
        env={'PATH':_feed_path(b),
             'HOME':os.path.expanduser('~'),'LANG':'C','LC_ALL':'C'}
        for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
                'http_proxy','https_proxy','all_proxy','no_proxy'):
            if key in os.environ: env[key]=os.environ[key]
        env.update(PGCAT_LANE_ROLE='provider',PGCAT_LANE_REAL_CLI=b['workbenchCli'],
                   PGCAT_LANE_LOCK=b['laneLockPath'])
        if mode == 'disabled-rehearsal':
            now=self._china_now()
            require(now < now.replace(hour=21,minute=30,second=0,microsecond=0),'timeout')
            remaining=int((now.replace(hour=22,minute=30,second=0,microsecond=0)-now).total_seconds())
            # Reserve two minutes for startup outside provider.run_loop. This
            # is a conservative scheduling margin, not a hard child exit timer.
            require(remaining >= 120+75,'timeout')
            max_seconds=min(max_seconds,remaining-120)
        if mode == 'no-scale':
            self._no_scale_admission()
            require(max_seconds == 180,'timeout')
        argv=[sys.executable,'-u',b['providerFeedPath'],
            '--mode','disabled-rehearsal','--interval','15','--max-seconds',str(max_seconds),
            '--workbench',b['laneWrapper'],'--worker-inventory',value['workerInventory'],
            '--kubeconfig',value['workerKubeconfig'],'--status-file',b['receiptPath'],
            '--run-context',str(Path(value['laneRoot'])/'run-context.json'),
            '--contract-dir',b['contractDir']]
        if mode == 'no-scale':argv += ['--no-scale-target',encode(b['target']).decode('ascii')]
        if mode=='no-scale':
            self.feed_deadline=monotonic()+180.+60.
            self.feed=self.client.start_feed(argv,env,self.binding_path)
        else:
            self.feed=subprocess.Popen(argv,
                stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
                start_new_session=True,close_fds=True,env=env)
        self.state='active'; self._next_receipt(1)

    def _baseline_collector(self):
        self._worker_call('baseline-start')
        while self._worker_call('baseline-status')['status'] != 'PREPARED': pass
        launch,status=(('collector-launch','collector-status') if self.mode == 'no-scale' else
                       ('rehearsal-collector-launch','rehearsal-collector-status'))
        self._worker_call(launch)
        self._worker_call(status)

    def start_no_scale(self):
        try:
            require(self.state == 'new')
            require(load_bindings(self.binding_path) == self.bindings,'source')
            local=_absolute(self.bindings.get('localKubeconfig'))
            require(local == Path(self.bindings['captureRoot']).parent/'kubeconfig','configuration')
            for parent in local.parents:require(not parent.is_symlink(),'configuration')
            private(local); self.local_credential_identity=os.lstat(local)
            self._no_scale_admission()
            self._start('no-scale',180)
            self._baseline_collector()
            return copy.deepcopy(self.receipt)
        except BaseException:
            self.close(); raise

    def _no_scale_admission(self):
        now=self._china_now()
        # 120s startup + 180s feed + 60s cleanup before the 22:30 stop.
        require(now < now.replace(hour=22,minute=24,second=0,microsecond=0),'timeout')

    def stop_no_scale(self, verdict_result):
        """One sealed verdict, one durable stop, then the exact feed's clean exit."""
        try:
            require(self.mode == 'no-scale' and self.state == 'active' and
                    not self.stop_attempted and self.collector_exit is not None and
                    self.verdict_result is not None and verdict_result == self.verdict_result,'incomplete')
            self._live(); require(isinstance(self.feed,Popen),'child')
            proof=Journal(Path(self.bindings['laneRoot'])).lane_verdict()
            require(proof == self.verdict_proof and proof['payload'] == verdict_result and
                    proof['runId'] == self.client.run_id and
                    proof['feedSession'] == self.receipt['feedSession'] and
                    proof['sequence'] < self.receipt['sequence'] and
                    proof['collectorExit']['payload'] == self.collector_exit,'source')
            receipt=read_json(self.bindings['laneBindings']['receiptPath'])
            # Stop uses the finalization freshness contract; the one-second
            # action-admission mtime rule still applies to every lane call.
            require(type(receipt) is dict and set(receipt) == lane.RECEIPT_FIELDS and
                    receipt['feedSession'] == self.receipt['feedSession'] and
                    receipt['mode'] == 'disabled-rehearsal' and
                    type(receipt['sequence']) is int and receipt['sequence'] >= self.receipt['sequence'] and
                    type(receipt['desired']) is int and receipt['desired'] == 2 and receipt['rosterSha256'] ==
                    roster_sha(original_roster(self.bindings['laneBindings']['target']['originalInstanceIds'])) and
                    lane.finite_number(receipt['observedAt']) and 0 <= time.time()-receipt['observedAt'] < 30,'receipt')
            root=Path(self.bindings['captureRoot'])
            require(Path(self.bindings['laneBindings']['receiptPath']).parent == root,'configuration')
            intent=dict(schemaVersion=1,runId=self.client.run_id,feedSession=receipt['feedSession'],
                minimumSequence=receipt['sequence'],verdictSha256=verdict_result['verdictSha256'])
            validate_no_scale_stop(intent)
            self.stop_attempted=True
            save_once(root,NO_SCALE_STOP_INTENT,intent,atomic=True)
            try: code=self.feed.wait(timeout=30.)
            except subprocess.TimeoutExpired: raise Refused('timeout') from None
            polled=self.feed.poll()
            require(type(code) is int and code == 0 and type(polled) is int and polled == code,'child')
            self.client._active()
            require(encode(read_json(root/NO_SCALE_STOP_INTENT,publication_marker=True)) == encode(intent),'receipt')
            final=read_json(self.bindings['laneBindings']['receiptPath'])
            require(type(final) is dict and set(final) == lane.RECEIPT_FIELDS and
                    final['feedSession'] == receipt['feedSession'] and
                    final['mode'] == 'disabled-rehearsal' and type(final['desired']) is int and final['desired'] == 2 and
                    type(final['sequence']) is int and receipt['sequence'] <= final['sequence'] <= receipt['sequence']+1 and
                    final['rosterSha256'] == roster_sha(original_roster(self.bindings['laneBindings']['target']['originalInstanceIds'])) and
                    lane.finite_number(final['observedAt']) and 0 <= time.time()-final['observedAt'] < 30,'receipt')
            ack=read_json(root/NO_SCALE_STOP_ACK,publication_marker=True)
            require(encode(ack) == encode(no_scale_stop_ack(intent,final)),'receipt')
            exit_=dict(pid=self.feed.pid,exitCode=code,category='ok',feedSession=final['feedSession'],sequence=final['sequence'])
            self.receipt=copy.deepcopy(final)
            self.no_scale_stop=dict(intent=intent,ack=ack,receipt=copy.deepcopy(final),feedExit=copy.deepcopy(exit_))
            return exit_
        except (OSError,ValueError,KeyError,TypeError):
            self.close(); raise Refused('receipt') from None
        except BaseException:
            self.close(); raise

    def _paid_evidence(self, path):
        private(path.parent,True)
        folder=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        try:
            value=json.loads(read_private(folder,path.name,8192).decode('ascii'),object_pairs_hook=unique)
        except (ValueError,UnicodeError): raise Refused('parse') from None
        finally: os.close(folder)
        require(type(value) is dict and set(value) == {'target','checkedAt','expiresAt',
            'gateway','clients','metrics','singleOperator','noScaleProof','cost','bindingSha256'})
        require(value['target'] == self.bindings['laneBindings']['target'],'target')
        require(value['bindingSha256'] == hashlib.sha256(self.binding_path.read_bytes()).hexdigest(),'source')
        def utc(text):
            require(type(text) is str)
            try: parsed=datetime.fromisoformat(text.replace('Z','+00:00'))
            except ValueError: raise Refused() from None
            require(parsed.tzinfo is not None and parsed.utcoffset() == timedelta(0))
            return parsed.timestamp()
        checked=utc(value['checkedAt']); expires=utc(value['expiresAt']); now=time.time()
        require(0 <= now-checked < 300 and checked < expires <= checked+300 and now < expires,'timeout')
        g=value['gateway']; c=value['clients']; m=value['metrics']; n=value['noScaleProof']; cost=value['cost']
        require(type(g) is dict and set(g) == {'replacementEnabled','idle'} and
                g['replacementEnabled'] is False and g['idle'] is True)
        require(type(c) is dict and set(c) == {'ready','sqlFailures','identitySha256'} and
                type(c['ready']) is int and c['ready'] == 2 and type(c['sqlFailures']) is int and
                c['sqlFailures'] == 0 and sha(c['identitySha256']))
        require(type(m) is dict and set(m) == {'fresh','complete'} and m['fresh'] is True and m['complete'] is True)
        require(value['singleOperator'] is True)
        require(type(n) is dict and set(n) == {'status','evidenceSha256','proofPath'} and n['status'] == 'PASS' and sha(n['evidenceSha256']))
        self._verify_prior_proof(n,c['identitySha256'])
        require(type(cost) is dict and set(cost) == {'day','net','reserve','allowance','ceiling'})
        require(all(lane.finite_number(cost[k]) and cost[k] >= 0 for k in ('net','reserve','allowance','ceiling')))
        require(cost['ceiling'] == 1800 and cost['net']+cost['reserve']+cost['allowance'] <= 1800)
        require(cost['day'] == self._china_now().date().isoformat())
        return value

    def _verify_prior_proof(self, reference, client_sha):
        path=_absolute(reference['proofPath']);capture=_absolute(self.bindings['captureRoot'])
        require(path.name==NO_SCALE_PROOF and capture not in path.parents and path.parent!=capture,'source')
        # Reject symlinked ancestors as well as the exact private file.
        for parent in path.parents:require(not parent.is_symlink(),'source')
        record,raw=private_json(path)
        require(raw==encode(record) and hashlib.sha256(raw).hexdigest()==reference['evidenceSha256'],'source')
        require(record['day']==self._china_now().date().isoformat(),'timeout')
        hashes=dict(lane=self.bindings['laneBindings']['sourceHashes'],local=self.bindings['sourceHashes'])
        require(record['targetSha256']==digest(self.bindings['laneBindings']['target']) and
                record['sourceSha256']==digest(hashes) and record['clientIdentitySha256']==client_sha,'source')
        private(path.parent/'journal.sqlite3')
        journal=Journal(path.parent);run=journal.run_snapshot()
        require(run['runState']=='finished' and run['mode']=='no-scale','incomplete')
        worker=journal.lane_verdict()
        journal.verify_feed_closure(record,run['feedExit'])
        derived=no_scale_assessment(record,run,worker,run['feedExit'])
        sealed_run,sealed_exit,sealed_assessment=read_finish(path.parent)
        require(sealed_run==run['runId'] and encode(sealed_exit)==encode(run['feedExit']) and
                encode(sealed_assessment)==encode(derived)==encode(run['assessment']) and
                derived['status']=='PASS' and derived['evidenceSha256']==reference['evidenceSha256'],'source')

    @staticmethod
    def _china_now():
        return datetime.fromtimestamp(time.time(),timezone(timedelta(hours=8)))

    def _paid_gate(self, evidence_path, evidence, probed_at):
        self._live()
        require(self._paid_evidence(evidence_path) == evidence,'source')
        require(0 <= time.time()-probed_at < 300,'timeout')
        now=self._china_now()
        require(now < now.replace(hour=21,minute=30,second=0,microsecond=0),'timeout')
        b=self.bindings['laneBindings']
        try: _,current,_=lane.read_receipt(b['receiptPath'],self.receipt['feedSession'],self.receipt['sequence'])
        except lane.Refused: raise Refused('receipt') from None
        require(current == self.receipt and current['desired'] == 2 and
                0 <= time.time()-current['observedAt'] < 3.5,'receipt')

    def _provider_preflight(self):
        """Read-only CLI probe, exclusively BEFORE a provider feed exists."""
        require(self.feed is None and self.state == 'new')
        b=self.bindings['laneBindings']; target=b['target']
        operator=provider.Operator(SimpleNamespace(mode='disabled-rehearsal',status_file=None,
                                                   listener_id=provider.LISTENER_ID))
        env={'PATH':_feed_path(b),'HOME':os.path.expanduser('~'),'LANG':'C','LC_ALL':'C'}
        for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
                    'http_proxy','https_proxy','all_proxy','no_proxy'):
            if key in os.environ: env[key]=os.environ[key]
        def command(argv):
            require(self.feed is None and argv[0] == 'aliyun')
            try:
                result=subprocess.run([b['aliyunCli']]+argv[1:],check=True,capture_output=True,
                    text=True,timeout=10,env=env)
                return result.stdout
            except Exception: raise Refused('remote') from None
        operator.command=command
        try:
            identity=operator.aliyun('sts','GetCallerIdentity')
            require(identity.get('AccountId') == target['accountId'],'target')
            snap=operator.read(); group=operator.fetch_group(); ids=sorted(target['originalInstanceIds'])
            require(snap['desired'] == 2 and snap['members'] == ids and snap['healthyRegistered'] == ids,'target')
            require(snap['memberStates'] == [dict(id=i,lifecycle='Protected',health='Healthy',addressKnown=True) for i in ids],'target')
            require(snap['backendStates'] == [dict(id=i,status='Available',weight=100,listenerHealth='Normal') for i in ids],'target')
            require(group['ScalingGroupId'] == target['scalingGroupId'] and
                    all(type(group[k]) is int and group[k] == v for k,v in
                        (('DesiredCapacity',2),('MinSize',2),('MaxSize',4))),'target')
            activity=operator.aliyun('ess','DescribeScalingActivities',ScalingGroupId=target['scalingGroupId'],
                                    StatusCode='InProgress',PageNumber=1,PageSize=50)
            require(type(activity.get('PageNumber')) is int and activity['PageNumber'] == 1 and
                    type(activity.get('TotalCount')) is int and activity['TotalCount'] == 0 and
                    activity.get('ScalingActivities',{}).get('ScalingActivity') == [],'target')
        except (ValueError,KeyError,TypeError,AttributeError): raise Refused('remote') from None

    def start_rehearsal(self, preflight_path):
        """Single paid start; unknown outcome requires separate exact-state recovery.

        The private operator receipt attests action-day evidence; this API does
        not produce that evidence or turn an acknowledgement into rehearsal PASS.
        """
        from pgcat_lane_protocol import _release_admission
        _release_admission('disabled-rehearsal')
        try:
            require(self.state == 'new')
            require(load_bindings(self.binding_path) == self.bindings,'source')
            require('localKubeconfig' not in self.bindings,'configuration')
            preflight_path=_absolute(str(preflight_path)); private(preflight_path.parent,True)
            evidence=self._paid_evidence(preflight_path)
            now=self._china_now()
            require(now < now.replace(hour=21,minute=30,second=0,microsecond=0),'timeout')
            probed_at=time.time(); self._provider_preflight()
            now=self._china_now()
            require(now < now.replace(hour=21,minute=30,second=0,microsecond=0),'timeout')
            remaining=int((now.replace(hour=22,minute=30,second=0,microsecond=0)-now).total_seconds())
            require(remaining >= 120,'timeout')
            self._start('disabled-rehearsal',min(5400,remaining))
            self._baseline_collector()
            self.pre_scale_minimum=self.receipt['sequence']
            self._worker_call('pre-scale-start')
            while True:
                proof=self._worker_call('pre-scale-status')
                if proof['status'] == 'PREPARED': break
            self._worker_call('rehearsal-collector-status')
            self._paid_gate(preflight_path,evidence,probed_at)
            b=self.bindings['laneBindings']; proof_path=Path(b['preScaleProofPath'])
            save_once(proof_path.parent,proof_path.name,dict(feedSession=proof['feedSession'],sequence=proof['sequence']))
            self._paid_gate(preflight_path,evidence,probed_at)
            intent_path=Path(b['scaleoutIntentPath']); receipt=copy.deepcopy(self.receipt)
            save_once(intent_path.parent,intent_path.name,dict(action='ModifyScalingGroup',
                account=b['target']['accountId'],group=b['target']['scalingGroupId'],fromDesired=2,toDesired=3,
                feedSession=receipt['feedSession'],sequence=receipt['sequence'],
                at=datetime.fromtimestamp(time.time(),timezone.utc).isoformat()))
            self._paid_gate(preflight_path,evidence,probed_at)
            deadline=monotonic()+20.
            result=self.client.call('scaleout-two-to-three',receipt,float(deadline))
            completed_at=time.time(); self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok','incomplete')
            require(set(result['payload']) == {'requestId'} and token(result['payload']['requestId']),'parse')
            self._next_receipt(receipt['sequence']+1,observed_after=completed_at,desired=3,timeout=90.)
            # A completed ACK/receipt cannot outlive its active supervisor run.
            self._live()
            self.state='scaled-three'
            return copy.deepcopy(self.receipt)
        except BaseException:
            self.close(); raise

    def bind_pending(self):
        """Request the fixed worker proof once after 2→3 observation.

        The supervisor validates and seals its result. This stage cannot
        deregister an NLB backend or change ESS capacity.
        """
        try:
            require(self.mode == 'disabled-rehearsal' and self.state == 'scaled-three')
            hold_deadline=monotonic()+420.
            self._pending_marker('pending-start')
            while True:
                require(monotonic() < hold_deadline, 'timeout')
                if self._pending_marker('pending-status') == 'PREPARED': break
            self._live(); receipt=copy.deepcopy(self.receipt)
            require(receipt is not None and receipt['desired'] == 3, 'receipt')
            started=monotonic()
            deadline=min(started+5.,started+receipt['observedAt']+12.-time.time())
            require(deadline > started, 'timeout')
            result=self.client.call('pending-proof',receipt,float(deadline))
            completed_at=time.time()
            self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok','incomplete')
            proof=result['payload']
            require(proof['runId'] == self.client.run_id and
                    proof['feedSession'] == receipt['feedSession'] and
                    proof['originalInstanceIds'] == self.bindings['laneBindings']['target']['originalInstanceIds'] and
                    proof['newId'] not in proof['originalInstanceIds'] and
                    0 < proof['sequence'] <= receipt['sequence'] and
                    proof['sourceSha256'] == self.bindings['laneBindings']['sourceHashes']['pendingProofWorker'] and
                    0 <= completed_at-proof['observedAt'] < 30, 'target')
            self._next_receipt(receipt['sequence']+1,observed_after=completed_at,desired=3)
            self.pending_proof=copy.deepcopy(proof)
            self.state='pending-bound'
            return copy.deepcopy(proof)
        except BaseException:
            self.close(); raise

    def bind_pre_drain(self):
        """Seal a fresh worker proof against the journal's Pending candidate."""
        try:
            require(self.mode == 'disabled-rehearsal' and self.state == 'pending-bound' and
                    self.pending_proof is not None,'incomplete')
            self._live(); receipt=copy.deepcopy(self.receipt)
            require(receipt['desired'] == 3,'receipt')
            started=monotonic()
            deadline=min(started+5.,started+receipt['observedAt']+12.-time.time())
            require(deadline > started,'timeout')
            result=self.client.call('pre-drain-proof',receipt,float(deadline))
            completed_at=time.time(); self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok','incomplete')
            proof=result['payload']; pending=self.pending_proof
            require(proof['runId'] == self.client.run_id and
                    proof['feedSession'] == receipt['feedSession'] and
                    proof['newId'] == pending['newId'] and
                    proof['originalInstanceIds'] == pending['originalInstanceIds'] and
                    proof['pendingBindingSha256'] == hashlib.sha256(encode(pending)).hexdigest() and
                    proof['clientRosterSha256'] == pending['clientRosterSha256'] and
                    proof['sourceSha256'] == self.bindings['laneBindings']['sourceHashes']['preDrainProofWorker'] and
                    pending['sequence'] < proof['sequence'] <= receipt['sequence'] and
                    0 <= completed_at-proof['observedAt'] < 30,'target')
            self._next_receipt(receipt['sequence']+1,observed_after=completed_at,desired=3)
            self.pre_drain_proof=copy.deepcopy(proof)
            self.state='pre-drain-bound'
            return copy.deepcopy(proof)
        except BaseException:
            self.close(); raise

    def deregister_new_node(self):
        """One fixed NLB removal; a later provider absence is required after ACK."""
        try:
            require(self.mode == 'disabled-rehearsal' and self.state == 'pre-drain-bound' and
                    self.pending_proof is not None and self.pre_drain_proof is not None,'incomplete')
            self._live(); receipt=copy.deepcopy(self.receipt)
            pending=self.pending_proof; pre=self.pre_drain_proof
            originals=self.bindings['laneBindings']['target']['originalInstanceIds']
            three=roster_sha(expected_roster(originals,pending['newId'],registered=True))
            absent=roster_sha(expected_roster(originals,pending['newId'],registered=False))
            removing=removing_roster_shas(originals,pending['newId'])
            require(receipt['desired'] == 3 and receipt['rosterSha256'] == three and
                    0 <= time.time()-receipt['observedAt'] < 3.5 and
                    0 <= time.time()-pre['observedAt'] < 30,'receipt')
            intent=dict(action='RemoveServersFromServerGroup',runId=self.client.run_id,
                feedSession=receipt['feedSession'],sequence=receipt['sequence'],
                newId=pending['newId'],serverGroupId=self.bindings['laneBindings']['target']['nlbServerGroupId'],
                preDrainProofSha256=hashlib.sha256(encode(pre)).hexdigest(),
                rosterSha256=three,at=datetime.now(timezone.utc).isoformat())
            save_once(Path(self.bindings['laneBindings']['nlbIntentPath']).parent,
                Path(self.bindings['laneBindings']['nlbIntentPath']).name,intent)
            deadline=monotonic()+20.
            result=self.client.call('deregister-new-node',receipt,float(deadline))
            completed_at=time.time(); self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok' and
                    set(result['payload']) == {'requestId','jobId'} and
                    token(result['payload']['requestId']) and token(result['payload']['jobId']),'parse')
            latest=receipt['sequence']
            end=monotonic()+90.
            phase='available'
            while monotonic() < end:
                later=self._next_receipt(latest+1,observed_after=completed_at,desired=3,
                    timeout=min(20.,end-monotonic()))
                latest=later['sequence']
                roster=later['rosterSha256']
                if roster == three:
                    require(phase == 'available','receipt')
                elif roster in removing:
                    phase='removing'
                elif roster == absent:
                    self.state='nlb-absent'
                    return copy.deepcopy(later)
                else:
                    raise Refused('receipt')
            raise Refused('timeout')
        except BaseException:
            self.close(); raise

    def bind_zero(self):
        """Seal one fresh worker zero proof after the observed NLB absence."""
        try:
            require(self.mode == 'disabled-rehearsal' and self.state == 'nlb-absent' and
                    self.pending_proof is not None and self.pre_drain_proof is not None,
                    'incomplete')
            self._live(); receipt=copy.deepcopy(self.receipt)
            pending=self.pending_proof; pre=self.pre_drain_proof
            originals=self.bindings['laneBindings']['target']['originalInstanceIds']
            absent=roster_sha(expected_roster(originals,pending['newId'],registered=False))
            require(receipt['desired'] == 3 and receipt['rosterSha256'] == absent and
                    0 <= time.time()-receipt['observedAt'] < 3.5,'receipt')
            started=monotonic()
            deadline=min(started+5.,started+receipt['observedAt']+12.-time.time())
            require(deadline > started,'timeout')
            result=self.client.call('zero-proof',receipt,float(deadline))
            completed_at=time.time(); self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok','incomplete')
            proof=result['payload']
            require(proof['runId'] == self.client.run_id and
                    proof['feedSession'] == receipt['feedSession'] and
                    proof['newId'] == pending['newId'] and
                    proof['originalInstanceIds'] == originals and
                    proof['preDrainBindingSha256'] == hashlib.sha256(encode(pre)).hexdigest() and
                    proof['clientRosterSha256'] == pre['clientRosterSha256'] and
                    proof['sourceSha256'] == self.bindings['laneBindings']['sourceHashes']['zeroProofWorker'] and
                    pre['sequence'] < proof['sequence'] <= receipt['sequence'] and
                    0 <= completed_at-proof['observedAt'] < 30,'target')
            later=self._next_receipt(receipt['sequence']+1,observed_after=completed_at,desired=3)
            require(later['rosterSha256'] == absent,'receipt')
            self.zero_proof=copy.deepcopy(proof)
            self.state='zero-bound'
            return copy.deepcopy(proof)
        except BaseException:
            self.close(); raise

    def scalein_three_to_two(self):
        """Check current activity, mutate once, then observe exact two originals."""
        try:
            require(self.mode == 'disabled-rehearsal' and self.state == 'zero-bound' and
                    self.zero_proof is not None and self.pending_proof is not None,'incomplete')
            self._live(); receipt=copy.deepcopy(self.receipt)
            b=self.bindings['laneBindings']; originals=b['target']['originalInstanceIds']
            absent=roster_sha(expected_roster(originals,self.pending_proof['newId'],registered=False))
            require(receipt['desired'] == 3 and receipt['rosterSha256'] == absent and
                    receipt['sequence'] > self.zero_proof['sequence'] and
                    0 <= time.time()-receipt['observedAt'] < 3.5,'receipt')
            now=self._china_now()
            require(now < now.replace(hour=21,minute=30,second=0,microsecond=0),'timeout')
            activity_deadline=monotonic()+5.
            activity=self.client.call('scaling-activity-check',receipt,float(activity_deadline))
            self._live(); require(monotonic() < activity_deadline,'timeout')
            validate_result(activity)
            require(activity['state'] == 'complete' and activity['category'] == 'ok' and
                    set(activity['payload']) == {'status','requestId'} and
                    activity['payload']['status'] == 'CLEAR','incomplete')
            identifier(activity['payload']['requestId'])
            # Activity is read-only. The fixed action reuses this very receipt;
            # the client and supervisor refuse any replacement publication.
            require(0 <= time.time()-receipt['observedAt'] < 3.5,'receipt')
            intent=dict(action='ModifyScalingGroup',runId=self.client.run_id,
                feedSession=receipt['feedSession'],sequence=receipt['sequence'],
                fromDesired=3,toDesired=2,newId=self.pending_proof['newId'],
                zeroProofSha256=hashlib.sha256(encode(self.zero_proof)).hexdigest(),
                activityRequestId=activity['payload']['requestId'],
                rosterSha256=absent,at=datetime.now(timezone.utc).isoformat())
            save_once(Path(b['scaleinIntentPath']).parent,Path(b['scaleinIntentPath']).name,intent)
            self._live(); require(0 <= time.time()-receipt['observedAt'] < 3.5,'receipt')
            deadline=monotonic()+20.
            result=self.client.call('scalein-three-to-two',receipt,float(deadline))
            completed_at=time.time(); self._live(); require(monotonic() < deadline,'timeout')
            validate_result(result)
            require(result['state'] == 'complete' and result['category'] == 'ok' and
                    set(result['payload']) == {'requestId'} and
                    token(result['payload']['requestId']),'parse')
            latest=receipt['sequence']; end=monotonic()+90.
            two=roster_sha(original_roster(originals))
            while monotonic() < end:
                later=self._next_receipt(latest+1,observed_after=completed_at,
                    desired=None,timeout=min(20.,end-monotonic()))
                latest=later['sequence']
                if later['desired'] == 3 and later['rosterSha256'] == absent:
                    continue
                require(later['desired'] == 2 and later['rosterSha256'] == two,'receipt')
                self.state='scaled-two'
                return copy.deepcopy(later)
            raise Refused('timeout')
        except BaseException:
            self.close(); raise

    def reconcile_rehearsal(self):
        """Reconcile once while the exact feed remains alive; require its successor."""
        try:
            require(self.mode == 'disabled-rehearsal' and self.state == 'scaled-two' and
                    self.reconcile_proof is None,'incomplete')
            self._live();receipt=self.receipt
            require(receipt is not None and receipt['desired'] == 2 and
                    0 <= time.time()-receipt['observedAt'] < 3.5 and
                    receipt['rosterSha256'] == roster_sha(original_roster(
                        self.bindings['laneBindings']['target']['originalInstanceIds'])),'receipt')
            self._worker_call('reconcile-close')
            self.state='reconciled-two'
            return copy.deepcopy(self.reconcile_proof)
        except BaseException:
            self.close();raise

    def _pending_marker(self, action):
        require(action in ('pending-start','pending-status') and self.state == 'scaled-three')
        self._live(); receipt=copy.deepcopy(self.receipt)
        require(receipt is not None and receipt['desired'] == 3, 'receipt')
        started=monotonic()
        deadline=min(started+5.,started+receipt['observedAt']+12.-time.time())
        require(deadline > started, 'timeout')
        result=self.client.call(action,receipt,float(deadline))
        completed_at=time.time(); self._live()
        require(monotonic() < deadline, 'timeout')
        validate_result(result)
        require(result['state'] == 'complete' and result['category'] == 'ok', 'incomplete')
        status=result['payload'].get('status')
        require(set(result['payload']) == {'status'} and
                status in (('STARTED',) if action == 'pending-start' else ('RUNNING','PREPARED')), 'parse')
        self._next_receipt(receipt['sequence']+1,observed_after=completed_at,desired=3)
        return status

    def _local_cleanup(self):
        """Never follow or delete a replacement for the credential seen at start."""
        if self.local_cleanup_verified:return True
        try:
            path=_absolute(self.bindings['localKubeconfig'])
            require(path==Path(self.bindings['captureRoot']).parent/'kubeconfig')
            private(path.parent,True);private(path)
            before=self.local_credential_identity;current=os.lstat(path)
            fields=lambda st:(st.st_dev,st.st_ino,st.st_size,st.st_mtime_ns,st.st_ctime_ns)
            require(fields(before)==fields(current),'source')
            fd=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
            try:
                require(fields(os.stat(path.name,dir_fd=fd,follow_symlinks=False))==fields(before),'source')
                os.unlink(path.name,dir_fd=fd);os.fsync(fd)
                require(not os.path.lexists(path),'incomplete')
            finally:os.close(fd)
            self.local_cleanup_verified=True
            return True
        except (Refused,OSError,KeyError,AttributeError):return False

    def finish_no_scale(self, feed_exit, assessment=None):
        """Derive final proof; caller text cannot supply or upgrade its verdict."""
        try:
            require(self.mode=='no-scale' and self.state=='active' and self.collector_identity is not None,'incomplete')
            require(isinstance(self.feed,Popen),'child')
            code=self.feed.poll();require(type(code) is int,'child')
            cleanup=self._local_cleanup()
            require(feed_exit['pid']==self.feed.pid and feed_exit['exitCode']==code,'child')
            require(hashlib.sha256(self.binding_path.read_bytes()).hexdigest()==self.binding_sha,'source')
            final=read_json(self.bindings['laneBindings']['receiptPath'])
            require(final['feedSession']==self.receipt['feedSession'] and
                    final['sequence']==feed_exit['sequence'] and
                    type(final['observedAt']) in (int,float) and 0<=time.time()-final['observedAt']<30,'receipt')
            journal=Journal(Path(self.bindings['laneRoot']))
            try:worker=journal.lane_verdict()
            except Refused:worker=None
            require(worker==self.verdict_proof,'source')
            if worker is not None:
                require(worker['collectorExit']['payload']==self.collector_exit and
                        worker['collectorExit']['payload']['identity']==self.collector_identity,'child')
            stop=copy.deepcopy(self.no_scale_stop)
            if stop is not None:
                require(encode(stop['receipt'])==encode(final),'receipt')
                root=Path(self.bindings['captureRoot'])
                require(encode(read_json(root/NO_SCALE_STOP_INTENT))==encode(stop['intent']) and
                        encode(read_json(root/NO_SCALE_STOP_ACK))==encode(stop['ack']),'receipt')
            hashes=dict(lane=self.bindings['laneBindings']['sourceHashes'],local=self.bindings['sourceHashes'])
            record=dict(schemaVersion=1,day=self._china_now().date().isoformat(),runId=self.client.run_id,
                feedSession=feed_exit['feedSession'],targetSha256=digest(self.bindings['laneBindings']['target']),
                sourceSha256=digest(hashes),sourceHashes=hashes,
                clientIdentitySha256=worker['payload']['clientIdentitySha256'] if worker else None,
                bindingSha256=self.binding_sha,workerVerdict=worker,stop=stop,localCredentialCleanup=cleanup)
            bound=dict(runId=self.client.run_id,target=self.bindings['laneBindings']['target'],
                hashes=dict(hashes['lane'],bundleSha256=digest(hashes['lane'])))
            derived=no_scale_assessment(record,bound,worker,feed_exit,now=time.time())
            require(assessment is None or encode(assessment)==encode(derived),'source')
            save_once(Path(self.bindings['laneRoot']),NO_SCALE_PROOF,record,atomic=True)
            snapshot=self.client.finish(self.feed,copy.deepcopy(feed_exit),derived)
            require(snapshot['runState']=='finished' and encode(snapshot['assessment'])==encode(derived),'incomplete')
            self.state='finished'
            return copy.deepcopy(derived)
        except BaseException:
            self.close();raise
