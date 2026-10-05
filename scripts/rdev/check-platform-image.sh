#!/usr/bin/env bash
# Offline image acceptance: packaging, non-root user and missing-config fail-closed.
set -euo pipefail
image=${1:?image required}
component=${2:?raptor or gateway required}
case "$component" in
  raptor) binaries=(backend frontend open-api admin); required=backend ;;
  gateway) binaries=(gateway); required=gateway ;;
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
if [[ "$component" == raptor ]]; then
  if docker run --rm --network none --entrypoint /usr/local/bin/open-api "$image" > /dev/null 2>&1; then
    echo 'Open API unexpectedly started without authentication' >&2
    exit 1
  fi
fi
printf 'Offline image checks passed: %s\n' "$component"
