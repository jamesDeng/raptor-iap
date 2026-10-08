#!/usr/bin/env bash
# Verify the runtime bridge against the exact reviewed upstream pair.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
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
mkdir -p cmd/raptor-bridge
cp "$root"/components/ax-runtime-bridge/*.go cmd/raptor-bridge/
cp "$root"/components/ax-runtime-bridge/substrate/*.go internal/substrate/
go mod edit -replace=github.com/agent-substrate/substrate=../substrate
go mod tidy
go test ./...
go vet ./cmd/raptor-bridge
