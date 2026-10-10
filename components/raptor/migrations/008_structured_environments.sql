ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS cloud text NOT NULL DEFAULT '';
ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS region text NOT NULL DEFAULT '';
ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS account_id text NOT NULL DEFAULT '';
ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS ack_cluster_id text NOT NULL DEFAULT '';
ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS cluster_id text NOT NULL DEFAULT '';
ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS namespace text NOT NULL DEFAULT '';
ALTER TABLE raptor.environments ADD COLUMN IF NOT EXISTS infra_api_url text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS raptor.environment_repositories (
 id bigserial PRIMARY KEY,
 environment_code text NOT NULL REFERENCES raptor.environments(code) ON DELETE CASCADE,
 purpose text NOT NULL CHECK (purpose IN ('terraform','kubernetes')),
 repository_url text NOT NULL,
 base_branch text NOT NULL DEFAULT '',
 directory text NOT NULL DEFAULT '',
 UNIQUE(environment_code,purpose,repository_url,directory)
);
CREATE TABLE IF NOT EXISTS raptor.environment_settings (
 environment_code text NOT NULL REFERENCES raptor.environments(code) ON DELETE CASCADE,
 key text NOT NULL,
 value_json text NOT NULL,
 PRIMARY KEY(environment_code,key)
);

-- This migration is replayed at startup. The source column exists only on the first run.
DO $migration$
BEGIN
 IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='raptor' AND table_name='environments' AND column_name='config') THEN
  UPDATE raptor.environments SET
   cloud = CASE WHEN jsonb_typeof(config->'cloud')='string' THEN config->>'cloud' ELSE cloud END,
   region = CASE WHEN jsonb_typeof(config->'region')='string' THEN config->>'region' ELSE region END,
   account_id = CASE WHEN jsonb_typeof(config->'accountId')='string' THEN config->>'accountId' ELSE account_id END,
   ack_cluster_id = CASE WHEN jsonb_typeof(config->'ackClusterId')='string' THEN config->>'ackClusterId' ELSE ack_cluster_id END,
   cluster_id = CASE WHEN jsonb_typeof(config->'clusterId')='string' THEN config->>'clusterId' ELSE cluster_id END,
   namespace = CASE WHEN jsonb_typeof(config->'namespace')='string' THEN config->>'namespace' ELSE namespace END,
   infra_api_url = CASE WHEN jsonb_typeof(config->'infraApiUrl')='string' THEN config->>'infraApiUrl' ELSE infra_api_url END;
  INSERT INTO raptor.environment_repositories(environment_code,purpose,repository_url,base_branch,directory)
   SELECT code,'terraform',config->>'terraformRepo',CASE WHEN jsonb_typeof(config->'terraformBaseBranch')='string' THEN config->>'terraformBaseBranch' ELSE '' END,CASE WHEN jsonb_typeof(config->'terraformPath')='string' THEN config->>'terraformPath' ELSE '' END
   FROM raptor.environments WHERE jsonb_typeof(config->'terraformRepo')='string' AND config->>'terraformRepo'<>''
   ON CONFLICT DO NOTHING;
  INSERT INTO raptor.environment_repositories(environment_code,purpose,repository_url,base_branch,directory)
   SELECT code,'kubernetes',config->>'kubernetesRepo',CASE WHEN jsonb_typeof(config->'kubernetesBaseBranch')='string' THEN config->>'kubernetesBaseBranch' ELSE '' END,CASE WHEN jsonb_typeof(config->'kubernetesPath')='string' THEN config->>'kubernetesPath' ELSE '' END
   FROM raptor.environments WHERE jsonb_typeof(config->'kubernetesRepo')='string' AND config->>'kubernetesRepo'<>''
   ON CONFLICT DO NOTHING;
  -- Keep all non-string recognized values and every unknown value verbatim as JSON text.
  INSERT INTO raptor.environment_settings(environment_code,key,value_json)
   SELECT e.code, kv.key, kv.value::text FROM raptor.environments e CROSS JOIN LATERAL jsonb_each(e.config) kv
   WHERE NOT (kv.key IN ('cloud','region','accountId','ackClusterId','clusterId','namespace','infraApiUrl') AND jsonb_typeof(kv.value)='string' AND kv.value#>>'{}'<>'')
     AND NOT (kv.key IN ('terraformRepo','kubernetesRepo') AND jsonb_typeof(kv.value)='string' AND kv.value#>>'{}'<>'')
     AND NOT (kv.key IN ('terraformPath','terraformBaseBranch') AND jsonb_typeof(e.config->'terraformRepo')='string' AND e.config->>'terraformRepo'<>'' AND jsonb_typeof(kv.value)='string' AND kv.value#>>'{}'<>'')
     AND NOT (kv.key IN ('kubernetesPath','kubernetesBaseBranch') AND jsonb_typeof(e.config->'kubernetesRepo')='string' AND e.config->>'kubernetesRepo'<>'' AND jsonb_typeof(kv.value)='string' AND kv.value#>>'{}'<>'')
   ON CONFLICT DO NOTHING;
  ALTER TABLE raptor.environments DROP COLUMN config;
 END IF;
END $migration$;
GRANT SELECT,INSERT,UPDATE,DELETE ON raptor.environment_repositories,raptor.environment_settings TO raptor_app;
GRANT USAGE,SELECT ON SEQUENCE raptor.environment_repositories_id_seq TO raptor_app;
