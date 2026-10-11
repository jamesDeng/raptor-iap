#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 ]]
root=$(cd "$(dirname "$0")/../.." && pwd)
docker run --rm --volume "$root/scripts/contracts:/scripts/contracts:ro" --entrypoint /bin/sh "$1" -ec '
 test -x /usr/local/bin/ax-task-runner
 test "$(pi --version)" = 0.99.2
 aliyun version
 kubectl version --client
 psql --version
 cd /opt/raptor-harness
 node --test *.test.mjs
'
