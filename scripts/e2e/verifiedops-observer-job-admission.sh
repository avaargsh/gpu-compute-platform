#!/usr/bin/env bash
set -euo pipefail

# Validate the intentionally suspended operator template against a REAL kind
# API server. No signing material or work ticket is created; the only persisted
# test Job is suspended and is deleted before this script exits.
manifest=deploy/kubernetes/verifiedops-observer/observer-job.example.yaml
kubectl apply --dry-run=server -f deploy/kubernetes/verifiedops-observer/rbac.yaml >/dev/null
namespace_component="$(kubectl get namespace verifiedops-observer -o jsonpath='{.metadata.labels.app\.kubernetes\.io/component}')"
if [[ "$namespace_component" != "verifiedops-observer" ]]; then
  echo "Observer namespace is missing its required policy-scope label: app.kubernetes.io/component=$namespace_component" >&2
  kubectl get namespace verifiedops-observer --show-labels >&2 || true
  exit 1
fi

# Install the operator-pinned image/key-provider parameters and the API-server
# policy. This is an ephemeral kind fixture; production must replace the
# example digest and key IDs with reviewed values before installing.
kubectl apply -f deploy/kubernetes/verifiedops-observer/observer-policy.example.yaml >/dev/null
kubectl apply -f deploy/kubernetes/verifiedops-observer/observer-job-policy.yaml >/dev/null
# Surface the complete admission matching chain before any negative test.
kubectl get namespace verifiedops-observer --show-labels
kubectl get validatingadmissionpolicybinding verifiedops-observer-job-envelope -o yaml
kubectl get configmap verifiedops-observer-policy -n verifiedops-observer -o yaml
# ValidatingAdmissionPolicy in Kubernetes 1.30-1.34 does not expose an
# Accepted condition for kubectl wait. Its status reports observedGeneration
# and typeChecking warnings; actual CEL evaluation is exercised by server-side
# dry-run requests below. Do not wait on a condition the API server never sets.
kubectl get validatingadmissionpolicy verifiedops-observer-job-envelope -o yaml
policy_generation="$(kubectl get validatingadmissionpolicy verifiedops-observer-job-envelope -o jsonpath='{.metadata.generation}')"
observed_generation="$(kubectl get validatingadmissionpolicy verifiedops-observer-job-envelope -o jsonpath='{.status.observedGeneration}')"
if [[ -z "$policy_generation" || "$observed_generation" != "$policy_generation" ]]; then
  echo "Observer admission policy status is stale (generation=$policy_generation observedGeneration=$observed_generation); refusing to continue acceptance." >&2
  kubectl version -o yaml >&2 || true
  kubectl get validatingadmissionpolicy verifiedops-observer-job-envelope -o yaml >&2 || true
  exit 1
fi
kubectl apply --dry-run=server -f "$manifest" >/dev/null
canary_err="$(mktemp)"
trap 'rm -f "$canary_err"' EXIT

# Verify the API server actually evaluates VAP bindings in this cluster.
# First use a Job-CREATE-scoped deny-all canary to separate VAP engine behavior
# from namespace-selector matching. It only targets batch/v1 Jobs and is removed
# before testing the production policy; the sole test Job remains suspended.
kubectl apply -f - >/dev/null <<'VAP_CANARY'
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: verifiedops-observer-vap-canary
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: ["batch"]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["jobs"]
  validations:
    - expression: "false"
      message: "temporary VAP engine canary must deny"
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: verifiedops-observer-vap-canary
spec:
  policyName: verifiedops-observer-vap-canary
  # Explicitly scope the binding to Job CREATE requests. Do not rely on an
  # omitted matchResources field to exercise the VAP admission path.
  matchResources:
    resourceRules:
      - apiGroups: ["batch"]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["jobs"]
  validationActions: ["Deny"]
VAP_CANARY
# Admission policy/binding updates can take a short time to reach the
# API-server admission path. Retry only this suspended canary CREATE for a
# bounded interval; every unexpectedly admitted Job is immediately deleted.
# A non-admission error is still a hard failure, and timeout never passes.
canary_denied=false
for attempt in $(seq 1 10); do
  if kubectl create -f "$manifest" >/dev/null 2>"$canary_err"; then
    kubectl delete -f "$manifest" --wait=true >/dev/null 2>&1 || true
    sleep 1
    continue
  fi
  if grep -Eiq 'denied|forbidden|validation|temporary VAP engine canary' "$canary_err"; then
    canary_denied=true
    break
  fi
  echo "VAP canary request failed for a reason other than admission denial (attempt $attempt)" >&2
  cat "$canary_err" >&2
  rm -f "$canary_err"
  exit 1
done
if [[ "$canary_denied" != true ]]; then
  echo "VAP canary failed: API server admitted a suspended Job throughout the bounded propagation window" >&2
  kubectl get validatingadmissionpolicy verifiedops-observer-vap-canary -o yaml >&2 || true
  kubectl get validatingadmissionpolicybinding verifiedops-observer-vap-canary -o yaml >&2 || true
  rm -f "$canary_err"
  exit 1
fi
rm -f "$canary_err"
kubectl delete validatingadmissionpolicybinding verifiedops-observer-vap-canary --wait=true
kubectl delete validatingadmissionpolicy verifiedops-observer-vap-canary --wait=true

# Prove the policy rejects a mutable process and an image that differs from
# the operator-pinned digest. Neither negative fixture is persisted.
unsafe="$(mktemp)"
update_manifest="$(mktemp)"
trap 'rm -f "$canary_err" "$unsafe" "$unsafe.err" "$update_manifest"' EXIT
sed '/image: example.invalid\/verifiedops-observer@/a\          command: ["sh"]' "$manifest" > "$unsafe"
if kubectl create --dry-run=server -f "$unsafe" >/dev/null 2>"$unsafe.err"; then
  echo 'Observer Job with command override was admitted' >&2
  cat "$unsafe" >&2
  exit 1
fi
if ! grep -Eiq 'denied|forbidden|validation' "$unsafe.err"; then
  echo 'Observer command-override fixture failed for a reason other than an admission rejection' >&2
  cat "$unsafe.err" >&2
  exit 1
fi
sed 's#example.invalid/verifiedops-observer@sha256:0000000000000000000000000000000000000000000000000000000000000000#attacker.invalid/observer@sha256:1111111111111111111111111111111111111111111111111111111111111111#' "$manifest" > "$unsafe"
if kubectl create --dry-run=server -f "$unsafe" >/dev/null 2>"$unsafe.err"; then
  echo 'Observer Job with unpinned image was admitted' >&2
  cat "$unsafe" >&2
  exit 1
fi
if ! grep -Eiq 'denied|forbidden|validation' "$unsafe.err"; then
  echo 'Observer unpinned-image fixture failed for a reason other than an admission rejection' >&2
  cat "$unsafe.err" >&2
  exit 1
fi

# The only allowed UPDATE is the reviewed suspended Job's suspend=true -> false
# transition. A template mutation in that same update must fail closed.
sed 's/name: verifiedops-observer-once-example/name: verifiedops-observer-policy-update-test/' "$manifest" > "$update_manifest"
# Deleting a VAP and its binding is eventually propagated to admission
# evaluation. Do not let a stale cached canary reject the next legitimate Job
# CREATE; retry only that exact transient denial and fail on every other error.
update_job_created=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if kubectl apply -f "$update_manifest" >/dev/null 2>"$canary_err"; then
    update_job_created=true
    break
  fi
  if ! grep -Fq 'temporary VAP engine canary must deny' "$canary_err"; then
    echo "Observer policy update fixture failed for a reason other than stale canary propagation" >&2
    cat "$canary_err" >&2
    exit 1
  fi
  sleep 1
done
if [[ "$update_job_created" != true ]]; then
  echo "Temporary VAP canary remained active after deletion; refusing to continue policy acceptance" >&2
  cat "$canary_err" >&2
  exit 1
fi
kubectl -n verifiedops-observer patch job verifiedops-observer-policy-update-test --type=merge --patch '{"spec":{"suspend":false}}' --dry-run=server >/dev/null
if kubectl -n verifiedops-observer patch job verifiedops-observer-policy-update-test --type=json --patch='[{"op":"replace","path":"/spec/suspend","value":false},{"op":"replace","path":"/spec/template/spec/containers/0/image","value":"attacker.invalid/observer@sha256:1111111111111111111111111111111111111111111111111111111111111111"}]' --dry-run=server >/dev/null 2>&1; then
  echo 'Observer Job template mutation during unsuspend was admitted' >&2
  exit 1
fi
kubectl -n verifiedops-observer delete job verifiedops-observer-policy-update-test --wait=true >/dev/null

# Client-side projection assertions are defense-in-depth: server dry-run
# alone does not establish runtime identity or signer isolation.
readfield() {
  kubectl create --dry-run=client -f "$manifest" -o "jsonpath=$1"
}
[[ "$(readfield '{.spec.suspend}')" == true ]]
[[ "$(readfield '{.spec.template.spec.serviceAccountName}')" == verifiedops-observer ]]
[[ "$(readfield '{.spec.template.spec.automountServiceAccountToken}')" == false ]]
[[ "$(readfield '{.spec.template.spec.securityContext.runAsNonRoot}')" == true ]]
[[ "$(readfield '{.spec.template.spec.securityContext.seccompProfile.type}')" == RuntimeDefault ]]
[[ "$(readfield '{.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation}')" == false ]]
[[ "$(readfield '{.spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem}')" == true ]]
[[ "$(readfield '{.spec.template.spec.containers[0].securityContext.capabilities.drop[0]}')" == ALL ]]
[[ "$(readfield '{.spec.template.spec.volumes[0].projected.sources[0].serviceAccountToken.expirationSeconds}')" == 600 ]]
[[ "$(readfield '{.spec.template.spec.volumes[3].csi.readOnly}')" == true ]]
[[ "$(readfield '{.spec.backoffLimit}')" == 0 ]]

# The placeholder image digest must not accidentally become a runnable
# release artifact. The suspended template must have no actual Job created.
image="$(readfield '{.spec.template.spec.containers[0].image}')"
[[ "$image" == 'example.invalid/verifiedops-observer@sha256:0000000000000000000000000000000000000000000000000000000000000000' ]]
if kubectl -n verifiedops-observer get job verifiedops-observer-once-example >/dev/null 2>&1; then
  echo 'Observer example was created by a dry-run acceptance test' >&2
  exit 1
fi
echo 'PASS: API-server dry-run and suspended least-privilege Observer Job assertions'
