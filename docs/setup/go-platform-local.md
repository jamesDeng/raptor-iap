# Local Go platform

Work in an isolated checkout. The Go platform is separate from the existing Python/SQLite prototype and will use ports 8870–8874. It does not import legacy private data.

## Prerequisites

Go >=1.25 and PostgreSQL 17. On a Mac using Homebrew: `brew install go postgresql@17`. Do not enable a system-wide PostgreSQL service for this test.

Create a private directory `.raptor-local/go-platform` with permissions 0700. Generate a strong local PostgreSQL password into a 0600 file using a password manager or a cryptographic generator; never paste credentials into chat, Git or a command argument.

Initialize a dedicated cluster:

```sh
/opt/homebrew/opt/postgresql@17/bin/initdb -D .raptor-local/go-platform/postgres -U postgres -A scram-sha-256 --pwfile=.raptor-local/go-platform/postgres-password
/opt/homebrew/opt/postgresql@17/bin/pg_ctl -D .raptor-local/go-platform/postgres -l .raptor-local/go-platform/postgres.log -o '-p 55432 -h 127.0.0.1 -k /tmp' start
```

The command runner must allow local shared-memory and loopback socket access. Use the dedicated cluster only; do not reset an existing user's database.

## Tests

Supply `TEST_DATABASE_URL` privately, pointing to the cluster's administrative test connection. Tests create uniquely named disposable databases, create schema owner/runtime roles if absent, migrate with the owner role and drop only their own databases. No cloud calls occur.

```sh
cd components/raptor
go test ./...
cd ../agent-gateway
go test ./...
```

Runtime services use different `raptor_app` and `gateway_app` credentials. Schema owners perform migrations; application processes must never run with the administrative test URL. Credentials and PostgreSQL files remain ignored.

## Local service database

Use a new database for this platform. Connect to the dedicated cluster with `psql -h 127.0.0.1 -p 55432 -U postgres -d postgres`; enter its password at the terminal prompt. The SQL below creates only the named local platform roles/database. If these login roles or database already exist, inspect their ownership rather than dropping or replacing them.

```sql
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='raptor_owner') THEN CREATE ROLE raptor_owner NOLOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='gateway_owner') THEN CREATE ROLE gateway_owner NOLOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='raptor_app') THEN CREATE ROLE raptor_app NOLOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='gateway_app') THEN CREATE ROLE gateway_app NOLOGIN; END IF;
END $$;
CREATE ROLE platform_migrator LOGIN NOINHERIT;
CREATE ROLE raptor_local LOGIN;
CREATE ROLE gateway_local LOGIN;
GRANT raptor_owner, gateway_owner TO platform_migrator;
GRANT raptor_app TO raptor_local;
GRANT gateway_app TO gateway_local;
CREATE DATABASE raptor_go_poc;
GRANT CREATE ON DATABASE raptor_go_poc TO raptor_owner,gateway_owner;
```

Set each login password privately using `\password platform_migrator`, `\password raptor_local` and `\password gateway_local`. Runtime logins inherit only their own application's privileges; neither inherits an owner role. Keep the migration credential out of the runtime environment after migration.

Store these settings in a private, ignored `.raptor-local/go-platform/local.env` file with mode 0600. Replace password references with local secrets; URL-encode reserved characters in database passwords. A random hexadecimal password avoids URL-encoding ambiguity.

```sh
RAPTOR_DATABASE_URL='postgresql://raptor_local:PASSWORD@127.0.0.1:55432/raptor_go_poc'
GATEWAY_DATABASE_URL='postgresql://gateway_local:PASSWORD@127.0.0.1:55432/raptor_go_poc'
SERVICE_USERNAME='local-platform'
SERVICE_PASSWORD='LOCAL_SERVICE_SECRET'
RAPTOR_BACKEND_URL='http://127.0.0.1:8871'
RAPTOR_OPEN_API_URL='http://127.0.0.1:8872'
GATEWAY_URL='http://127.0.0.1:8874'
RAPTOR_REPOSITORY='/absolute/path/to/raptor-iap'
GATEWAY_CHECKPOINT_DIR='/absolute/path/to/raptor-iap/.raptor-local/go-platform/checkpoints'
GITHUB_WEBHOOK_SECRET='LOCAL_WEBHOOK_SECRET'
RAPTOR_SIMULATION='true'
RAPTOR_FIXTURE_FILE='/absolute/path/to/raptor-iap/.raptor-local/go-platform/deployments.json'
GATEWAY_SIMULATION='true'
```

Use synthetic deployments only: copy `docs/contracts/fixtures/deployments.json` to the private fixture path. After creating an application through the catalog, synthetic Deployment entries can use its generated code, selected environment, cluster, namespace, workload name and UID. They do not establish a live ACK deployment. A missing/unreadable fixture returns unavailable; it is not treated as an empty cloud inventory.

Skills selection requires a stable published local tag containing `agent-skills/`. Use the release convention in [agent-skills/README.md](../../agent-skills/README.md). An empty release list does not invent a release. For disposable UI verification, the explicit browser fixture below supplies synthetic releases instead of changing tags in this repository.

## Build, migrate and start

Build from the repository root:

```sh
mkdir -p .raptor-local/go-platform/bin
cd components/raptor
go build -o ../../.raptor-local/go-platform/bin/raptor-backend ./cmd/backend
go build -o ../../.raptor-local/go-platform/bin/raptor-open-api ./cmd/open-api
go build -o ../../.raptor-local/go-platform/bin/raptor-frontend ./cmd/frontend
go build -o ../../.raptor-local/go-platform/bin/raptor-admin ./cmd/admin
cd ../agent-gateway
go build -o ../../.raptor-local/go-platform/bin/agent-gateway ./cmd/gateway
cd ../..
```

Set `MIGRATION_DATABASE_URL` privately to the migration login's URL, then run both owner migrations. Do not use `TEST_DATABASE_URL` as a service credential.

```sh
.raptor-local/go-platform/bin/raptor-backend -migrate
.raptor-local/go-platform/bin/agent-gateway -migrate
unset MIGRATION_DATABASE_URL
set -a
source .raptor-local/go-platform/local.env
set +a
.raptor-local/go-platform/bin/raptor-backend -bootstrap-admin admin
```

The bootstrap command reads the administrator's password privately from the terminal. It does not import the legacy owner or SQLite data.

Run each of the following in its own terminal, after loading the same private environment file:

```sh
.raptor-local/go-platform/bin/raptor-backend
.raptor-local/go-platform/bin/raptor-open-api
.raptor-local/go-platform/bin/raptor-frontend
.raptor-local/go-platform/bin/raptor-admin
.raptor-local/go-platform/bin/agent-gateway
```

Open `http://127.0.0.1:8870/`; administration is at `http://127.0.0.1:8873/`. Leave the existing 8765 prototype alone. Close only these foreground processes with Ctrl-C; do not use a broad process-name kill. Stop while execution is idle. Unknown ownership after an interrupted running process blocks another runtime rather than replaying it. Checkpoint/cleanup failures require explicit reconciliation; there is no automatic timeout-based takeover.

The default simulated worker completes an inert fixture. The acceptance-only `GATEWAY_SIMULATED_SCENARIO=approval-review` adds deterministic approval/review pauses; it is not a PgCat replacement workflow. No OpenAI, Aliyun, GitHub mutation or Terraform apply credential is needed for this local slice.

## Verification

With a private administrative `TEST_DATABASE_URL` for the dedicated test cluster:

```sh
cd components/raptor
go test -race ./...
go vet ./...
cd ../agent-gateway
go test -race ./...
go vet ./...
cd ../..
node --test tests/harness/go-platform-web.test.mjs
```

The Raptor acceptance test builds a Gateway child process and connects real local HTTP endpoints using restricted runtime roles in a disposable database. Its repository/runtime/provider data is synthetic. Gateway acceptance checks request-specific checkpoint restoration and saved progress.

For a disposable browser fixture, start the frontend above, then run from `components/raptor` with the test connection:

```sh
POC_BROWSER_FIXTURE=true go test ./internal/backend -run TestBrowserFixtureServer -v
```

This intentionally isolated fixture uses `poc-reviewer` / `local-fixture-only-2026`, synthetic releases and deployments, and a fixture Gateway reader. These are test-only identities, not real account credentials. Interrupt only this test process to remove its disposable database. The fixture is skipped during normal tests.

See [the acceptance receipt](go-platform-local-acceptance.md) for what was actually tested and its limits.
