#!/usr/bin/env bash
set -euo pipefail

# Validate the intentionally suspended operator template against a REAL kind
# API server. No signing material, work ticket or workload is created.
manifest=deploy/kubernetes/verifiedops-observer/observer-job.example.yaml
kubectl apply --dry-run=server -f deploy/kubernetes/verifiedops-observer/rbac.yaml >/dev/null
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
echo 'PASS: API-server dry-run and suspended least-privilege Observer Job assertions'
