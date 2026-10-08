#!/usr/bin/env bash
# Source bundle is created by materialize_sources.py; overlay is this repository's reviewed checkout.
set -euo pipefail
[[ "$(hostname)" == raptor-ecs-ax ]] || exit 1
: "${RAPTOR_SOURCE_ROOT:?Reviewed Raptor checkout required}"
downloads=/var/lib/raptor-ecs-ax/downloads
python3 "$RAPTOR_SOURCE_ROOT/scripts/ecs_ax/verify_sources.py" "$RAPTOR_SOURCE_ROOT/third_party/ax/pins.json" "$downloads"
install -d -m 700 /opt/raptor/ax-verified
cd /opt/raptor/ax-verified
tar --no-same-owner -xzf "$downloads/ecs-ax-upstream-ax.tar.gz"
patch -p1 < "$downloads/substrate-v1-compat.patch"
mkdir -p cmd/raptor-bridge bin/linux_amd64
cp "$RAPTOR_SOURCE_ROOT"/components/ax-runtime-bridge/*.go cmd/raptor-bridge/
cp "$RAPTOR_SOURCE_ROOT"/components/ax-runtime-bridge/substrate/*.go internal/substrate/
cp "$RAPTOR_SOURCE_ROOT/components/ax-runtime-bridge/Dockerfile" Dockerfile.bridge
go mod edit -replace=github.com/agent-substrate/substrate=../substrate
export CGO_ENABLED=0 GOFLAGS=-p=2 GOMAXPROCS=2
go mod tidy
go test ./cmd/raptor-bridge ./internal/substrate
go build -o bin/linux_amd64/raptor-bridge ./cmd/raptor-bridge
docker build -f Dockerfile.bridge -t localhost:5001/raptor-ax-bridge:verified .
docker push localhost:5001/raptor-ax-bridge:verified
