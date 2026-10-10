import pathlib, unittest, yaml
ROOT=pathlib.Path(__file__).resolve().parents[2]
class SandboxImageContract(unittest.TestCase):
 def test_qualified_image_built_from_exact_sources(self):
  workflow=yaml.safe_load((ROOT/'.github/workflows/ax-sandbox-image.yml').read_text())
  jobs=workflow['jobs']; self.assertIn('build-test',jobs);self.assertIn('publish',jobs)
  self.assertEqual(jobs['publish']['needs'],'build-test')
  self.assertIn("github.ref == 'refs/heads/main'",jobs['publish']['if'])
  text=(ROOT/'scripts/ecs_ax/prepare-sandbox-image.sh').read_text()
  for required in ['pins.json','materialize_sources.py','substrate-v1-compat.patch','ax-task-runner','CGO_ENABLED=0','agent-harness']:
   self.assertIn(required,text)
  check=(ROOT/'scripts/ecs_ax/check-sandbox-image.sh').read_text()
  for required in ['aliyun version','kubectl version --client','psql --version','node --test','ax-task-runner']:
   self.assertIn(required,check)
if __name__=='__main__':unittest.main()
