# PgCat OSS bootstrap (in development)

This component implements the dedicated-test OSS bootstrap command and systemd packaging. Cloud composition remains disabled pending Linux/ECS qualification.

Completed local contracts: strict identity/revision and versioned OSS-reference parsing; strict JSON bundle parsing; bounded object retrieval with encryption/version checks and sanitized failures; TOML string escaping. Credentials in tests are synthetic.

Implemented: pinned official SDK ECS-role adapter, private installation, plaintext PostgreSQL startup probe and systemd packaging. Remaining: Linux service/PgCat and live RDS qualification. No IAM grants or secret uploads are performed by the local tests.

Metadata matches the pgcat-ess module: env, proxy_code, target_db_code, db_host, database, secret_reference, revision. The reference is oss://bucket/key?versionId=version. Region is fixed to ap-southeast-1 in the production adapter, not selected from secret content.

Bundle schema version 1 binds the same identities and contains tls_hostname, app_user/app_password, admin_user/admin_password, server_certificate/server_private_key, client_ca/backend_ca. This is a private object contract, never a Terraform input or committed sample containing real credentials.

Run tests from this module: go test ./... .

Owner-approved POC amendment: both SQL hops use plaintext. OSS remains HTTPS/SSE-OSS. The command requires an unprivileged service account and a baked-in 40-character Git revision. See packaging/image-build.md.
