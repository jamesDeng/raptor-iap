import copy, json, tempfile, unittest, tracemalloc
from unittest.mock import patch
from pathlib import Path
from test_capture_window import frame
from segmented_capture import SegmentedCapture, assess_manifest

class SegmentedTests(unittest.TestCase):
    def run_window(self, directory, bad=False):
        w=SegmentedCapture(directory,max_segment_seconds=2,max_total_seconds=10)
        for t in range(5):
            f=frame(t,t,t==4)
            if t==4:f['phase']='settling'
            if bad and t==1:f['metrics']['available']=False
            w.append(f)
        return w.finish(completed=True,cancelled=False,errors=[])
    def test_complete_and_sequence_continuity(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'capture';self.assertEqual(self.run_window(p)['status'],'PASS')
            m=json.loads((p/'completion.json').read_text());self.assertEqual(len(m['segments']),3)
            self.assertEqual(json.loads((p/m['segments'][1]['basename']).read_text().splitlines()[0])['pods'][0]['snapshot']['events'][0]['sequence'],3)
            self.assertEqual((p/'completion.json').stat().st_mode&0o777,0o600)
    def test_bad_first_segment_cannot_be_cured(self):
        with tempfile.TemporaryDirectory() as d:self.assertEqual(self.run_window(Path(d)/'capture',True)['status'],'INCONCLUSIVE')
    def test_missing_tampered_reordered_overlapping(self):
        for kind in ('missing','tamper','reorder','overlap'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                p=Path(d)/'capture';self.run_window(p);m=json.loads((p/'completion.json').read_text());s=m['segments']
                if kind=='missing':(p/s[0]['basename']).unlink()
                if kind=='tamper':(p/s[0]['basename']).write_text('{}\n')
                if kind=='reorder':s.reverse()
                if kind=='overlap':s[1]=copy.deepcopy(s[0])
                (p/'completion.json').write_text(json.dumps(m))
                self.assertEqual(assess_manifest(p)['status'],'INCONCLUSIVE')
    def test_cancel_error_gap_missing_final_and_failure(self):
        for kind in ('cancel','error','gap','final','failure'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                w=SegmentedCapture(Path(d)/'capture')
                for t in range(3):
                    f=frame(t if kind!='gap' else t*4,t,t==2 and kind!='final')
                    if t==2:f['phase']='settling'
                    if kind=='failure' and t==1:f['pods'][0]['snapshot']['sample']['Failure']=1
                    w.append(f)
                r=w.finish(completed=True,cancelled=kind=='cancel',errors=[{'at':1}] if kind=='error' else [])
                self.assertEqual(r['status'],'FAIL' if kind=='failure' else 'INCONCLUSIVE')
    def test_later_sql_failure_survives_missing_first_segment(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'capture';w=SegmentedCapture(p,max_segment_seconds=2)
            for t in range(4):
                f=frame(t,t,t==3)
                if t==3:
                    f['phase']='settling';snap=f['pods'][0]['snapshot'];snap['sample']['Success']-=1;snap['sample']['Failure']=1
                    snap['events'][-1]['outcome']='failure'
                w.append(f)
            w.finish(completed=True,cancelled=False,errors=[])
            first=p/'segment-0001.jsonl';saved=first.read_text()
            for kind in ('missing','tampered','invalid'):
                if first.exists():first.unlink()
                if kind=='tampered':first.write_text('{}\n')
                if kind=='invalid':
                    first.write_text(saved);m=json.loads((p/'completion.json').read_text());m['segments'][0]['firstAt']='invalid';(p/'completion.json').write_text(json.dumps(m))
                with self.subTest(kind=kind):self.assertEqual(assess_manifest(p)['status'],'FAIL')

    def test_semantically_malformed_early_frame_cannot_hide_later_failure(self):
        from segmented_capture import digest
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'capture';w=SegmentedCapture(p,max_segment_seconds=2)
            for t in range(4):
                f=frame(t,t,t==3)
                if t==3:
                    f['phase']='settling';snap=f['pods'][0]['snapshot'];snap['sample']['Success']-=1;snap['sample']['Failure']=1;snap['events'][-1]['outcome']='failure'
                w.append(f)
            w.finish(completed=True,cancelled=False,errors=[])
            first=p/'segment-0001.jsonl';rows=[json.loads(line) for line in first.read_text().splitlines()]
            rows[0]['pods'][0]['snapshot']['sample']['Success']='invalid'
            first.write_text(''.join(json.dumps(row)+'\n' for row in rows))
            m=json.loads((p/'completion.json').read_text());m['segments'][0]['sha256']=digest(first);(p/'completion.json').write_text(json.dumps(m))
            result=assess_manifest(p)
            self.assertEqual(result['status'],'FAIL')
            self.assertIn('malformed retained evidence',result['reasons'])

    def test_corrupt_lower_counter_cannot_invent_failure(self):
        from segmented_capture import digest
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'capture';w=SegmentedCapture(p,max_segment_seconds=2)
            for t in range(4):
                f=frame(t,t,t==3);sample=f['pods'][0]['snapshot']['sample']
                sample['Failure']=5;sample['Scheduled']+=5;sample['Attempts']+=5
                if t==3:f['phase']='settling'
                w.append(f)
            self.assertEqual(w.finish(completed=True,cancelled=False,errors=[])['status'],'PASS')
            first=p/'segment-0001.jsonl';rows=[json.loads(line) for line in first.read_text().splitlines()]
            rows[1]['pods'][0]['snapshot']['sample'].update(Success='invalid',Failure=0)
            first.write_text(''.join(json.dumps(row)+'\n' for row in rows))
            m=json.loads((p/'completion.json').read_text());m['segments'][0]['sha256']=digest(first);(p/'completion.json').write_text(json.dumps(m))
            result=assess_manifest(p)
            self.assertEqual(result['status'],'INCONCLUSIVE')
            self.assertIn('malformed retained evidence',result['reasons'])

    def test_large_capture_streams_without_whole_file_reads(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'capture';w=SegmentedCapture(p)
            for t in range(300):
                f=frame(t,t,t==299);f['pods'][0]['uid']='p'*100000
                f['pods'][0]['snapshot']['events']=f['pods'][0]['snapshot']['events'][-2:]
                if t==299:f['phase']='settling'
                w.append(f)
            with patch.object(Path,'read_bytes',side_effect=AssertionError('whole-file read')):
                tracemalloc.start()
                try:
                    result=w.finish(completed=True,cancelled=False,errors=[])
                    peak=tracemalloc.get_traced_memory()[1]
                finally:tracemalloc.stop()
            self.assertEqual(result['status'],'PASS')
            self.assertLess(peak,8*1024*1024)

    def test_restart_on_boundary(self):
        with tempfile.TemporaryDirectory() as d:
            w=SegmentedCapture(Path(d)/'capture',max_segment_seconds=2)
            w.append(frame(0,0));w.append(frame(1,1))
            for t in (2,3):
                f=frame(t,t-1,t==3);pod=f['pods'][0];pod['uid']='newpod';snap=pod['snapshot'];snap['sample']['processId']='newprocess'
                for e in snap['events']:e['processId']='newprocess';e['at']+=1
                if t==2:f['pods'].append(frame(2,1,True)['pods'][0]);f['expectedReplicas']=1
                if t==3:f['phase']='settling'
                w.append(f)
            self.assertEqual(w.finish(completed=True,cancelled=False,errors=[])['status'],'PASS')

    def test_31_minutes_and_approval_wait_rosters(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'capture';w=SegmentedCapture(p)
            for t in range(1861):
                f=frame(t,t,t==1860);nodes=['a','b','c','d'] if t<600 else ['a','b','c'] if t<1200 else ['a','b']
                f['inventory'].update(desired=len(nodes),members=nodes,healthyRegistered=nodes);f['metrics']['nodes']=nodes
                f['metrics']['raw']=[dict(node=n,pool='test',user='poc_app',kind=k,value=0,sourceAt=t) for n in nodes for k in ('active','idle','waiting')]
                # Keep the fixture ring bounded like the live client.
                f['pods'][0]['snapshot']['events']=f['pods'][0]['snapshot']['events'][-2:]
                if t==1860:f['phase']='settling'
                w.append(f)
            self.assertEqual(w.finish(completed=True,cancelled=False,errors=[])['status'],'PASS')
            m=json.loads((p/'completion.json').read_text());self.assertEqual(len(m['segments']),2);self.assertEqual(m['segments'][1]['firstAt'],1800)


class SamePodProcessHistory(unittest.TestCase):
    def test_prior_process_final_survives_rotation_in_same_pod(self):
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'capture';writer=SegmentedCapture(path,max_segment_seconds=2)
            writer.append(frame(0,0));writer.append(frame(1,1,True))
            for t in (2,3):
                f=frame(t,t-2,t==3);snap=f['pods'][0]['snapshot'];snap['sample']['processId']='replacement'
                for event in snap['events']:event['processId']='replacement';event['at']+=2
                if t==3:f['phase']='settling'
                writer.append(f)
            result=writer.finish(completed=True,cancelled=False,errors=[])
            # The interim measurement frame has no active client; finals must
            # still be retained even though traffic coverage is inconclusive.
            self.assertEqual(result['status'],'INCONCLUSIVE')
            markers=json.loads((path/'completion.json').read_text())['clientFinalMarkers']
            self.assertEqual(markers,[{'uid':'pod','processId':'process','final':True},{'uid':'pod','processId':'replacement','final':True}])


class TransitionSegments(unittest.TestCase):
    def test_transition_obligations_cross_segments(self):
        from test_capture_window import transition_window
        for missing in (False,True):
            with self.subTest(missing=missing),tempfile.TemporaryDirectory() as d:
                w=SegmentedCapture(Path(d)/'capture',max_segment_seconds=2)
                fs=transition_window()
                if missing:
                    for row in fs[2]['metrics']['raw']:
                        if row['node']=='a':row['value']=1
                    fs[3]['metrics']['nodes'].remove('a');fs[3]['metrics']['raw']=[r for r in fs[3]['metrics']['raw'] if r['node']!='a']
                for f in fs:w.append(f)
                self.assertEqual(w.finish(completed=True,cancelled=False,errors=[])['status'],'INCONCLUSIVE' if missing else 'PASS')
    def test_first_frame_failure_survives_retention_and_missing_provider(self):
        from test_capture_window import transition_frame
        with tempfile.TemporaryDirectory() as d:
            w=SegmentedCapture(Path(d)/'capture',max_segment_seconds=1)
            for t in range(2):
                f=transition_frame(t);s=f['pods'][0]['snapshot']['sample'];s.update(Failure=1,Attempts=t+1,Scheduled=t+1)
                if t==0:del f['inventory']['backendStates']
                else:f['phase']='settling';f['pods'][0]['snapshot'].update(final=True,connected=0)
                w.append(f)
            self.assertEqual(w.finish(completed=True,cancelled=False,errors=[])['status'],'FAIL')
