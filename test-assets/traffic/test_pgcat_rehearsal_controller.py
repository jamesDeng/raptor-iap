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

from pgcat_lane_protocol import Refused, encode, roster_sha, expected_roster, removing_roster_shas


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
            laneIntentDir=str(self.capture/'intents'),
            phaseWorker='/fresh-worker/run/pgcat_rehearsal_worker.py',
            pendingWorker='/fresh-worker/run/pgcat_rehearsal_worker.py',
            pendingProofWorker='/fresh-worker/run/pgcat_pending_proof_worker.py',
            preDrainProofWorker='/fresh-worker/run/pgcat_pre_drain_proof_worker.py',
            zeroProofWorker='/fresh-worker/run/pgcat_zero_proof_worker.py',
            workerKubeconfig='/fresh-worker/kubeconfig', remoteRun='/fresh-worker/run',
            contractDir='/fresh-worker/contract', scaleoutIntentPath=str(self.capture/'scaleout.intent'),
            nlbIntentPath=str(self.capture/'nlb-deregister.intent'),
            scaleinIntentPath=str(self.capture/'scalein.intent'),
            preScaleProofPath=str(self.capture/'proof.json'),
            target=dict(accountId='1360282071200743', regionId='ap-southeast-1',
                workerId='i-t4nj1bitmcz7cuoch82h', scalingGroupId='asg-t4necb4dfmu6o3z2rn0g',
                nlbServerGroupId='sgp-xs5s42tihtl6ja2a67', originalInstanceIds=['original-one','original-two']))
        names = dict(laneWrapper='pgcat_workbench_lane.py', providerFeed='provider_feed.py',
            laneProtocol='pgcat_lane_protocol.py', laneSupervisor='pgcat_lane_supervisor.py',
            laneActions='pgcat_lane_actions.py',pendingProofWorker='pgcat_pending_proof_worker.py',
            preDrainProofWorker='pgcat_pre_drain_proof_worker.py',
            zeroProofWorker='pgcat_zero_proof_worker.py',
            rehearsalWorker='pgcat_rehearsal_worker.py',
            rehearsalContract='pgcat_rehearsal_contract.py',
            captureWindow='capture_window.py', finishClients='finish_clients.py',
            workerCapture='worker_capture.py', segmentedCapture='segmented_capture.py',
            collectorControl='pgcat_collector_control.py')
        lane['sourceHashes'] = {k:hashlib.sha256((here/v).read_bytes()).hexdigest() for k,v in names.items()}
        self.binding = dict(laneBindings=lane, laneRoot=str(self.capture/'lane'), captureRoot=str(self.capture),
            workerInventory='/fresh-worker/run/provider-inventory.json', workerKubeconfig='/fresh-worker/kubeconfig',
            sourceHashes={k:hashlib.sha256((here/v).read_bytes()).hexdigest() for k,v in
                dict(controller='pgcat_rehearsal_controller.py',client='pgcat_lane_client.py',runner='pgcat_lane_runner.py').items()})
        self.write_binding()
        self.session = str(uuid.uuid4()); self.seq = 0; self.clock = 1000.; self.events = []
        self.feed = Mock(spec=subprocess.Popen); self.feed.pid = 123; self.feed.poll.return_value = None
        self.client = Mock(); self.client.call.side_effect = self.call
        self.client.finish.side_effect = lambda *args: dict(runState='finished',assessment=copy.deepcopy(args[2]))
        self.auto_publish = True; self.die_on_call = False; self.result_override = None
        self.pending_publish = False; self.wall = time.time()
        self.addCleanup(patch.stopall)
        patch.object(self.module.LaneClient, 'start', side_effect=self.start_client).start()
        self.spawn = patch.object(self.module.subprocess, 'Popen', side_effect=self.start_feed).start()
        patch.object(self.module, 'monotonic', side_effect=lambda:self.clock).start()
        patch.object(self.module.time, 'sleep', side_effect=self.sleep).start()
        patch.object(self.module.time, 'time', side_effect=lambda:self.wall).start()

    def write_binding(self):
        self.manifest.write_text(json.dumps(self.binding)); self.manifest.chmod(0o600)

    def test_worker_kubeconfig_binding_must_match_feed_binding(self):
        value=copy.deepcopy(self.binding)
        value['laneBindings']['workerKubeconfig']='/fresh-worker/other-kubeconfig'
        self.manifest.write_text(json.dumps(value))
        with self.assertRaises(Refused):
            self.module.load_bindings(self.manifest)

    def publish(self, session=None, desired=2):
        self.seq += 1
        p = Path(self.binding['laneBindings']['receiptPath'])
        p.write_text(json.dumps(dict(feedSession=session or self.session, sequence=self.seq,
            desired=desired, mode='disabled-rehearsal', observedAt=time.time(),
            rosterSha256='a'*64)))
        p.chmod(0o600)
        os.utime(p,(self.wall,self.wall))

    def sleep(self, seconds):
        self.clock += seconds; self.wall += seconds
        if self.pending_publish:
            self.pending_publish = False
            self.publish()

    def start_client(self, bindings, mode, root):
        self.events.append(('client', mode)); self.assertEqual(mode, 'no-scale')
        self.assertEqual(bindings,self.binding['laneBindings']); return self.client

    def start_feed(self, args, **kwargs):
        self.events.append(('feed',args,kwargs)); self.publish(); return self.feed

    def call(self, action, receipt, deadline):
        self.events.append(('call',action,receipt['sequence'],deadline))
        if self.die_on_call: self.feed.poll.return_value = 1
        if self.auto_publish: self.pending_publish = True
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

    def test_feed_source_change_during_supervisor_start_refuses_feed_launch(self):
        source=Path(self.binding['laneBindings']['providerFeedPath'])
        staged=self.base/'provider-feed.py'; staged.write_bytes(source.read_bytes())
        self.binding['laneBindings']['providerFeedPath']=str(staged)
        self.binding['laneBindings']['sourceHashes']['providerFeed']=hashlib.sha256(staged.read_bytes()).hexdigest()
        self.write_binding()
        def change(bindings,mode,root):
            client=self.start_client(bindings,mode,root)
            staged.write_bytes(staged.read_bytes()+b'\n# changed after supervisor startup\n')
            return client
        with patch.object(self.module.LaneClient,'start',side_effect=change):
            with self.assertRaises(Refused):self.controller().start_no_scale()
        self.assertEqual(self.spawn.call_count,0)

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

    def test_contract_source_cannot_be_placed_in_renamed_capture_tree(self):
        value=copy.deepcopy(self.binding)
        value['laneBindings']['contractDir']=value['laneBindings']['remoteRun']+'/retained-evidence'
        self.manifest.write_text(json.dumps(value))
        with self.assertRaises(Refused):self.module.load_bindings(self.manifest)
        self.assertEqual(self.events,[])

    def test_fresh_worker_run_and_contract_siblings_are_accepted(self):
        value=copy.deepcopy(self.binding)
        root='/root/pgcat-fresh-worker'
        value['laneBindings']['remoteRun']=root+'/run'
        value['laneBindings']['contractDir']=root+'/contract'
        value['laneBindings']['phaseWorker']=root+'/run/pgcat_rehearsal_worker.py'
        value['laneBindings']['pendingWorker']=root+'/run/pgcat_rehearsal_worker.py'
        value['laneBindings']['pendingProofWorker']=root+'/run/pgcat_pending_proof_worker.py'
        value['laneBindings']['preDrainProofWorker']=root+'/run/pgcat_pre_drain_proof_worker.py'
        value['laneBindings']['zeroProofWorker']=root+'/run/pgcat_zero_proof_worker.py'
        value['workerInventory']=root+'/run/provider-inventory.json'
        self.manifest.write_text(json.dumps(value))
        self.assertEqual(self.module.load_bindings(self.manifest)['workerInventory'],
                         root+'/run/provider-inventory.json')

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
        self.assertEqual(args[args.index('--run-context')+1],
                         str(Path(self.binding['laneRoot'])/'run-context.json'))
        self.assertEqual(args[args.index('--contract-dir')+1],
                         self.binding['laneBindings']['contractDir'])
        self.assertTrue(options['start_new_session']); self.assertTrue(options['close_fds'])
        self.assertEqual(options['env']['PGCAT_LANE_ROLE'],'provider')
        calls=[e for e in self.events if e[0]=='call']
        self.assertEqual([e[1] for e in calls],['baseline-start','baseline-status','collector-launch','collector-status'])
        self.assertEqual([e[2] for e in calls],[1,2,3,4]); self.assertEqual(controller.receipt['sequence'],5)
        for index,event in enumerate(calls):
            self.assertAlmostEqual(event[3],1005.+index*.05)
        with self.assertRaises(Refused): controller.start_no_scale()

    def test_successor_must_be_observed_strictly_after_call_completion(self):
        controller=self.controller(); controller.start_no_scale()
        for during_call_delay in (.1,0.):
            with self.subTest(during_call_delay=during_call_delay):
                before=controller.receipt['sequence']
                def publish_during_call(*args):
                    result=self.call(*args)  # Schedules the real post-call publication.
                    self.publish()  # Higher sequence, but before/equal to completion.
                    self.wall += during_call_delay
                    return result
                self.client.call.side_effect=publish_during_call
                controller.call_no_scale('collector-status')
                self.assertEqual(controller.receipt['sequence'],before+2)
                self.assertFalse(self.pending_publish)

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
        for action in ('scaleout-two-to-three','pending-start','pending-proof','zero-gate-start','reconcile-close','arbitrary'):
            with self.assertRaises(Refused): controller.call_no_scale(action)
        self.assertEqual(self.client.call.call_count,before)

    def test_pending_binding_checks_reviewed_source_before_successor(self):
        controller=self.controller()
        controller.client=self.client; controller.feed=self.feed
        controller.mode='disabled-rehearsal'; controller.state='scaled-three'
        controller.receipt=dict(feedSession=self.session,sequence=4,desired=3,
                                observedAt=self.wall,mode='disabled-rehearsal')
        self.client.run_id=str(uuid.uuid4())
        proof=dict(runId=self.client.run_id,feedSession=self.session,
            newId='new-node',originalInstanceIds=self.binding['laneBindings']['target']['originalInstanceIds'],
            sequence=3,observedAt=self.wall,resourceVersion='11',
            clientRosterSha256='b'*64,evidenceSha256='c'*64,sourceSha256='0'*64)
        staged=[dict(status='STARTED'),dict(status='PREPARED'),proof]
        self.client.call.side_effect=lambda action,*args: dict(state='complete',category='ok',payload=staged.pop(0))
        with patch.object(controller,'_next_receipt') as successor:
            with self.assertRaises(Refused): controller.bind_pending()
            self.assertEqual(successor.call_count,2)
        self.assertEqual([call.args[0] for call in self.client.call.call_args_list],
                         ['pending-start','pending-status','pending-proof'])
        self.assertTrue(self.client.close.called)

    def test_pending_binding_only_after_prepared_and_newer_publication(self):
        controller=self.controller()
        controller.client=self.client; controller.feed=self.feed
        controller.mode='disabled-rehearsal'; controller.state='scaled-three'
        controller.receipt=dict(feedSession=self.session,sequence=4,desired=3,
                                observedAt=self.wall,mode='disabled-rehearsal')
        self.client.run_id=str(uuid.uuid4())
        proof=dict(runId=self.client.run_id,feedSession=self.session,newId='new-node',
            originalInstanceIds=self.binding['laneBindings']['target']['originalInstanceIds'],
            sequence=3,observedAt=self.wall,resourceVersion='11',
            clientRosterSha256='b'*64,evidenceSha256='c'*64,
            sourceSha256=self.binding['laneBindings']['sourceHashes']['pendingProofWorker'])
        outputs=[dict(status='STARTED'),dict(status='RUNNING'),dict(status='PREPARED'),proof]
        self.client.call.side_effect=lambda action,*args: dict(state='complete',category='ok',payload=outputs.pop(0))
        with patch.object(controller,'_next_receipt') as successor:
            self.assertEqual(controller.bind_pending(),proof)
            self.assertEqual(successor.call_count,4)
            self.assertEqual(successor.call_args.kwargs['desired'],3)
        self.assertEqual([call.args[0] for call in self.client.call.call_args_list],
                         ['pending-start','pending-status','pending-status','pending-proof'])
        self.assertEqual(controller.state,'pending-bound')
        with self.assertRaises(Refused): controller.bind_pending()

    def nlb_controller(self):
        controller=self.controller();self.capture.mkdir(mode=0o700)
        controller.client=self.client;controller.feed=self.feed
        controller.mode='disabled-rehearsal';controller.state='pre-drain-bound'
        self.client.run_id=str(uuid.uuid4())
        original=self.binding['laneBindings']['target']['originalInstanceIds']
        pending=dict(runId=self.client.run_id,feedSession=self.session,newId='new-node',
            originalInstanceIds=original,sequence=3,observedAt=self.wall-2,
            resourceVersion='11',clientRosterSha256='b'*64,evidenceSha256='c'*64,
            sourceSha256=self.binding['laneBindings']['sourceHashes']['pendingProofWorker'])
        pre=dict(runId=self.client.run_id,feedSession=self.session,newId='new-node',
            originalInstanceIds=original,pendingBindingSha256=hashlib.sha256(encode(pending)).hexdigest(),
            sequence=4,observedAt=self.wall-1,resourceVersion='12',
            clientRosterSha256='b'*64,evidenceSha256='d'*64,
            sourceSha256=self.binding['laneBindings']['sourceHashes']['preDrainProofWorker'])
        controller.pending_proof=pending;controller.pre_drain_proof=pre
        three=roster_sha(expected_roster(original,'new-node',registered=True))
        absent=roster_sha(expected_roster(original,'new-node',registered=False))
        controller.receipt=dict(feedSession=self.session,sequence=5,desired=3,
            observedAt=self.wall,mode='disabled-rehearsal',rosterSha256=three)
        self.client.call.side_effect=None
        self.client.call.return_value=dict(state='complete',category='ok',
                                           payload=dict(requestId='request-ok',jobId='job-ok'))
        return controller,three,absent

    def test_nlb_ack_needs_later_absence_publication(self):
        controller,three,absent=self.nlb_controller()
        later=dict(controller.receipt,sequence=6,observedAt=self.wall+1,rosterSha256=three)
        gone=dict(later,sequence=7,observedAt=self.wall+2,rosterSha256=absent)
        with patch.object(controller,'_next_receipt',side_effect=[later,gone]) as next_receipt:
            self.assertEqual(controller.deregister_new_node(),gone)
        self.assertEqual(controller.state,'nlb-absent')
        self.assertEqual(next_receipt.call_count,2)
        self.assertEqual([call.args[0] for call in self.client.call.call_args_list],['deregister-new-node'])
        intent=json.loads(Path(self.binding['laneBindings']['nlbIntentPath']).read_text())
        self.assertEqual(intent['newId'],'new-node')
        self.assertEqual(intent['rosterSha256'],three)
        self.assertEqual(intent['preDrainProofSha256'],hashlib.sha256(encode(controller.pre_drain_proof)).hexdigest())
        with self.assertRaises(Refused):controller.deregister_new_node()

    def test_nlb_removing_then_absent_is_valid(self):
        controller,three,absent=self.nlb_controller()
        originals=self.binding['laneBindings']['target']['originalInstanceIds']
        roster=expected_roster(originals,'new-node',registered=True)
        roster['healthyRegistered']=sorted(originals)
        candidate=next(row for row in roster['backendStates'] if row['id']=='new-node')
        candidate.update(status='Removing',weight=0,listenerHealth='Unavailable')
        removing=roster_sha(roster)
        observations=[dict(controller.receipt,sequence=6,observedAt=self.wall+1,rosterSha256=three),
            dict(controller.receipt,sequence=7,observedAt=self.wall+2,rosterSha256=removing),
            dict(controller.receipt,sequence=8,observedAt=self.wall+3,rosterSha256=absent)]
        with patch.object(controller,'_next_receipt',side_effect=observations) as next_receipt:
            self.assertEqual(controller.deregister_new_node(),observations[-1])
        self.assertEqual(next_receipt.call_count,3)

    def test_nlb_removing_cannot_return_to_available(self):
        controller,three,_=self.nlb_controller()
        originals=self.binding['laneBindings']['target']['originalInstanceIds']
        roster=expected_roster(originals,'new-node',registered=True)
        roster['healthyRegistered']=sorted(originals)
        next(row for row in roster['backendStates'] if row['id']=='new-node').update(
            status='Removing',weight=0,listenerHealth='Unavailable')
        observations=[dict(controller.receipt,sequence=6,observedAt=self.wall+1,
                           rosterSha256=roster_sha(roster)),
            dict(controller.receipt,sequence=7,observedAt=self.wall+2,rosterSha256=three)]
        with patch.object(controller,'_next_receipt',side_effect=observations):
            with self.assertRaises(Refused):controller.deregister_new_node()
        self.assertEqual(controller.state,'uncertain')

    def zero_controller(self):
        controller,_,absent=self.nlb_controller()
        controller.state='nlb-absent'
        controller.receipt=dict(controller.receipt,sequence=6,rosterSha256=absent)
        proof=dict(runId=self.client.run_id,feedSession=self.session,newId='new-node',
            originalInstanceIds=self.binding['laneBindings']['target']['originalInstanceIds'],
            preDrainBindingSha256=hashlib.sha256(encode(controller.pre_drain_proof)).hexdigest(),
            sequence=6,observedAt=self.wall,clientRosterSha256='b'*64,
            evidenceSha256='c'*64,
            sourceSha256=self.binding['laneBindings']['sourceHashes']['zeroProofWorker'])
        return controller,proof,absent

    def test_zero_proof_requires_later_absent_provider_publication(self):
        controller,proof,absent=self.zero_controller()
        self.client.call.return_value=dict(state='complete',category='ok',payload=proof)
        later=dict(controller.receipt,sequence=7,observedAt=self.wall+1,rosterSha256=absent)
        def next_receipt(*args,**kwargs):
            controller.receipt=later
            return later
        with patch.object(controller,'_next_receipt',side_effect=next_receipt) as successor:
            self.assertEqual(controller.bind_zero(),proof)
        self.assertEqual(controller.state,'zero-bound')
        self.assertEqual(successor.call_args.kwargs['observed_after'],self.wall)
        self.assertEqual(self.client.call.call_args.args[0],'zero-proof')
        with self.assertRaises(Refused): controller.bind_zero()

    def test_scalein_checks_activity_on_same_receipt_and_exact_two_convergence(self):
        controller,proof,absent=self.zero_controller()
        controller.state='zero-bound';controller.zero_proof=proof
        controller.receipt=dict(controller.receipt,sequence=7,rosterSha256=absent)
        check_id=str(uuid.uuid4())
        outputs=[dict(state='complete',category='ok',payload={'status':'CLEAR','requestId':check_id}),
                 dict(state='complete',category='ok',payload={'requestId':'ess-ack'})]
        self.client.call.side_effect=lambda *args: outputs.pop(0)
        originals=self.binding['laneBindings']['target']['originalInstanceIds']
        two=dict(members=originals,healthyRegistered=originals,
            memberStates=[dict(id=i,lifecycle='Protected',health='Healthy',addressKnown=True) for i in originals],
            backendStates=[dict(id=i,status='Available',weight=100,listenerHealth='Normal') for i in originals])
        final=dict(controller.receipt,sequence=8,desired=2,observedAt=self.wall+1,
                   rosterSha256=roster_sha(two))
        with patch.object(controller,'_next_receipt',return_value=final):
            self.assertEqual(controller.scalein_three_to_two(),final)
        self.assertEqual(controller.state,'scaled-two')
        calls=self.client.call.call_args_list
        self.assertEqual([call.args[0] for call in calls],
                         ['scaling-activity-check','scalein-three-to-two'])
        self.assertEqual(calls[0].args[1],calls[1].args[1])
        intent=json.loads(Path(self.binding['laneBindings']['scaleinIntentPath']).read_text())
        self.assertEqual(intent['activityRequestId'],check_id)
        self.assertEqual(intent['zeroProofSha256'],hashlib.sha256(encode(proof)).hexdigest())

    def test_slow_activity_refuses_before_ess_child(self):
        controller,proof,absent=self.zero_controller()
        controller.state='zero-bound';controller.zero_proof=proof
        controller.receipt=dict(controller.receipt,sequence=7,rosterSha256=absent)
        check_id=str(uuid.uuid4())
        def slow(action,*args):
            self.assertEqual(action,'scaling-activity-check')
            self.wall += 4
            return dict(state='complete',category='ok',
                        payload={'status':'CLEAR','requestId':check_id})
        self.client.call.side_effect=slow
        with self.assertRaises(Refused): controller.scalein_three_to_two()
        self.assertEqual(self.client.call.call_count,1)
        self.assertFalse(Path(self.binding['laneBindings']['scaleinIntentPath']).exists())

    def test_scalein_wrong_final_roster_is_uncertain(self):
        controller,proof,absent=self.zero_controller()
        controller.state='zero-bound';controller.zero_proof=proof
        controller.receipt=dict(controller.receipt,sequence=7,rosterSha256=absent)
        outputs=[dict(state='complete',category='ok',payload={'status':'CLEAR',
                 'requestId':str(uuid.uuid4())}),
                 dict(state='complete',category='ok',payload={'requestId':'ess-ack'})]
        self.client.call.side_effect=lambda *args: outputs.pop(0)
        wrong=dict(controller.receipt,sequence=8,desired=2,
                   observedAt=self.wall+1,rosterSha256='f'*64)
        with patch.object(controller,'_next_receipt',return_value=wrong):
            with self.assertRaises(Refused): controller.scalein_three_to_two()
        self.assertEqual(controller.state,'uncertain')

    def test_removing_roster_digest_excludes_other_changes(self):
        originals=self.binding['laneBindings']['target']['originalInstanceIds']
        base=expected_roster(originals,'new-node',registered=True)
        base['healthyRegistered']=sorted(originals)
        next(row for row in base['backendStates'] if row['id']=='new-node').update(
            status='Removing',weight=0,listenerHealth='Unavailable')
        allowed=removing_roster_shas(originals,'new-node')
        self.assertIn(roster_sha(base),allowed)
        for change in ('extra','protection','health','weight','backend'):
            with self.subTest(change=change):
                altered=copy.deepcopy(base)
                if change=='extra':
                    altered['backendStates'].append(dict(id='other',status='Available',
                        weight=100,listenerHealth='Normal'))
                elif change=='protection':
                    next(row for row in altered['memberStates'] if row['id']==originals[0])['lifecycle']='InService'
                elif change=='health':
                    next(row for row in altered['memberStates'] if row['id']==originals[0])['health']='Unhealthy'
                elif change=='weight':
                    next(row for row in altered['backendStates'] if row['id']=='new-node')['weight']=50
                else:
                    next(row for row in altered['backendStates'] if row['id']==originals[0])['status']='Removing'
                self.assertNotIn(roster_sha(altered),allowed)

    def test_nlb_later_changed_roster_cannot_claim_absence(self):
        controller,_,_=self.nlb_controller()
        changed=dict(controller.receipt,sequence=6,observedAt=self.wall+1,rosterSha256='e'*64)
        with patch.object(controller,'_next_receipt',return_value=changed):
            with self.assertRaises(Refused):controller.deregister_new_node()
        self.assertEqual(controller.state,'uncertain')
        self.assertTrue(self.client.close.called)

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


class PaidStartTests(unittest.TestCase):
    setUp = ControllerTests.setUp
    write_binding = ControllerTests.write_binding
    publish = ControllerTests.publish
    controller = ControllerTests.controller

    def start_client(self, bindings, mode, root):
        self.events.append(('client', mode))
        self.assertEqual(mode, 'disabled-rehearsal')
        return self.client

    def start_feed(self, args, **kwargs):
        self.events.append(('feed', args, kwargs)); self.publish(); return self.feed

    def sleep(self, seconds):
        self.clock += seconds; self.wall += seconds
        if self.pending_publish:
            self.pending_publish = False
            self.publish(desired=3 if getattr(self, 'scaled', False) else 2)

    def call(self, action, receipt, deadline):
        self.events.append(('call', action, receipt['sequence'], deadline))
        if self.die_on_call: self.feed.poll.return_value = 1
        if self.auto_publish: self.pending_publish = True
        if action == 'scaleout-two-to-three':
            self.scaled = True
            payload = dict(requestId='provider-request')
        elif action.startswith('pre-scale-'):
            payload = dict(status='STARTED' if action.endswith('start') else 'PREPARED', phase='pre-scale')
            if action.endswith('status'):
                payload.update(feedSession=self.session, sequence=receipt['sequence'])
        else:
            payload = {'baseline-start':dict(status='STARTED',phase='baseline'),
                'baseline-status':dict(status='PREPARED',phase='baseline'),
                'collector-launch':dict(status='LAUNCHED'),
                'collector-status':dict(status='ALIVE',identity=dict(pid=9,bootId='boot',startTicks=1,commandSha256='a'*64))}[action]
        return self.result_override or dict(state='complete',category='ok',payload=payload)

    def prepare(self):
        from datetime import datetime, timezone
        self.assertTrue(hasattr(self.module.Controller, 'start_rehearsal'), 'missing guarded paid start')
        self.wall = datetime(2026,10,27,12,0,tzinfo=timezone.utc).timestamp()
        iso = lambda t: datetime.fromtimestamp(t,timezone.utc).isoformat()
        self.preflight = self.base/'preflight.json'
        self.evidence = dict(target=self.binding['laneBindings']['target'],
            checkedAt=iso(self.wall), expiresAt=iso(self.wall+299),
            gateway=dict(replacementEnabled=False,idle=True),
            clients=dict(ready=2,sqlFailures=0,identitySha256='a'*64),
            metrics=dict(fresh=True,complete=True),singleOperator=True,
            noScaleProof=dict(status='PASS',evidenceSha256='b'*64),
            cost=dict(day='2026-10-27',net=100,reserve=100,allowance=100,ceiling=1800),
            bindingSha256=hashlib.sha256(self.manifest.read_bytes()).hexdigest())
        self.write_evidence()
        self.probe = patch.object(self.module.Controller,'_provider_preflight',return_value=None).start()
        patch.object(self.module.subprocess,'run',side_effect=AssertionError('direct CLI forbidden')).start()
        return self.controller()

    def write_evidence(self):
        self.preflight.write_text(json.dumps(self.evidence)); self.preflight.chmod(0o600)

    def calls(self): return [e for e in self.events if e[0]=='call']

    def test_paid_exact_once_proof_intent_and_successor(self):
        c=self.prepare(); result=c.start_rehearsal(self.preflight)
        self.assertEqual(c.state,'scaled-three'); self.assertEqual(result['desired'],3)
        calls=self.calls()
        self.assertEqual([e[1] for e in calls],['baseline-start','baseline-status','collector-launch',
            'collector-status','pre-scale-start','pre-scale-status','collector-status','scaleout-two-to-three'])
        self.assertEqual([e[2] for e in calls],list(range(1,9)))
        self.assertEqual(result['sequence'],9)
        self.assertAlmostEqual(calls[-1][3],1020.35)
        b=self.binding['laneBindings']; intent=json.loads(Path(b['scaleoutIntentPath']).read_text())
        self.assertEqual({k:v for k,v in intent.items() if k!='at'},dict(action='ModifyScalingGroup',
            account='1360282071200743',group='asg-t4necb4dfmu6o3z2rn0g',fromDesired=2,toDesired=3,
            feedSession=self.session,sequence=8))
        self.assertEqual(json.loads(Path(b['preScaleProofPath']).read_text()),dict(feedSession=self.session,sequence=6))
        for key in ('scaleoutIntentPath','preScaleProofPath'):
            self.assertEqual(Path(b[key]).stat().st_mode & 0o777,0o600)
        args=self.events[1][1]
        self.assertEqual(args[args.index('--max-seconds')+1],'5400')
        with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
        self.assertEqual(len(self.calls()),8)

    def test_paid_refuses_invalid_operator_receipt_before_spawn(self):
        c=self.prepare()
        for key,value in [('singleOperator',False),('bindingSha256','0'*64),('expiresAt','2026-10-27T11:00:00+00:00'),
                          ('gateway',dict(replacementEnabled=True,idle=True)),('noScaleProof',dict(status='FAIL',evidenceSha256='b'*64))]:
            with self.subTest(key=key):
                original=self.evidence[key]; self.evidence[key]=value; self.write_evidence()
                with self.assertRaises(Refused): self.controller().start_rehearsal(self.preflight)
                self.assertEqual(self.events,[])
                self.evidence[key]=original

    def test_paid_dead_feed_and_uncertain_worker_never_reach_ess(self):
        for fault in ('dead','uncertain','session','reused'):
            with self.subTest(fault=fault):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                c=self.prepare(); self.feed.poll.return_value=None
                def call(*args):
                    result=self.call(*args)
                    if args[0]=='pre-scale-status':
                        if fault=='dead': self.feed.poll.return_value=1
                        elif fault=='uncertain': return dict(state='uncertain',category='timeout',payload={})
                        elif fault=='session': result['payload']['feedSession']=str(uuid.uuid4())
                        else: self.pending_publish=False; self.auto_publish=False
                    return result
                self.client.call.side_effect=call
                with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                self.assertNotIn('scaleout-two-to-three',[e[1] for e in self.calls()])
                self.assertEqual(c.state,'uncertain'); self.assertTrue(self.client.close.called)
                self.auto_publish=True

    def test_cutoff_and_expiry_before_intent_and_dispatch(self):
        for point in ('before-intent','before-dispatch','expiry'):
            with self.subTest(point=point):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                c=self.prepare(); self.events=[]
                original_save=self.module.save_once
                def save(root,name,value,*args,**kwargs):
                    result=original_save(root,name,value,*args,**kwargs)
                    if name==Path(self.binding['laneBindings']['scaleoutIntentPath']).name:
                        if point=='before-dispatch': self.wall+=5401
                    return result
                def call(*args):
                    result=self.call(*args)
                    if args[0]=='collector-status' and len(self.calls())==7:
                        if point=='before-intent': self.wall+=5401
                        if point=='expiry': self.wall+=300
                    return result
                self.client.call.side_effect=call
                with patch.object(self.module,'save_once',side_effect=save):
                    with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                self.assertNotIn('scaleout-two-to-three',[e[1] for e in self.calls()])
                self.assertEqual(Path(self.binding['laneBindings']['scaleoutIntentPath']).exists(),point=='before-dispatch')

    def test_private_evidence_rejects_duplicate_keys(self):
        c=self.prepare()
        raw=self.preflight.read_text()
        self.preflight.write_text('{"singleOperator":false,'+raw[1:])
        with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
        self.assertEqual(self.events,[])

    def test_feed_loop_reserves_startup_margin_at_actual_spawn(self):
        from datetime import datetime, timezone
        c=self.prepare()
        self.wall=datetime(2026,10,27,13,29,tzinfo=timezone.utc).timestamp()
        self.evidence['checkedAt']=datetime.fromtimestamp(self.wall,timezone.utc).isoformat()
        self.evidence['expiresAt']=datetime.fromtimestamp(self.wall+299,timezone.utc).isoformat()
        self.write_evidence()
        def client(*args):
            self.wall+=2
            return self.start_client(*args)
        with patch.object(self.module.LaneClient,'start',side_effect=client):
            c.start_rehearsal(self.preflight)
        args=self.events[1][1]
        self.assertEqual(args[args.index('--max-seconds')+1],'3538')

    def test_operator_receipt_cost_client_metrics_and_time_checks(self):
        c=self.prepare()
        mutations=[('cost',dict(self.evidence['cost'],net=1801)),
            ('cost',dict(self.evidence['cost'],reserve=-1)),
            ('cost',dict(self.evidence['cost'],allowance=float('nan'))),
            ('cost',dict(self.evidence['cost'],day='2026-10-26')),
            ('clients',dict(self.evidence['clients'],ready=True)),
            ('clients',dict(self.evidence['clients'],sqlFailures=1)),
            ('clients',dict(self.evidence['clients'],identitySha256='bad')),
            ('metrics',dict(fresh=True,complete=False)),
            ('checkedAt','2026-10-27T12:01:00+00:00'),
            ('expiresAt','2026-10-27T12:06:00+00:00'),
            ('checkedAt','2026-10-27T20:00:00+08:00')]
        for key,value in mutations:
            with self.subTest(key=key,value=value):
                original=self.evidence[key]; self.evidence[key]=value; self.write_evidence()
                with self.assertRaises(Refused): self.controller().start_rehearsal(self.preflight)
                self.assertEqual(self.events,[]); self.evidence[key]=original

    def test_paid_successor_during_call_is_insufficient(self):
        c=self.prepare()
        def during(*args):
            result=self.call(*args)
            if args[0]=='pre-scale-status':
                self.publish(); self.pending_publish=False; self.auto_publish=False
            return result
        self.client.call.side_effect=during
        with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
        self.assertNotIn('scaleout-two-to-three',[e[1] for e in self.calls()])

    def test_unknown_ack_and_missing_post_ack_publication_never_retry(self):
        for fault in ('unknown','late','no-publication','changed-session'):
            with self.subTest(fault=fault):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                c=self.prepare(); self.events=[]; self.scaled=False; self.auto_publish=True
                def faulty(*args):
                    result=self.call(*args)
                    if args[0]=='scaleout-two-to-three':
                        if fault=='unknown': return dict(state='uncertain',category='timeout',payload={})
                        if fault=='late': self.clock+=20
                        if fault=='no-publication': self.pending_publish=False
                        if fault=='changed-session': self.publish(session=str(uuid.uuid4()),desired=3)
                    return result
                self.client.call.side_effect=faulty
                with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                self.assertEqual(c.state,'uncertain')
                self.assertEqual([e[1] for e in self.calls()].count('scaleout-two-to-three'),1)
                self.assertNotIn('reconcile-close',[e[1] for e in self.calls()])

    def test_paid_controller_process_death_leaves_intent_and_eof(self):
        c=self.prepare(); reader,writer=os.pipe(); pid=os.fork()
        if pid==0:
            os.close(reader)
            def die(*args):
                result=self.call(*args)
                if args[0]=='scaleout-two-to-three':
                    (self.capture/'fake-admitted').write_text('durable admission')
                    os._exit(9)
                return result
            self.client.call.side_effect=die
            try: c.start_rehearsal(self.preflight)
            finally: os._exit(10)
        os.close(writer)
        try:
            _,status=os.waitpid(pid,0)
            self.assertEqual(os.waitstatus_to_exitcode(status),9)
            self.assertEqual(os.read(reader,1),b'')
            self.assertTrue((self.capture/'fake-admitted').exists())
            self.assertTrue(Path(self.binding['laneBindings']['scaleoutIntentPath']).exists())
            self.assertFalse((self.capture/'lane'/'finish.ready').exists())
            with self.assertRaises(Refused): self.controller()
        finally: os.close(reader)

    def test_existing_proof_or_intent_cannot_be_overwritten(self):
        for key in ('preScaleProofPath','scaleoutIntentPath'):
            with self.subTest(key=key):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                c=self.prepare(); self.events=[]
                path=Path(self.binding['laneBindings'][key])
                def call(*args):
                    result=self.call(*args)
                    if args[0]=='pre-scale-status':
                        path.write_text('existing evidence'); path.chmod(0o600)
                    return result
                self.client.call.side_effect=call
                with self.assertRaises(FileExistsError): c.start_rehearsal(self.preflight)
                self.assertEqual(path.read_text(),'existing evidence')
                self.assertNotIn('scaleout-two-to-three',[e[1] for e in self.calls()])

    def test_stale_pre_scale_proof_and_changed_provider_session_refuse(self):
        for fault in ('old-proof','session','stale-publication'):
            with self.subTest(fault=fault):
                if self.capture.exists():
                    import shutil
                    shutil.rmtree(self.capture)
                c=self.prepare(); self.events=[]
                def call(*args):
                    result=self.call(*args)
                    if args[0]=='pre-scale-status':
                        if fault=='old-proof': result['payload']['sequence']=1
                        if fault=='session': self.publish(session=str(uuid.uuid4()))
                        if fault=='stale-publication':
                            self.publish(); self.wall+=30; self.pending_publish=False
                    return result
                self.client.call.side_effect=call
                with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                self.assertNotIn('scaleout-two-to-three',[e[1] for e in self.calls()])

    def test_initial_paid_cutoff_refuses_before_probe_or_spawn(self):
        from datetime import datetime, timezone
        c=self.prepare(); self.wall=datetime(2026,10,27,13,30,tzinfo=timezone.utc).timestamp()
        self.evidence['checkedAt']=datetime.fromtimestamp(self.wall,timezone.utc).isoformat()
        self.evidence['expiresAt']=datetime.fromtimestamp(self.wall+299,timezone.utc).isoformat()
        self.write_evidence()
        with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
        self.assertFalse(self.probe.called); self.assertEqual(self.events,[])

    def test_post_ack_supervisor_death_or_cancel_never_returns_scaled_three(self):
        # Exercise the real client's active validator against fake process and
        # journal boundaries, rather than making an _active mock always fail.
        from pgcat_lane_protocol import encode
        for failure in ('dead-process','cancelled-journal'):
            for point in ('waiting','receipt-read','before-return'):
                with self.subTest(failure=failure,point=point):
                    if self.capture.exists():
                        import shutil
                        shutil.rmtree(self.capture)
                    c=self.prepare(); self.events=[]; self.scaled=False
                    self.client.liveness_fd=123
                    self.client.process=Mock(); self.client.process.poll.return_value=None
                    self.client.run_id=str(uuid.uuid4()); self.client.mode='disabled-rehearsal'
                    self.client.bindings=copy.deepcopy(self.binding['laneBindings'])
                    self.client.bundle=hashlib.sha256(encode(self.client.bindings['sourceHashes'])).hexdigest()
                    snapshot=dict(runState='active',runId=self.client.run_id,mode='disabled-rehearsal',
                        target=self.client.bindings['target'],
                        hashes=dict(self.client.bindings['sourceHashes'],bundleSha256=self.client.bundle))
                    self.client.journal=Mock(); self.client.journal.run_snapshot.side_effect=lambda:copy.deepcopy(snapshot)
                    self.client._active.side_effect=lambda:self.module.LaneClient._active(self.client)
                    def invalidate():
                        if failure=='dead-process': self.client.process.poll.return_value=9
                        else: snapshot['runState']='cancelled'
                    original_read=self.module.lane.read_receipt
                    original_next=c._next_receipt
                    def sleep(seconds):
                        self.sleep(seconds)
                        if self.scaled and point=='waiting': invalidate()
                    def read(*args,**kwargs):
                        result=original_read(*args,**kwargs)
                        if self.scaled and point=='receipt-read': invalidate()
                        return result
                    def next_receipt(*args,**kwargs):
                        result=original_next(*args,**kwargs)
                        if kwargs.get('desired')==3 and point=='before-return': invalidate()
                        return result
                    with patch.object(self.module.time,'sleep',side_effect=sleep), \
                         patch.object(self.module.lane,'read_receipt',side_effect=read), \
                         patch.object(c,'_next_receipt',side_effect=next_receipt):
                        with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                    self.assertEqual(c.state,'uncertain')
                    self.assertTrue(self.client.close.called)
                    with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
                    self.assertEqual([e[1] for e in self.calls()].count('scaleout-two-to-three'),1)
                    self.assertNotIn('reconcile-close',[e[1] for e in self.calls()])
                    self.assertFalse(self.feed.kill.called)

    def test_scaleout_crash_requires_recovery(self):
        c=self.prepare()
        def crash(*args):
            result=self.call(*args)
            if args[0]=='scaleout-two-to-three': raise BrokenPipeError('supervisor died after admission')
            return result
        self.client.call.side_effect=crash
        with self.assertRaises(BrokenPipeError): c.start_rehearsal(self.preflight)
        self.assertEqual(c.state,'uncertain')
        with self.assertRaises(Refused): c.start_rehearsal(self.preflight)
        self.assertEqual([e[1] for e in self.calls()].count('scaleout-two-to-three'),1)
        self.assertFalse(self.feed.kill.called)
        self.assertNotIn('reconcile-close',[e[1] for e in self.calls()])


class ProviderPreflightTests(unittest.TestCase):
    setUp = ControllerTests.setUp
    write_binding = ControllerTests.write_binding
    controller = ControllerTests.controller
    start_client = ControllerTests.start_client
    start_feed = ControllerTests.start_feed
    call = ControllerTests.call
    sleep = ControllerTests.sleep
    def test_provider_probe_checks_exact_fleet_account_and_activity(self):
        original_operator=self.module.provider.Operator
        ids=['original-one','original-two']
        good=dict(desired=2,members=ids,healthyRegistered=ids,
            memberStates=[dict(id=i,lifecycle='Protected',health='Healthy',addressKnown=True) for i in ids],
            backendStates=[dict(id=i,status='Available',weight=100,listenerHealth='Normal') for i in ids])
        group=dict(ScalingGroupId='asg-t4necb4dfmu6o3z2rn0g',DesiredCapacity=2,MinSize=2,MaxSize=4)
        for fault in ('none','account','roster','protected','nlb','group','activity'):
            with self.subTest(fault=fault):
                c=self.controller(); snap=copy.deepcopy(good); g=copy.deepcopy(group); seen=[]; environments=[]
                if fault=='roster': snap['members']=['other','original-two']
                if fault=='protected': snap['memberStates'][0]['lifecycle']='InService'
                if fault=='nlb': snap['backendStates'][0]['listenerHealth']='Unhealthy'
                if fault=='group': g['MinSize']=1
                def factory(args):
                    probe=original_operator(args)
                    probe.read=lambda:snap; probe.fetch_group=lambda:g
                    return probe
                def run(argv,**kwargs):
                    self.assertIsNone(c.feed); seen.append(argv)
                    environments.append(kwargs['env'])
                    self.assertEqual(argv[0],str(self.cli))
                    if argv[1:3]==['sts','GetCallerIdentity']:
                        value=dict(AccountId='wrong' if fault=='account' else '1360282071200743')
                    else:
                        self.assertEqual(argv[1:3],['ess','DescribeScalingActivities'])
                        value=dict(PageNumber=1,TotalCount=1 if fault=='activity' else 0,
                                   ScalingActivities=dict(ScalingActivity=[]))
                    return SimpleNamespace(stdout=json.dumps(value))
                from types import SimpleNamespace
                with patch.dict(os.environ,HTTPS_PROXY='http://fake-proxy.invalid:1',ALIYUN_ACCESS_KEY_SECRET='test-never-forward'), patch.object(self.module.provider,'Operator',side_effect=factory), patch.object(self.module.subprocess,'run',side_effect=run):
                    if fault=='none': c._provider_preflight()
                    else:
                        with self.assertRaises(Refused): c._provider_preflight()
                self.assertTrue(seen)
                for env in environments:
                    self.assertEqual(env.get('HTTPS_PROXY'),'http://fake-proxy.invalid:1')
                    self.assertNotIn('ALIYUN_ACCESS_KEY_SECRET',env)
                c.feed=self.feed
                with self.assertRaises(Refused): c._provider_preflight()


if __name__ == '__main__': unittest.main()
