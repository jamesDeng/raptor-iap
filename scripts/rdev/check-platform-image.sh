#!/usr/bin/env bash
# Offline image acceptance: packaging, non-root user and missing-config fail-closed.
set -euo pipefail
image=${1:?image required}
component=${2:?raptor or gateway required}
case "$component" in
  raptor) binaries=(backend frontend open-api admin); required=backend ;;
  gateway) binaries=(gateway sandbox-compat gateway-observer-probe); required=gateway ;;
  *) exit 2 ;;
esac
user=$(docker image inspect --format '{{.Config.User}}' "$image")
[[ "$user" == '10001:10001' ]]
for binary in "${binaries[@]}"; do
  docker run --rm --network none --entrypoint /bin/sh "$image" -c 'test -x "$1"' check "/usr/local/bin/$binary"
done
# Database owners must refuse to run with absent database configuration.
if docker run --rm --network none --entrypoint "/usr/local/bin/$required" "$image" > /dev/null 2>&1; then
  echo 'Database owner unexpectedly started without configuration' >&2
  exit 1
fi
if [[ "$component" == gateway ]]; then
  # Import the complete validator dependency graph without any network access.
  docker run --rm --network none --entrypoint /usr/bin/node "$image" --input-type=module -e 'await import("/opt/raptor-harness/gateway-observation-runner.mjs")'
  # The actual runner and helper must reject unsupported input, without output.
  docker run --rm --network none --entrypoint /bin/sh "$image" -ec '
    if printf "{}" | /usr/bin/node /opt/raptor-harness/gateway-observation-runner.mjs > /tmp/result 2> /tmp/error; then exit 1; fi
    test ! -s /tmp/result; test "$(cat /tmp/error)" = InvalidObservation
    if printf "{}" | /usr/local/bin/gateway-observer-probe > /tmp/result 2> /tmp/error; then exit 1; fi
    test ! -s /tmp/result
  '

  docker run --rm --network none --entrypoint /bin/sh "$image" -ec 'for file in package.json package-lock.json live-runner.mjs live-contract.mjs mcp-runtime.mjs; do test -r "/opt/raptor-harness/$file"; done'
fi
if [[ "$component" == raptor ]]; then
  docker run --rm --network none --entrypoint /bin/sh "$image" -ec 'test "$(/usr/local/bin/node --version)" = v22.23.3; test -r /opt/raptor-model-auth/model-auth-helper.mjs; test -r /opt/raptor-model-auth/node_modules/@earendil-works/pi-coding-agent/package.json'
  if docker run --rm --network none --entrypoint /usr/local/bin/open-api "$image" > /dev/null 2>&1; then
    echo 'Open API unexpectedly started without authentication' >&2
    exit 1
  fi
fi
printf 'Offline image checks passed: %s\n' "$component"
