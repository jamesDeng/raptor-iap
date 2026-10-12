import pathlib
import unittest

from runner import select_stack, summary, validate_inputs

ROOT = pathlib.Path(__file__).resolve().parents[2]


class RaptorGitHubPATContracts(unittest.TestCase):
    def test_dedicated_stack_and_sanitized_plan_comment(self):
        stack = select_stack('raptor-github-pat')
        self.assertEqual(stack['root'], 'infra-terraform/environments/rdev.ali/raptor-github-pat')
        self.assertEqual(stack['prefix'], 'raptor-github-pat.ali')
        inputs = {'oidc_provider_arn': 'acs:ram::1360282071200743:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1', 'oidc_issuer': 'https://oidc-ack-ap-southeast-1.oss-ap-southeast-1.aliyuncs.com/cluster'}
        validate_inputs(inputs, 'raptor-github-pat')
        with self.assertRaises(ValueError):
            validate_inputs({**inputs, 'token': 'SECRET'}, 'raptor-github-pat')
        plan = {'complete': True, 'resource_changes': [{'mode': 'managed', 'address': 'module.raptor_github_pat.alicloud_oss_bucket.pat', 'change': {'actions': ['create'], 'after': {'secret': 'SECRET'}}}]}
        comment = summary(plan, 'a' * 40, 'raptor-github-pat')
        self.assertIn('create', comment)
        self.assertNotIn('SECRET', comment)

    def test_pr_workflow_posts_summary_and_requires_reviewed_apply(self):
        workflow = (ROOT / '.github/workflows/terraform-raptor-github-pat-gitops.yml').read_text()
        self.assertIn('gh pr comment "$PR_NUMBER" --body-file "$RUNNER_TEMP/summary.md"', workflow)
        self.assertIn('environment: rdev.ali-apply', workflow)
        self.assertIn('scripts/gitops/runner.py guard', workflow)
        self.assertIn('TF_STACK_NAME: raptor-github-pat', workflow)
