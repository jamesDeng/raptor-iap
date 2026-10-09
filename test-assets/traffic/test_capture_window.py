import copy, tempfile, unittest
from pathlib import Path
from capture_window import CaptureWindow, assess as raw_assess, retain
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
    def test_output_is_allowlisted(self):
        with tempfile.TemporaryDirectory() as d:
            w=CaptureWindow(Path(d)/'e.jsonl');f=frame(0,0);f['password']='never retain';f['metrics']['raw'][0]['password']='never retain'
            w.append(f);self.assertNotIn('password',w.path.read_text())

if __name__=='__main__':unittest.main()
