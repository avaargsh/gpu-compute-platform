#!/usr/bin/env bash
set -euo pipefail

# Validate the intentionally suspended operator template against a REAL kind
# API server. No signing material, work ticket or workload is created.
manifest=deploy/kubernetes/verifiedops-observer/observer-job.example.yaml
kubectl apply --dry-run=server -f deploy/kubernetes/verifiedops-observer/rbac.yaml >/dev/null
policy=deploy/kubernetes/verifiedops-observer/observer-admission-policy.example.yaml
kubectl apply -f "$policy" >/dev/null
# Give the API server a moment to publish the newly bound admission policies.
sleep 2
kubectl apply --dry-run=server -f "$manifest" >/dev/null

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

# Prove the API server rejects an unsafe Job override (not merely that the
# manifest looks correct client-side).
mutated="$(mktemp)"
trap 'rm -f "$mutated"' EXIT
sed 's/allowPrivilegeEscalation: false/allowPrivilegeEscalation: true/' "$manifest" > "$mutated"
if kubectl apply --dry-run=server -f "$mutated" >/dev/null 2>&1; then
  echo 'Admission policy accepted an Observer Job with privilege escalation enabled' >&2
  exit 1
fi

# Keep the environment list length unchanged while replacing one required key
# with a duplicate of another. This proves name uniqueness is enforced rather
# than relying only on the exact list length.
sed 's/VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID/VERIFIEDOPS_DISPATCHER_KEY_ID/' "$manifest" > "$mutated"
if kubectl apply --dry-run=server -f "$mutated" >/dev/null 2>&1; then
  echo 'Admission policy accepted duplicate Observer environment variable names' >&2
  exit 1
fi

# A user must not bypass the Job policy by directly creating a Pod that selects
# the signer-bearing ServiceAccount and overrides the process.
if cat <<'POD' | kubectl apply --dry-run=server -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: observer-policy-bypass-probe
  namespace: verifiedops-observer
spec:
  serviceAccountName: verifiedops-observer
  automountServiceAccountToken: false
  restartPolicy: Never
  containers:
    - name: observer
      image: example.invalid/verifiedops-observer@sha256:0000000000000000000000000000000000000000000000000000000000000000
      command: ["sh", "-c", "id"]
POD
then
  echo 'Admission policy accepted a direct Pod with an overridden process' >&2
  exit 1
fi

echo 'PASS: API-server admission rejects unsafe Job overrides and direct-Pod bypasses'
