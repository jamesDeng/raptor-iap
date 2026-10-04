import contextlib,io,json,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
from test_sandbox_lifecycle_state import config_data
from test_sandbox_lifecycle_lifecycle import ExternalCloud
from tools.sandbox_lifecycle.__main__ import main
class CLITests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name).resolve();self.cfg=self.root/'cfg.json';self.cfg.write_text(json.dumps(config_data()));self.request=self.root/'request.json';self.request.write_text('{"kind":"read-probe","force_refresh":false}');self.cloud=ExternalCloud(self.root/'1234567890123456/ledger.json')
 def run_cli(self,args):
  stream=io.StringIO()
  with patch('tools.sandbox_lifecycle.__main__.state_root',return_value=self.root),patch('tools.sandbox_lifecycle.__main__.Cloud.from_operator_profile',return_value=self.cloud),contextlib.redirect_stdout(stream):code=main(['--config',str(self.cfg),*args])
  return code,json.loads(stream.getvalue())
 def test_default_status_never_creates_compute(self):
  code,result=self.run_cli([]);self.assertEqual(code,0);self.assertTrue(result['passed']);self.assertNotIn('key',self.cloud.calls);self.assertNotIn('create',self.cloud.calls)
 def test_run_without_request_rejected(self):
  code,result=self.run_cli(['run']);self.assertEqual(code,2);self.assertEqual(result['error'],'InvalidRequest')
 def test_failure_exit_and_logs_do_not_expose_secret(self):
  self.cloud.fail='key-cleanup';code,result=self.run_cli(['run','--request',str(self.request)]);self.assertEqual(code,1);self.assertFalse(result['passed']);self.assertNotIn('synthetic-secret',json.dumps(result))
 def test_unknown_request_fields_are_rejected_before_cloud(self):
  self.request.write_text('{"kind":"read-probe","force_refresh":false,"api_key":"synthetic-secret"}');code,result=self.run_cli(['run','--request',str(self.request)]);self.assertEqual(code,2);self.assertNotIn('create',self.cloud.calls);self.assertNotIn('synthetic-secret',json.dumps(result))

 def test_operator_authentication_error_is_explicit_and_safe(self):
  from tools.sandbox_lifecycle.cloud import CloudError
  stream=io.StringIO()
  with patch('tools.sandbox_lifecycle.__main__.Cloud.from_operator_profile',side_effect=CloudError('OperatorAuthenticationFailed')),contextlib.redirect_stdout(stream):code=main(['--config',str(self.cfg)])
  self.assertEqual(code,1);self.assertEqual(json.loads(stream.getvalue())['error'],'OperatorAuthenticationFailed')
