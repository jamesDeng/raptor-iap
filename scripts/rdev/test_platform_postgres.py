"""Exclusive disposable local/CI PostgreSQL cluster acceptance, never cloud."""
import base64,json,os,pathlib,subprocess,tempfile,unittest,urllib.parse
from platform_bootstrap import write_kit
@unittest.skipUnless(os.environ.get('POC_TEST_POSTGRES')=='true','Dedicated local PostgreSQL integration opt-in required')
class PostgreSQLBootstrap(unittest.TestCase):
 def test_migrations_roles_and_repeat_catalog(self):
  self.assertIn(os.environ.get('PGHOST'),['127.0.0.1','localhost'])
  self.assertEqual(os.environ.get('PGDATABASE'),'postgres')
  env=dict(os.environ);psql=os.environ.get('PSQL','psql')
  def run(args,env=env,input=None,ok=True):
   r=subprocess.run(args,env=env,input=input,capture_output=True,text=True)
   if ok:self.assertEqual(r.returncode,0,'Private integration operation failed; no credential-bearing output printed')
   return r
  with tempfile.TemporaryDirectory() as tmp:
   root=pathlib.Path(tmp)/'kit';c={'account':'1360282071200743','region':'ap-southeast-1','cluster':'c92787e953503492ea141a744c81498f1','database':'pgm-t4n389ymwx7otro7','host':'test.pgsql.singapore.rds.aliyuncs.com','source_sha':'a'*40,'raptor_image':'ghcr.io/jamesdeng/raptor-iap-raptor@sha256:'+'b'*64,'gateway_image':'ghcr.io/jamesdeng/raptor-iap-gateway@sha256:'+'c'*64,'postgres_image':'postgres@sha256:'+'d'*64};write_kit(root,c)
   # Exclusive fresh cluster: refuse pre-existing platform DB rather than dropping it.
   found=run([psql,'-X','-Atc',"SELECT count(*) FROM pg_database WHERE datname='raptor_platform'"]).stdout.strip();self.assertEqual(found,'0')
   run([psql,'-X','-v','ON_ERROR_STOP=1','-f',str(root/'roles.sql')])
   bootstrap=json.loads((root/'bootstrap-secrets.json').read_text())['items']
   migration=next(x for x in bootstrap if x['metadata']['name']=='platform-migrator')
   url=base64.b64decode(migration['data']['MIGRATION_DATABASE_URL']).decode();parsed=urllib.parse.urlsplit(url)
   local=f'postgresql://{parsed.username}:{parsed.password}@{env["PGHOST"]}:{env.get("PGPORT","5432")}/raptor_platform?sslmode=disable&pool_max_conns=10'
   menv=dict(env,MIGRATION_DATABASE_URL=local)
   for binary in [os.environ['RAPTOR_TEST_BINARY'],os.environ['GATEWAY_TEST_BINARY']]:run([binary,'-migrate'],menv);run([binary,'-migrate'],menv)
   runtime_secrets=json.loads((root/'runtime-secrets.json').read_text())['items']
   for login,secret_name,url_key,own,other in [('raptor_runtime','raptor-runtime','RAPTOR_DATABASE_URL','raptor.objects','gateway.executions'),('gateway_runtime','gateway-runtime','GATEWAY_DATABASE_URL','gateway.executions','raptor.objects')]:
    secret=next(x for x in runtime_secrets if x['metadata']['name']==secret_name)
    credentials=urllib.parse.urlsplit(base64.b64decode(secret['data'][url_key]).decode())
    runtime_env=dict(env,PGDATABASE='raptor_platform',PGUSER=credentials.username,PGPASSWORD=credentials.password)
    run([psql,'-X','-v','ON_ERROR_STOP=1','-c',f'SELECT * FROM {own} LIMIT 0'],runtime_env)
    for sql in [f'SELECT * FROM {other} LIMIT 0',f'INSERT INTO {other} SELECT * FROM {other} WHERE false','SET ROLE raptor_owner','SET ROLE gateway_owner','SET ROLE platform_migrator']:
     self.assertNotEqual(run([psql,'-X','-v','ON_ERROR_STOP=1','-c',sql],runtime_env,ok=False).returncode,0)
    for owner in ['raptor_owner','gateway_owner','platform_migrator']:
     membership=run([psql,'-X','-Atc',f"SELECT pg_has_role(current_user,'{owner}','MEMBER')"],runtime_env).stdout.strip();self.assertEqual(membership,'f')
   for role,own,other in [('raptor_app','raptor.objects','gateway.executions'),('gateway_app','gateway.executions','raptor.objects')]:
    e=dict(env,PGDATABASE='raptor_platform');run([psql,'-X','-v','ON_ERROR_STOP=1','-c',f'SET ROLE {role}; SELECT * FROM {own} LIMIT 0'],e)
    denied=run([psql,'-X','-v','ON_ERROR_STOP=1','-c',f'SET ROLE {role}; SELECT * FROM {other} LIMIT 0'],e,ok=False);self.assertNotEqual(denied.returncode,0)
   seed=json.loads((root/'catalog-secrets.json').read_text())['items'];data=next(x for x in seed if x['metadata']['name']=='platform-catalog-sql')['data'];sql=base64.b64decode(data['catalog.sql']).decode()
   runtime=next(x for x in seed if x['metadata']['name']=='platform-catalog-db');credentials=runtime['data'];e=dict(env,PGDATABASE='raptor_platform',PGUSER=base64.b64decode(credentials['PGUSER']).decode(),PGPASSWORD=base64.b64decode(credentials['PGPASSWORD']).decode());a=run([psql,'-X','-qAt','-v','ON_ERROR_STOP=1'],e,input=sql).stdout.strip().splitlines()[-1];b=run([psql,'-X','-qAt','-v','ON_ERROR_STOP=1'],e,input=sql).stdout.strip().splitlines()[-1]
   self.assertEqual(json.loads(a),json.loads(b));self.assertEqual(len(json.loads(a)),5);self.assertEqual(len(set(json.loads(a).values())),5)
   run([psql,'-X','-c',"UPDATE raptor.environments SET config='{}' WHERE code='rdev.ali'"],e)
   conflict=run([psql,'-X','-qAt','-v','ON_ERROR_STOP=1'],e,input=sql,ok=False);self.assertNotEqual(conflict.returncode,0)
