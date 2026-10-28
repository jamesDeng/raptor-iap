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


class ReviewedSegmentedFailures(unittest.TestCase):
    def test_later_first_failure_survives_invalid_early_counter(self):
        from test_capture_window import review_failure_frames
        for different in (False,True):
            with self.subTest(different_process=different),tempfile.TemporaryDirectory() as d:
                w=SegmentedCapture(Path(d)/'capture',max_segment_seconds=1)
                for f in review_failure_frames(different_process=different):w.append(f)
                result=w.finish(completed=True,cancelled=False,errors=[])
                self.assertEqual(result['status'],'FAIL')
                self.assertIn('malformed retained evidence',result['reasons'])
    def test_negative_events_override_unchanged_counters(self):
        from test_capture_window import review_failure_frames
        for outcome in ('failure','timeout','ambiguous'):
            with self.subTest(outcome=outcome),tempfile.TemporaryDirectory() as d:
                w=SegmentedCapture(Path(d)/'capture',max_segment_seconds=1)
                for f in review_failure_frames(outcome=outcome,event_only=True):w.append(f)
                result=w.finish(completed=True,cancelled=False,errors=[])
                self.assertEqual(result['status'],'FAIL')
                self.assertIn('event/counter mismatch',result['reasons'])

    def test_in_window_failure_in_first_snapshot_is_retained(self):
        from test_capture_window import transition_frame,assess
        fs=[transition_frame(0),transition_frame(1)]
        initial=fs[0]['pods'][0]['snapshot'];initial['sample']['sequence']=2
        initial['events']=[dict(processId='process',sequence=1,at=0,kind='scheduled',operationId='first'),
                           dict(processId='process',sequence=2,at=0,kind='operation',operationId='first',outcome='failure')]
        fs[1]['phase']='settling';fs[1]['pods'][0]['snapshot'].update(final=True,connected=0)
        self.assertEqual(assess(fs)['status'],'FAIL')
        with tempfile.TemporaryDirectory() as d:
            w=SegmentedCapture(Path(d)/'capture',max_segment_seconds=1)
            for f in fs:w.append(f)
            self.assertEqual(w.finish(completed=True,cancelled=False,errors=[])['status'],'FAIL')


class PartialPodManifestEvidence(unittest.TestCase):
    def test_authenticated_partial_frame_preserves_other_pod_failure(self):
        from test_capture_window import partial_pod_failure_frame,transition_frame
        from segmented_capture import digest
        for event_only in (False,True):
            with self.subTest(event_only=event_only),tempfile.TemporaryDirectory() as d:
                directory=Path(d)/'capture';w=SegmentedCapture(directory,max_segment_seconds=1)
                first=partial_pod_failure_frame(event_only)
                first['pods'][0]['snapshot']['sample']['Scheduled']=0
                w.append(first)
                final=transition_frame(1);final['phase']='settling';final['pods'][0]['snapshot'].update(final=True,connected=0)
                w.append(final);w.finish(completed=True,cancelled=False,errors=[])
                path=directory/'segment-0001.jsonl';row=json.loads(path.read_text())
                del row['pods'][0]['snapshot']['sample']['Scheduled']
                path.write_text(json.dumps(row)+'\n')
                manifest=directory/'completion.json';metadata=json.loads(manifest.read_text())
                metadata['segments'][0]['sha256']=digest(path);manifest.write_text(json.dumps(metadata))
                result=assess_manifest(directory)
                self.assertEqual(result['status'],'FAIL')
                self.assertIn('malformed retained evidence',result['reasons'])

    def test_partial_shapes_cannot_break_metadata_after_append(self):
        from test_capture_window import partial_pod_failure_frame,transition_frame
        for case in ('missing uid','missing process','missing sequence','opaque counter','opaque event','overflow duration'):
            with self.subTest(case=case),tempfile.TemporaryDirectory() as d:
                directory=Path(d)/'capture';w=SegmentedCapture(directory,max_segment_seconds=1)
                first=partial_pod_failure_frame();pod=first['pods'][0];snap=pod['snapshot']
                if case=='missing uid':del pod['uid']
                if case=='missing process':del snap['sample']['processId']
                if case=='missing sequence':del snap['sample']['sequence']
                if case=='opaque counter':snap['sample']['Scheduled']={'password':'do-not-retain'}
                if case=='opaque event':snap['events']=[{'privateAddress':'10.20.30.40'}]
                if case=='overflow duration':
                    snap['events']=[dict(processId='process',at=0,sequence=1,kind='connected',durationSeconds=10**400)]
                try:
                    w.append(first)
                    final=transition_frame(1);final['phase']='settling';final['pods'][0]['snapshot'].update(final=True,connected=0)
                    w.append(final);result=w.finish(completed=True,cancelled=False,errors=[])
                except (KeyError,TypeError,ValueError,OverflowError):result={'status':'INCONCLUSIVE'}
                self.assertEqual(result['status'],'FAIL')
                metadata=json.loads((directory/'completion.json').read_text())
                self.assertEqual([row['frames'] for row in metadata['segments']],[1,1])
                raw=''.join(p.read_text() for p in directory.glob('segment-*.jsonl'))
                self.assertNotIn('do-not-retain',raw);self.assertNotIn('10.20.30.40',raw)

# Witness fixtures use real files; a writer that hashes source input instead of
# the filtered durable line, or trusts a replaced inode, must fail these tests.
def witness_context():
    from test_finish_clients import ROSTER
    return dict(schemaVersion=1,runId='11111111-1111-4111-8111-111111111111',
        feedSession='22222222-2222-4222-8222-222222222222',
        providerRunContextSha256='a'*64,bundleSha256='b'*64,
        sourceHashes={key:'c'*64 for key in __import__('pgcat_collector_control').SOURCES},
        baselineSha256='d'*64,originalRoster=copy.deepcopy(ROSTER),
        deployment=dict(name='rdev-test-client-client',namespace='raptor-test',uid='deployment',generation=1),
        collectorIdentity=dict(pid=123,bootId='boot',startTicks=456,commandSha256='e'*64))


def witness_frame(t,n=None,finals=()):
    from test_capture_window import transition_frame
    context=witness_context();f=transition_frame(t);f['inventory']['feedSession']=context['feedSession'];f['pods']=[];f['expectedReplicas']=2
    for i,row in enumerate(context['originalRoster']):
        p=copy.deepcopy(frame(t,t if n is None else n)['pods'][0]);p.update({k:v for k,v in row.items() if k!='processId'});p['ready']=i not in finals
        snap=p['snapshot'];snap['sample']['processId']=row['processId']
        for event in snap['events']:event['processId']=row['processId']
        snap.update(final=i in finals,connected=0 if i in finals else 4)
        f['pods'].append(p)
    f['deployment']=dict(uid='deployment',generation=1,observedGeneration=1,desired=2,ready=2-len(finals))
    if finals:f['phase']='settling'
    return f


class BoundedWitnessTests(unittest.TestCase):
    def writer(self,directory,**kwargs):
        return SegmentedCapture(directory,progress_context=witness_context(),**kwargs)
    def read(self,directory,context=None):
        from segmented_capture import read_progress
        return read_progress(directory,context or witness_context())
    def test_actual_filtered_bytes_and_rollover_pair(self):
        import hashlib
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p,max_segment_seconds=2)
            for t in range(3):w.append(witness_frame(t))
            progress,previous,latest=self.read(p)
            self.assertEqual((progress['attempts'],progress['committedFrames'],progress['generation']),(3,3,4))
            self.assertEqual((previous['at'],latest['at']),(1,2))
            self.assertEqual(progress['previous']['basename'],'segment-0001.jsonl')
            self.assertEqual(progress['latest']['basename'],'segment-0002.jsonl')
            ref=progress['previous'];raw=(p/ref['basename']).read_bytes()[ref['offset']:ref['offset']+ref['length']]
            self.assertTrue(raw.endswith(b'\n'));self.assertEqual(hashlib.sha256(raw).hexdigest(),ref['sha256'])
            self.assertEqual(len(latest['pods'][0]['snapshot']['events']),2)
            self.assertNotEqual(json.dumps(witness_frame(2),separators=(',',':')).encode()+b'\n',(p/'segment-0002.jsonl').read_bytes())
            self.assertEqual((p/'progress-binding.json').stat().st_mode&0o777,0o600)
            self.assertEqual(set(progress),{'schemaVersion','bindingSha256','generation','attempts','committedFrames','firstAt','lastAttemptAt','lastCommitMonotonic','maxGapSeconds','flags','previous','latest'})
    def test_initial_error_and_sticky_adverse_evidence(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.note_poll_error(0)
            zero,previous,latest=self.read(p);self.assertEqual(zero['committedFrames'],0);self.assertIsNone(latest)
            self.assertTrue(zero['flags']['pollError'])
            f=witness_frame(1);f['pods'][0]['snapshot']['sample']['Failure']=1;f['pods'][0]['snapshot']['sample']['Skipped']=1
            w.append(f);f=witness_frame(5);f['pods'][0]['containerId']='changed';w.append(f);w.append(witness_frame(6))
            value,_,_=self.read(p)
            self.assertEqual(value['maxGapSeconds'],4)
            for flag in ('pollError','sqlFailure','skipped','identityChanged'):self.assertTrue(value['flags'][flag],flag)
            self.assertEqual(value['attempts'],4)
    def test_context_unknown_fields_and_binding_mismatch_refuse(self):
        for mutation in ('extra','bool','hash','roster','deployment','identity'):
            with self.subTest(mutation=mutation),tempfile.TemporaryDirectory() as d:
                c=witness_context()
                if mutation=='extra':c['extra']=1
                if mutation=='bool':c['schemaVersion']=True
                if mutation=='hash':c['sourceHashes']['workerCapture']='bad'
                if mutation=='roster':c['originalRoster'][0]['processId']=''
                if mutation=='deployment':c['deployment']['generation']=True
                if mutation=='identity':c['collectorIdentity']['pid']=True
                with self.assertRaises(ValueError):SegmentedCapture(Path(__import__('os').path.realpath(d))/'capture',progress_context=c)
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(0))
            for key,value in [('runId','33333333-3333-4333-8333-333333333333'),('baselineSha256','f'*64),('bundleSha256','f'*64)]:
                c=witness_context();c[key]=value
                with self.subTest(key=key),self.assertRaises(ValueError):self.read(p,c)
    def test_replaced_symlink_truncated_partial_and_duplicate_artifacts_refuse(self):
        for kind in ('inode','symlink','truncated','newline','hash','duplicate','directory','progress-symlink','binding'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(0));w.append(witness_frame(1));v,_,_=self.read(p)
                segment=p/v['latest']['basename']
                if kind in ('inode','symlink'):
                    saved=segment.read_bytes();segment.rename(p/'old');segment.write_bytes(saved);segment.chmod(0o600)
                    if kind=='symlink':segment.unlink();segment.symlink_to(p/'old')
                if kind=='truncated':segment.write_bytes(segment.read_bytes()[:-10])
                if kind=='newline':segment.write_bytes(segment.read_bytes()[:-1]+b' ')
                if kind=='hash':segment.write_bytes(segment.read_bytes().replace(b'"at":1',b'"at":2'))
                if kind=='duplicate':(p/'progress.json').write_text('{"schemaVersion":1,"schemaVersion":1}')
                if kind=='directory':p.rename(Path(__import__('os').path.realpath(d))/'old');p.mkdir(mode=0o700)
                if kind=='progress-symlink':(p/'progress.json').rename(p/'old-progress');(p/'progress.json').symlink_to(p/'old-progress')
                if kind=='binding':(p/'progress-binding.json').write_text('{}')
                with self.assertRaises((ValueError,OSError)):self.read(p)
    def test_missing_identity_invalid_and_settling_readiness_is_allowed(self):
        for kind in ('container','deployment','ready','counter','events'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);f=witness_frame(0)
                if kind=='container':del f['pods'][0]['containerId']
                if kind=='deployment':f.pop('deployment')
                if kind=='ready':f['pods'][0]['ready']=None
                if kind=='counter':f['pods'][0]['snapshot']['sample']['Skipped']=True
                if kind=='events':f['pods'][0]['snapshot']['events']=[{}]
                w.append(f);w.append(witness_frame(1));self.assertTrue(self.read(p)[0]['flags']['invalidEvidence'])
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(0));w.append(witness_frame(1,finals=(0,1)))
            self.assertFalse(any(self.read(p)[0]['flags'].values()))
    def test_default_has_no_witness_artifacts(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=SegmentedCapture(p);w.append(frame(0,0))
            self.assertEqual([f.name for f in p.iterdir()],['segment-0001.jsonl'])

class WitnessRaceAndFailureTests(unittest.TestCase):
    writer=BoundedWitnessTests.writer
    read=BoundedWitnessTests.read
    def test_replaced_segment_cannot_be_adopted_by_later_clean_polls(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(0))
            path=p/'segment-0001.jsonl';path.rename(p/'old');path.write_bytes((p/'old').read_bytes());path.chmod(0o600)
            with self.assertRaises((ValueError,OSError)):w.append(witness_frame(1))
            value=json.loads((p/'progress.json').read_text());self.assertTrue(value['flags']['invalidEvidence']);self.assertEqual(value['committedFrames'],1)
    def test_partial_append_preserves_old_tail_and_sticky_error(self):
        from capture_window import CaptureWindow
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(0));old=json.loads((p/'progress.json').read_text())
            def partial(window,raw):
                with window.path.open('ab') as stream:stream.write(b'{"at":')
                raise OSError('partial write')
            with patch.object(CaptureWindow,'append',partial),self.assertRaises(OSError):w.append(witness_frame(1))
            value=json.loads((p/'progress.json').read_text())
            self.assertEqual(value['latest'],old['latest']);self.assertEqual(value['committedFrames'],1);self.assertTrue(value['flags']['invalidEvidence'])
            with self.assertRaises((ValueError,OSError)):self.read(p)
    def test_concurrent_progress_or_segment_replacement_during_pread_refuses(self):
        import os
        for name in ('progress.json','segment-0001.jsonl'):
            with self.subTest(name=name),tempfile.TemporaryDirectory() as d:
                p=Path(os.path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(0));w.append(witness_frame(1));original=os.pread;triggered=[False]
                target=p/name;inode=target.stat().st_ino
                def read(fd,length,offset):
                    raw=original(fd,length,offset)
                    if os.fstat(fd).st_ino==inode and not triggered[0]:
                        triggered[0]=True;target.rename(p/'replaced');target.write_bytes((p/'replaced').read_bytes());target.chmod(0o600)
                    return raw
                with patch.object(os,'pread',side_effect=read),self.assertRaises((ValueError,OSError)):self.read(p)
                self.assertTrue(triggered[0])
    def test_no_segment_digest_or_whole_file_read_on_progress_path(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p,max_segment_seconds=1)
            with patch('segmented_capture.digest',side_effect=AssertionError('unbounded segment hashing')),patch.object(Path,'read_bytes',side_effect=AssertionError('whole file read')):
                for t in range(4):w.append(witness_frame(t))
                self.assertEqual(self.read(p)[0]['committedFrames'],4)

class WitnessValidationRegression(unittest.TestCase):
    writer=BoundedWitnessTests.writer
    read=BoundedWitnessTests.read
    def test_internally_partial_poll_and_foreign_feed_stay_invalid(self):
        for kind in ('partial','feed'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);f=witness_frame(0)
                if kind=='partial':f['expectedReplicas']=0
                else:f['inventory']['feedSession']='33333333-3333-4333-8333-333333333333'
                w.append(f);w.append(witness_frame(1));value=self.read(p)[0]
                self.assertTrue(value['flags']['invalidEvidence'])
                if kind=='partial':self.assertTrue(value['flags']['pollError'])
    def test_nonincreasing_timestamp_attempt_is_sticky_without_tail_advance(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p);w.append(witness_frame(1));before=self.read(p)[0]
            with self.assertRaises(ValueError):w.append(witness_frame(0))
            value=self.read(p)[0];self.assertEqual(value['latest'],before['latest']);self.assertEqual(value['committedFrames'],1)
            self.assertEqual(value['attempts'],2);self.assertTrue(value['flags']['invalidEvidence'])
    def test_reader_refuses_nonadjacent_pair_and_inconsistent_zero_metadata(self):
        for kind in ('adjacency','zero','first','generation'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as d:
                p=Path(__import__('os').path.realpath(d))/'capture';w=self.writer(p)
                for t in range(3):w.append(witness_frame(t))
                value=self.read(p)[0]
                if kind=='adjacency':
                    lines=(p/'segment-0001.jsonl').read_bytes().splitlines(True);raw=lines[0]
                    value['previous'].update(offset=0,length=len(raw),at=0,sha256=__import__('hashlib').sha256(raw).hexdigest())
                if kind=='zero':value.update(attempts=0,committedFrames=0,firstAt=None,lastCommitMonotonic=None,previous=None,latest=None)
                if kind=='first':value['firstAt']=10
                if kind=='generation':value['generation']=1
                (p/'progress.json').write_text(json.dumps(value))
                with self.assertRaises(ValueError):self.read(p)

class ImmutableWitnessBinding(unittest.TestCase):
    def test_binding_inode_replacement_stops_publication(self):
        import os
        with tempfile.TemporaryDirectory() as d:
            p=Path(os.path.realpath(d))/'capture';w=SegmentedCapture(p,progress_context=witness_context());w.append(witness_frame(0))
            path=p/'progress-binding.json';path.rename(p/'old-binding');path.write_bytes((p/'old-binding').read_bytes());path.chmod(0o600)
            with self.assertRaises((ValueError,OSError)):w.append(witness_frame(1))

class WitnessBindingSchemaRegression(unittest.TestCase):
    def test_binding_rejects_bool_schema_even_when_digest_matches(self):
        import os,hashlib
        from segmented_capture import read_progress
        with tempfile.TemporaryDirectory() as d:
            p=Path(os.path.realpath(d))/'capture';w=SegmentedCapture(p,progress_context=witness_context());w.append(witness_frame(0))
            path=p/'progress-binding.json';binding=json.loads(path.read_text());binding['schemaVersion']=True
            raw=json.dumps(binding).encode();path.write_bytes(raw)
            progress=json.loads((p/'progress.json').read_text());progress['bindingSha256']=hashlib.sha256(raw).hexdigest();(p/'progress.json').write_text(json.dumps(progress))
            with self.assertRaises(ValueError):read_progress(p,witness_context())
