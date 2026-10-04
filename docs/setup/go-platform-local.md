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

The first persistence checks prove repeatable migrations, unique catalog/environment codes and denied cross-schema access. Full process startup and acceptance instructions are added as the remaining components are implemented.
