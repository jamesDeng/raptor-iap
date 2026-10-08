#!/usr/bin/env python3
"""Prepare a private, one-shot Kubernetes bootstrap kit; default is preview only."""
import argparse,base64,json,os,pathlib,re,secrets
OWNED={'account':'1360282071200743','region':'ap-southeast-1','cluster':'c92787e953503492ea141a744c81498f1','database':'pgm-t4n389ymwx7otro7'}
NAMESPACE='raptor-system'

def validate(c):
    if any(c.get(k)!=v for k,v in OWNED.items()):raise ValueError('Owned rdev scope differs')
    if not re.fullmatch(r'[a-f0-9]{40}',c.get('source_sha','')):raise ValueError('Reviewed source commit required')
    for kind in ('raptor','gateway'):
        if not re.fullmatch('ghcr.io/jamesdeng/raptor-iap-'+kind+r'@sha256:[a-f0-9]{64}',c.get(kind+'_image','')):raise ValueError('Immutable image required')
    if not re.fullmatch(r'postgres@sha256:[a-f0-9]{64}',c.get('postgres_image','')):raise ValueError('Official PostgreSQL digest required')
    if not re.fullmatch(r'[a-z0-9.-]+\.pgsql\.singapore\.rds\.aliyuncs\.com',c.get('host','')):raise ValueError('Private RDS host required')

def validate_tls_plan(plan):
    updates=[]
    for row in plan.get('resource_changes',[]):
        change=row['change'];actions=change['actions']
        if actions==['no-op']:continue
        if row.get('address')!='module.foundation.alicloud_db_instance.platform' or actions!=['update']:raise ValueError('Only owned database TLS update allowed')
        before,after=change.get('before',{}),change.get('after',{})
        if before.get('id')!=OWNED['database'] or after.get('id')!=OWNED['database'] or after.get('ssl_action')!='Open':raise ValueError('Database TLS scope differs')
        allowed={'ssl_action','ssl_status','ssl_connection_string'}
        for key in set(before)|set(after):
            if key not in allowed and before.get(key)!=after.get(key):raise ValueError('Unexpected database change')
        if any(v and k not in allowed for k,v in change.get('after_unknown',{}).items()):raise ValueError('Unresolved database change')
        updates.append(row)
    if len(updates)!=1:raise ValueError('Exactly one TLS update required')


def preview(c):
    validate(c)
    return {'execute':False,'new_compute_resources':0,'scope':OWNED,'namespace':NAMESPACE,'steps':['create private bootstrap kit','verify live ownership and create named RDS bootstrap account separately','initialize database roles','run two migrations','remove migration secrets','bootstrap admin privately','install pinned Argo CD','publish generated app codes and image digests through GitOps']}

def _secret(name,data):
    return {'apiVersion':'v1','kind':'Secret','metadata':{'name':name,'namespace':NAMESPACE},'type':'Opaque','data':{k:base64.b64encode(v.encode()).decode() for k,v in data.items()}}

def _job(name,image,command,secret,mount=None,args=None):
    container={'name':'bootstrap','image':image,'command':command,'envFrom':[{'secretRef':{'name':secret}}],'resources':{'requests':{'cpu':'20m','memory':'64Mi'},'limits':{'cpu':'200m','memory':'256Mi'}},'securityContext':{'allowPrivilegeEscalation':False,'capabilities':{'drop':['ALL']}}}
    pod={'automountServiceAccountToken':False,'restartPolicy':'Never','securityContext':{'runAsNonRoot':True,'runAsUser':10001,'runAsGroup':10001,'seccompProfile':{'type':'RuntimeDefault'}},'containers':[container]}
    if args:container['args']=args
    if mount:
        container['volumeMounts']=[{'name':'roles','mountPath':'/bootstrap','readOnly':True}];pod['volumes']=[{'name':'roles','secret':{'secretName':mount,'defaultMode':292}}]
    return {'apiVersion':'batch/v1','kind':'Job','metadata':{'name':name,'namespace':NAMESPACE},'spec':{'backoffLimit':0,'activeDeadlineSeconds':180,'template':{'spec':pod}}}

def write_kit(directory,c):
    validate(c);directory=pathlib.Path(directory)
    if directory.exists():raise ValueError('Private kit already exists; inspect instead of overwriting credentials')
    directory.mkdir(mode=0o700,parents=True)
    passwords={k:secrets.token_hex(12)+'Aa9_' for k in ('admin','migrator','raptor','gateway','service','webhook')}
    host=c['host']
    def url(user,password):return f'postgresql://{user}:{password}@{host}:5432/raptor_platform?sslmode=require&pool_max_conns=10'
    sql=f'''\\set ON_ERROR_STOP on
CREATE DATABASE raptor_platform;
\\connect raptor_platform
BEGIN;
CREATE ROLE raptor_owner NOLOGIN;
CREATE ROLE gateway_owner NOLOGIN;
CREATE ROLE raptor_app NOLOGIN;
CREATE ROLE gateway_app NOLOGIN;
CREATE ROLE platform_migrator LOGIN NOINHERIT PASSWORD '{passwords['migrator']}';
CREATE ROLE raptor_runtime LOGIN PASSWORD '{passwords['raptor']}';
CREATE ROLE gateway_runtime LOGIN PASSWORD '{passwords['gateway']}';
GRANT raptor_owner,gateway_owner TO platform_migrator;
GRANT raptor_app TO raptor_runtime;
GRANT gateway_app TO gateway_runtime;
REVOKE ALL ON DATABASE raptor_platform FROM PUBLIC;
GRANT CONNECT ON DATABASE raptor_platform TO platform_migrator,raptor_runtime,gateway_runtime;
GRANT CREATE ON DATABASE raptor_platform TO raptor_owner,gateway_owner;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
COMMIT;
'''
    service={'SERVICE_USERNAME':'rdev-platform','SERVICE_PASSWORD':passwords['service']}
    items=[_secret('platform-service-auth',service),_secret('raptor-runtime',dict(service,RAPTOR_DATABASE_URL=url('raptor_runtime',passwords['raptor']),GITHUB_WEBHOOK_SECRET=passwords['webhook'])),_secret('gateway-runtime',dict(service,GATEWAY_DATABASE_URL=url('gateway_runtime',passwords['gateway'])))]
    # Bootstrap-only credentials never appear in the chart's runtime envFrom.
    bootstrap=[_secret('platform-db-admin',{'PGHOST':host,'PGPORT':'5432','PGUSER':'raptor_bootstrap','PGPASSWORD':passwords['admin'],'PGDATABASE':'postgres','PGSSLMODE':'require'}),_secret('platform-role-sql',{'roles.sql':sql}),_secret('platform-migrator',{'MIGRATION_DATABASE_URL':url('platform_migrator',passwords['migrator'])})]
    init=_job('platform-db-init',c['postgres_image'],['/bin/sh','-ec'],'platform-db-admin','platform-role-sql',['if psql -X -f /bootstrap/roles.sql > /tmp/bootstrap.log 2>&1; then echo database-initialized; else echo database-initialization-failed-inspect-private-log; exit 1; fi'])
    migrations=[_job('raptor-migrate',c['raptor_image'],['/usr/local/bin/backend'],'platform-migrator',args=['-migrate']),_job('gateway-migrate',c['gateway_image'],['/usr/local/bin/gateway'],'platform-migrator',args=['-migrate'])]
    env_config={'cloud':'aliyun','cloudAccountId':c['account'],'region':c['region'],'clusterId':c['cluster'],'namespace':NAMESPACE,'terraformRepo':'https://github.com/jamesDeng/raptor-iap','terraformPath':'infra-terraform/environments/rdev.ali','k8sRepo':'https://github.com/jamesDeng/raptor-iap','k8sPath':'infra-kubernetes/environments/rdev.ali'}
    names=('raptor-frontend','raptor-backend','raptor-open-api','raptor-admin','agent-gateway')
    sql_names=','.join("'"+n+"'" for n in names)
    catalog_sql=f'''\\set ON_ERROR_STOP on
BEGIN;
SELECT pg_advisory_xact_lock(hashtext('raptor:platform-bootstrap'));
INSERT INTO raptor.environment_groups(code,name) VALUES ('raptor','Raptor') ON CONFLICT DO NOTHING;
INSERT INTO raptor.environments(code,group_code,stage,config) VALUES ('rdev.ali','raptor','dev','{json.dumps(env_config)}'::jsonb) ON CONFLICT DO NOTHING;
DO $$ BEGIN
 IF NOT EXISTS (SELECT FROM raptor.environment_groups WHERE code='raptor' AND name='Raptor') OR NOT EXISTS (SELECT FROM raptor.environments WHERE code='rdev.ali' AND group_code='raptor' AND stage='dev' AND config='{json.dumps(env_config)}'::jsonb) THEN RAISE EXCEPTION 'Environment conflict'; END IF;
 IF EXISTS (SELECT name FROM raptor.objects WHERE kind='application' AND name IN ({sql_names}) GROUP BY name HAVING count(*)>1) OR EXISTS (SELECT FROM raptor.objects WHERE kind='application' AND name IN ({sql_names}) AND description<>'Platform service created during rdev bootstrap') THEN RAISE EXCEPTION 'Catalog ownership conflict'; END IF;
END $$;
INSERT INTO raptor.objects(id,kind,code,name,description)
SELECT gen_random_uuid(),'application',raptor.next_object_code('application'),n.name,'Platform service created during rdev bootstrap' FROM unnest(ARRAY[{sql_names}]) AS n(name) WHERE NOT EXISTS (SELECT FROM raptor.objects o WHERE o.kind='application' AND o.name=n.name);
COMMIT;
SELECT jsonb_object_agg(name,code) FROM raptor.objects WHERE kind='application' AND name IN ({sql_names});
'''
    seed_secrets=[_secret('platform-catalog-sql',{'catalog.sql':catalog_sql}),_secret('platform-catalog-db',{'PGHOST':host,'PGPORT':'5432','PGUSER':'raptor_runtime','PGPASSWORD':passwords['raptor'],'PGDATABASE':'raptor_platform','PGSSLMODE':'require'})]
    seed_job=_job('platform-catalog-seed',c['postgres_image'],['psql','-X','-q','-t','-A','-f','/bootstrap/catalog.sql'],'platform-catalog-db','platform-catalog-sql')
    files={'catalog-job.json':json.dumps(seed_job),'catalog-secrets.json':json.dumps({'apiVersion':'v1','kind':'List','items':seed_secrets}),'config.json':json.dumps(c,indent=2),'admin-account.json':json.dumps({'DBInstanceId':c['database'],'AccountName':'raptor_bootstrap','AccountType':'Super','AccountPassword':passwords['admin']}),'roles.sql':sql,'runtime-secrets.json':json.dumps({'apiVersion':'v1','kind':'List','items':items}),'bootstrap-secrets.json':json.dumps({'apiVersion':'v1','kind':'List','items':bootstrap}),'database-job.json':json.dumps(init),'migration-jobs.json':json.dumps({'apiVersion':'v1','kind':'List','items':migrations})}
    for name,content in files.items():
        fd=os.open(directory/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as f:f.write(content+'\n')
    return {'prepared':True,'scope':OWNED,'secret_values_printed':False,'cloud_mutations_performed':False}

def prepare_account(api,c,directory,execute=False):
    import hashlib
    validate(c);directory=pathlib.Path(directory)
    if directory.is_symlink() or directory.stat().st_mode&0o077:raise ValueError('Private kit directory required')
    path=directory/'admin-account.json'
    if path.is_symlink() or path.stat().st_mode&0o077:raise ValueError('Private account input required')
    account=json.loads(path.read_text())
    if account.get('DBInstanceId')!=OWNED['database'] or account.get('AccountName')!='raptor_bootstrap' or account.get('AccountType')!='Super' or not re.fullmatch(r'[a-f0-9]{24}Aa9_',account.get('AccountPassword','')):raise ValueError('Bootstrap account differs')
    if api.call('sts','GetCallerIdentity').get('AccountId')!=OWNED['account']:raise ValueError('Wrong cloud caller')
    attrs=api.call('rds','DescribeDBInstanceAttribute',{'DBInstanceId':c['database']})['Items']['DBInstanceAttribute']
    if len(attrs)!=1:raise ValueError('Expected owned RDS')
    db=attrs[0];vpc='vpc-t4n4fi6r1a7bi6n93ftq3'
    if db.get('DBInstanceId')!=c['database'] or db.get('RegionId')!=c['region'] or db.get('VpcId')!=vpc or db.get('Engine')!='PostgreSQL' or db.get('EngineVersion','').split('.')[0]!='14' or db.get('DBInstanceStatus')!='Running':raise ValueError('RDS scope differs')
    tags=api.call('rds','DescribeTags',{'DBInstanceId':c['database']})['Items']['TagInfos']
    owned={t['TagKey']:t['TagValue'] for t in tags if c['database'] in t.get('DBInstanceIds',{}).get('DBInstanceIds',[])}
    if any(owned.get(k)!=v for k,v in {'Project':'raptor-iap','Environment':'rdev.ali','Owner':'rdev-foundation'}.items()):raise ValueError('RDS ownership differs')
    cluster=api.call('cs','GET',{'path':'/clusters/'+c['cluster']})
    if cluster.get('cluster_id')!=c['cluster'] or cluster.get('state')!='running' or cluster.get('vpc_id')!=vpc:raise ValueError('ACK scope differs')
    endpoints=api.call('rds','DescribeDBInstanceNetInfo',{'DBInstanceId':c['database']})['DBInstanceNetInfos']['DBInstanceNetInfo']
    if not any(e.get('IPType')=='Private' and e.get('VPCId')==vpc and e.get('ConnectionString')==c['host'] for e in endpoints):raise ValueError('Private endpoint differs')
    if api.call('rds','DescribeDBInstanceSSL',{'DBInstanceId':c['database']}).get('SSLEnabled')!='on':raise ValueError('RDS TLS must be enabled before account initialization')
    accounts=api.call('rds','DescribeAccounts',{'DBInstanceId':c['database']})['Accounts']['DBInstanceAccount']
    if any(a.get('AccountName')=='raptor_bootstrap' for a in accounts):raise ValueError('Bootstrap account exists; reconcile without resetting credentials')
    receipt=directory/'account-create-receipt.json'
    if receipt.exists():raise ValueError('Prior create intent exists; reconcile without replay')
    if not execute:return {'execute':False,'live_scope_verified':True,'account_exists':False}
    fingerprint=hashlib.sha256(path.read_bytes()).hexdigest()
    fd=os.open(receipt,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:json.dump({'intent_started':True,'input_sha256':fingerprint,'database':c['database']},f)
    try:result=api.call('rds','CreateAccount',account)
    except Exception:raise ValueError('Account submission outcome unknown; reconcile the private intent receipt without replay') from None
    receipt.write_text(json.dumps({'create_submitted':True,'input_sha256':fingerprint,'database':c['database'],'request_id':result.get('RequestId')}))
    return {'execute':True,'account_create_submitted':True,'secret_values_printed':False}


def main():
    p=argparse.ArgumentParser();p.add_argument('--config',required=True);p.add_argument('--prepare-directory');p.add_argument('--confirm');p.add_argument('--account-kit');p.add_argument('--execute',action='store_true');a=p.parse_args()
    try:
        c=json.loads(pathlib.Path(a.config).read_text());result=preview(c)
        if a.prepare_directory:
            if a.confirm!='rdev.ali':raise ValueError('Explicit rdev.ali confirmation required for local secret preparation')
            dest=pathlib.Path(a.prepare_directory).resolve();root=pathlib.Path(__file__).resolve().parents[2]/'.raptor-local'
            if root not in dest.parents:raise ValueError('Kit must remain in ignored private checkout directory')
            result=write_kit(dest,c)
        if a.account_kit:
            if a.execute and a.confirm!='rdev.ali':raise ValueError('Explicit cloud account confirmation required')
            from overnight import CLI
            result=prepare_account(CLI('infra-ops-poc'),c,pathlib.Path(a.account_kit),a.execute)
        print(json.dumps(result,indent=2))
    except (ValueError,OSError,json.JSONDecodeError):p.exit(1,'Bootstrap preparation failed; inspect private configuration. No credential details printed.\n')
if __name__=='__main__':main()
