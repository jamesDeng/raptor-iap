-- Stable UUID identity, human-readable catalog codes, and immutable legacy aliases.
-- Requests/results are historical evidence and are deliberately not rewritten.
CREATE SEQUENCE IF NOT EXISTS raptor.application_code_seq;
CREATE SEQUENCE IF NOT EXISTS raptor.database_code_seq;
CREATE SEQUENCE IF NOT EXISTS raptor.proxy_code_seq;
CREATE TABLE IF NOT EXISTS raptor.object_code_aliases(
 code text PRIMARY KEY,
 object_id uuid NOT NULL REFERENCES raptor.objects(id) ON DELETE CASCADE,
 kind text NOT NULL,
 canonical_code text NOT NULL,
 migrated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS raptor.catalog_code_migrations(version integer PRIMARY KEY);
DO $$
DECLARE item record; prefix text; seq_name text; next_code text; n bigint;
BEGIN
 IF EXISTS (SELECT FROM raptor.catalog_code_migrations WHERE version=1) THEN RETURN; END IF;
 LOCK TABLE raptor.objects,raptor.requests IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT FROM raptor.requests WHERE status NOT IN ('completed','failed','cancelled')) THEN
  RAISE EXCEPTION 'Catalog code migration requires all existing requests to finish first';
 END IF;
 -- Reserve any already canonical codes before allocating the legacy records.
 FOR item IN SELECT * FROM (VALUES ('application','A','application_code_seq'),('database','D','database_code_seq'),('db-proxy','DP','proxy_code_seq')) AS types(kind,prefix,seq_name) LOOP
  SELECT max(substring(code FROM length(item.prefix)+1)::bigint) INTO n FROM raptor.objects
   WHERE kind=item.kind AND code ~ ('^'||item.prefix||'[0-9]{5,}$') AND substring(code FROM length(item.prefix)+1)::bigint>0;
  IF n IS NOT NULL THEN PERFORM setval(('raptor.'||item.seq_name)::regclass,n,true); END IF;
 END LOOP;
 FOR item IN SELECT id,kind,code FROM raptor.objects ORDER BY kind,id LOOP
  CASE item.kind
   WHEN 'application' THEN prefix:='A';seq_name:='raptor.application_code_seq';
   WHEN 'database' THEN prefix:='D';seq_name:='raptor.database_code_seq';
   WHEN 'db-proxy' THEN prefix:='DP';seq_name:='raptor.proxy_code_seq';
  END CASE;
  IF item.code ~ ('^'||prefix||'[0-9]{5,}$') AND substring(item.code FROM length(prefix)+1)::bigint>0 THEN CONTINUE; END IF;
  LOOP
   n:=nextval(seq_name::regclass);
   next_code:=prefix||lpad(n::text,greatest(5,length(n::text)),'0');
   EXIT WHEN NOT EXISTS(SELECT FROM raptor.objects WHERE code=next_code) AND NOT EXISTS(SELECT FROM raptor.object_code_aliases WHERE code=next_code);
  END LOOP;
  INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) VALUES(item.code,item.id,item.kind,next_code);
  UPDATE raptor.objects SET code=next_code WHERE id=item.id;
 END LOOP;
 INSERT INTO raptor.catalog_code_migrations(version) VALUES(1);
END $$;
CREATE OR REPLACE FUNCTION raptor.next_object_code(object_kind text) RETURNS text LANGUAGE plpgsql AS $$
DECLARE prefix text; seq_name text; n bigint; candidate text;
BEGIN
 CASE object_kind
  WHEN 'application' THEN prefix:='A';seq_name:='raptor.application_code_seq';
  WHEN 'database' THEN prefix:='D';seq_name:='raptor.database_code_seq';
  WHEN 'db-proxy' THEN prefix:='DP';seq_name:='raptor.proxy_code_seq';
  ELSE RAISE EXCEPTION 'Invalid object kind';
 END CASE;
 LOOP
  n:=nextval(seq_name::regclass);
  candidate:=prefix||lpad(n::text,greatest(5,length(n::text)),'0');
  EXIT WHEN NOT EXISTS(SELECT FROM raptor.objects WHERE code=candidate) AND NOT EXISTS(SELECT FROM raptor.object_code_aliases WHERE code=candidate);
 END LOOP;
 RETURN candidate;
END $$;
REVOKE ALL ON FUNCTION raptor.next_object_code(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION raptor.next_object_code(text) TO raptor_app;
GRANT USAGE,SELECT ON raptor.application_code_seq,raptor.database_code_seq,raptor.proxy_code_seq TO raptor_app;
REVOKE ALL ON raptor.object_code_aliases,raptor.catalog_code_migrations FROM raptor_app;
GRANT SELECT ON raptor.object_code_aliases TO raptor_app;
