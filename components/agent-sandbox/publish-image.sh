#!/usr/bin/env bash
set +x
set -euo pipefail
umask 077

[[ $# == 1 && ${GITHUB_SHA:-} =~ ^[0-9a-f]{40}$ ]]
: "${GITHUB_ACTOR:?}" "${GH_TOKEN:?}" "${GITHUB_STEP_SUMMARY:?}"
image_dir="$1"
repository='jamesdeng/raptor-iap-pi'
tag="sha-$GITHUB_SHA"
reference="ghcr.io/$repository:$tag"
expected="$(skopeo manifest-digest "$image_dir/manifest.json")"
[[ "$expected" == "$(cat "$image_dir/digest.txt")" ]]
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Keep authentication material in temporary files, never command output.
printf 'user = "%s:%s"\n' "$GITHUB_ACTOR" "$GH_TOKEN" > "$work/auth"
curl --silent --show-error --fail --config "$work/auth" \
  --get --data-urlencode 'service=ghcr.io' \
  --data-urlencode "scope=repository:$repository:pull,push" \
  https://ghcr.io/token > "$work/token.json"
bearer="$(jq -er '.token // .access_token' "$work/token.json")"
printf 'header = "Authorization: Bearer %s"\n' "$bearer" > "$work/bearer"
unset bearer GH_TOKEN
status="$(curl --silent --show-error --config "$work/bearer" \
  --header 'Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' \
  --output "$work/manifest" --write-out '%{http_code}' \
  "https://ghcr.io/v2/$repository/manifests/$tag")"
case "$status" in
  200)
    actual="$(skopeo manifest-digest "$work/manifest")"
    [[ "$actual" == "$expected" ]] || {
      echo "Source tag already exists with a different digest; refusing overwrite." >&2
      exit 1
    }
    result='Reused existing image'
    ;;
  404)
    jq -e '.errors | length > 0 and all(.[]; .code == "MANIFEST_UNKNOWN" or .code == "NAME_UNKNOWN")' "$work/manifest" >/dev/null || {
      echo 'Registry returned an unrecognized 404; publication stopped.' >&2
      exit 1
    }
    skopeo copy --preserve-digests --authfile "$HOME/.docker/config.json" \
      "dir:$image_dir" "docker://$reference"
    result='Published tested image'
    ;;
  *)
    echo "Registry lookup failed (HTTP $status); publication stopped." >&2
    exit 1
    ;;
esac
# Confirm registry identity after either route. No rebuild occurs here.
skopeo inspect --raw --authfile "$HOME/.docker/config.json" "docker://$reference" > "$work/published"
[[ "$(skopeo manifest-digest "$work/published")" == "$expected" ]]
# shellcheck disable=SC2016 # Backticks are literal Markdown formatting.
printf '### Pi image release\n\n%s\n\n- Source: `%s`\n- Image: `%s`\n- Digest: `%s`\n' \
  "$result" "$GITHUB_SHA" "$reference" "$expected" >> "$GITHUB_STEP_SUMMARY"
