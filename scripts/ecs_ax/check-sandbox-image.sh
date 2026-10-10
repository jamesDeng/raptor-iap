#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 ]]
docker run --rm --entrypoint /bin/sh "$1" -ec '
 test -x /usr/local/bin/ax-task-runner
 test "$(pi --version)" = 0.99.2
 aliyun version
 kubectl version --client
 psql --version
 cd /opt/raptor-harness
 node --test *.test.mjs
'
