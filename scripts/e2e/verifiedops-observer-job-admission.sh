#!/usr/bin/env bash
set -euo pipefail

# Validate the intentionally suspended operator template against a REAL kind
# API server. No signing material or work ticket is created; the only persisted
# test Job is suspended and is deleted before this script exits.
manifest=deploy/kubernetes/verifiedops-observer/observer-job.example.yaml
kubectl apply --dry-run=server -f deploy/kubernetes/verifiedops-observer/rbac.yaml >/dev/null

# Install the operator-pinned image/key-provider parameters and the API-server
# policy. This is an ephemeral kind fixture; production must replace the
# example digest and key IDs with reviewed values before installing.
kubectl apply -f deploy/kubernetes/verifiedops-observer/observer-policy.example.yaml >/dev/null
kubectl apply -f deploy/kubernetes/verifiedops-observer/observer-job-policy.yaml >/dev/null
kubectl wait --for=condition=Accepted validatingadmissionpolicy/verifiedops-observer-job-envelope --timeout=30s >/dev/null
kubectl apply --dry-run=server -f "$manifest" >/dev/null

# Prove the policy rejects a mutable process and an image that differs from
# the operator-pinned digest. Neither negative fixture is persisted.
unsafe="$(mktemp)"
update_manifest="$(mktemp)"
trap 'rm -f "$unsafe" "$update_manifest"' EXIT
sed '/image: example.invalid\/verifiedops-observer@/a\          command: ["sh"]' "$manifest" > "$unsafe"
if kubectl apply --dry-run=server -f "$unsafe" >/dev/null 2>&1; then
  echo 'Observer Job with command override was admitted' >&2
  exit 1
fi
sed 's#example.invalid/verifiedops-observer@sha256:0000000000000000000000000000000000000000000000000000000000000000#attacker.invalid/observer@sha256:1111111111111111111111111111111111111111111111111111111111111111#' "$manifest" > "$unsafe"
if kubectl apply --dry-run=server -f "$unsafe" >/dev/null 2>&1; then
  echo 'Observer Job with unpinned image was admitted' >&2
  exit 1
fi

# The only allowed UPDATE is the reviewed suspended Job's suspend=true -> false
# transition. A template mutation in that same update must fail closed.
sed 's/name: verifiedops-observer-once-example/name: verifiedops-observer-policy-update-test/' "$manifest" > "$update_manifest"
kubectl apply -f "$update_manifest" >/dev/null
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
