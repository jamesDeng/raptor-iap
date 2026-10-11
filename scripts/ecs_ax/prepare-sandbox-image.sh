#!/usr/bin/env bash
# Build the guest runner from the exact verified upstream pair.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
[[ $# == 1 ]]
output=$(python3 -c 'import pathlib,sys;print(pathlib.Path(sys.argv[1]).resolve())' "$1")
[[ ! -e "$output" ]]
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
read -r ax_commit substrate_commit < <(python3 - "$root/third_party/ax/pins.json" <<'PY'
import json,sys
v=json.load(open(sys.argv[1]));print(v['ax_commit'],v['substrate_commit'])
PY
)
for item in ax substrate; do
  git init -q "$workspace/$item"
  git -C "$workspace/$item" remote add origin "https://github.com/$(if [[ "$item" == ax ]]; then echo google/ax; else echo agent-substrate/substrate; fi).git"
  revision=$ax_commit
  [[ "$item" != substrate ]] || revision=$substrate_commit
  git -C "$workspace/$item" fetch -q --depth=1 origin "$revision"
  git -C "$workspace/$item" checkout -q --detach FETCH_HEAD
  [[ "$(git -C "$workspace/$item" rev-parse HEAD)" == "$revision" ]]
done
python3 "$root/scripts/ecs_ax/materialize_sources.py" "$workspace/ax" "$workspace/substrate" "$workspace/verified"
cd "$workspace/ax"
patch -p1 < "$root/third_party/ax/substrate-v1-compat.patch"
go mod edit -replace=github.com/agent-substrate/substrate=../substrate
go mod tidy
mkdir -p "$output/bin/linux_amd64" "$output/harness"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$output/bin/linux_amd64/ax-task-runner" ./cmd/ax-task-runner
# Only tracked reviewed harness files; no credentials, node_modules or local state.
git -C "$root" ls-files -z components/agent-harness | while IFS= read -r -d '' file; do
  relative=${file#components/agent-harness/}
  mkdir -p "$output/harness/$(dirname "$relative")"
  cp "$root/$file" "$output/harness/$relative"
done
cp "$root/components/ax-sandbox/Dockerfile.pi" "$output/Dockerfile"
