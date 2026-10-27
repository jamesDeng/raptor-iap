"""Tracked no-scale orchestration. Terminal text is never completion evidence.

No executable CLI entry point: a reviewed private binding and an independent
assessment are required by the caller. The historical local helper is unused.
"""
import copy
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
    sha, monotonic, validate_result, validate_finish_receipts)
from pgcat_lane_supervisor import read_json
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
        require(inventory != kubeconfig)
        here=Path(__file__).resolve().parent
        _hashes(b['sourceHashes'],dict(laneWrapper=Path(b['laneWrapper']),
            providerFeed=Path(b['providerFeedPath']),laneProtocol=here/'pgcat_lane_protocol.py',
            laneSupervisor=here/'pgcat_lane_supervisor.py',laneActions=here/'pgcat_lane_actions.py'))
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
        self.state='new'; self.collector_identity=None

    def close(self):
        """Disconnect the owner on interruption; retain the bounded feed handle.

        EOF before durable finish makes the supervisor uncertain. Do not signal
        numeric PIDs, clear its global record, retry, or claim remote cleanup.
        """
        if self.state != 'finished': self.state='uncertain'
        if self.client is not None: self.client.close()

    def _live(self):
        require(self.state == 'active' and self.feed is not None and self.feed.poll() is None,'child')

    def _next_receipt(self, minimum, observed_after=None):
        deadline=monotonic()+45.
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
                    and current['desired'] == 2,'receipt')
            require(type(current['sequence']) is int and current['sequence'] > 0,'receipt')
            require(lane.finite_number(current['observedAt']),'receipt')
            # A higher sequence can have been observed during the worker call.
            # Only a later observation establishes post-call provider progress.
            if (current['sequence'] >= minimum and
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
            if action == 'baseline-start':
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

    def start_no_scale(self):
        try:
            require(self.state == 'new')
            require(load_bindings(self.binding_path) == self.bindings,'source')
            self.state='starting'
            value=self.bindings; b=value['laneBindings']
            Path(value['captureRoot']).mkdir(mode=0o700)
            Path(b['laneIntentDir']).mkdir(mode=0o700)
            self.client=LaneClient.start(copy.deepcopy(b),'no-scale',Path(value['laneRoot']))
            env={'PATH':_feed_path(b),
                 'HOME':os.path.expanduser('~'),'LANG':'C','LC_ALL':'C'}
            for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
                        'http_proxy','https_proxy','all_proxy','no_proxy'):
                if key in os.environ: env[key]=os.environ[key]
            env.update(PGCAT_LANE_ROLE='provider',PGCAT_LANE_REAL_CLI=b['workbenchCli'],
                       PGCAT_LANE_LOCK=b['laneLockPath'])
            self.feed=subprocess.Popen([sys.executable,'-u',b['providerFeedPath'],
                '--mode','disabled-rehearsal','--interval','15','--max-seconds','75',
                '--workbench',b['laneWrapper'],'--worker-inventory',value['workerInventory'],
                '--kubeconfig',value['workerKubeconfig'],'--status-file',b['receiptPath']],
                stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
                start_new_session=True,close_fds=True,env=env)
            self.state='active'; self._next_receipt(1)
            self.call_no_scale('baseline-start')
            while self.call_no_scale('baseline-status')['status'] != 'PREPARED': pass
            self.call_no_scale('collector-launch')
            self.call_no_scale('collector-status')
            return copy.deepcopy(self.receipt)
        except BaseException:
            self.close(); raise

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
