#!/usr/bin/env bash
# Real kind API-server RBAC authorization negative suite. Impersonation is
# performed only by CI's local kind admin to evaluate the separate SA grants.
# This does not mint observer identity tokens or simulate a signed result.
set -euo pipefail
source_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
identity="system:serviceaccount:verifiedops-observer:verifiedops-observer"
project_a="verifiedops-rbac-a"
project_b="verifiedops-rbac-b"

kubectl apply -f "$source_root/deploy/kubernetes/verifiedops-observer/rbac.yaml"
kubectl create namespace "$project_a" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "$project_b" --dry-run=client -o yaml | kubectl apply -f -
bash "$source_root/scripts/e2e/verifiedops-observer-project-rbac.sh" "$project_a"
# Apply twice to test idempotence of the reviewed per-project grant.
bash "$source_root/scripts/e2e/verifiedops-observer-project-rbac.sh" "$project_a"

expect_allowed() {
  local actual
  actual="$(kubectl auth can-i --as="$identity" "$@" 2>&1)" || {
    echo "expected allowed: $*; got $actual" >&2
    return 1
  }
  [[ "$actual" == "yes" ]] || { echo "unexpected RBAC response for $*: $actual" >&2; return 1; }
}
expect_denied() {
  local actual
  actual="$(kubectl auth can-i --as="$identity" "$@" 2>&1)" || {
    [[ "$actual" == "no" ]] && return 0
    echo "RBAC check failed (not a denial): $* => $actual" >&2
    return 1
  }
  [[ "$actual" == "no" ]] || { echo "privilege escalation: $* => $actual" >&2; return 1; }
}

expect_allowed get jobs --namespace "$project_a"
expect_allowed get pods --namespace "$project_a"
expect_allowed list pods --namespace "$project_a"
expect_allowed get nodes
expect_denied list nodes
expect_denied watch nodes
expect_denied list jobs --namespace "$project_a"
expect_denied watch pods --namespace "$project_a"
expect_denied create pods --namespace "$project_a"
expect_denied patch jobs --namespace "$project_a"
expect_denied update jobs --namespace "$project_a"
expect_denied delete jobs --namespace "$project_a"
expect_denied create jobs --namespace "$project_a"
expect_denied get pods/log --namespace "$project_a"
expect_denied create pods/exec --namespace "$project_a"
expect_denied get secrets --namespace "$project_a"
expect_denied list secrets --namespace "$project_a"
expect_denied get configmaps --namespace "$project_a"
expect_denied get workloads.kueue.x-k8s.io --namespace "$project_a"
expect_denied get jobs --namespace "$project_b"
expect_denied list pods --namespace "$project_b"
expect_denied get jobs --namespace kube-system
expect_denied create rolebindings.rbac.authorization.k8s.io --namespace "$project_a"
expect_denied bind clusterroles.rbac.authorization.k8s.io
expect_denied impersonate users
echo "VERIFIEDOPS OBSERVER RBAC PASS: dedicated SA limited to Job/Pod reads in $project_a and get Node"
