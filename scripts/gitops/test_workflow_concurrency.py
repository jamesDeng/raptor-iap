"""A reviewed deployment must not occupy the queue used by PR plans."""
import pathlib, unittest, re
ROOT = pathlib.Path(__file__).resolve().parents[2]
class WorkflowConcurrencyTests(unittest.TestCase):
 def test_pr_plans_do_not_wait_on_reviewed_main_applies(self):
  for name in ('terraform-test-gitops.yml','terraform-infra-api-gitops.yml','terraform-gateway-gitops.yml'):
   with self.subTest(workflow=name):
    workflow=(ROOT/'.github/workflows'/name).read_text()
    group=re.search(r'^concurrency:\n  group: (.+)$',workflow,re.M).group(1)
    self.assertIn("github.event_name == 'pull_request'",group)
    self.assertIn('github.event.pull_request.number',group)
    self.assertIn('||',group)
    self.assertIn('  cancel-in-progress: false',workflow)
    apply=workflow.split('\n  apply:',1)[1]
    self.assertIn('environment: rdev.ali-apply',apply)
    self.assertIn("github.event_name != 'pull_request'",apply)
if __name__=='__main__': unittest.main()
