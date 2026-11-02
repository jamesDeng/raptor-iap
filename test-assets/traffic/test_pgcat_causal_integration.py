"""Offline causal ingress boundaries: private originals, never live services."""
import copy
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch
import pgcat_causal_scrape as causal
import pgcat_native_controller as native
import pgcat_native_ingress as ingress
from test_pgcat_native_ingress import Fixture,START,plan,write
import test_pgcat_causal_scrape as causal_tests


class IntegrationContractTests(unittest.TestCase):
    def test_source_closure_contains_kernel_only_in_native_mode(self):
        from pgcat_source_bootstrap import NATIVE_SOURCES,SOURCES
        from pgcat_lane_actions import NATIVE_SOURCE_NAMES
        self.assertEqual(NATIVE_SOURCES.get('causalScrape'),'pgcat_causal_scrape.py')
        self.assertIn('causalScrape',NATIVE_SOURCE_NAMES)
        self.assertNotIn('causalScrape',SOURCES)

    def test_current_owner_ceiling_is_exact_and_bounded(self):
        value=dict(schemaVersion=1,bindingSha256='a'*64,checkedAt=START,expiresAt=START+300,
            singleOperator=True,producersQuiesced=True,cost=dict(day='2026-10-29',net=2000,reserve=120,allowance=600,ceiling=3000))
        native.validate_preflight(value,'a'*64,plan(),START+1)
        for cost in (dict(value['cost'],ceiling=3001),dict(value['cost'],ceiling=1800),dict(value['cost'],allowance=1200)):
            with self.assertRaises(ValueError):native.validate_preflight(dict(value,cost=cost),'a'*64,plan(),START+1)

    def test_native_cli_remains_closed_even_if_source_capability_admits(self):
        with patch.object(native.sys,'argv',['native','run-native','binding','template','preflight']),patch.object(native,'_closed'),patch.object(native,'_require_verified'),patch('pgcat_native_closure.runtime_admission'),patch.object(native,'NativeController') as owner:
            self.assertEqual(native.main(),1)
            owner.assert_not_called()
        with patch('pgcat_native_closure.runtime_admission'):
            with self.assertRaises(ValueError):native.NativeController.run_native(object(),'template','preflight')

    def test_diagnostic_operation_assessment_requires_original_causal_read(self):
        import pgcat_native_assessment as assessment
        from types import SimpleNamespace
        chain=SimpleNamespace(plan=plan('disabled-proof'),run=Path('/unused'),context={})
        with patch.object(assessment,'read_progress',return_value=({},None,None)),patch('pgcat_native_closure._coverage_prefix',return_value=(['a','b'],{})):
            with self.assertRaises(ValueError):assessment._operation_assessment(chain,{'ingressSha256':'a'*64},[])

    def test_resume_and_pending_export_are_restricted_interfaces(self):
        self.assertTrue(callable(getattr(causal,'pending_evidence',None)))
        self.assertTrue(callable(getattr(causal,'restore_ingress_pending',None)))
        with self.assertRaises(ValueError):causal.pending_evidence({})
        with self.assertRaises(ValueError):causal.restore_ingress_pending(gate=object(),run=Path('/tmp/run'),binding={})


class CollectionBudgetTests(unittest.TestCase):
    def setUp(self):
        helper=causal_tests.CausalCollectionTests();helper.setUp();self.addCleanup(helper.doCleanups)
        self.reader=helper.reader
    # Inherit real fixed-reader transport fixtures, not success callbacks.
    def test_total_collection_budget_does_not_replace_short_action_io_deadline(self):
        self.reader.deadline=time.monotonic()+.5
        original=self.reader.deadline
        deadline=time.monotonic()+25
        result=causal.collect_causal_scrapes(source_read=self.reader.metrics_source,prometheus_get=self.reader.prometheus_get,
            nodes=__import__('test_pgcat_causal_scrape').NODES,metric_groups=__import__('test_pgcat_causal_scrape').GROUPS,
            max_combined=1,now=lambda:104.,deadline=deadline)
        self.assertEqual(result['status'],'VERIFIED')
        self.assertLessEqual(self.reader.deadline,original)
        self.assertEqual(causal.issued_evidence(result['receipt'])['deadlineMonotonic'],deadline)


class OriginalIngressTests(unittest.TestCase):
    def test_whole_original_chain_interface_and_forged_owner_refuse(self):
        self.assertTrue(callable(getattr(ingress,'validate_causal_read',None)))
        self.assertTrue(callable(getattr(ingress,'_causal_continuation',None)))
        with self.assertRaises(ValueError):ingress._causal_continuation(object(),Path('/tmp/run'),{})

    def test_old_retirement_schema_cannot_claim_new_causal_authority(self):
        from types import SimpleNamespace
        chain=SimpleNamespace()
        with self.assertRaises(ValueError):ingress.validate_retirement_witness({'retirementWitness':{'schemaVersion':1}},chain)

# Tests substitute ONLY the network/verified-entry boundaries. Collection uses
# the real fixed reader, kernel issuance, ingress seal, original files and parser.
from contextlib import contextmanager
@contextmanager
def causal_fixture(f):
    from contextlib import ExitStack
    import test_pgcat_causal_scrape as samples
    with ExitStack() as stack:
        reader=object.__new__(native.FixedWorkerReads);reader.deadline=time.monotonic()+5
        raw=copy.deepcopy(getattr(f,'causal_source',samples.source()))
        nodes=f.baseline['originals']
        for target,node in zip(raw['targets']['activeTargets'],nodes):
            target.update(labels=samples.labels(node),scrapeUrl='http://'+node['address']+':9930/metrics',discoveredLabels=dict(__address__=node['address']+':9930'))
        def response(path):
            f.calls.append(('metrics',path))
            if path=='/api/v1/status/tsdb':return dict(status='success',data=dict(headStats=dict(maxTime=int((f.now-1)*1000))))
            if path.startswith('/api/v1/query?'):
                matrix=samples.matrix(getattr(f,'causal_times',(f.now-.3,f.now-.2,f.now-.1)),getattr(f,'causal_totals',None))
                for row in matrix['data']['result']:
                    node=nodes[0 if row['metric']['ecs_instance_id']=='old-a' else 1]
                    row['metric'].update(ecs_instance_id=node['id'],instance=node['address']+':9930')
                if hasattr(f,'causal_matrix_change'):f.causal_matrix_change(matrix)
                return matrix
            key={'/api/v1/status/config':'config','/api/v1/status/buildinfo':'build','/api/v1/status/runtimeinfo':'runtime','/api/v1/status/flags':'flags','/api/v1/targets?state=active':'targets','/api/v1/rules':'rules'}[path]
            return dict(status='success',data=dict(yaml=raw[key]) if key=='config' else copy.deepcopy(raw[key]))
        f.reads=reader;f.prior_ingress=lambda:copy.deepcopy(getattr(f,'causal_prior',None))
        for mock in (patch.object(causal,'SOURCE_POLICY',samples.fixture_policy()),patch.object(reader,'_metrics_kubernetes',return_value=raw['kubernetes']),
            patch.object(reader,'_prometheus_get',side_effect=response),patch('pgcat_source_bootstrap.require_verified'),
            patch.object(ingress,'_worker_boot',return_value=getattr(f,'causal_boot','11111111-1111-4111-8111-111111111111')),
            patch.object(ingress,'_causal_owner',side_effect=lambda gate,chain:reader if gate is f and chain.run==f.run else (_ for _ in ()).throw(ValueError('fixture owner')))):
            stack.enter_context(mock)
        yield reader


class ContinuationTests(unittest.TestCase):
    def setUp(self):
        from test_pgcat_native_ingress import DiagnosticTests
        from test_pgcat_native_evidence import rolling,kubernetes
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        root=Path(self.tmp.name).resolve();root.chmod(0o700);self.f=Fixture(root,'disabled-proof')
        restart=DiagnosticTests();restart.f=self.f;restart.restart()
        for n in (2,3,4):
            value=rolling(n-1);value['inventory']=copy.deepcopy(self.f.frame['inventory']);self.f.append(n,value)
        self.f.live=kubernetes(3);self.addCleanup(ingress._close_origins)
    def read(self):
        with causal_fixture(self.f):return ingress.seal_ingress(self.f.run,ingress.read_ingress(self.f.run,{},gate=self.f,kube_read=self.f.kube,http_get=self.f.http,now=lambda:self.f.now))
    def test_pending_original_resumes_without_renewing_seed_or_watermark(self):
        self.f.causal_times=(self.f.now-.2,self.f.now-.1)
        first=self.read();self.assertIn('causalRead',first['record'])
        self.assertEqual(first['record']['causalRead']['status'],'PENDING')
        self.f.causal_prior=first['ingressFile'];del self.f.causal_times
        second=self.read();a=first['record']['causalRead'];b=second['record']['causalRead']
        self.assertEqual(b['status'],'VERIFIED')
        for key in ('source','watermarkMs','startedMonotonic','deadlineMonotonic'):self.assertEqual(a['evidence'][key],b['evidence'][key])
        self.assertEqual(b['evidence']['passes'],2);self.assertEqual(a['rolloutFrame'],b['rolloutFrame'])
        self.assertEqual(sum(row==('metrics','/api/v1/status/tsdb') for row in self.f.calls),1)
    def test_boot_source_original_tamper_and_deadline_refuse_resume(self):
        self.f.causal_times=(self.f.now-.2,self.f.now-.1);first=self.read();self.f.causal_prior=first['ingressFile'];del self.f.causal_times
        self.f.causal_boot='22222222-2222-4222-8222-222222222222'
        with self.assertRaises(ValueError):self.read()
        del self.f.causal_boot
        import test_pgcat_causal_scrape as samples
        self.f.causal_source=samples.source();self.f.causal_source['runtime']['lastConfigTime']='2026-11-02T00:40:00Z'
        with self.assertRaises(ValueError):self.read()
        del self.f.causal_source
        path=self.f.run/first['ingressFile']['basename'];path.write_bytes(path.read_bytes()+b' ')
        with self.assertRaises(ValueError):self.read()
    def test_diagnostic_permits_counts_above_one_but_never_partial_samples(self):
        self.f.causal_totals={'old-a':(3,4,5),'old-b':(6,7,8)}
        value=self.read()['record'];self.assertEqual(value['causalRead']['evidence']['samples']['combinedMaximum'],13)
        self.f.causal_matrix_change=lambda matrix:matrix['data']['result'].pop()
        with self.assertRaises(ValueError):self.read()


def bind_fixture_policy(test):
    mock=patch.object(causal,'SOURCE_POLICY',causal_tests.fixture_policy());mock.start();test.addCleanup(mock.stop)

class NativeContinuationTests(unittest.TestCase):
    def setUp(self):
        from test_pgcat_native_ingress import REQUEST,APPROVAL,timestamp
        from test_pgcat_native_evidence import rolling,kubernetes
        bind_fixture_policy(self)
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        root=Path(self.tmp.name).resolve();root.chmod(0o700);f=Fixture(root);self.f=f
        for n in (2,3,4):
            value=copy.deepcopy(f.frame if n==2 else rolling(n-1))
            value['inventory'].update(desired=4,members=['a','b','c','d'],healthyRegistered=['c','d'],memberStates=[dict(id=node,lifecycle='Protected',health='Healthy',addressKnown=True) for node in ('a','b','c','d')],backendStates=[dict(id=node,status='Available',weight=100,listenerHealth='Normal') for node in ('c','d')])
            value['metrics']=copy.deepcopy(f.frame['metrics']);f.append(n,value)
        f.live=kubernetes(3)
        f.execution.update(status='waiting_approval',stage='waiting_approval',attemptId='attempt-2',cleanupStatus=dict(sandboxAbsent=True,keyAbsent=True,accessRevoked=True))
        f.events=[]
        for n,tool in enumerate(('mcp__infra__db_proxy_scale','mcp__infra__deployment_restart','mcp__raptor__approval_request'),1):
            f.events.append(dict(eventId='e'+str(n),requestId=REQUEST,attemptId='attempt-2',sequence=n,occurredAt=timestamp(START+2),kind='tool_start',summary='safe',details=dict(tool=tool,status='started',runtimeSequence=n),evidenceMode='live'))
        self.approval=dict(approvalId=APPROVAL,requestId=REQUEST,actionId='shrink',state='pending',guidance=None,binding=dict(requestId=REQUEST,actionId='shrink',interface='db-proxy.scale-in',envCode='rdev.ali',target=dict(proxyCode='DP00001'),parameters=dict(desiredCapacity=2)))
        original=f.http
        def http(origin,path,auth):
            if path=='/v1/approvals/'+APPROVAL:
                f.calls.append((origin,path));return __import__('segmented_capture').canonical(dict(data=self.approval))
            return original(origin,path,auth)
        f.http=http;self.addCleanup(ingress._close_origins)
    def read(self):
        from test_pgcat_native_ingress import REQUEST,APPROVAL
        with causal_fixture(self.f):return ingress.seal_ingress(self.f.run,ingress.read_ingress(self.f.run,dict(requestId=REQUEST,approvalIds=[APPROVAL]),gate=self.f,kube_read=self.f.kube,http_get=self.f.http,now=lambda:self.f.now))
    def test_actual_read_is_inside_same_released_wait_and_binds_exact_action(self):
        from test_pgcat_native_ingress import REQUEST,APPROVAL
        record=self.read()['record'];witness,pair=ingress.validate_retirement_witness(record,ingress._Chain(self.f.run))
        self.assertEqual(witness['schemaVersion'],2);self.assertEqual(pair,['c','d'])
        binding=record['causalRead']['binding'];self.assertEqual(binding['wait']['requestId'],REQUEST)
        self.assertEqual(binding['wait']['attemptId'],'attempt-2');self.assertEqual(binding['approval']['approvalId'],APPROVAL)
        self.assertEqual(binding['approval']['actionId'],'shrink');self.assertEqual(binding['generation'],2)
        self.assertEqual(len(binding['successors']),2)
        calls=self.f.calls;gets=[i for i,row in enumerate(calls) if len(row)==2 and row[1]=='/v1/requests/'+REQUEST]
        watermark=calls.index(('metrics','/api/v1/status/tsdb'))
        self.assertLess(gets[0],watermark);self.assertLess(watermark,gets[1])
    def test_pending_then_changed_wait_or_approval_never_reseeds(self):
        self.f.causal_times=(self.f.now-.2,self.f.now-.1);first=self.read()
        self.assertIsNone(first['record']['retirementWitness']);self.f.causal_prior=first['ingressFile'];del self.f.causal_times
        self.f.execution['attemptId']='attempt-3'
        with self.assertRaises(ValueError):self.read()
        self.f.execution['attemptId']='attempt-2';self.approval['state']='approved'
        with self.assertRaises(ValueError):self.read()
        self.approval['state']='pending';second=self.read()
        self.assertIsNotNone(second['record']['retirementWitness'])
    def test_threshold_and_missing_sample_age_do_not_issue_witness(self):
        self.f.causal_totals={'old-a':(1,0,0),'old-b':(0,1,0)}
        with self.assertRaises(ValueError):self.read()
        del self.f.causal_totals;self.f.causal_times=(self.f.now-16,self.f.now-.2,self.f.now-.1)
        result=self.read()['record'];self.assertIsNone(result['retirementWitness'])
        self.assertEqual(result['causalRead']['status'],'PENDING')
    def test_resume_race_after_metrics_discards_actual_read(self):
        from test_pgcat_native_ingress import REQUEST
        http=self.f.http;count=[0]
        def raced(*args):
            if args[1]=='/v1/requests/'+REQUEST:
                count[0]+=1
                if count[0]>=2:self.f.execution['attemptId']='resumed'
            return http(*args)
        self.f.http=raced
        with self.assertRaises(ValueError):self.read()
        self.assertFalse(list(self.f.run.glob('native-ingress-*.json')))
    def test_no_completed_rollout_means_no_source_watermark_or_witness(self):
        # An actual initial cohort frame cannot seed a post-rollout proof.
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        f=Fixture(root);self.f=f
        f.execution.update(status='waiting_approval',stage='waiting_approval',cleanupStatus=dict(sandboxAbsent=True,keyAbsent=True,accessRevoked=True))
        original=f.http
        from test_pgcat_native_ingress import APPROVAL
        f.http=lambda origin,path,auth:__import__('segmented_capture').canonical(dict(data=self.approval)) if path=='/v1/approvals/'+APPROVAL else original(origin,path,auth)
        record=self.read()['record'];self.assertIsNone(record['causalRead']);self.assertIsNone(record['retirementWitness'])
        self.assertFalse(any(row[0]=='metrics' for row in f.calls))


class CausalAssessmentRegressionTests(unittest.TestCase):
    def test_native_final_order_uses_causal_read_despite_guest_clock_offset(self):
        from test_pgcat_native_assessment import native_complete_fixture
        from pgcat_native_assessment import assess_native
        f,a=native_complete_fixture(self,guest_skew=1000)
        with patch('pgcat_collector_control.process_identity',return_value=None):result=assess_native(f.run)
        self.assertEqual(result['status'],'PASS',result)

class ApprovalRaceTests(unittest.TestCase):
    setUp=NativeContinuationTests.setUp
    read=NativeContinuationTests.read
    def test_approval_changed_during_source_read_is_rejected(self):
        self.f.causal_matrix_change=lambda matrix:self.approval.update(state='approved')
        with self.assertRaises(ValueError):self.read()
        self.assertFalse(list(self.f.run.glob('native-ingress-*.json')))

class EvidenceValidationTests(unittest.TestCase):
    setUp=ContinuationTests.setUp
    read=ContinuationTests.read
    def test_malformed_serialized_source_is_sanitized_refusal(self):
        value=self.read()['record']['causalRead']['evidence']
        for key in ('imageId','targets','containerId','activeRules'):
            bad=copy.deepcopy(value);bad['source'][key]=None
            with patch.object(causal,'SOURCE_POLICY',causal_tests.fixture_policy()),self.subTest(key=key),self.assertRaises(ValueError):causal.validate_read_evidence(bad)
    def test_original_collection_expiry_cannot_be_reset_by_a_new_action(self):
        self.f.causal_times=(self.f.now-.2,self.f.now-.1);first=self.read();self.f.causal_prior=first['ingressFile'];del self.f.causal_times
        until=first['record']['causalRead']['evidence']['deadlineMonotonic']
        with patch('time.monotonic',return_value=until+.001),self.assertRaises(ValueError):self.read()
        self.assertEqual(sum(row==('metrics','/api/v1/status/tsdb') for row in self.f.calls),1)
    def test_missing_earliest_original_invalidates_whole_pending_chain(self):
        self.f.causal_times=(self.f.now-.2,self.f.now-.1);first=self.read();self.f.causal_prior=first['ingressFile']
        second=self.read();self.f.causal_prior=second['ingressFile'];del self.f.causal_times
        (self.f.run/first['ingressFile']['basename']).unlink()
        with self.assertRaises(ValueError):self.read()
    def test_verified_original_retains_every_discovered_group_on_next_read(self):
        def discover(matrix):
            for row in copy.deepcopy(matrix['data']['result'][1:4]):
                row['metric'].update(pool='discovered',user='another');matrix['data']['result'].append(row)
        self.f.causal_matrix_change=discover;first=self.read();self.f.causal_prior=first['ingressFile'];del self.f.causal_matrix_change
        with self.assertRaises(ValueError):self.read()
    def test_pending_evidence_cannot_be_resealed_from_a_copied_projection(self):
        self.f.causal_times=(self.f.now-.2,self.f.now-.1);first=self.read()
        with self.assertRaises(ValueError):ingress.seal_ingress(self.f.run,ingress._Projection(copy.deepcopy(first['record'])))
        with self.assertRaises(ValueError):causal.pending_evidence(first['record']['causalRead']['evidence'])


class JournalContinuationTests(unittest.TestCase):
    def test_journal_selects_latest_completed_original_not_selector_input(self):
        from test_pgcat_native_controller import Fixture as PinnedFixture
        from test_pgcat_native_ingress import file_record
        from pgcat_lane_protocol import Journal,encode,digest
        import json
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve();root.chmod(0o700);f=PinnedFixture(root,'disabled-proof')
            lane=root/'lane';lane.mkdir(mode=0o700);journal=Journal(lane)
            raw=(f.run/'native-plan.json').read_bytes();(lane/'native-plan.json').write_bytes(raw);(lane/'native-plan.json').chmod(0o600)
            first=dict(basename='native-ingress-0001.json',identity=file_record(f.run/'native-plan.json'))
            last=dict(basename='native-ingress-0002.json',identity=file_record(f.run/'native-baseline.json'))
            request=dict(requestId=f.admitted['localRequestId'],action='native-ingress',deadlineMonotonic=123.)
            write(lane/('native-selector-'+request['requestId']+'.json'),{})
            with patch('pgcat_lane_protocol._release_admission'),journal._transaction() as db:
                db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',(f.plan['runId'],native.MODE,encode(f.plan['target']).decode(),encode(dict(f.plan['sourceHashes'],bundleSha256=digest(f.plan['sourceHashes']))).decode(),'active',None,None))
                db.execute('INSERT INTO native_plan VALUES (1,?,?)',(raw,encode(file_record(lane/'native-plan.json')).decode()))
                db.execute('INSERT INTO native_prepared VALUES (1,?)',(encode(f.prepared).decode(),))
                db.execute('INSERT INTO feed_binding VALUES (1,?)',(encode(f.feed).decode(),))
                db.execute('INSERT INTO native_support VALUES (?,?)',('ready',encode(dict(action='native-collector-launch',outputs=dict(readyFile=file_record(f.run/'native-ready.json'),ready=f.ready))).decode()))
                for name,artifact in [('earlier',first),('latest',last)]:
                    db.execute('INSERT INTO native_support VALUES (?,?)',(name,encode(dict(action='native-ingress',outputs=dict(ingressFile=artifact))).decode()))
                journal._native_admit_in_tx(db,request)
                proof=json.loads(db.execute('SELECT proof FROM native_admission').fetchone()['proof'])
                self.assertEqual(proof['envelope']['priorIngress'],last)
                self.assertEqual(proof['selector'],{})
                with self.assertRaises(ValueError):native.validate_selector(f.plan,'native-ingress',dict(priorIngress=first))

class DiagnosticOriginalTests(unittest.TestCase):
    def test_changed_restart_original_cannot_requalify_causal_diagnostic(self):
        from test_pgcat_native_assessment import diagnostic_complete_fixture
        from pgcat_native_assessment import assess_native
        import json
        f,a=diagnostic_complete_fixture(self)
        path=f.run/'native-restart-receipt.json';value=json.loads(path.read_text());value['deploymentUid']='changed';write(path,value)
        with patch('pgcat_collector_control.process_identity',return_value=None):result=assess_native(f.run)
        self.assertEqual(result['operationStatus'],'INCONCLUSIVE',result)


class DiagnosticAdmissionTests(unittest.TestCase):
    def test_old_source_plan_is_not_a_causal_capability(self):
        from pgcat_native_closure import runtime_admission
        current=plan('disabled-proof');runtime_admission(current)
        old=copy.deepcopy(current);old['sourceHashes'].pop('causalScrape')
        with self.assertRaises(ValueError):runtime_admission(old)

if __name__=='__main__':unittest.main()
