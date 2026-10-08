# Linux image qualification

Build from the reviewed bootstrap Git revision (40 lowercase hex characters) and PgCat source 5b038813eb14f181434ab7b5509e74d9b1fe123b. Do not use an unpinned upstream branch or claim a local cross-compile qualifies PgCat on ECS.

Bootstrap cross-build from this module:

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-X main.revision=REVIEWED_GIT_SHA" -o bootstrap ./cmd/bootstrap

REVIEWED_GIT_SHA must be replaced at image build time with the actual reviewed source revision, also supplied as bootstrap_revision in Terraform. Metadata does not override the baked-in revision.

On a private Linux image builder, build the pinned PgCat source using Cargo.lock (`cargo build --release --locked`). Record Rust toolchain, source commit and SHA256 of both binaries. Create service account raptor-pgcat with no interactive login; install binaries root-owned 0755 under /opt/raptor-pgcat and the supplied systemd unit root-owned 0644. No credentials or private certificates belong in the image.

Create /etc/raptor-pgcat root-owned 0755. Its bootstrap.json contains only identity and OSS reference metadata, root-owned 0644. systemd StateDirectory creates the service-owned 0700 runtime directory. User data starts the unit; it must not directly invoke bootstrap as root. Capture/import the immutable ECS image, record its image ID and region, then configure an encrypted system disk.

Before enabling bootstrap_reviewed, prove Linux service startup, bounded failure on role/object/DB refusal, private file ownership, PgCat SQL readiness and metrics. Both SQL hops use explicit plaintext for this private POC; OSS access remains HTTPS/AES256. RDS may offer TLS independently. Do not expose PgCat or metrics through a public listener.

SQL SELECT 1 verifies availability only; it does not initialize the traffic schema, validate full traffic evidence, or prove node-replacement safety. These remain separate live integration gates.
