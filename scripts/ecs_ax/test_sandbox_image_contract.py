import json, os, pathlib, subprocess, sys, tempfile, unittest, yaml
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
  for required in ['aliyun version','kubectl version --client','psql --version','gh --version','node --test','ax-task-runner']:
   self.assertIn(required,check)
  dockerfile=(ROOT/'components/ax-sandbox/Dockerfile.pi').read_text()
  self.assertIn('apt-get install -y --no-install-recommends',dockerfile)
  self.assertIn(' gh ',dockerfile)
 def test_offline_acceptance_mounts_authoritative_fixture_trees_readonly(self):
  with tempfile.TemporaryDirectory(prefix='ax-image-contract-') as folder:
   temporary=pathlib.Path(folder);docker=temporary/'docker';record=temporary/'docker-args.json'
   docker.write_text('#!'+sys.executable+'\nimport json,os,sys\nwith open(os.environ["DOCKER_ARGS_FILE"],"w") as output: json.dump(sys.argv[1:],output)\n')
   docker.chmod(0o700)
   env={**os.environ,'PATH':str(temporary)+os.pathsep+os.environ['PATH'],'DOCKER_ARGS_FILE':str(record)}
   result=subprocess.run(['bash',str(ROOT/'scripts/ecs_ax/check-sandbox-image.sh'),'fixture:image'],cwd=temporary,env=env,text=True,capture_output=True)
   self.assertEqual(result.returncode,0,result.stderr)
   args=json.loads(record.read_text())
   mounts=[args[i+1] for i,value in enumerate(args) if value=='--mount']
   self.assertEqual(mounts,[
    f'type=bind,source={ROOT}/agent-skills/pgcat-replacement,target=/agent-skills/pgcat-replacement,readonly',
    f'type=bind,source={ROOT}/components/agent-gateway/internal/execution/testdata,target=/opt/agent-gateway/internal/execution/testdata,readonly',
   ])
   self.assertIn(str(ROOT)+'/scripts/contracts:/scripts/contracts:ro',args)
   self.assertEqual(args[0:2],['run','--rm'])
   self.assertEqual(args[-3:-1],['fixture:image','-ec'])
   self.assertIn('cd /opt/raptor-harness',args[-1])
   self.assertIn('node --test *.test.mjs',args[-1])
if __name__=='__main__':unittest.main()
