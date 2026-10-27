from pgcat_lane_protocol import monotonic
import copy
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
import uuid

import pgcat_lane_actions as actions
import pgcat_lane_supervisor as supervisor
from pgcat_lane_protocol import Journal, Refused, encode, publish_request
import pgcat_workbench_lane as lane


class SupervisorTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.root = self.base/'run'; self.root.mkdir(mode=0o700)
        self.global_dir = self.base/'global'
        self.override = patch.object(supervisor, 'GLOBAL_ROOT', self.global_dir)
        self.override.start(); self.addCleanup(self.override.stop)
        self.session = str(uuid.uuid4()); self.run = str(uuid.uuid4())
        self.cli = self.base/'fake-cli'
        self.behavior = self.base/'behavior'
        self.behavior.write_text('ok')
        self.marker = self.base/'executed'
        self.fork_ready=self.base/'fork-ready'
        self.fork_release=self.base/'fork-release'
        self.addCleanup(lambda:self.fork_release.touch())
        self.cli.write_text('#!'+sys.executable+'\nimport json,os,signal,time\n'
            +'from pathlib import Path\n'
            +'Path('+repr(str(self.marker))+').write_text(str(os.getpid()))\n'
            +'mode=Path('+repr(str(self.behavior))+').read_text()\n'
            +'assert not (signal.pthread_sigmask(signal.SIG_BLOCK, []) & {signal.SIGINT,signal.SIGTERM,signal.SIGHUP})\n'
            +'if mode == "sleep": time.sleep(5)\n'
            +'if mode == "fork":\n'
            +' if os.fork() == 0:\n'
            +'  Path('+repr(str(self.fork_ready))+').touch()\n'
            +'  until=time.monotonic()+2\n'
            +'  while not Path('+repr(str(self.fork_release))+').exists() and time.monotonic()<until: time.sleep(.01)\n'
            +'  os._exit(0)\n'
            +'print(json.dumps({"status":"PREPARED","phase":"baseline"}),flush=True)\n')
        self.cli.chmod(0o700)
        self.target = dict(accountId='1360282071200743', regionId='ap-southeast-1',
            workerId=lane.WORKER, scalingGroupId='asg-t4necb4dfmu6o3z2rn0g',
            nlbServerGroupId='sgp-xs5s42tihtl6ja2a67', originalInstanceIds=['i-t4n8ofwrovy3mv198ela','i-t4n8ofwrovy3mv198elb'])
        here = Path(__file__).resolve().parent
        self.bindings = dict(workbenchCli=str(self.cli), aliyunCli=str(self.cli), laneWrapper=str(here/'pgcat_workbench_lane.py'),
            providerFeedPath=str(here/'provider_feed.py'), receiptPath=str(self.root/'receipt.json'),
            laneLockPath=str(self.root/'lane.lock'), laneIntentDir=str(self.root/'intents'),
            phaseWorker='/reviewed/phase.py', pendingWorker='/reviewed/pending.py', remoteRun='/reviewed/run',
            contractDir='/reviewed/contract', scaleoutIntentPath=str(self.root/'scaleout.intent'),
            preScaleProofPath=str(self.root/'proof.json'), target=self.target)
        (self.root/'intents').mkdir(mode=0o700)
        sources = dict(laneWrapper=here/'pgcat_workbench_lane.py', providerFeed=here/'provider_feed.py',
                       laneProtocol=here/'pgcat_lane_protocol.py', laneSupervisor=here/'pgcat_lane_supervisor.py',
                       laneActions=here/'pgcat_lane_actions.py')
        self.hashes = {key:hashlib.sha256(path.read_bytes()).hexdigest() for key,path in sources.items()}
        self.bindings['sourceHashes'] = self.hashes
        self.bundle = hashlib.sha256(encode(self.hashes)).hexdigest()
        self.journal = Journal(self.root)
        self.journal.create_run(self.run, 'disabled-rehearsal', self.target, dict(self.hashes, bundleSha256=self.bundle))
        self.owner = supervisor.LaneSupervisor(self.root, self.bindings, self.journal)
        self.addCleanup(self.owner.close)
        self.receipt(1)

    def private_json(self, path, value):
        path.write_bytes(encode(value)); path.chmod(0o600)

    def receipt(self, seq):
        self.private_json(Path(self.bindings['receiptPath']), dict(feedSession=self.session,sequence=seq,
            mode='disabled-rehearsal',desired=2,observedAt=time.time()))

    def request(self, **changes):
        identity, value, _ = lane.read_receipt(self.bindings['receiptPath'], self.session, 1)
        value = dict(requestId=str(uuid.uuid4()),runId=self.run,supervisorId=self.owner.supervisor_id,
            action='baseline-status',feedSession=self.session,minimumSequence=value['sequence'],
            receiptIdentity=supervisor.receipt_identity(identity),target=self.target,
            bundleSha256=self.bundle,nonce=str(uuid.uuid4()),deadlineMonotonic=monotonic()+4.0)
        value.update(changes)
        publish_request(self.root,value)
        return value

    def run_request(self, **changes):
        req = self.request(**changes)
        self.owner.run_once(req['requestId'])
        return self.journal.snapshot(req['requestId'])

    def test_finish_refuses_adopted_child(self):
        self.owner.child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(5)'])
        try:
            with self.assertRaises(Refused): self.owner.finish()
        finally: self.owner._cleanup()
        with self.journal._connection() as db:
            self.assertNotEqual(db.execute('SELECT state FROM run').fetchone()['state'], 'finished')

    def test_fixed_catalog_rejects_unknown_actions_and_extra_bindings(self):
        for action in ('exec','baseline-status; touch /tmp/x', []):
            with self.assertRaises(Refused): actions.ActionCatalog.command(action,self.bindings)
        with self.assertRaises(Refused):
            actions.ActionCatalog.command('baseline-status',dict(self.bindings,command='echo unsafe'))
        args, env, budget = actions.ActionCatalog.command('baseline-status',self.bindings)
        self.assertIn('python3 /reviewed/phase.py status baseline',args)
        self.assertEqual(budget,5.0)
        self.assertNotIn('AWS_SECRET_ACCESS_KEY',env)

    def test_clean_child_mask_restored_and_one_use(self):
        row = self.run_request()
        self.assertEqual(row['state'],'complete')
        self.assertEqual(row['runState'],'active')
        self.assertIsNone(self.owner.child)
        self.assertTrue(self.marker.exists())
        with self.assertRaises(Refused): self.owner.run_once(row['requestId'])

    def test_pre_admission_cancel_spawns_nothing(self):
        req = self.request(); self.owner.cancel('cancelled')
        with self.assertRaises(Refused): self.owner.run_once(req['requestId'])
        self.assertFalse(self.marker.exists())

    def test_signal_during_adoption_reaps_exact_child(self):
        original = subprocess.Popen
        adopted = []
        def spawn(*args, **kwargs):
            child = original(*args, **kwargs); adopted.append(child)
            os.kill(os.getpid(), signal.SIGTERM)
            return child
        with patch.object(supervisor.subprocess,'Popen',spawn): row = self.run_request()
        self.assertEqual(row['state'],'uncertain')
        self.assertNotEqual(row['runState'],'active')
        self.assertIsNotNone(adopted[0].returncode)
        self.assertIsNone(self.owner.child)

    def test_signal_during_seal_blocks_scaleout(self):
        original = self.journal.seal
        def seal(req, result):
            os.kill(os.getpid(),signal.SIGINT)
            return original(req,result)
        with patch.object(self.journal,'seal',seal): row = self.run_request()
        self.assertEqual(row['state'],'complete')
        self.assertNotEqual(row['runState'],'active')
        with self.assertRaises(Refused): self.owner.run_once(self.request(action='scaleout-two-to-three')['requestId'])

    def test_signal_before_terminal_commit(self):
        original = actions.ActionCatalog.parse
        def parse(*args):
            value = original(*args)
            os.kill(os.getpid(),signal.SIGHUP)
            return value
        with patch.object(actions.ActionCatalog,'parse',parse): row = self.run_request()
        self.assertEqual(row['state'],'uncertain')

    def test_spawn_overrun_adopts_and_reaps(self):
        original = subprocess.Popen
        def spawn(*args,**kwargs):
            child=original(*args,**kwargs); time.sleep(.12); return child
        with patch.object(supervisor.subprocess,'Popen',spawn):
            row = self.run_request(deadlineMonotonic=monotonic()+.06)
        self.assertEqual(row['state'],'uncertain')
        self.assertEqual(row['result']['category'],'timeout')
        self.assertIsNone(self.owner.child)

    def test_timeout_and_fork_retaining_pipes_are_uncertain(self):
        self.behavior.write_text('fork')
        row=self.run_request(deadlineMonotonic=monotonic()+.8)
        self.assertEqual(row['state'],'uncertain')
        self.assertIsNone(self.owner.child)
        self.fork_release.touch()
        time.sleep(.05)

    def test_parent_eof_cancels(self):
        read,write=os.pipe(); os.close(write)
        try: self.owner.serve(read)
        finally: os.close(read)
        with self.assertRaises(Refused): self.owner.run_once(self.request()['requestId'])

    def test_global_cross_root_and_restart_refused(self):
        other=self.base/'other'; other.mkdir(mode=0o700)
        other_journal=Journal(other)
        other_journal.create_run(str(uuid.uuid4()),'no-scale',self.target,dict(self.hashes,bundleSha256=self.bundle))
        with self.assertRaises(Refused): supervisor.LaneSupervisor(other,self.bindings,other_journal)
        self.owner.close()
        with self.assertRaises(Refused): supervisor.LaneSupervisor(self.root,self.bindings,self.journal)

    def test_changed_source_refuses_without_dispatch(self):
        self.owner.bindings['sourceHashes']['providerFeed']='0'*64
        with self.assertRaises(Refused): self.owner.run_once(self.request()['requestId'])
        self.assertFalse(self.marker.exists())

    def test_no_scale_rejects_ess(self):
        with self.journal._transaction() as db: db.execute("UPDATE run SET mode='no-scale'")
        with self.assertRaises(Refused): self.owner.run_once(self.request(action='scaleout-two-to-three')['requestId'])
        self.assertFalse(self.marker.exists())

    def test_scaleout_consumes_shared_sequence(self):
        req=self.request(action='scaleout-two-to-three',deadlineMonotonic=monotonic()+10.)
        self.private_json(Path(self.bindings['preScaleProofPath']),dict(feedSession=self.session,sequence=1))
        self.private_json(Path(self.bindings['scaleoutIntentPath']),dict(action='ModifyScalingGroup',
            account=self.target['accountId'],group=self.target['scalingGroupId'],fromDesired=2,toDesired=3,
            feedSession=self.session,sequence=1,at='2026-10-27T00:00:00+00:00'))
        # Only the fake process substitutes for Alibaba CLI; admission/ledger are real.
        command=([sys.executable,'-c','print(\'{"RequestId":"ack"}\')'], {'PATH':os.defpath},20.)
        with patch.object(actions.ActionCatalog,'command',return_value=command), patch.object(supervisor,'before_cutoff',return_value=True):
            self.owner.run_once(req['requestId'])
        self.assertEqual(self.journal.snapshot(req['requestId'])['state'],'complete')
        with self.assertRaises(lane.Refused): lane.consume_intent(self.bindings['laneIntentDir'],self.session,1)
        with self.assertRaises(Refused): self.owner.run_once(self.request()['requestId'])

    def test_pending_exact_marker_parser(self):
        req=self.request(action='pending-start')
        self.assertEqual(actions.ActionCatalog.parse('pending-start',b'PENDING_DETACHED:STARTED\n',req),{'status':'STARTED'})
        for raw in (b'PENDING_DETACHED:PREPARED\nextra\n',b'PENDING_DETACHED:INCONCLUSIVE\n',b'PENDING_DETACHED:PREPARED\nPENDING_DETACHED:PREPARED\n'):
            with self.assertRaises(Refused): actions.ActionCatalog.parse('pending-status',raw,req)

    def test_explicit_aliyun_executable_binding(self):
        bindings=dict(self.bindings,aliyunCli=str(self.cli))
        argv,env,budget=actions.ActionCatalog.command('scaleout-two-to-three',bindings)
        self.assertEqual(argv[0],str(self.cli))
        self.assertEqual(budget,20.)

    def test_signal_after_intent_is_uncertain(self):
        original=self.journal.admit
        def admit(request):
            original(request)
            os.kill(os.getpid(),signal.SIGTERM)
        with patch.object(self.journal,'admit',admit): row=self.run_request()
        self.assertEqual(row['state'],'uncertain')
        self.assertIsNone(self.owner.child)

    def test_signal_during_communication_reaps(self):
        self.behavior.write_text('sleep')
        original=self.owner._communicate
        def communicate(deadline):
            os.kill(os.getpid(),signal.SIGINT)
            return original(deadline)
        with patch.object(self.owner,'_communicate',communicate): row=self.run_request()
        self.assertEqual(row['state'],'uncertain')
        self.assertIsNone(self.owner.child)

    def test_after_commit_notification_cancels_run(self):
        original=self.journal.snapshot
        sent=[]
        def snapshot(request_id):
            if not sent:
                sent.append(True)
                os.kill(os.getpid(),signal.SIGHUP)
            return original(request_id)
        with patch.object(self.journal,'snapshot',snapshot): row=self.run_request()
        self.assertEqual(row['state'],'complete')
        self.assertNotEqual(row['runState'],'active')

    def test_output_overflow_and_duplicate_json_refused(self):
        req=self.request()
        for raw in (b'x'*8193,b'{"status":"PREPARED","status":"PREPARED","phase":"baseline"}'):
            with self.assertRaises(Refused): actions.ActionCatalog.parse('baseline-status',raw,req)

    def test_unsafe_remote_path_is_not_shell_fragment(self):
        bindings=dict(self.bindings,phaseWorker='/reviewed/a; touch unsafe')
        args,_,_=actions.ActionCatalog.command('baseline-status',bindings)
        self.assertEqual(args[-1],"python3 '/reviewed/a; touch unsafe' status baseline")
        with self.assertRaises(Refused):
            actions.ActionCatalog.command('baseline-status',dict(self.bindings,phaseWorker='/x/../y'))

    def test_sigkill_leaves_incomplete_journal_and_global_record(self):
        request=self.request()
        read,write=os.pipe()
        pid=os.fork()
        if pid == 0:
            os.close(read)
            def in_flight(deadline):
                os.write(write,b'1')
                signal.pause()
            self.owner._communicate=in_flight
            self.owner.run_once(request['requestId'])
            os._exit(0)
        os.close(write)
        try:
            self.assertEqual(os.read(read,1),b'1')
            os.kill(pid,signal.SIGKILL)
            os.waitpid(pid,0)
        finally: os.close(read)
        self.assertEqual(self.journal.snapshot(request['requestId'])['state'],'dispatching')
        self.owner.close()
        with self.assertRaises(Refused): supervisor.LaneSupervisor(self.root,self.bindings,self.journal)
        self.assertTrue((self.global_dir/'active-run').exists())

    def test_reconcile_requires_positive_resource_version(self):
        req=self.request(action='reconcile-close')
        prepared={'status':'PREPARED','stage':'reconcile','category':'rehearsal-only'}
        for rv in ('0','abc','-1'):
            raw=encode(prepared)+b'\n'+encode(dict(feedSession=self.session,sequence=1,resourceVersion=rv))+b'\n'
            with self.assertRaises(Refused): actions.ActionCatalog.parse('reconcile-close',raw,req)
        raw=encode(prepared)+b'\n'+encode(dict(feedSession=self.session,sequence=1,resourceVersion='42'))+b'\n'
        self.assertEqual(actions.ActionCatalog.parse('reconcile-close',raw,req)['resourceVersion'],'42')

    def test_parent_eof_during_child_communication(self):
        read,write=os.pipe(); self.owner.liveness_fd=read
        original=self.owner._communicate
        def communicate(deadline):
            os.close(write)
            return original(deadline)
        try:
            with patch.object(self.owner,'_communicate',communicate): row=self.run_request()
            self.assertEqual(row['state'],'uncertain')
            self.assertEqual(row['result']['category'],'incomplete')
        finally:
            self.owner.liveness_fd=None; os.close(read)

    def test_timeout_without_fork_and_bounded_output(self):
        self.behavior.write_text('sleep')
        row=self.run_request(deadlineMonotonic=monotonic()+.15)
        self.assertEqual(row['result']['category'],'timeout')
        self.assertIsNone(self.owner.child)

    def test_child_output_is_not_persisted_on_parse_failure(self):
        self.cli.write_text('#!'+sys.executable+'\nprint("private-output-"*1000)\n')
        row=self.run_request()
        self.assertEqual(row['state'],'uncertain')
        self.assertEqual(row['result']['payload'],{})
        self.assertNotIn(b'private-output',self.journal.path.read_bytes())

    def test_actual_fork_retains_lane_lock_after_direct_child_exit(self):
        import fcntl
        self.behavior.write_text('fork')
        row=self.run_request(deadlineMonotonic=monotonic()+.8)
        self.assertEqual(row['state'],'uncertain')
        self.assertTrue(self.fork_ready.exists())
        fd=os.open(self.bindings['laneLockPath'],os.O_RDWR)
        try:
            with self.assertRaises(BlockingIOError): fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
            self.fork_release.touch()
            until=monotonic()+1
            while True:
                try:
                    fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    self.assertLess(monotonic(),until)
                    time.sleep(.01)
        finally: os.close(fd)

    def test_serve_processes_published_request_then_eof(self):
        request=self.request()
        read,write=os.pipe()
        pid=os.fork()
        if pid == 0:
            os.close(read)
            until=monotonic()+3
            while monotonic()<until:
                try:
                    if self.journal.snapshot(request['requestId'])['state'] != 'dispatching': break
                except Refused: pass
                time.sleep(.01)
            os.close(write)
            os._exit(0)
        os.close(write)
        try:
            self.owner.serve(read)
            row=self.journal.snapshot(request['requestId'])
            self.assertEqual(row['state'],'complete')
            self.assertNotEqual(row['runState'],'active')
        finally:
            os.close(read); os.waitpid(pid,0)

    def test_malformed_scaleout_intent_is_sanitized_refusal(self):
        request=self.request(action='scaleout-two-to-three')
        self.private_json(Path(self.bindings['scaleoutIntentPath']),[])
        with patch.object(supervisor,'before_cutoff',return_value=True):
            with self.assertRaises(Refused): self.owner.run_once(request['requestId'])
        self.assertFalse(self.marker.exists())

    def test_second_signal_during_cleanup_still_reaps(self):
        self.behavior.write_text('sleep')
        original=self.owner._cleanup
        sent=[]
        def cleanup():
            if not sent:
                sent.append(True)
                with self.owner._masked():
                    os.kill(os.getpid(),signal.SIGTERM)
                    original()
            else: original()
        with patch.object(self.owner,'_cleanup',cleanup):
            row=self.run_request(deadlineMonotonic=monotonic()+.15)
        self.assertEqual(row['state'],'uncertain')
        self.assertIsNone(self.owner.child)

    def test_scaleout_cutoff_is_rechecked_at_admission(self):
        req=self.request(action='scaleout-two-to-three')
        self.private_json(Path(self.bindings['preScaleProofPath']),dict(feedSession=self.session,sequence=1))
        self.private_json(Path(self.bindings['scaleoutIntentPath']),dict(action='ModifyScalingGroup',
            account=self.target['accountId'],group=self.target['scalingGroupId'],fromDesired=2,toDesired=3,
            feedSession=self.session,sequence=1,at='2026-10-27T00:00:00+00:00'))
        with patch.object(supervisor,'before_cutoff',side_effect=[True,False]):
            with self.assertRaises(Refused): self.owner.run_once(req['requestId'])
        self.assertFalse(self.marker.exists())

    def test_terminal_commit_overrun_cannot_report_active_success(self):
        request=self.request(deadlineMonotonic=monotonic()+1.)
        original=self.journal.seal
        def slow_seal(request_id,result):
            original(request_id,result)
            time.sleep(max(0,request['deadlineMonotonic']-monotonic())+.02)
        with patch.object(self.journal,'seal',slow_seal):
            self.owner.run_once(request['requestId'])
        row=self.journal.snapshot(request['requestId'])
        self.assertNotEqual(row['runState'],'active')

    def pre_scale_response(self, sequence, session=None):
        self.receipt(10)
        response=dict(status='PREPARED',phase='pre-scale',feedSession=session or self.session,sequence=sequence)
        self.cli.write_text('#!'+sys.executable+'\nprint('+repr(encode(response).decode('ascii'))+')\n')
        return self.run_request(action='pre-scale-status',minimumSequence=10)

    def test_pre_scale_persisted_earlier_proof_accepted_on_later_poll(self):
        row=self.pre_scale_response(2)
        self.assertEqual(row['state'],'complete')
        self.assertEqual(row['result']['payload']['sequence'],2)

    def test_pre_scale_future_proof_refused(self):
        row=self.pre_scale_response(11)
        self.assertEqual(row['state'],'uncertain')
        self.assertEqual(row['result']['category'],'parse')

    def test_pre_scale_nonpositive_proof_refused(self):
        row=self.pre_scale_response(0)
        self.assertEqual(row['state'],'uncertain')

    def test_pre_scale_different_session_refused(self):
        row=self.pre_scale_response(2,str(uuid.uuid4()))
        self.assertEqual(row['state'],'uncertain')

    def prepare_scaleout_request(self):
        req=self.request(action='scaleout-two-to-three',deadlineMonotonic=monotonic()+19.)
        self.private_json(Path(self.bindings['preScaleProofPath']),dict(feedSession=self.session,sequence=1))
        self.private_json(Path(self.bindings['scaleoutIntentPath']),dict(action='ModifyScalingGroup',
            account=self.target['accountId'],group=self.target['scalingGroupId'],fromDesired=2,toDesired=3,
            feedSession=self.session,sequence=1,at='2026-10-27T00:00:00+00:00'))
        return req

    def test_ess_ack_after_twelve_seconds_within_twenty_second_budget(self):
        from types import SimpleNamespace
        req=self.prepare_scaleout_request()
        offset=[0.]
        clock=SimpleNamespace(time=lambda:time.time()+offset[0],monotonic=lambda:monotonic()+offset[0])
        original=self.owner._communicate
        def communicate(deadline):
            raw=original(deadline)
            offset[0]=13.
            return raw
        command=([sys.executable,'-c','print(\'{"RequestId":"ack"}\')'],{'PATH':os.defpath},20.)
        with patch.object(actions.ActionCatalog,'command',return_value=command), patch.object(supervisor,'before_cutoff',return_value=True), patch.object(supervisor,'time',clock), patch.object(supervisor,'monotonic',clock.monotonic), patch.object(self.owner,'_communicate',communicate):
            self.owner.run_once(req['requestId'])
        row=self.journal.snapshot(req['requestId'])
        self.assertEqual(row['state'],'complete')
        self.assertEqual(row['runState'],'active')

    def test_deadline_expired_during_admission_never_spawns(self):
        req=self.request(deadlineMonotonic=monotonic()+.15)
        original=self.journal.admit
        def admit(request):
            original(request)
            time.sleep(max(0,req['deadlineMonotonic']-monotonic())+.01)
        with patch.object(self.journal,'admit',admit), patch.object(supervisor.subprocess,'Popen',wraps=subprocess.Popen) as launch:
            row=self.owner.run_once(req['requestId'])
        self.assertEqual(row['state'],'uncertain')
        self.assertEqual(row['result']['category'],'timeout')
        self.assertEqual(launch.call_count,0)
        self.assertFalse(self.marker.exists())

    def test_deadline_expired_during_ess_intent_never_spawns(self):
        req=self.prepare_scaleout_request()
        original=lane.consume_intent
        from types import SimpleNamespace
        offset=[0.]
        clock=SimpleNamespace(time=time.time,monotonic=lambda:monotonic()+offset[0])
        def consume(*args):
            original(*args)
            offset[0]=20.
        with patch.object(lane,'consume_intent',consume), patch.object(supervisor,'before_cutoff',return_value=True), patch.object(supervisor,'time',clock), patch.object(supervisor,'monotonic',clock.monotonic), patch.object(supervisor.subprocess,'Popen',wraps=subprocess.Popen) as launch:
            row=self.owner.run_once(req['requestId'])
        self.assertEqual(row['state'],'uncertain')
        self.assertEqual(row['result']['category'],'timeout')
        self.assertEqual(launch.call_count,0)
        self.assertTrue((Path(self.bindings['laneIntentDir'])/(self.session+'.1.intent')).exists())


if __name__ == '__main__': unittest.main()
