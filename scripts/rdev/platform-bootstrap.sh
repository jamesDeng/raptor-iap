#!/usr/bin/env bash
# Run only inside the authenticated rdev Workbench session. Default is preview.
set -euo pipefail
set +x
stage=${1:-preview}
kit=${2:-}
confirmation=${3:-}
case "$stage" in preview|verify|argocd|database|migrations|catalog|cleanup-bootstrap) ;; *) echo 'Unknown stage; stopped' >&2; exit 1 ;; esac
expected_worker='ap-southeast-1.i-t4nj1bitmcz7cuoch82h'
actual_workers=$(kubectl get nodes -o jsonpath='{.items[*].spec.providerID}')
[[ "$actual_workers" == "$expected_worker" ]] || { echo 'Wrong Kubernetes worker identity; stopped' >&2; exit 1; }
kubectl wait --for=condition=Ready node/ap-southeast-1.10.70.1.169 --timeout=30s >/dev/null
if [[ "$stage" == preview || "$stage" == verify ]]; then
  kubectl get nodes
  kubectl get pods -A
  echo 'Private rdev access verified; no changes performed'
  exit 0
fi
[[ "$confirmation" == rdev.ali ]] || { echo 'Explicit rdev.ali confirmation required' >&2; exit 1; }
ensure_namespace() {
  local namespace=$1
  if kubectl get namespace "$namespace" >/dev/null 2>&1; then
    [[ $(kubectl get namespace "$namespace" -o jsonpath='{.metadata.labels.raptor\.env}') == rdev.ali ]] || { echo 'Existing namespace is not bootstrap-owned' >&2; exit 1; }
  else
    kubectl create namespace "$namespace" >/dev/null
    kubectl label namespace "$namespace" raptor.env=rdev.ali raptor.project=raptor-iap >/dev/null
  fi
}
if [[ "$stage" == argocd ]]; then
  ensure_namespace argocd
  manifest=$(mktemp)
  trap 'rm -f "$manifest"' EXIT
  curl -fsSL https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.3/manifests/install.yaml -o "$manifest"
  printf '%s  %s\n' '7efe2d6bbc03f63623640f1e4198f16c84009d510fb810ef71e56df1b7614ba9' "$manifest" | sha256sum -c - >/dev/null
  kubectl apply --server-side -n argocd -f "$manifest" >/dev/null
  kubectl -n argocd rollout status deployment/argocd-server --timeout=300s
  kubectl -n argocd rollout status deployment/argocd-repo-server --timeout=300s
  kubectl -n argocd rollout status statefulset/argocd-application-controller --timeout=300s
  echo 'Argo CD initialized privately; verify remaining controller workloads before acceptance'
  exit 0
fi
[[ -d "$kit" && ! -L "$kit" && $(stat -c '%a' "$kit") == 700 ]] || { echo 'Private kit directory required' >&2; exit 1; }
for file in runtime-secrets.json bootstrap-secrets.json catalog-secrets.json database-job.json migration-jobs.json catalog-job.json; do
  [[ -f "$kit/$file" && ! -L "$kit/$file" && $(stat -c '%a' "$kit/$file") == 600 ]] || { echo 'Private bootstrap files required' >&2; exit 1; }
done
ensure_namespace raptor-system
case "$stage" in
  database)
    kubectl create -f "$kit/runtime-secrets.json" >/dev/null
    kubectl create -f "$kit/bootstrap-secrets.json" >/dev/null
    kubectl create -f "$kit/database-job.json" >/dev/null
    kubectl -n raptor-system wait --for=condition=complete job/platform-db-init --timeout=180s
    ;;
  migrations)
    kubectl create -f "$kit/migration-jobs.json" >/dev/null
    kubectl -n raptor-system wait --for=condition=complete job/raptor-migrate --timeout=180s
    kubectl -n raptor-system wait --for=condition=complete job/gateway-migrate --timeout=180s
    ;;
  catalog)
    kubectl create -f "$kit/catalog-secrets.json" >/dev/null
    kubectl create -f "$kit/catalog-job.json" >/dev/null
    kubectl -n raptor-system wait --for=condition=complete job/platform-catalog-seed --timeout=180s
    kubectl -n raptor-system logs job/platform-catalog-seed
    ;;
  cleanup-bootstrap)
    for job in platform-db-init raptor-migrate gateway-migrate platform-catalog-seed; do
      [[ $(kubectl -n raptor-system get job "$job" -o jsonpath='{.status.succeeded}') == 1 ]] || { echo 'Bootstrap incomplete; credentials retained for reconciliation' >&2; exit 1; }
    done
    kubectl -n raptor-system delete secret platform-db-admin platform-role-sql platform-migrator platform-catalog-sql platform-catalog-db >/dev/null
    echo 'Bootstrap credential Secrets removed; runtime Secrets retained'
    ;;
  *) echo 'Unknown stage; stopped' >&2; exit 1 ;;
esac
