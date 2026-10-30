"""Fixed post-stop catalog behavior; no CLI execution."""
import json
import shlex
import unittest
from pgcat_lane_actions import ActionCatalog
from pgcat_lane_protocol import Refused,encode
import test_pgcat_lane_protocol as protocol_tests

class PostStopActionTests(unittest.TestCase):
    def setUp(self):
        fixture=protocol_tests.PostStopJournalTests();fixture.setUp();self.addCleanup(fixture.doCleanups);self.f=fixture

    def test_fixed_command_has_seven_worker_arguments_and_private_role(self):
        f=self.f;b=f.f.outer['laneBindings'];context=f.journal.post_stop_context(f.request)
        command=ActionCatalog.post_stop_worker_command(b,f.f.run,context)
        parts=shlex.split(command)
        self.assertEqual(parts[7:11],['post-stop-finish',b['contractDir'],f.f.run,b['workerKubeconfig']])
        self.assertEqual(len(parts),14);self.assertEqual(len(json.loads(parts[11])),11)
        self.assertEqual(json.loads(parts[13]),context)
        argv,env,budget=ActionCatalog.command('rehearsal-finish',b,f.f.run,run_context=context,
            lane_root=str(f.root),request_id=f.request['requestId'])
        self.assertEqual(env['PGCAT_LANE_ROLE'],'post-stop');self.assertEqual(budget,5.)
        self.assertEqual(env['PGCAT_LANE_ROOT'],str(f.root))
        with self.assertRaises(Refused):ActionCatalog.command('baseline-status',b,f.f.run,lane_root=str(f.root))

    def test_parser_only_accepts_exact_two_lines_bound_summary(self):
        f=self.f;value=f.result()['payload'];header=dict(status='PREPARED',stage='post-stop-finish',category='rehearsal-only')
        raw=b'\n'.join(map(encode,[header,value]))
        source=value['sourceSha256']
        self.assertEqual(ActionCatalog.parse('rehearsal-finish',raw,f.request,source_sha=source),value)
        for changes in ({'sequence':11},{'feedSession':f.request['requestId']},{'sourceSha256':'0'*64},
                        {'boundaryRequestId':'x'},{'extra':True}):
            with self.subTest(changes=changes),self.assertRaises(Refused):
                ActionCatalog.parse('rehearsal-finish',b'\n'.join(map(encode,[header,dict(value,**changes)])),f.request,source_sha=source)
