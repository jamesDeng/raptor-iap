"""Fake process boundaries; never run provider/Workbench/Alibaba commands."""
import copy
import hashlib
import importlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
import uuid
from unittest.mock import Mock, patch

from pgcat_lane_protocol import Refused


class ControllerTests(unittest.TestCase):
    def setUp(self):
        try:
            self.module = importlib.import_module('pgcat_rehearsal_controller')
        except ImportError:
            self.fail('missing tracked Controller/load_bindings interface')
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name); self.base.chmod(0o700)
        self.capture = self.base/'capture'; here = Path(__file__).resolve().parent
        self.manifest = self.base/'binding.json'
        self.cli = self.base/'aliyun'; self.cli.write_text('not executable by tests'); self.cli.chmod(0o700)
        lane = dict(workbenchCli=str(self.cli), aliyunCli=str(self.cli),
            laneWrapper=str(here/'pgcat_workbench_lane.py'), providerFeedPath=str(here/'provider_feed.py'),
            receiptPath=str(self.capture/'status.json'), laneLockPath=str(self.capture/'lane.lock'),
            laneIntentDir=str(self.capture/'intents'), phaseWorker='/fresh-worker/phase.py',
            pendingWorker='/fresh-worker/pending.py', remoteRun='/fresh-worker/run',
            contractDir='/fresh-worker/contract', scaleoutIntentPath=str(self.capture/'scaleout.intent'),
            preScaleProofPath=str(self.capture/'proof.json'),
            target=dict(accountId='1360282071200743', regionId='ap-southeast-1',
                workerId='i-t4nj1bitmcz7cuoch82h', scalingGroupId='asg-t4necb4dfmu6o3z2rn0g',
                nlbServerGroupId='sgp-xs5s42tihtl6ja2a67', originalInstanceIds=['original-one','original-two']))
        names = dict(laneWrapper='pgcat_workbench_lane.py', providerFeed='provider_feed.py',
            laneProtocol='pgcat_lane_protocol.py', laneSupervisor='pgcat_lane_supervisor.py', laneActions='pgcat_lane_actions.py')
        lane['sourceHashes'] = {k:hashlib.sha256((here/v).read_bytes()).hexdigest() for k,v in names.items()}
        self.binding = dict(laneBindings=lane, laneRoot=str(self.capture/'lane'), captureRoot=str(self.capture),
            workerInventory='/fresh-worker/inventory.json', workerKubeconfig='/fresh-worker/kubeconfig',
            sourceHashes={k:hashlib.sha256((here/v).read_bytes()).hexdigest() for k,v in
                dict(controller='pgcat_rehearsal_controller.py',client='pgcat_lane_client.py',runner='pgcat_lane_runner.py').items()})
        self.write_binding()
        self.session = str(uuid.uuid4()); self.seq = 0; self.clock = 1000.; self.events = []
        self.feed = Mock(spec=subprocess.Popen); self.feed.pid = 123; self.feed.poll.return_value = None
        self.client = Mock(); self.client.call.side_effect = self.call
        self.client.finish.side_effect = lambda *args: dict(runState='finished',assessment=copy.deepcopy(args[2]))
        self.auto_publish = True; self.die_on_call = False; self.result_override = None
        self.addCleanup(patch.stopall)
        patch.object(self.module.LaneClient, 'start', side_effect=self.start_client).start()
        self.spawn = patch.object(self.module.subprocess, 'Popen', side_effect=self.start_feed).start()
        patch.object(self.module, 'monotonic', side_effect=lambda:self.clock).start()
        patch.object(self.module.time, 'sleep', side_effect=self.sleep).start()

    def write_binding(self):
        self.manifest.write_text(json.dumps(self.binding)); self.manifest.chmod(0o600)

    def publish(self, session=None, desired=2):
        self.seq += 1
        p = Path(self.binding['laneBindings']['receiptPath'])
        p.write_text(json.dumps(dict(feedSession=session or self.session, sequence=self.seq,
            desired=desired, mode='disabled-rehearsal', observedAt=time.time())))
        p.chmod(0o600)

    def sleep(self, seconds):
        self.clock += seconds

    def start_client(self, bindings, mode, root):
        self.events.append(('client', mode)); self.assertEqual(mode, 'no-scale')
        self.assertEqual(bindings,self.binding['laneBindings']); return self.client

    def start_feed(self, args, **kwargs):
        self.events.append(('feed',args,kwargs)); self.publish(); return self.feed

    def call(self, action, receipt, deadline):
        self.events.append(('call',action,receipt['sequence'],deadline))
        if self.die_on_call: self.feed.poll.return_value = 1
        if self.auto_publish: self.publish()
        payload = {'baseline-start':dict(status='STARTED',phase='baseline'),
            'baseline-status':dict(status='PREPARED',phase='baseline'),
            'collector-launch':dict(status='LAUNCHED'),
            'collector-status':dict(status='ALIVE',identity=dict(pid=9,bootId='boot',startTicks=1,commandSha256='a'*64))}[action]
        return self.result_override or dict(state='complete',category='ok',payload=payload)

    def controller(self):
        return self.module.Controller(self.manifest)

    def test_private_fresh_binding_and_exact_keys(self):
        for mutation in ('mode','extra','missing','used','symlink'):
            with self.subTest(mutation=mutation):
                self.write_binding()
                if mutation == 'mode': self.manifest.chmod(0o644)
                if mutation == 'extra':
                    value=copy.deepcopy(self.binding); value['command']='arbitrary'; self.manifest.write_text(json.dumps(value))
                if mutation == 'missing':
                    value=copy.deepcopy(self.binding); del value['workerKubeconfig']; self.manifest.write_text(json.dumps(value))
                if mutation == 'used': self.capture.mkdir(mode=0o700)
                if mutation == 'symlink':
                    self.manifest.rename(self.base/'real'); self.manifest.symlink_to(self.base/'real')
                with self.assertRaises(Refused): self.controller().start_no_scale()
                self.assertEqual(self.spawn.call_count,0)
                if mutation == 'used': self.capture.rmdir()
                if mutation == 'symlink': self.manifest.unlink()

    def test_source_mismatch_before_any_process(self):
        for section,key in [('sourceHashes','controller'),('sourceHashes','client'),('sourceHashes','runner'),
                            ('laneBindings','providerFeed')]:
            with self.subTest(key=key):
                value=copy.deepcopy(self.binding)
                hashes=value[section] if section=='sourceHashes' else value[section]['sourceHashes']
                hashes[key]='0'*64; self.manifest.write_text(json.dumps(value))
                with self.assertRaises(Refused): self.controller().start_no_scale()
                self.assertEqual(self.events,[])

    def test_feed_cli_and_provider_target_cannot_differ_from_bindings(self):
        for change in ('cli','nlb'):
            with self.subTest(change=change):
                value=copy.deepcopy(self.binding)
                if change == 'cli':
                    other=self.base/'different-cli'; other.write_text('fake'); other.chmod(0o700)
                    value['laneBindings']['aliyunCli']=str(other)
                else: value['laneBindings']['target']['nlbServerGroupId']='other-group'
                self.manifest.write_text(json.dumps(value))
                with self.assertRaises(Refused): self.module.load_bindings(self.manifest)
                self.assertEqual(self.events,[])

    def test_feed_cli_resolution_must_match_exact_bound_inode(self):
        import shutil
        with patch.object(shutil,'which',return_value='/different/aliyun'):
            with self.assertRaises(Refused): self.module.load_bindings(self.manifest)
        self.assertEqual(self.events,[])

    def test_feed_cli_symlink_refuses(self):
        self.cli.rename(self.base/'cli-target'); self.cli.symlink_to(self.base/'cli-target')
        with self.assertRaises(Refused): self.module.load_bindings(self.manifest)
        self.assertEqual(self.events,[])

    def test_normal_fixed_actions_and_shared_clock_deadlines(self):
        controller=self.controller(); controller.start_no_scale()
        self.assertEqual(self.events[0],('client','no-scale'))
        args,options=self.events[1][1:]
        for flag,want in [('--max-seconds','75'),('--interval','15'),('--mode','disabled-rehearsal')]:
            self.assertEqual(args[args.index(flag)+1],want)
        self.assertTrue(options['start_new_session']); self.assertTrue(options['close_fds'])
        self.assertEqual(options['env']['PGCAT_LANE_ROLE'],'provider')
        calls=[e for e in self.events if e[0]=='call']
        self.assertEqual([e[1] for e in calls],['baseline-start','baseline-status','collector-launch','collector-status'])
        self.assertEqual([e[2] for e in calls],[1,2,3,4]); self.assertEqual(controller.receipt['sequence'],5)
        self.assertTrue(all(1000 < e[3] <= 1005 for e in calls))
        with self.assertRaises(Refused): controller.start_no_scale()

    def test_no_successor_blocks_all_later_calls(self):
        self.auto_publish=False; controller=self.controller()
        with self.assertRaises(Refused): controller.start_no_scale()
        self.assertEqual(len([e for e in self.events if e[0]=='call']),1)
        self.assertTrue(self.client.close.called)
        with self.assertRaises(Refused): controller.start_no_scale()

    def test_dead_feed_during_call_cannot_continue(self):
        self.die_on_call=True
        with self.assertRaises(Refused): self.controller().start_no_scale()
        self.assertEqual(len([e for e in self.events if e[0]=='call']),1)
        self.assertTrue(self.client.close.called)

    def test_changed_session_and_desired_refuse(self):
        original=self.call
        for change in ('session','desired'):
            with self.subTest(change=change):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                def changed(*args):
                    result=original(*args)
                    self.publish(session=str(uuid.uuid4()) if change=='session' else None,
                                 desired=3 if change=='desired' else 2)
                    return result
                self.client.call.side_effect=changed
                with self.assertRaises(Refused): self.controller().start_no_scale()

    def test_uncertain_result_and_interrupt_disconnect_owner(self):
        for outcome in (dict(state='uncertain',category='timeout',payload={}),KeyboardInterrupt()):
            with self.subTest(outcome=type(outcome).__name__):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                if isinstance(outcome,BaseException): self.client.call.side_effect=outcome
                else: self.result_override=outcome
                with self.assertRaises((Refused,KeyboardInterrupt)): self.controller().start_no_scale()
                self.assertTrue(self.client.close.called)
                self.assertFalse(self.feed.kill.called)

    def test_no_scale_action_gate_rejects_mutations(self):
        controller=self.controller(); controller.start_no_scale(); before=self.client.call.call_count
        for action in ('scaleout-two-to-three','pending-start','zero-gate-start','reconcile-close','arbitrary'):
            with self.assertRaises(Refused): controller.call_no_scale(action)
        self.assertEqual(self.client.call.call_count,before)

    def finish_receipts(self, controller, status):
        seq=controller.receipt['sequence']
        return (dict(pid=self.feed.pid,exitCode=0,category='ok',feedSession=self.session,sequence=seq),
            dict(status=status,mode='no-scale',feedSession=self.session,endSequence=seq,evidenceSha256='e'*64))

    def test_finish_preserves_independent_fail_and_inconclusive(self):
        for status in ('FAIL','INCONCLUSIVE'):
            with self.subTest(status=status):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                self.feed.poll.return_value=None
                controller=self.controller(); controller.start_no_scale(); self.feed.poll.return_value=0
                receipts=self.finish_receipts(controller,status)
                self.assertEqual(controller.finish_no_scale(*receipts),receipts[1])
                with self.assertRaises(Refused): controller.finish_no_scale(*receipts)

    def test_finish_rejects_wrong_process_session_sequence_and_live_feed(self):
        controller=self.controller(); controller.start_no_scale(); exit_,assessment=self.finish_receipts(controller,'PASS')
        with self.assertRaises(Refused): controller.finish_no_scale(exit_,assessment)
        self.assertFalse(self.client.finish.called)

    def test_final_assessment_cannot_omit_later_publication(self):
        controller=self.controller(); controller.start_no_scale(); self.feed.poll.return_value=0
        receipts=self.finish_receipts(controller,'PASS'); self.publish()
        with self.assertRaises(Refused): controller.finish_no_scale(*receipts)
        self.assertFalse(self.client.finish.called)

    def test_finish_accepts_exact_fresh_final_publication(self):
        controller=self.controller(); controller.start_no_scale(); self.feed.poll.return_value=0
        self.publish(); exit_,assessment=self.finish_receipts(controller,'INCONCLUSIVE')
        exit_['sequence']=self.seq; assessment['endSequence']=self.seq
        self.assertEqual(controller.finish_no_scale(exit_,assessment),assessment)

    def test_finish_refuses_stale_final_publication(self):
        controller=self.controller(); controller.start_no_scale(); self.feed.poll.return_value=0
        receipts=self.finish_receipts(controller,'PASS')
        p=Path(self.binding['laneBindings']['receiptPath']); value=json.loads(p.read_text())
        value['observedAt']=time.time()-31; p.write_text(json.dumps(value))
        with self.assertRaises(Refused): controller.finish_no_scale(*receipts)
        self.assertFalse(self.client.finish.called)

    def test_finish_exact_pid_and_exit_code(self):
        controller=self.controller(); controller.start_no_scale(); self.feed.poll.return_value=0
        exit_,assessment=self.finish_receipts(controller,'PASS'); exit_['pid'] += 1
        with self.assertRaises(Refused): controller.finish_no_scale(exit_,assessment)
        self.assertFalse(self.client.finish.called)

    def test_collector_identity_change_refuses(self):
        controller=self.controller(); controller.start_no_scale()
        self.result_override=dict(state='complete',category='ok',payload=dict(status='ALIVE',
            identity=dict(pid=10,bootId='boot',startTicks=1,commandSha256='a'*64)))
        with self.assertRaises(Refused): controller.call_no_scale('collector-status')
        self.assertTrue(self.client.close.called)

    def test_controller_death_leaves_liveness_eof(self):
        # Fake LaneClient holds a real OS pipe: process death must not leave an
        # apparent live owner. Supervisor EOF/journal behavior has its own tests.
        reader,writer=os.pipe(); pid=os.fork()
        if pid == 0:
            os.close(reader)
            self.client.close.side_effect=lambda:os.close(writer)
            self.client.call.side_effect=lambda *args:os._exit(9)
            try: self.controller().start_no_scale()
            finally: os._exit(10)
        os.close(writer)
        try:
            _,status=os.waitpid(pid,0)
            self.assertEqual(os.waitstatus_to_exitcode(status),9)
            self.assertEqual(os.read(reader,1),b'')
            self.assertFalse((self.capture/'lane'/'finish.ready').exists())
        finally: os.close(reader)


if __name__ == '__main__': unittest.main()
