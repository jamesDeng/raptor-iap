import copy, tempfile, unittest
from pathlib import Path
from capture_window import CaptureWindow, assess as raw_assess, retain, clean
def assess(frames,**kwargs):
    kwargs.setdefault("completed",True);return raw_assess(frames,**kwargs)

def frame(t, n, final=False):
    s={'processId':'process','at':t,'sequence':n*2,'Scheduled':n,'Attempts':n,'Success':n,'Failure':0,'Timeout':0,'Ambiguous':0,'Skipped':0}
    events=[]
    for i in range(1,n+1):
        events.extend([{'processId':'process','sequence':i*2-1,'at':i,'kind':'scheduled','operationId':str(i)}, {'processId':'process','sequence':i*2,'at':i,'kind':'operation','operationId':str(i),'outcome':'success'}])
    return {'at':t,'pods':[{'uid':'pod','snapshot':{'sample':s,'events':events,'connected':0 if final else 4,'truncated':False,'final':final}}], 'inventory':{'observedAt':t,'desired':2,'members':['a','b'],'healthyRegistered':['a','b']},'metrics':{'observedAt':t,'sourceAt':t,'nodes':['a','b'],'available':True,'raw':[{'node':node,'pool':'test','user':'poc_app','kind':kind,'value':0,'sourceAt':t} for node in ['a','b'] for kind in ['active','idle','waiting']]}}

class WindowTests(unittest.TestCase):
    def good(self):
        a=[frame(0,0),frame(1,1),frame(2,2,True)];a[-1]['phase']='settling';return a
    def test_monotonic_complete(self):
        r=assess(self.good());self.assertEqual(r['status'],'PASS');self.assertEqual(r['success'],2)
    def test_failure_timeout_ambiguous(self):
        for name in ['Failure','Timeout','Ambiguous']:
            with self.subTest(name=name):
                a=self.good();a[-1]['pods'][0]['snapshot']['sample'][name]=1
                self.assertEqual(assess(a)['status'],'FAIL')
    def test_skipped_inconclusive(self):
        a=self.good();a[-1]['pods'][0]['snapshot']['sample']['Skipped']=1
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_counter_reset(self):
        a=self.good();a[-1]['pods'][0]['snapshot']['sample']['Success']=0
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_missed_poll(self):
        a=self.good();a[-1]['at']=8
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_event_ring_gap(self):
        a=self.good();a[-1]['pods'][0]['snapshot']['events']=[];a[-1]['pods'][0]['snapshot']['truncated']=True
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_complete_ring_after_truncation(self):
        a=self.good();a[-1]['pods'][0]['snapshot']['truncated']=True
        self.assertEqual(assess(a)['status'],'PASS')
    def test_in_flight_end(self):
        a=self.good();a[-1]['pods'][0]['snapshot']['sample']['Scheduled']=3
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_missing_metrics(self):
        a=self.good();a[1]['metrics']['available']=False
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_cancelled_or_unfinished_capture(self):
        self.assertEqual(assess(self.good(),cancelled=True)['status'],'INCONCLUSIVE')
        self.assertEqual(assess(self.good(),completed=False)['status'],'INCONCLUSIVE')
    def test_new_pod_does_not_erase_missing_final(self):
        a=self.good();a[1]['pods'][0]['snapshot']['final']=False;a[-1]=frame(2,0)
        a[-1]['pods'][0]['uid']='newpod';a[-1]['pods'][0]['snapshot']['sample']['processId']='newprocess'
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_final_old_pod_survives_disappearance(self):
        a=self.good();a.append(frame(3,0));a[-1]['phase']='settling';a[-1]['pods'][0]['uid']='newpod';s=a[-1]['pods'][0]['snapshot'];s['sample']['processId']='newprocess';s['events']=[]
        self.assertEqual(assess(a)['status'],'PASS')
    def test_readiness_not_zero_traffic_proof(self):
        self.assertEqual(assess([frame(0,0),frame(1,0)])['status'],'INCONCLUSIVE')
    def test_exact_metrics_coverage(self):
        a=self.good();a[1]['metrics']['nodes']=['a']
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_evidence_retained_without_prometheus(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'evidence.jsonl';w=CaptureWindow(p)
            for f in self.good():w.append(f)
            retain(p,completed=True)
            self.assertEqual(assess(list(w.read()))['status'],'PASS')
            self.assertTrue(p.exists())
    def test_stale_provider_and_metrics(self):
        for key in ['inventory','metrics']:
            a=self.good();a[-1][key]['observedAt']=-100
            self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_final_snapshot_cannot_change(self):
        a=self.good();a.append(frame(3,3,True));self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_duplicate_process_across_pods(self):
        a=self.good();other=copy.deepcopy(a[1]['pods'][0]);other['uid']='another';a[1]['pods'].append(other)
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_delayed_discovery_with_complete_in_window_history(self):
        a=self.good();a.append(frame(3,1));a[-1]['phase']='settling'
        a[-1]['pods'][0]['uid']='newpod';snap=a[-1]['pods'][0]['snapshot'];snap['sample']['processId']='newprocess'
        for e in snap['events']:e['processId']='newprocess'
        self.assertEqual(assess(a)['status'],'PASS')
    def test_new_process_history_before_window_refuses(self):
        a=self.good();a.append(frame(3,1));a[-1]['phase']='settling'
        a[-1]['pods'][0]['uid']='newpod';snap=a[-1]['pods'][0]['snapshot'];snap['sample']['processId']='newprocess'
        for e in snap['events']:e['processId']='newprocess';e['at']=-1
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_uncovered_new_process_history(self):
        a=self.good();a.append(frame(3,1));a[-1]['pods'][0]['uid']='newpod';snap=a[-1]['pods'][0]['snapshot'];snap['sample']['processId']='newprocess'
        for e in snap['events']:e['processId']='newprocess'
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_nonfinite_timestamp(self):
        a=self.good();a[1]['at']=float('nan');self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_finalized_only_roster_during_measurement(self):
        a=[frame(0,0),frame(1,1,True)]+[frame(t,1,True) for t in range(2,11)]
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_stalled_active_scheduling(self):
        a=[frame(0,0),frame(1,1)]+[frame(t,1) for t in range(2,11)]
        self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_up_without_expected_pool_metrics(self):
        a=self.good();a[1]['metrics']['raw']=[];self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_missing_raw_triplet(self):
        a=self.good();a[1]['metrics']['raw'].pop();self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_stale_or_invalid_raw_observation(self):
        for key,value in [('sourceAt',-100),('value',-1),('value',0.5),('value',float('nan'))]:
            a=self.good();a[1]['metrics']['raw'][0][key]=value;self.assertEqual(assess(a)['status'],'INCONCLUSIVE')
    def test_completion_required_by_default(self):
        self.assertEqual(raw_assess(self.good())['status'],'INCONCLUSIVE')
    def test_first_boundary_does_not_retain_old_full_rings(self):
        with tempfile.TemporaryDirectory() as d:
            w=CaptureWindow(Path(d)/'e.jsonl');w.append(frame(10000,10000))
            self.assertEqual(next(w.read())['pods'][0]['snapshot']['events'],[])
    def test_output_is_allowlisted(self):
        with tempfile.TemporaryDirectory() as d:
            w=CaptureWindow(Path(d)/'e.jsonl');f=frame(0,0);f['password']='never retain';f['metrics']['raw'][0]['password']='never retain'
            w.append(f);self.assertNotIn('password',w.path.read_text())



def transition_frame(t, members=('a','b'), backends=None, pending=(), metrics=None, desired=None):
    f=frame(t,t)
    if backends is None:backends=members
    if metrics is None:metrics=backends
    f['inventory'].update(schemaVersion=2,feedSession='00000000-0000-4000-8000-000000000001',sequence=t+1,
        desired=len(members) if desired is None else desired,members=list(members),
        memberStates=[dict(id=n,lifecycle='Pending' if n in pending else 'Protected',health='Healthy',addressKnown=n not in pending) for n in members],
        backendStates=[dict(id=n,status='Available',weight=100,listenerHealth='Normal') for n in backends],
        healthyRegistered=[n for n in backends if n in members])
    f['metrics'].update(nodes=list(metrics),raw=[dict(node=n,pool='test',user='poc_app',kind=k,value=0,sourceAt=t) for n in metrics for k in ('active','idle','waiting')])
    return f

class UpScrapeEvidenceTests(unittest.TestCase):
    def test_per_node_scrapes_survive_cleaning_without_extra_metrics(self):
        f=transition_frame(1)
        f['metrics']['upSourceAt']={'a':1000.0,'b':1001.0}
        f['metrics']['private']='do not retain'
        cleaned=clean(f)
        self.assertEqual(cleaned['metrics'].get('upSourceAt'),{'a':1000.0,'b':1001.0})
        self.assertNotIn('private',cleaned['metrics'])
        self.assertEqual(clean(cleaned),cleaned)
        self.assertNotIn('upSourceAt',clean(transition_frame(1))['metrics'])
        with tempfile.TemporaryDirectory() as directory:
            window=CaptureWindow(Path(directory)/'capture.jsonl')
            window.append(f)
            self.assertEqual(next(window.read())['metrics']['upSourceAt'],{'a':1000.0,'b':1001.0})
            self.assertNotIn('do not retain',window.path.read_text())
        from segmented_capture import SegmentedCapture
        with tempfile.TemporaryDirectory() as directory:
            window=SegmentedCapture(Path(directory)/'capture')
            window.append(f)
            self.assertEqual(next(window.window.read())['metrics']['upSourceAt'],{'a':1000.0,'b':1001.0})

    def test_invalid_per_node_scrapes_do_not_look_available(self):
        for invalid in ({'a':1000.0}, {'a':1000.0,'b':float('nan')},
                        {'a':1000.0,'b':1001.0,'extra':1001.0},
                        {'a':1000.0,'b':True}, {'a':1000.0,'b':10**1000},
                        {'a':1000.0,'b':1001.0,'https://private.example/secret':1002.0}):
            with self.subTest(invalid=invalid):
                f=transition_frame(1)
                f['metrics']['upSourceAt']=invalid
                cleaned=clean(f)
                self.assertFalse(cleaned['metrics']['available'])
                self.assertEqual(cleaned['metrics']['raw'],[])
                self.assertNotIn('upSourceAt',cleaned['metrics'])


def transition_window():
    frames=[transition_frame(0),transition_frame(1,('a','b','c','d'),('a','b'),('c','d')),
            transition_frame(2,('a','b','c','d')),transition_frame(3,('b','c','d'),('b','c','d'),metrics=('a','b','c','d')),
            transition_frame(4,('b','c','d')),transition_frame(5,('c','d'),metrics=('b','c','d')),
            transition_frame(6,('c','d'))]
    frames[-1]['phase']='settling';frames[-1]['pods'][0]['snapshot'].update(final=True,connected=0)
    return frames


class TransitionWindowTests(unittest.TestCase):
    def test_lane_opt_out_skips_only_transition_completion(self):
        fs=[transition_frame(t) for t in range(3)]
        for f in fs:f['inventory']['mode']='disabled-rehearsal'
        self.assertEqual(raw_assess(fs,completed=True)['status'],'INCONCLUSIVE')
        self.assertEqual(raw_assess(fs,completed=True,require_transition=False)['status'],'PASS')
        self.assertEqual(raw_assess(fs,completed=True,cancelled=True,require_transition=False)['status'],'INCONCLUSIVE')
        fs[-1]['at']=9
        self.assertEqual(raw_assess(fs,completed=True,require_transition=False)['status'],'INCONCLUSIVE')

    def test_complete_transition_and_pending_addressless_members(self):
        self.assertEqual(assess(transition_window())['status'],'PASS')
    def test_schema_two_retained_allowlist(self):
        from capture_window import clean
        f=transition_frame(0);f['inventory']['privateAddresses']={'a':'private'}
        f['inventory']['memberStates'][0]['privateAddress']='private'
        got=clean(f)['inventory']
        self.assertEqual(set(got),{'schemaVersion','feedSession','sequence','observedAt','desired','members','memberStates','backendStates','healthyRegistered'})
        self.assertNotIn('private',str(got))
    def test_all_backend_statuses_and_zero_weight_require_metrics(self):
        for status in ('Adding','Available','Configuring','Removing'):
            for weight in (0,100):
                with self.subTest(status=status,weight=weight):
                    fs=transition_window();inv=fs[1]['inventory']
                    inv['backendStates'].append(dict(id='c',status=status,weight=weight,listenerHealth='Initial'))
                    inv['memberStates'][2]['addressKnown']=True
                    self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_all_unhealthy_cannot_pass(self):
        fs=[transition_frame(0),transition_frame(1)]
        for f in fs:
            f['inventory']['healthyRegistered']=[]
            for row in f['inventory']['backendStates']:row['listenerHealth']='Unhealthy'
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_first_negative_counters_not_hidden(self):
        for counter in ('Failure','Timeout','Ambiguous','Skipped'):
            fs=[transition_frame(0),transition_frame(1)]
            for f in fs:
                sample=f['pods'][0]['snapshot']['sample'];sample[counter]=1;sample['Scheduled']+=1
                if counter!='Skipped':sample['Attempts']+=1
            with self.subTest(counter=counter):self.assertEqual(assess(fs)['status'],'INCONCLUSIVE' if counter=='Skipped' else 'FAIL')
    def test_malformed_provider_cannot_hide_sql_failure(self):
        for broken in ('memberStates','backendStates','feedSession'):
            fs=[transition_frame(0),transition_frame(1)]
            del fs[1]['inventory'][broken]
            fs[1]['pods'][0]['snapshot']['sample']['Failure']=1
            self.assertEqual(assess(fs)['status'],'FAIL')
    def test_invalid_state_rows(self):
        for case in ('missing','duplicate','foreign','unknown','missing address','healthy mismatch','unknown version'):
            fs=[transition_frame(0),transition_frame(1)];inv=fs[1]['inventory']
            if case=='missing':del inv['memberStates'][0]['health']
            if case=='duplicate':inv['memberStates'].append(copy.deepcopy(inv['memberStates'][0]))
            if case=='foreign':inv['backendStates'][0]['id']='foreign'
            if case=='unknown':inv['backendStates'][0]['status']='Unknown'
            if case=='missing address':inv['memberStates'][0]['addressKnown']=False
            if case=='healthy mismatch':inv['healthyRegistered']=['a']
            if case=='unknown version':inv['schemaVersion']=3
            with self.subTest(case=case):self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_feed_continuity(self):
        for case in ('restart','rollback','skip','same sequence changed','backdated'):
            fs=[transition_frame(0),transition_frame(1)];inv=fs[1]['inventory']
            if case=='restart':inv['feedSession']='00000000-0000-4000-8000-000000000002'
            if case in ('rollback','same sequence changed'):inv['sequence']=0 if case=='rollback' else 1
            if case=='skip':inv['sequence']=3
            if case=='backdated':inv['observedAt']=0
            with self.subTest(case=case):self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
        fs=[transition_frame(0),transition_frame(1)];fs[1]['inventory']=copy.deepcopy(fs[0]['inventory'])
        self.assertEqual(assess(fs)['status'],'PASS')
    def test_old_metrics_obligation_until_zero_then_later_absence(self):
        for case in ('nonzero','other pool nonzero','incomplete other pool','stale zero','same provider','no zero'):
            fs=transition_window()
            # No earlier zero may retire a still-busy backend.
            for f in fs[:4]:
                for row in f['metrics']['raw']:
                    if row['node']=='a':row['value']=1
            if case!='no zero':
                for row in fs[3]['metrics']['raw']:
                    if row['node']=='a':row['value']=0
            if case=='nonzero':fs[3]['metrics']['raw'][0]['value']=1
            if case in ('other pool nonzero','incomplete other pool'):
                kinds=('active',) if case=='incomplete other pool' else ('active','idle','waiting')
                fs[3]['metrics']['raw'] += [dict(node='a',pool='other',user='other',kind=k,value=1,sourceAt=3) for k in kinds]
            if case=='stale zero':
                for row in fs[3]['metrics']['raw']:
                    if row['node']=='a':row['sourceAt']=-100
            if case=='same provider':fs[4]['inventory']=copy.deepcopy(fs[3]['inventory'])
            with self.subTest(case=case):self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_nonzero_restores_retired_obligation(self):
        fs=transition_window();f=fs[4]
        f['metrics']['nodes'].append('a');f['metrics']['raw'] += [dict(node='a',pool='test',user='poc_app',kind=k,value=1,sourceAt=4) for k in ('active','idle','waiting')]
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_metrics_scope_freshness_and_poll_gaps(self):
        for case in ('duplicate','foreign','stale provider','stale scrape','stale raw','poll gap'):
            fs=transition_window();f=fs[2]
            if case=='duplicate':f['metrics']['nodes'].append('a')
            if case=='foreign':f['metrics']['nodes'].append('foreign')
            if case=='stale provider':f['inventory']['observedAt']=-100
            if case=='stale scrape':f['metrics']['sourceAt']=-100
            if case=='stale raw':f['metrics']['raw'][0]['sourceAt']=-100
            if case=='poll gap':f['at']=20
            with self.subTest(case=case):self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_historical_schema_one_baseline_unchanged(self):
        fs=[frame(0,0),frame(1,1)]
        for f in fs:
            s=f['pods'][0]['snapshot']['sample'];s.update(Failure=1,Attempts=s['Attempts']+1,Scheduled=s['Scheduled']+1)
        self.assertEqual(assess(fs),{'status':'PASS','success':1,'reasons':[]})


class TransitionCoverageEdges(unittest.TestCase):
    def test_full_replacement_retires_zero_verified_nlb_absent_lingering_member(self):
        fs=transition_window()
        for t in (3,4):
            fs[t]['inventory']['members'].insert(0,'a')
            fs[t]['inventory']['memberStates'].insert(0,dict(id='a',lifecycle='Removing',health='Healthy',addressKnown=True))
        result=assess(fs)
        self.assertEqual(result['status'],'PASS',result)

    def test_reappearing_backend_restores_metrics_for_every_status(self):
        for status in ('Adding','Available','Configuring','Removing'):
            for listener_health in ('Normal','Unhealthy'):
                fs=transition_window()
                # Node a was zero-verified then retired at frame 3.
                fs[4]['inventory']['backendStates'].insert(0,dict(id='a',status=status,weight=0,listenerHealth=listener_health))
                with self.subTest(status=status,listener_health=listener_health):
                    result=assess(fs)
                    self.assertEqual(result['status'],'INCONCLUSIVE',result)
                    self.assertIn('missing metrics coverage',result['reasons'])

    def test_malformed_early_metrics_do_not_hide_later_failure(self):
        for field in ('metrics','raw key','inventory'):
            fs=[transition_frame(0),transition_frame(1)]
            if field=='metrics':del fs[0]['metrics']['nodes']
            if field=='raw key':del fs[0]['metrics']['raw'][0]['kind']
            if field=='inventory':fs[0]['inventory']={'schemaVersion':2}
            fs[1]['pods'][0]['snapshot']['sample']['Failure']=1
            with self.subTest(field=field):self.assertEqual(assess(fs)['status'],'FAIL')
    def test_metrics_required_for_previously_ready_pending_member(self):
        fs=[transition_frame(0),transition_frame(1,('a','b'),('b',),('a',),metrics=('b',))]
        fs[1]['inventory']['memberStates'][0]['addressKnown']=True
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_missing_address_cannot_reset_after_known_address(self):
        fs=transition_window();fs[2]['inventory']['memberStates'][2].update(lifecycle='Pending',addressKnown=False)
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_backend_can_lag_ess_after_sequential_decrease(self):
        fs=transition_window();fs[3]['inventory']['backendStates'].insert(0,dict(id='a',status='Removing',weight=0,listenerHealth='Unavailable'))
        self.assertEqual(assess(fs)['status'],'PASS')
    def test_member_absence_without_capacity_decrease_is_inconclusive(self):
        fs=transition_window();fs[3]['inventory']['desired']=4
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_all_transitional_backend_states_with_complete_metrics(self):
        for status in ('Adding','Configuring','Removing'):
            fs=transition_window();inv=fs[2]['inventory'];inv['backendStates'][2]['status']=status;inv['healthyRegistered'].remove('c')
            with self.subTest(status=status):self.assertEqual(assess(fs)['status'],'PASS')
    def test_absent_backend_missing_metrics_before_zero_is_not_retired(self):
        fs=transition_window()
        for f in fs[:4]:
            for row in f['metrics']['raw']:
                if row['node']=='a':row['value']=1
        # All other evidence is identical to the known passing transition.
        fs[4]['metrics']['nodes'].append('a')
        fs[4]['metrics']['raw'] += [dict(node='a',pool='test',user='poc_app',kind=k,value=0,sourceAt=4) for k in ('active','idle','waiting')]
        self.assertEqual(assess(fs)['status'],'PASS')
        fs[4]['metrics']['nodes'].remove('a');fs[4]['metrics']['raw']=[r for r in fs[4]['metrics']['raw'] if r['node']!='a']
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')


class TransitionDepartureOrder(unittest.TestCase):
    def test_schema_two_poll_gap_cannot_be_relaxed(self):
        fs=[transition_frame(0),transition_frame(4)];fs[1]['inventory']['sequence']=2
        self.assertEqual(assess(fs,max_gap=10)['status'],'INCONCLUSIVE')

    def test_three_to_two_without_prior_four_to_three_is_not_authorized(self):
        fs=[transition_frame(0,('a','b','c')),transition_frame(1,('b','c'),metrics=('a','b','c'))]
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')


def review_failure_frames(outcome='failure', different_process=False, event_only=False):
    fs=[transition_frame(0),transition_frame(1)]
    if not event_only:fs[0]['pods'][0]['snapshot']['sample']['Scheduled']=-1
    snap=fs[1]['pods'][0]['snapshot']
    if different_process:
        fs[1]['pods'][0]['uid']='later-pod';snap['sample']['processId']='later-process'
        for event in snap['events']:event['processId']='later-process'
    if event_only:
        for counter in ('Scheduled','Attempts','Success'):snap['sample'][counter]=0
    else:
        snap['sample'].update(Failure=1,Attempts=2,Scheduled=2)
    snap['events'][-1]['outcome']=outcome if event_only else 'success'
    snap.update(final=True,connected=0);fs[1]['phase']='settling'
    return fs


class ReviewedFailurePrecedence(unittest.TestCase):
    def test_invalid_counter_does_not_hide_later_first_failure(self):
        for different in (False,True):
            with self.subTest(different_process=different):
                result=assess(review_failure_frames(different_process=different))
                self.assertEqual(result['status'],'FAIL')
                self.assertIn('malformed retained evidence',result['reasons'])
    def test_negative_events_override_unchanged_counters(self):
        for outcome in ('failure','timeout','ambiguous'):
            with self.subTest(outcome=outcome):
                result=assess(review_failure_frames(outcome=outcome,event_only=True))
                self.assertEqual(result['status'],'FAIL')
                self.assertIn('event/counter mismatch',result['reasons'])
    def test_invalid_or_pre_window_event_cannot_establish_failure(self):
        for case in ('old','future','foreign process','bad sequence','absent operation'):
            fs=review_failure_frames(event_only=True);event=fs[1]['pods'][0]['snapshot']['events'][-1]
            if case=='old':event['at']=-1
            if case=='future':event['at']=100
            if case=='foreign process':event['processId']='foreign'
            if case=='bad sequence':event['sequence']=-1
            if case=='absent operation':event['operationId']=''
            with self.subTest(case=case):self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_historical_event_mismatch_verdict_is_unchanged(self):
        fs=review_failure_frames(event_only=True)
        for f in fs:f['inventory']={k:f['inventory'][k] for k in ('observedAt','desired','members','healthyRegistered')}
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')


def partial_pod_failure_frame(event_only=False, negative=True):
    f=transition_frame(0);f['expectedReplicas']=2
    malformed=f['pods'][0]
    del malformed['snapshot']['sample']['Scheduled']
    malformed['snapshot']['privatePayload']={'address':'10.20.30.40','password':'do-not-retain'}
    malformed['privateAddress']='10.20.30.40'
    other=frame(0,0)['pods'][0];other['uid']='pod-b';snap=other['snapshot'];snap['sample']['processId']='process-b'
    if negative and event_only:
        snap['sample']['sequence']=2
        snap['events']=[dict(processId='process-b',sequence=1,at=0,kind='scheduled',operationId='b'),
                        dict(processId='process-b',sequence=2,at=0,kind='operation',operationId='b',outcome='failure')]
    elif negative:snap['sample'].update(Failure=1,Attempts=1,Scheduled=1)
    snap['privatePayload']={'password':'do-not-retain'}
    f['pods'].append(other);f['privateAddress']='10.20.30.40'
    return f


class PartialPodEvidence(unittest.TestCase):
    def test_direct_assessment_preserves_other_pod_failure(self):
        for event_only in (False,True):
            with self.subTest(event_only=event_only):
                result=assess([partial_pod_failure_frame(event_only),transition_frame(1)])
                self.assertEqual(result['status'],'FAIL')
                self.assertIn('malformed retained evidence',result['reasons'])
    def test_live_append_preserves_safe_failure_and_no_raw_payload(self):
        for event_only in (False,True):
            with self.subTest(event_only=event_only),tempfile.TemporaryDirectory() as d:
                w=CaptureWindow(Path(d)/'capture.jsonl')
                try:
                    w.append(partial_pod_failure_frame(event_only));w.append(transition_frame(1))
                    result=assess(w.read())
                except (KeyError,TypeError,ValueError):result={'status':'INCONCLUSIVE'}
                self.assertEqual(result['status'],'FAIL')
                stored=w.path.read_text();self.assertNotIn('10.20.30.40',stored);self.assertNotIn('do-not-retain',stored)
    def test_partial_pod_never_becomes_complete_after_repeated_cleaning(self):
        from capture_window import clean
        f=partial_pod_failure_frame(negative=False)
        try:
            cleaned=clean(clean(f));got=cleaned['expectedReplicas']
        except (KeyError,TypeError,ValueError):got=None
        self.assertEqual(got,0)
        self.assertEqual(assess([cleaned,transition_frame(1)])['status'],'INCONCLUSIVE')
    def test_schema_one_partial_structure_still_rejected(self):
        from capture_window import clean
        f=partial_pod_failure_frame();f['inventory']={k:f['inventory'][k] for k in ('observedAt','desired','members','healthyRegistered')}
        with self.assertRaises(KeyError):clean(f)


class ProviderMinimumBounds(unittest.TestCase):
    def test_desired_one_with_two_valid_members_is_inconclusive(self):
        fs=[transition_frame(0),transition_frame(1)]
        for f in fs:f['inventory']['desired']=1
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')
    def test_single_member_with_valid_desired_is_inconclusive(self):
        fs=[transition_frame(0,('a',),desired=2),transition_frame(1,('a',),desired=2)]
        self.assertEqual(assess(fs)['status'],'INCONCLUSIVE')




class LaneIdentityRetention(unittest.TestCase):
    def identity_frame(self):
        f=transition_frame(1)
        f['deployment']={'uid':'deployment-uid','generation':7,'observedGeneration':7,'desired':2,'ready':2}
        f['pods'][0].update(name='client-0',containerId='containerd://abc',ready=True)
        return f

    def test_retains_only_valid_bounded_identity_and_round_trips(self):
        f=self.identity_frame()
        f['deployment']['metadata']={'secret':'opaque'}
        f['pods'][0].update(podIP='10.0.0.1',status={'secret':'opaque'})
        got=clean(f)
        self.assertEqual(got.get('deployment'),{'uid':'deployment-uid','generation':7,'observedGeneration':7,'desired':2,'ready':2})
        self.assertEqual({k:got['pods'][0].get(k) for k in ('name','containerId','ready')},
                         {'name':'client-0','containerId':'containerd://abc','ready':True})
        self.assertNotIn('10.0.0.1',str(got));self.assertNotIn('opaque',str(got))
        self.assertEqual(clean(got),got)
        with tempfile.TemporaryDirectory() as directory:
            w=CaptureWindow(Path(directory)/'frames.jsonl');w.append(f)
            self.assertEqual(next(w.read()),got)

    def test_malformed_or_partial_identity_is_incomplete_without_losing_sql(self):
        cases=[('deployment',key,value) for key,values in {
            'uid':['','x'*257,{},'bad token'],
            'generation':[True,-1,2**53+1,1.0],
            'observedGeneration':[False,-1,2**53+1],
            'desired':[True,-1,17,2.0], 'ready':[True,-1,17]}.items() for value in values]
        cases += [('pod',key,value) for key,values in {
            'name':['','x'*257,{},'bad token'], 'containerId':['','x'*257,[], 'bad token'],
            'ready':[1,'True',None]}.items() for value in values]
        for owner,key,value in cases:
            with self.subTest(owner=owner,key=key,value=value):
                f=self.identity_frame();target=f['deployment'] if owner=='deployment' else f['pods'][0]
                target[key]=value
                got=clean(f)
                self.assertEqual(got['expectedReplicas'],0)
                self.assertEqual(got['pods'][0]['snapshot'],f['pods'][0]['snapshot'])
                self.assertIsNone((got.get('deployment') or {}).get(key) if owner=='deployment' else got['pods'][0].get(key))
        for owner,keys in [('deployment',('uid','generation','observedGeneration','desired','ready')),
                           ('pod',('name','containerId','ready'))]:
            for key in keys:
                with self.subTest(missing=key,owner=owner):
                    f=self.identity_frame();del (f['deployment'] if owner=='deployment' else f['pods'][0])[key]
                    self.assertEqual(clean(f)['expectedReplicas'],0)
        for invalid in (None,[],{'uid':'only'}):
            f=self.identity_frame();f['deployment']=invalid
            self.assertEqual(clean(f)['expectedReplicas'],0)

    def test_valid_adverse_values_and_bounds_remain_visible(self):
        f=self.identity_frame();f['deployment'].update(uid='u'*256,generation=0,observedGeneration=2**53,desired=0,ready=16)
        f['pods'][0].update(name='n'*256,containerId='c'*256,ready=False)
        got=clean(f)
        self.assertEqual(got.get('deployment'),f['deployment'])
        self.assertIs(got['pods'][0].get('ready'),False)
        self.assertEqual(got['expectedReplicas'],1)

    def test_historical_v2_absence_and_v1_allowlist_remain_compatible(self):
        f=transition_frame(1);got=clean(f)
        self.assertEqual(got['expectedReplicas'],1)
        self.assertNotIn('deployment',got)
        self.assertNotIn('ready',got['pods'][0])
        f=self.identity_frame();f['inventory']=frame(1,1)['inventory']
        got=clean(f)
        self.assertNotIn('deployment',got)
        self.assertNotIn('name',got['pods'][0]);self.assertNotIn('containerId',got['pods'][0]);self.assertNotIn('ready',got['pods'][0])


    def test_invalid_new_identity_cannot_hide_observed_sql_failure(self):
        frames=[self.identity_frame(),self.identity_frame()]
        frames[1]['at']=2;frames[1]['pods'][0]['ready']='invalid'
        frames[1]['pods'][0]['snapshot']['sample']['Failure']=1
        self.assertEqual(assess(frames)['status'],'FAIL')

    def test_historical_v2_ordinary_assessment_still_passes(self):
        self.assertEqual(assess(transition_window())['status'],'PASS')


    def test_partial_new_contract_cannot_masquerade_as_historical_absence(self):
        for missing in ('deployment','pod fields'):
            with self.subTest(missing=missing):
                f=self.identity_frame()
                if missing=='deployment':del f['deployment']
                else:
                    for key in ('name','containerId','ready'):del f['pods'][0][key]
                self.assertEqual(clean(f)['expectedReplicas'],0)

if __name__ == '__main__':
    unittest.main()

class NativeOwnershipCleaning(unittest.TestCase):
    def test_direct_native_ownership_allowlist_preserves_only_typed_fields(self):
        from test_pgcat_native_evidence import rolling,ownership
        value=rolling(1);value['pods'][0]['ownership']['credential']='never-retain'
        result=clean(value)
        self.assertEqual(result['pods'][0].get('ownership'),ownership())
        value['inventory']['mode']='full-replacement'
        self.assertNotIn('ownership',clean(value)['pods'][0])
        value=rolling(1);value['pods'][0]['ownership']['deploymentUid']={'secret':'never-retain'}
        self.assertIsNone(clean(value)['pods'][0]['ownership']['deploymentUid'])
