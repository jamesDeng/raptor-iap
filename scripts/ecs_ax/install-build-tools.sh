#!/usr/bin/env bash
# Dedicated POC node only.
set -euo pipefail
[[ "$(hostname)" == raptor-ecs-ax ]] || exit 1
cd /var/lib/raptor-ecs-ax/downloads
curl --retry 3 -fsSL https://go.dev/dl/go1.27.1.linux-amd64.tar.gz -o go.tar.gz
printf '%s  go.tar.gz\n' 63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445 | sha256sum -c -
tar -C /usr/local -xzf go.tar.gz
ln -sf /usr/local/go/bin/go /usr/local/bin/go
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker.io git make
# Registry is loopback-only, persistent on the encrypted ECS disk.
install -d -m 700 /var/lib/raptor-ecs-ax/registry
docker run -d --restart=always --name raptor-registry -p 127.0.0.1:5001:5000 -v /var/lib/raptor-ecs-ax/registry:/var/lib/registry registry@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373
