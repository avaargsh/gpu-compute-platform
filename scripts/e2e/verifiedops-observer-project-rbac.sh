#!/usr/bin/env bash
# Grant the already-installed *dedicated* Observer identity read access to
# Jobs and Pods in exactly one reviewed project namespace. Never use a
# ClusterRoleBinding for project workload data.
set -euo pipefail

namespace="${1:-}"
if [[ ! "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || (( ${#namespace} > 63 )); then
  echo "usage: $0 <existing-project-namespace> (DNS-1123 label)" >&2
  exit 2
fi
case "$namespace" in
  default|kube-system|kube-public|kube-node-lease|verifiedops-observer)
    echo "refusing non-project or Observer namespace: $namespace" >&2
    exit 2
    ;;
esac

kubectl get namespace "$namespace" >/dev/null
kubectl get serviceaccount verifiedops-observer -n verifiedops-observer >/dev/null

# This Role is strictly namespace-scoped. The Observer's in-cluster client
# requires GET job, LIST pods and GET pod. No Job/Pod mutation, exec/log,
# watches, secrets, namespace listing or Kubernetes workloads are granted.
kubectl apply -n "$namespace" -f - <<YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: verifiedops-observer-job-pod-reader
  namespace: $namespace
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: verifiedops-observer-job-pod-reader
  namespace: $namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: verifiedops-observer-job-pod-reader
subjects:
  - kind: ServiceAccount
    name: verifiedops-observer
    namespace: verifiedops-observer
YAML
