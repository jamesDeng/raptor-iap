#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || -z "$1" ]]; then
  echo "Usage: $0 IMAGE_REFERENCE" >&2
  exit 2
fi

container=''
cleanup() {
  if [[ -n "$container" ]]; then
    docker rm -f "$container" >/dev/null
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

image="$1"
[[ "$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")" == linux/amd64 ]]
container="$(docker run --detach --network none --platform linux/amd64 "$image")"
sleep 3
[[ "$(docker inspect --format '{{.State.Running}}' "$container")" == true ]]
[[ "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$container")" == none ]]

docker exec "$container" /bin/bash -euo pipefail -c '
  [[ "$(pi --version)" == "0.99.2" ]]
  node -e '\''const actual = process.versions.node.split(".").map(Number); const floor = [22,19,0]; for (let i=0;i<3;i++) { if (actual[i]>floor[i]) process.exit(0); if (actual[i]<floor[i]) process.exit(1); }'\''
  [[ -x /bin/bash ]]
  git --version >/dev/null
  curl --version >/dev/null
  [[ -s /etc/ssl/certs/ca-certificates.crt ]]
  [[ -w /etc/passwd && -w /etc/group ]]
'
echo 'Offline image acceptance checks passed.'
