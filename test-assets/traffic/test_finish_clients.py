import json, tempfile, threading, time, unittest
from pathlib import Path
from unittest.mock import patch
import finish_clients as finish

ROSTER=[dict(name='client-'+str(i),uid='uid-'+str(i),containerId='container-'+str(i),processId='process-'+str(i)) for i in range(2)]

class FinishTests(unittest.TestCase):
    def test_exact_roster_ack_then_parallel_dispatch_and_private_files(self):
        import concurrent.futures
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary';gate=threading.Barrier(2);seen=[]
            def execute(row):
                self.assertTrue((p/'ack.json').exists());gate.wait(timeout=2);seen.append(row);return 'accepted'
            def ack():
                for _ in range(100):
                    if (p/'request.json').exists():
                        request=json.loads((p/'request.json').read_text());finish.atomic_json(p/'ack.json',request);return
                    time.sleep(.01)
            with patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'exec_finish',side_effect=execute):
                t=threading.Thread(target=ack);t.start();receipt=finish.finish(p);t.join()
            self.assertEqual(len(seen),2);self.assertEqual(set(receipt['clients']),{'uid-0/process-0','uid-1/process-1'})
            self.assertEqual(p.stat().st_mode&0o777,0o700)
            for f in p.iterdir():self.assertEqual(f.stat().st_mode&0o777,0o600)
    def test_missing_changed_identity_refuses_without_exec(self):
        for rows in (ROSTER[:1],[dict(ROSTER[0],processId=''),ROSTER[1]]):
            with tempfile.TemporaryDirectory() as d,patch.object(finish,'live_roster',return_value=rows),patch.object(finish,'exec_finish') as execute:
                with self.assertRaises(ValueError):finish.finish(Path(d)/'boundary')
                execute.assert_not_called()
    def test_uncertain_exec_only_read_reconciliation(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'boundary'
            def ack(path,request):finish.atomic_json(path/'ack.json',request)
            with patch.object(finish,'live_roster',return_value=ROSTER),patch.object(finish,'wait_ack',side_effect=ack),patch.object(finish,'exec_finish',side_effect=TimeoutError('secret raw output')) as execute:
                receipt=finish.finish(p)
            self.assertEqual(execute.call_count,2);self.assertEqual(receipt['status'],'INCONCLUSIVE');self.assertNotIn('secret',json.dumps(receipt));self.assertTrue(all(r['status']=='unknown' for r in receipt['clients'].values()))
    def test_changed_roster_after_ack_refuses(self):
        with tempfile.TemporaryDirectory() as d,patch.object(finish,'wait_ack'),patch.object(finish,'live_roster',side_effect=[ROSTER,[dict(ROSTER[0],uid='changed'),ROSTER[1]]]),patch.object(finish,'exec_finish') as execute:
            with self.assertRaises(ValueError):finish.finish(Path(d)/'boundary')
            execute.assert_not_called()
