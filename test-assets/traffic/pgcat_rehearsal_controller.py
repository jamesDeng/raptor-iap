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
    sha, token, monotonic, validate_result, validate_finish_receipts)
from pgcat_lane_supervisor import read_json, save_once
import provider_feed as provider
import pgcat_workbench_lane as lane
from provider_feed import GROUP_ID, SERVER_GROUP_ID

BINDING_KEYS = frozenset(('laneBindings','laneRoot','captureRoot','workerInventory',
                          'workerKubeconfig','sourceHashes'))
NO_SCALE_ACTIONS = frozenset(('baseline-start','baseline-status','collector-launch','collector-status'))


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
        require(type(value) is dict and set(value) == BINDING_KEYS)
        b=value['laneBindings']; validate_bindings(b)
        # provider_feed resolves this executable by its fixed name on PATH.
        _feed_path(b)
        require(b['target']['scalingGroupId'] == GROUP_ID and
                b['target']['nlbServerGroupId'] == SERVER_GROUP_ID,'target')
        capture=_absolute(value['captureRoot']); private(capture.parent,True)
        require(not os.path.lexists(capture))
        # All mutable local paths belong to this one new private capture root.
        local=[_absolute(value['laneRoot'])]+[_absolute(b[key]) for key in
            ('receiptPath','laneLockPath','laneIntentDir','scaleoutIntentPath','preScaleProofPath')]
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
            pendingProofWorker=here/'pgcat_pending_proof_worker.py',
            rehearsalWorker=here/'pgcat_rehearsal_worker.py',
            rehearsalContract=here/'pgcat_rehearsal_contract.py',
            captureWindow=here/'capture_window.py',finishClients=here/'finish_clients.py',
            workerCapture=here/'worker_capture.py',
            segmentedCapture=here/'segmented_capture.py'))
        _hashes(value['sourceHashes'],dict(controller=here/'pgcat_rehearsal_controller.py',
            client=here/'pgcat_lane_client.py',runner=here/'pgcat_lane_runner.py'))
        return value
    except (OSError,ValueError,UnicodeError,TypeError,KeyError):
        raise Refused('configuration') from None


class Controller:
    def __init__(self, binding_path):
        self.binding_path=Path(binding_path)
        self.bindings=load_bindings(self.binding_path)
        self.client=None; self.feed=None; self.receipt=None
        self.state='new'; self.collector_identity=None; self.mode=None

    def close(self):
        """Disconnect the owner on interruption; retain the bounded feed handle.

        EOF before durable finish makes the supervisor uncertain. Do not signal
        numeric PIDs, clear its global record, retry, or claim remote cleanup.
        """
        if self.state != 'finished': self.state='uncertain'
        if self.client is not None: self.client.close()

    def _live(self):
        require(self.state in ('active','scaled-three','pending-bound') and
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
                    and current['desired'] in ((2,3) if desired == 3 else (2,)),'receipt')
            require(type(current['sequence']) is int and current['sequence'] > 0,'receipt')
            require(lane.finite_number(current['observedAt']),'receipt')
            # A higher sequence can have been observed during the worker call.
            # Only a later observation establishes post-call provider progress.
            if (current['desired'] == desired and current['sequence'] >= minimum and
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
            require(type(action) is str and action in NO_SCALE_ACTIONS)
            return self._worker_call(action)
        except BaseException:
            self.close(); raise

    def _worker_call(self, action):
        try:
            require(action in NO_SCALE_ACTIONS or (self.mode == 'disabled-rehearsal'
                    and action in ('pre-scale-start','pre-scale-status')))
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
            elif action == 'collector-launch':
                require(payload == dict(status='LAUNCHED'),'parse')
            else:
                require(set(payload) == {'status','identity'} and payload['status'] == 'ALIVE','parse')
                if self.collector_identity is not None:
                    require(payload['identity'] == self.collector_identity,'child')
                self.collector_identity=copy.deepcopy(payload['identity'])
            self._next_receipt(receipt['sequence']+1,observed_after=completed_at)
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
            pendingProofWorker=here/'pgcat_pending_proof_worker.py',
            rehearsalWorker=here/'pgcat_rehearsal_worker.py',
            rehearsalContract=here/'pgcat_rehearsal_contract.py',
            captureWindow=here/'capture_window.py',
            finishClients=here/'finish_clients.py',
            workerCapture=here/'worker_capture.py',
            segmentedCapture=here/'segmented_capture.py'))
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
        self.feed=subprocess.Popen([sys.executable,'-u',b['providerFeedPath'],
            '--mode','disabled-rehearsal','--interval','15','--max-seconds',str(max_seconds),
            '--workbench',b['laneWrapper'],'--worker-inventory',value['workerInventory'],
            '--kubeconfig',value['workerKubeconfig'],'--status-file',b['receiptPath'],
            '--run-context',str(Path(value['laneRoot'])/'run-context.json'),
            '--contract-dir',b['contractDir']],
            stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
            start_new_session=True,close_fds=True,env=env)
        self.state='active'; self._next_receipt(1)

    def _baseline_collector(self):
        self.call_no_scale('baseline-start')
        while self.call_no_scale('baseline-status')['status'] != 'PREPARED': pass
        self.call_no_scale('collector-launch')
        self.call_no_scale('collector-status')

    def start_no_scale(self):
        try:
            require(self.state == 'new')
            require(load_bindings(self.binding_path) == self.bindings,'source')
            self._start('no-scale',75)
            self._baseline_collector()
            return copy.deepcopy(self.receipt)
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
        require(type(n) is dict and set(n) == {'status','evidenceSha256'} and n['status'] == 'PASS' and sha(n['evidenceSha256']))
        require(type(cost) is dict and set(cost) == {'day','net','reserve','allowance','ceiling'})
        require(all(lane.finite_number(cost[k]) and cost[k] >= 0 for k in ('net','reserve','allowance','ceiling')))
        require(cost['ceiling'] == 1800 and cost['net']+cost['reserve']+cost['allowance'] <= 1800)
        require(cost['day'] == self._china_now().date().isoformat())
        return value

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
        try:
            require(self.state == 'new')
            require(load_bindings(self.binding_path) == self.bindings,'source')
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
            self.call_no_scale('collector-status')
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
            self.state='pending-bound'
            return copy.deepcopy(proof)
        except BaseException:
            self.close(); raise

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

    def finish_no_scale(self, feed_exit, assessment):
        """Close once, preserving assessor FAIL/INCONCLUSIVE as well as PASS."""
        try:
            require(self.state == 'active' and self.collector_identity is not None,'incomplete')
            require(isinstance(self.feed,Popen),'child')
            code=self.feed.poll(); require(type(code) is int,'child')
            validate_finish_receipts(feed_exit,assessment,'no-scale')
            require(feed_exit['pid'] == self.feed.pid and feed_exit['exitCode'] == code,'child')
            final=read_json(self.bindings['laneBindings']['receiptPath'])
            require(type(final) is dict and set(final) == lane.RECEIPT_FIELDS,'receipt')
            require(final['feedSession'] == self.receipt['feedSession'] and
                    final['mode'] == 'disabled-rehearsal' and type(final['desired']) is int
                    and final['desired'] == 2,'receipt')
            require(type(final['sequence']) is int and final['sequence'] >= self.receipt['sequence'],'receipt')
            require(lane.finite_number(final['observedAt']) and
                    0 <= time.time()-final['observedAt'] < 30,'receipt')
            require(feed_exit['feedSession'] == final['feedSession'] and
                    feed_exit['sequence'] == final['sequence'],'receipt')
            require(code == 0 or assessment['status'] != 'PASS','child')
            require(feed_exit['category'] == 'ok' or assessment['status'] != 'PASS','child')
            snapshot=self.client.finish(self.feed,copy.deepcopy(feed_exit),copy.deepcopy(assessment))
            require(snapshot['runState'] == 'finished' and snapshot['assessment'] == assessment,'incomplete')
            self.state='finished'
            return copy.deepcopy(assessment)
        except BaseException:
            self.close(); raise
