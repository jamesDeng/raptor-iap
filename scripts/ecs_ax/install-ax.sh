#!/usr/bin/env bash
set -euo pipefail
[[ "$(hostname)" == raptor-ecs-ax ]] || exit 1
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
# The counter gate passed; AX receives its own worker namespace.
worker_image=$(kubectl get workerpool counter -n ate-demo-counter -o jsonpath='{.spec.workerImage}')
version=$(kubectl get node raptor-ecs-ax -o jsonpath='{.metadata.labels.ate\.dev/substrate-version}')
cat <<YAML | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ax-workers
---
apiVersion: ate.dev/v1alpha1
kind: WorkerPool
metadata:
  name: ax
  namespace: ax-workers
  labels:
    workload: ax
spec:
  replicas: 1
  workerImage: ${worker_image}
  template:
    nodeSelector:
      ate.dev/substrate-version: "${version}"
    resources:
      limits:
        cpu: "2"
        memory: 2Gi
      requests:
        cpu: 250m
        memory: 2Gi
YAML
kubectl apply -f /var/lib/raptor-ecs-ax/downloads/redis.yaml
kubectl rollout status statefulset/ax-redis -n ax-system --timeout=180s
kubectl apply -f /var/lib/raptor-ecs-ax/downloads/ax-server.yaml
kubectl rollout status deployment/ax-server -n ax-system --timeout=180s
kubectl get pods -n ax-system
