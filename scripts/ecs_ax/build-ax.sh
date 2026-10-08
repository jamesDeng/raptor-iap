#!/usr/bin/env bash
set -euo pipefail
[[ "$(hostname)" == raptor-ecs-ax ]] || exit 1
install -d -m 700 /opt/raptor/ax
cd /opt/raptor/ax
tar --no-same-owner -xzf /var/lib/raptor-ecs-ax/downloads/ecs-ax-upstream-ax.tar.gz
patch -p1 < /var/lib/raptor-ecs-ax/downloads/substrate-v1-compat.patch
go mod edit -replace=github.com/agent-substrate/substrate=../substrate
export GOFLAGS=-p=2 GOMAXPROCS=2 CGO_ENABLED=0
go mod tidy
mkdir -p bin/linux_amd64
# Build only; application deployment follows Substrate acceptance.
go build -o /usr/local/bin/ax ./cmd/ax
go build -o bin/linux_amd64/ax-task-runner ./cmd/ax-task-runner
go build -o bin/linux_amd64/ax-server ./cmd/ax-server
