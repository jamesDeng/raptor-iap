from pgcat_lane_protocol import monotonic
import copy
from datetime import datetime, timezone
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
from pgcat_lane_protocol import Journal, Refused, encode, publish_request, roster_sha, expected_roster
import pgcat_workbench_lane as lane


class SupervisorTests(unittest.TestCase):
    def test_stop_control_json_refuses_duplicate_binding_keys(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);root.chmod(0o700)
            path=root/'no-scale-stop.intent'
            path.write_text('{"runId":"one","runId":"two"}');path.chmod(0o600)
            with self.assertRaises(Refused):supervisor.read_json(path,publication_marker=True)

    def setUp(self):
        from unittest.mock import patch
        admission=patch('pgcat_lane_protocol._release_admission');admission.start();self.addCleanup(admission.stop)
        guard=patch.object(supervisor,'_require_verified');guard.start();self.addCleanup(guard.stop)
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
            +'if mode == "pending-prepared": print("PENDING_DETACHED:PREPARED",flush=True); raise SystemExit(0)\n'
            +'if mode == "nlb-ack": print(json.dumps({"RequestId":"request-ok","ServerGroupId":"sgp-xs5s42tihtl6ja2a67","JobId":"job-ok"}),flush=True); raise SystemExit(0)\n'
            +'if mode == "activity-clear": print(json.dumps({"PageNumber":1,"TotalCount":0,"ScalingActivities":{"ScalingActivity":[]}}),flush=True); raise SystemExit(0)\n'
            +'if mode == "ess-ack": print(json.dumps({"RequestId":"request-ok"}),flush=True); raise SystemExit(0)\n'
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
            phaseWorker='/reviewed/run/pgcat_rehearsal_worker.py',
            pendingWorker='/reviewed/run/pgcat_rehearsal_worker.py',
            pendingProofWorker='/reviewed/run/pgcat_pending_proof_worker.py',
            preDrainProofWorker='/reviewed/run/pgcat_pre_drain_proof_worker.py',
            zeroProofWorker='/reviewed/run/pgcat_zero_proof_worker.py',
            workerKubeconfig='/reviewed/kubeconfig', remoteRun='/reviewed/run',
            contractDir='/reviewed/contract', scaleoutIntentPath=str(self.root/'scaleout.intent'),
            nlbIntentPath=str(self.root/'nlb-deregister.intent'),
            scaleinIntentPath=str(self.root/'scalein.intent'),
            preScaleProofPath=str(self.root/'proof.json'), target=self.target)
        (self.root/'intents').mkdir(mode=0o700)
        sources = dict(laneWrapper=here/'pgcat_workbench_lane.py', providerFeed=here/'provider_feed.py',
                       laneProtocol=here/'pgcat_lane_protocol.py', laneSupervisor=here/'pgcat_lane_supervisor.py',
                       laneActions=here/'pgcat_lane_actions.py',
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
                       laneTimingAssessor=here/'pgcat_lane_timing_assessor.py')
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

    def receipt(self, seq, desired=2):
        self.private_json(Path(self.bindings['receiptPath']), dict(feedSession=self.session,sequence=seq,
            mode='disabled-rehearsal',desired=desired,observedAt=time.time(),rosterSha256='a'*64))

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

    def nlb_proofs(self, *, age=0, candidate='i-candidate'):
        pending=dict(runId=self.run,feedSession=self.session,newId=candidate,
            originalInstanceIds=self.target['originalInstanceIds'],sequence=3,
            observedAt=time.time()-2,resourceVersion='11',clientRosterSha256='b'*64,
            evidenceSha256='c'*64,sourceSha256=self.hashes['pendingProofWorker'])
        pre=dict(runId=self.run,feedSession=self.session,newId=candidate,
            originalInstanceIds=self.target['originalInstanceIds'],
            pendingBindingSha256=hashlib.sha256(encode(pending)).hexdigest(),
            sequence=4,observedAt=time.time()-1,resourceVersion='12',
            clientRosterSha256='b'*64,evidenceSha256='d'*64,
            sourceSha256=self.hashes['preDrainProofWorker'])
        with self.journal._connection() as db:
            db.execute('INSERT INTO pending_binding VALUES (1,?)',(encode(pending).decode(),))
            db.execute('INSERT INTO pre_drain_binding VALUES (1,?,?)',
                       (encode(pre).decode(),time.time()-age))
        roster=roster_sha(expected_roster(self.target['originalInstanceIds'],candidate,registered=True))
        self.private_json(Path(self.bindings['receiptPath']),dict(feedSession=self.session,
            sequence=5,mode='disabled-rehearsal',desired=3,observedAt=time.time(),rosterSha256=roster))
        intent=dict(action='RemoveServersFromServerGroup',runId=self.run,
            feedSession=self.session,sequence=5,newId=candidate,
            serverGroupId=self.target['nlbServerGroupId'],
            preDrainProofSha256=hashlib.sha256(encode(pre)).hexdigest(),
            rosterSha256=roster,at=datetime.now(timezone.utc).isoformat())
        self.private_json(Path(self.bindings['nlbIntentPath']),intent)
        return pending,pre

    def test_nlb_exact_candidate_command_and_sanitized_ack(self):
        pending,_=self.nlb_proofs()
        argv,_,budget=actions.ActionCatalog.command('deregister-new-node',self.bindings,self.run,pending)
        self.assertEqual(argv,[str(self.cli),'nlb','RemoveServersFromServerGroup',
            '--ServerGroupId','sgp-xs5s42tihtl6ja2a67',
            '--Servers.1.ServerType','Ecs','--Servers.1.ServerId','i-candidate',
            '--Servers.1.Port','6432','--profile','infra-ops-poc','--region','ap-southeast-1'])
        self.assertEqual(budget,20.)
        self.behavior.write_text('nlb-ack')
        with patch.object(supervisor,'before_cutoff',return_value=True):
            result=self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        self.assertEqual(result['result'],dict(state='complete',category='ok',
            payload=dict(requestId='request-ok',jobId='job-ok')))
        self.assertEqual(result['runState'],'active')
        with self.journal._connection() as db:
            self.assertEqual(db.execute('SELECT count(*) FROM mutation_keys').fetchone()[0],1)

    def test_nlb_wrong_group_ack_is_rejected(self):
        self.nlb_proofs()
        request=self.request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        raw=encode(dict(RequestId='request-ok',ServerGroupId='sgp-wrong',JobId='job-ok'))
        with self.assertRaises(Refused):actions.ActionCatalog.parse('deregister-new-node',raw,request)

    def test_nlb_stale_proof_or_roster_mismatch_never_spawns(self):
        self.nlb_proofs(age=31)
        with patch.object(supervisor,'before_cutoff',return_value=True):
            with self.assertRaises(Refused):
                self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        self.assertFalse(self.marker.exists())

    def test_nlb_stale_intent_refuses_before_child(self):
        self.nlb_proofs()
        path=Path(self.bindings['nlbIntentPath'])
        intent=json.loads(path.read_text())
        intent['at']='2020-01-01T00:00:00+00:00'
        self.private_json(path,intent)
        with patch.object(supervisor,'before_cutoff',return_value=True):
            with self.assertRaises(Refused):
                self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        self.assertFalse(self.marker.exists())

    def test_nlb_protection_or_backend_change_refuses_before_child(self):
        self.nlb_proofs()
        path=Path(self.bindings['receiptPath'])
        receipt=json.loads(path.read_text())
        changed=expected_roster(self.target['originalInstanceIds'],'i-candidate',registered=True)
        next(row for row in changed['memberStates']
             if row['id'] == self.target['originalInstanceIds'][0])['lifecycle']='InService'
        receipt['rosterSha256']=roster_sha(changed)
        self.private_json(path,receipt)
        with patch.object(supervisor,'before_cutoff',return_value=True):
            with self.assertRaises(Refused):
                self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        self.assertFalse(self.marker.exists())

    def test_nlb_changed_pending_candidate_refuses_before_child(self):
        self.nlb_proofs()
        with self.journal._connection() as db:
            pending=json.loads(db.execute('SELECT proof FROM pending_binding').fetchone()['proof'])
            pending['newId']='i-other'
            db.execute('UPDATE pending_binding SET proof=?',(encode(pending).decode(),))
        with patch.object(supervisor,'before_cutoff',return_value=True):
            with self.assertRaises(Refused):
                self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        self.assertFalse(self.marker.exists())

    def test_nlb_new_uuid_cannot_bypass_semantic_action_key(self):
        self.nlb_proofs();self.behavior.write_text('nlb-ack')
        with patch.object(supervisor,'before_cutoff',return_value=True):
            first=self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        self.assertEqual(first['state'],'complete')
        self.nlb_receipt_successor(6)
        later=self.request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        receipt=json.loads(Path(self.bindings['receiptPath']).read_text())
        with self.assertRaises(Refused): self.journal.admit(later,receipt)
        with self.journal._connection() as db:
            self.assertEqual(db.execute('SELECT count(*) FROM mutation_keys').fetchone()[0],1)

    def nlb_receipt_successor(self,sequence):
        path=Path(self.bindings['receiptPath'])
        receipt=json.loads(path.read_text())
        receipt.update(sequence=sequence,observedAt=time.time())
        self.private_json(path,receipt)

    def test_nlb_ack_timeout_is_uncertain_and_not_retried(self):
        self.nlb_proofs();self.behavior.write_text('sleep')
        with patch.object(supervisor,'before_cutoff',return_value=True), \
                patch.object(self.owner,'_communicate',side_effect=Refused('timeout')) as communicate:
            result=self.run_request(action='deregister-new-node',deadlineMonotonic=monotonic()+20.)
        communicate.assert_called_once()
        self.assertEqual(result['state'],'uncertain')
        self.assertEqual(result['runState'],'uncertain')
        with self.journal._connection() as db:
            self.assertEqual(db.execute('SELECT count(*) FROM mutation_keys').fetchone()[0],1)
        self.assertIsNone(self.owner.child)

    def scalein_ready(self):
        pending,pre=self.nlb_proofs()
        zero=dict(runId=self.run,feedSession=self.session,newId=pending['newId'],
            originalInstanceIds=self.target['originalInstanceIds'],
            preDrainBindingSha256=hashlib.sha256(encode(pre)).hexdigest(),
            sequence=5,observedAt=time.time()-1,clientRosterSha256='b'*64,
            evidenceSha256='c'*64,sourceSha256=self.hashes['zeroProofWorker'])
        with self.journal._connection() as db:
            db.execute('INSERT INTO zero_binding VALUES (1,?,?)',(encode(zero).decode(),time.time()-1))
        absent=roster_sha(expected_roster(self.target['originalInstanceIds'],pending['newId'],registered=False))
        self.private_json(Path(self.bindings['receiptPath']),dict(feedSession=self.session,
            sequence=6,mode='disabled-rehearsal',desired=3,observedAt=time.time(),rosterSha256=absent))
        return zero,absent

    def test_scalein_uses_sanitized_activity_and_exact_one_use_intent(self):
        zero,absent=self.scalein_ready()
        self.behavior.write_text('activity-clear')
        activity=self.run_request(action='scaling-activity-check',deadlineMonotonic=monotonic()+5.)
        self.assertEqual(activity['result']['payload'],
                         {'status':'CLEAR','requestId':activity['requestId']})
        self.assertEqual(activity['runState'],'active')
        intent=dict(action='ModifyScalingGroup',runId=self.run,feedSession=self.session,
            sequence=6,fromDesired=3,toDesired=2,newId=zero['newId'],
            zeroProofSha256=hashlib.sha256(encode(zero)).hexdigest(),
            activityRequestId=activity['requestId'],rosterSha256=absent,
            at=datetime.now(timezone.utc).isoformat())
        self.private_json(Path(self.bindings['scaleinIntentPath']),intent)
        self.behavior.write_text('ess-ack')
        with patch.object(supervisor,'before_cutoff',return_value=True):
            result=self.run_request(action='scalein-three-to-two',deadlineMonotonic=monotonic()+20.)
        self.assertEqual(result['result'],dict(state='complete',category='ok',
                                               payload={'requestId':'request-ok'}))
        with self.journal._connection() as db:
            self.assertEqual(db.execute("SELECT count(*) FROM mutation_keys WHERE action='scalein-three-to-two'").fetchone()[0],1)

    def publish_no_scale_finish(self, status='PASS', forged=None, bound=True, exited=True, caller_pid=123, caller_binding='f'*64):
        from datetime import datetime,timezone,timedelta
        from pgcat_lane_protocol import (digest,no_scale_stop_ack,no_scale_assessment,
            publish_finish,roster_sha,original_roster)
        here=Path(__file__).resolve().parent
        hashes=dict(lane=self.hashes,local={k:hashlib.sha256((here/v).read_bytes()).hexdigest()
            for k,v in dict(controller='pgcat_rehearsal_controller.py',client='pgcat_lane_client.py',runner='pgcat_lane_runner.py').items()})
        worker=dict(runId=self.run,feedSession=self.session,sequence=2,sourceSha256=self.hashes['laneTimingAssessor'],
            payload=dict(status=status,category='lane-timing',verdictSha256='d'*64,
                clientIdentitySha256='a'*64,workerCredentialCleanupSha256='b'*64),
            collectorExit=dict(runId=self.run,feedSession=self.session,sequence=1,payload=dict(status='EXITED',
                identity=dict(pid=9,bootId='boot',startTicks=1,commandSha256='a'*64),
                exit=dict(category='completed',exitCode=0,completionSha256='c'*64))))
        intent=dict(schemaVersion=1,runId=self.run,feedSession=self.session,minimumSequence=3,verdictSha256='d'*64)
        receipt=dict(feedSession=self.session,sequence=3,mode='disabled-rehearsal',desired=2,observedAt=time.time(),
            rosterSha256=roster_sha(original_roster(self.target['originalInstanceIds'])))
        feed_exit=dict(pid=caller_pid,exitCode=0,category='ok',feedSession=self.session,sequence=3)
        record=dict(schemaVersion=1,day=datetime.now(timezone(timedelta(hours=8))).date().isoformat(),
            runId=self.run,feedSession=self.session,targetSha256=digest(self.target),sourceSha256=digest(hashes),
            sourceHashes=hashes,clientIdentitySha256='a'*64,bindingSha256=caller_binding,workerVerdict=worker,
            localCredentialCleanup=True,stop=dict(intent=intent,ack=no_scale_stop_ack(intent,receipt),receipt=receipt,feedExit=feed_exit))
        with self.journal._transaction() as db:
            db.execute("UPDATE run SET mode='no-scale'")
            db.execute('INSERT INTO lane_verdict VALUES (1,?)',(encode(worker).decode(),))
        if bound:
            binding=dict(runId=self.run,feedId=str(uuid.uuid4()),pid=123,bindingSha256='f'*64,argvSha256='a'*64)
            self.journal.bind_feed(binding)
            if exited:self.journal.confirm_feed_exit(dict(runId=self.run,feedId=binding['feedId'],pid=123,exitCode=0))
        self.private_json(self.root/'no-scale-proof.json',record)
        self.private_json(self.root.parent/'no-scale-stop.intent',intent)
        self.private_json(self.root.parent/'no-scale-stop.ack',record['stop']['ack'])
        self.private_json(Path(self.bindings['receiptPath']),receipt)
        assessment=no_scale_assessment(record,self.journal.run_snapshot(),worker,feed_exit)
        if forged:assessment['status']=forged
        publish_finish(self.root,self.run,feed_exit,assessment)
        return record,assessment

    def test_direct_finish_refuses_unbound_feed(self):
        self.publish_no_scale_finish(bound=False)
        with self.assertRaises(Refused):self.owner.finish()
        self.assertNotEqual(self.journal.run_snapshot()['runState'],'finished')

    def test_direct_finish_refuses_changed_caller_pid(self):
        self.publish_no_scale_finish(caller_pid=999)
        with self.assertRaises(Refused):self.owner.finish()
        self.assertNotEqual(self.journal.run_snapshot()['runState'],'finished')

    def test_direct_finish_refuses_changed_outer_binding(self):
        self.publish_no_scale_finish(caller_binding='e'*64)
        with self.assertRaises(Refused):self.owner.finish()
        self.assertNotEqual(self.journal.run_snapshot()['runState'],'finished')

    def test_direct_finish_refuses_live_unconfirmed_feed(self):
        self.publish_no_scale_finish(exited=False)
        with self.assertRaises(Refused):self.owner.finish()
        self.assertNotEqual(self.journal.run_snapshot()['runState'],'finished')

    def test_exit_record_refuses_unrelated_pid_and_feed_identity(self):
        self.publish_no_scale_finish(exited=False)
        with self.journal._connection() as db:
            binding=json.loads(db.execute('SELECT proof FROM feed_binding').fetchone()['proof'])
        good=dict(runId=self.run,feedId=binding['feedId'],pid=123,exitCode=0)
        for changes in (dict(pid=999),dict(feedId=str(uuid.uuid4())),dict(runId=str(uuid.uuid4())),dict(exitCode=False)):
            with self.subTest(changes=changes),self.assertRaises(Refused):
                self.journal.confirm_feed_exit(dict(good,**changes))
        with self.assertRaises(Refused):self.owner.finish()

    def test_no_scale_finish_recomputes_exact_journaled_proof(self):
        record,assessment=self.publish_no_scale_finish()
        result=self.owner.finish()
        self.assertEqual(result['assessment'],assessment)
        self.assertEqual(result['assessment']['status'],'PASS')

    def test_no_scale_finish_cannot_upgrade_journaled_sql_failure(self):
        self.publish_no_scale_finish('FAIL',forged='PASS')
        with self.assertRaises(Refused):self.owner.finish()
        self.assertNotEqual(self.journal.run_snapshot()['runState'],'finished')

    def test_no_scale_finish_refuses_changed_worker_digest(self):
        self.publish_no_scale_finish()
        path=self.root/'no-scale-proof.json';record=json.loads(path.read_text())
        record['workerVerdict']['payload']['verdictSha256']='e'*64
        self.private_json(path,record)
        with self.assertRaises(Refused):self.owner.finish()

    def test_no_scale_finish_refuses_changed_actual_receipt(self):
        self.publish_no_scale_finish()
        self.receipt(4)
        with self.assertRaises(Refused):self.owner.finish()

    def publish_normal_finish(self):
        from pgcat_lane_protocol import publish_finish
        publish_finish(self.root, self.run,
            dict(pid=123, exitCode=0, category='ok', feedSession=self.session, sequence=1),
            dict(status='INCONCLUSIVE', mode='disabled-rehearsal', feedSession=self.session,
                 endSequence=1, evidenceSha256='e'*64))

    def test_eof_during_finish_receipt_validation_refuses_commit(self):
        import pgcat_lane_protocol as protocol
        self.publish_normal_finish()
        reader, writer = os.pipe(); self.owner.liveness_fd = reader
        original = protocol.validate_finish_receipts
        def validate(*args):
            original(*args)
            os.close(writer)
        try:
            with patch.object(protocol, 'validate_finish_receipts', validate):
                with self.assertRaises(Refused): self.owner.finish()
            self.assertEqual(self.journal.run_snapshot()['runState'], 'uncertain')
            with self.journal._connection() as db:
                self.assertIsNone(db.execute('SELECT * FROM finish_receipts').fetchone())
        finally: os.close(reader); self.owner.liveness_fd = None

    def test_supervisor_creates_exclusive_context_bound_to_active_journal(self):
        import provider_feed as feed
        path = self.root/'run-context.json'
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        context = feed.validate_run_context(self.root, path)
        self.assertEqual(context['runId'], self.run)
        self.assertEqual(context['supervisorId'], self.owner.supervisor_id)
        self.assertEqual(context['bundleSha256'], self.bundle)
        self.assertEqual(context['targetSha256'], hashlib.sha256(encode(self.target)).hexdigest())
        self.assertEqual(set(context), {'schemaVersion','runId','supervisorId','targetSha256','bundleSha256','nonce'})
        copied = self.root/'other-context.json'
        copied.write_bytes(path.read_bytes()); copied.chmod(0o600)
        with self.assertRaises(ValueError): feed.validate_run_context(self.root, copied)
        path.write_bytes(encode(dict(context, nonce=str(uuid.uuid4()))))
        with self.assertRaises(ValueError): feed.validate_run_context(self.root, path)

    def test_context_rejects_duplicate_global_active_record(self):
        import provider_feed as feed
        path=self.global_dir/'active-run'
        path.write_text('{"runId":"'+self.run+'","runId":"'+self.run+'","supervisorId":"'+self.owner.supervisor_id+'","laneRoot":"'+str(self.root)+'"}')
        with self.assertRaises(ValueError):
            feed.validate_run_context(self.root,self.root/'run-context.json')

    def test_cloned_same_shaped_run_root_is_not_active_owner(self):
        import provider_feed as feed
        clone=self.base/'clone';clone.mkdir(mode=0o700)
        for name in ('journal.sqlite3','run-context.json','supervisor-startup.json'):
            target=clone/name;target.write_bytes((self.root/name).read_bytes());target.chmod(0o600)
        with self.assertRaises(ValueError):
            feed.validate_run_context(clone,clone/'run-context.json')

    def test_eof_while_finish_waits_for_transaction_refuses_commit(self):
        import sqlite3
        import threading
        import pgcat_lane_protocol as protocol
        self.publish_normal_finish()
        reader, writer = os.pipe(); self.owner.liveness_fd = reader
        locked = threading.Event(); validation_done = threading.Event()
        def locker():
            db = sqlite3.connect(str(self.journal.path), isolation_level=None)
            try:
                db.execute('BEGIN IMMEDIATE'); locked.set()
                validation_done.wait(2)
                time.sleep(.05)  # Finish is now waiting for this writer transaction.
                os.close(writer)
                db.execute('ROLLBACK')
            finally: db.close()
        thread = threading.Thread(target=locker); thread.start(); locked.wait(2)
        original = protocol.read_finish
        def read(*args):
            result = original(*args); validation_done.set(); return result
        try:
            with patch.object(protocol, 'read_finish', read):
                with self.assertRaises(Refused): self.owner.finish()
            self.assertEqual(self.journal.run_snapshot()['runState'], 'uncertain')
            with self.journal._connection() as db:
                self.assertIsNone(db.execute('SELECT * FROM finish_receipts').fetchone())
        finally:
            validation_done.set(); thread.join(3)
            os.close(reader); self.owner.liveness_fd = None

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
        args, env, budget = actions.ActionCatalog.command('baseline-status',self.bindings,self.run)
        self.assertIn('python3 -I -S -c',args[-1])
        self.assertIn(' status baseline ',args[-1])
        self.assertIn(self.run,args[-1])
        self.assertEqual(budget,5.0)
        self.assertNotIn('AWS_SECRET_ACCESS_KEY',env)

    def test_rehearsal_collector_action_argv_and_parser(self):
        import shlex
        from pgcat_lane_protocol import NO_SCALE_ACTIONS
        identity=dict(pid=9,bootId='boot',startTicks=1,commandSha256='a'*64)
        alive=dict(status='ALIVE',identity=identity)
        exited=dict(status='EXITED',identity=identity,
            exit=dict(category='completed',exitCode=0,completionSha256='c'*64))
        req=self.request()
        for action,verb,payloads in (
                ('rehearsal-collector-launch','rehearsal-launch',[dict(status='LAUNCHED')]),
                ('rehearsal-collector-status','rehearsal-status',[alive,exited])):
            with self.subTest(action=action):
                args,env,budget=actions.ActionCatalog.command(action,self.bindings,self.run)
                parts=shlex.split(args[-1])
                self.assertEqual(parts[:4],['python3','-I','-S','-c'])
                self.assertEqual(parts[5:11],['/reviewed/run/pgcat_collector_control.py',
                    self.hashes['collectorControl'],verb,'/reviewed/contract',self.run,'/reviewed/kubeconfig'])
                self.assertEqual(len(parts),12)
                self.assertEqual(json.loads(parts[11]),{k:self.hashes[k] for k in
                    set(actions.WORKER_SOURCES)|{'collectorControl','laneTimingAssessor'}})
                old,old_env,_=actions.ActionCatalog.command('collector-launch',self.bindings,self.run)
                self.assertEqual(args[:-1],old[:-1]);self.assertEqual(env,old_env)
                self.assertEqual(budget,5.0);self.assertNotIn(action,NO_SCALE_ACTIONS)
                for payload in payloads:
                    self.assertEqual(actions.ActionCatalog.parse(action,encode(payload),req),payload)
        for payload in (dict(status='RUNNING',identity=identity),dict(status='ALIVE'),
                dict(alive,extra='unexpected'),dict(status='ALIVE',identity=dict(identity,pid=True)),
                dict(exited,exit=dict(category='completed',exitCode=1,completionSha256='c'*64)),
                dict(exited,exit=dict(category='completed',exitCode=0))):
            with self.subTest(payload=payload),self.assertRaises(Refused):
                actions.ActionCatalog.parse('rehearsal-collector-status',encode(payload),req)

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

    def test_no_scale_rejects_pending_proof_before_child(self):
        with self.journal._transaction() as db: db.execute("UPDATE run SET mode='no-scale'")
        with self.assertRaises(Refused):
            self.owner.run_once(self.request(action='pending-proof')['requestId'])
        self.assertFalse(self.marker.exists())

    def test_pending_proof_seals_binding_from_fake_child(self):
        self.receipt(3,desired=3)
        prepared=self.request(action='pending-status')
        self.journal.admit(prepared)
        self.journal.seal(prepared['requestId'],dict(state='complete',category='ok',
            payload={'status':'PREPARED'}))
        self.journal.record_prepared(prepared['requestId'])
        self.receipt(4,desired=3)
        proof=dict(runId=self.run,feedSession=self.session,newId='i-new-one',
            originalInstanceIds=self.target['originalInstanceIds'],sequence=3,
            observedAt=time.time()-1,resourceVersion='11',
            clientRosterSha256='b'*64,evidenceSha256='c'*64,
            sourceSha256=self.hashes['pendingProofWorker'])
        output=json.dumps(proof)
        fixed=([sys.executable,'-c','print('+repr(output)+')'],
               {'PATH':os.defpath,'LANG':'C'},5.)
        with patch.object(actions.ActionCatalog,'command',return_value=fixed):
            row=self.run_request(action='pending-proof')
        self.assertEqual(row['state'],'complete')
        self.assertEqual(self.journal.pending_binding(),proof)
        with self.assertRaises(Refused):
            self.owner.run_once(self.request(action='pending-proof')['requestId'])

    def test_pending_prepared_seals_post_result_marker(self):
        self.receipt(2,desired=3)
        self.behavior.write_text('pending-prepared')
        row=self.run_request(action='pending-status')
        self.assertEqual(row['state'],'complete')
        self.assertEqual(row['result']['payload'],{'status':'PREPARED'})
        with self.journal._connection() as db:
            marker=db.execute('SELECT * FROM prepared_marker').fetchone()
        self.assertIsNotNone(marker)
        self.assertEqual(marker['request_id'],row['requestId'])
        self.assertEqual(marker['feed_session'],self.session)

    def test_pending_marker_failure_never_reports_active_prepared(self):
        self.receipt(2,desired=3)
        self.behavior.write_text('pending-prepared')
        with patch.object(self.journal,'record_prepared',side_effect=Refused('storage')):
            row=self.run_request(action='pending-status')
        self.assertEqual(row['state'],'complete')
        self.assertEqual(row['runState'],'uncertain')
        with self.journal._connection() as db:
            self.assertIsNone(db.execute('SELECT * FROM prepared_marker').fetchone())

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
        with self.assertRaises(Refused):
            actions.ActionCatalog.command('baseline-status',bindings,self.run)
        with self.assertRaises(Refused):
            actions.ActionCatalog.command('baseline-status',dict(self.bindings,phaseWorker='/x/../y'),self.run)

    def test_transitive_worker_hash_change_refuses_supervisor_source_gate(self):
        self.owner.bindings['sourceHashes']['captureWindow'] = '0' * 64
        with self.assertRaises(Refused):
            self.owner._verify_sources()

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
