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

# Install the operator-pinned image/key-provider parameters. This is an
# ephemeral kind fixture; production must replace the example digest and key
# IDs with reviewed values before installing.
kubectl apply -f deploy/kubernetes/verifiedops-observer/observer-policy.example.yaml >/dev/null
canary_err="$(mktemp)"
canary_manifest="$(mktemp)"
unsafe="$(mktemp)"
update_manifest="$(mktemp)"
mutated="$(mktemp)"
pod_base="$(mktemp)"
pod_negative="$(mktemp)"
one_shot="$(mktemp)"
one_shot_err="$(mktemp)"
# Single EXIT trap: remove every temporary created by this script. Reassigning
# the trap later silently dropped earlier entries, so declare the full list once.
namespace_component_removed=false
# Restore the operator label even if a negative admission probe fails midway.
trap 'if [[ "$namespace_component_removed" == true ]]; then kubectl label namespace verifiedops-observer app.kubernetes.io/component=verifiedops-observer --overwrite >/dev/null || true; fi; rm -f "$canary_err" "$canary_manifest" "$unsafe" "$unsafe.err" "$update_manifest" "$mutated" "$pod_base" "$pod_negative" "$pod_negative.err" "$one_shot" "$one_shot_err"' EXIT

# A one-shot probe is only proven by a named Observer Job admission policy
# denying the request with its own one-shot message. The API server reports
# the first matching Observer Job policy that denies a request, so either
# installed template is accepted, but only together with that template's own
# identity and message. An unrelated API, schema or kubectl failure is never
# accepted as proof.
envelope_denial="ValidatingAdmissionPolicy 'verifiedops-observer-job-envelope' with binding 'verifiedops-observer-job-envelope' denied request: Observer Jobs must declare a one-shot configuration with completions=1, parallelism=1, NonIndexed completion mode, no manual selector, and a Never restart policy"
guard_denial="ValidatingAdmissionPolicy 'verifiedops-observer-job-guard' with binding 'verifiedops-observer-job-guard' denied request: VerifiedOps Job must be single-execution: completions=1, parallelism=1, NonIndexed completion mode, no manual selector, and a Never restart policy"
one_shot_denial_re="$envelope_denial|$guard_denial"

# assert_one_shot_denied requires the named denial above. It retries while the
# request is still admitted, because API-server admission configuration is
# propagated asynchronously after a policy or binding is created.
assert_one_shot_denied() {
  local label="$1" file="$2" pattern="$3"
  local attempt denied=false
  for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if kubectl create --dry-run=server -f "$file" >/dev/null 2>"$one_shot_err"; then
      sleep 1
      continue
    fi
    if ! grep -Eq "$pattern" "$one_shot_err"; then
      echo "Observer Job $label was rejected, but not by the expected admission policy message" >&2
      cat "$one_shot_err" >&2
      exit 1
    fi
    denied=true
    break
  done
  if [[ "$denied" != true ]]; then
    echo "Admission policy accepted an Observer Job with $label" >&2
    cat "$file" >&2
    exit 1
  fi
}

# Install the shipped example template first and prove the one-shot contract
# on its own: an operator who installs only this template must still be denied
# a multi-completion Observer Job. The operator policy is therefore removed
# for this window and re-installed below, so the proof does not depend on the
# cluster starting without it.
policy=deploy/kubernetes/verifiedops-observer/observer-admission-policy.example.yaml
kubectl apply -f "$policy" >/dev/null
kubectl delete validatingadmissionpolicybinding verifiedops-observer-job-envelope --ignore-not-found --wait=true >/dev/null
kubectl delete validatingadmissionpolicy verifiedops-observer-job-envelope --ignore-not-found --wait=true >/dev/null
# While only the shipped example policy is installed, exercise every
# one-shot conjunct against the real API server. A stale operator-policy cache
# is retried only when it returns that policy's exact denial message; all other
# rejections must be attributable to the shipped guard policy.
assert_guard_only_denied() {
  local label="$1" file="$2"
  local attempt denied=false
  for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if kubectl create --dry-run=server -f "$file" >/dev/null 2>"$one_shot_err"; then
      sleep 1
      continue
    fi
    if grep -Fq "$envelope_denial" "$one_shot_err"; then
      sleep 1
      continue
    fi
    if ! grep -Fq "$guard_denial" "$one_shot_err"; then
      echo "Observer Job $label was rejected, but not by the shipped example template policy message" >&2
      cat "$one_shot_err" >&2
      exit 1
    fi
    denied=true
    break
  done
  if [[ "$denied" != true ]]; then
    echo "The shipped example template did not deny Observer Job $label" >&2
    cat "$one_shot_err" >&2
    exit 1
  fi
}

sed 's/^  completions: 1$/  completions: 3/' "$manifest" > "$one_shot"
assert_guard_only_denied 'completions=3' "$one_shot"

sed 's/^  parallelism: 1$/  parallelism: 3/' "$manifest" > "$one_shot"
assert_guard_only_denied 'parallelism=3' "$one_shot"

sed 's/^  completionMode: NonIndexed$/  completionMode: Indexed/' "$manifest" > "$one_shot"
assert_guard_only_denied 'completionMode=Indexed' "$one_shot"

sed 's/^      restartPolicy: Never$/      restartPolicy: OnFailure/' "$manifest" > "$one_shot"
assert_guard_only_denied 'restartPolicy=OnFailure' "$one_shot"

awk '
  /^  completions: 1$/ && !inserted {
    print "  manualSelector: true"
    print "  selector:"
    print "    matchLabels:"
    print "      app.kubernetes.io/component: verifiedops-observer"
    inserted=1
  }
  { print }
' "$manifest" > "$one_shot"
assert_guard_only_denied 'manualSelector=true with a required matching selector' "$one_shot"

# Install the API-server policy that pins the operator-reviewed parameters.
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
# Allow the additional Job and Pod guards to propagate before probing.
sleep 2
kubectl apply --dry-run=server -f "$manifest" >/dev/null

# Verify the API server actually evaluates VAP bindings in this cluster.
# First use an intentionally unscoped canary to separate VAP engine behavior
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
  validationActions: ["Deny"]
VAP_CANARY
# Use real CREATE requests rather than dry-run. API-server admission
# configuration is propagated asynchronously after a policy/binding is created;
# retry with distinct suspended Job names so an initial admitted request cannot
# be mistaken for steady-state enforcement. Any admitted fixture is deleted
# immediately and never unsuspended.
canary_denied=false
for attempt in 1 2 3 4 5; do
  sed "s/name: verifiedops-observer-once-example/name: verifiedops-observer-vap-canary-$attempt/" "$manifest" > "$canary_manifest"
  if kubectl create -f "$canary_manifest" >/dev/null 2>"$canary_err"; then
    kubectl delete -f "$canary_manifest" --wait=true >/dev/null 2>&1 || true
    sleep 1
    continue
  fi
  if ! grep -Eiq 'denied|forbidden|validation|temporary VAP engine canary' "$canary_err"; then
    echo "VAP canary request failed for a reason other than admission denial" >&2
    cat "$canary_err" >&2
    exit 1
  fi
  canary_denied=true
  break
done
if [[ "$canary_denied" != true ]]; then
  echo "VAP canary failed: API server repeatedly admitted suspended Jobs despite an unscoped deny-all policy" >&2
  kubectl get validatingadmissionpolicy verifiedops-observer-vap-canary -o yaml >&2 || true
  kubectl get validatingadmissionpolicybinding verifiedops-observer-vap-canary -o yaml >&2 || true
  exit 1
fi
kubectl delete validatingadmissionpolicybinding verifiedops-observer-vap-canary --wait=true
kubectl delete validatingadmissionpolicy verifiedops-observer-vap-canary --wait=true

# Deleting the canary object is asynchronous with respect to API-server
# admission caches. Wait until a valid reviewed Job is no longer rejected by
# the removed canary before exercising the production policy's update path.
canary_cleared=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if kubectl apply --dry-run=server -f "$manifest" >/dev/null 2>"$canary_err"; then
    canary_cleared=true
    break
  fi
  if ! grep -Fq 'temporary VAP engine canary must deny' "$canary_err"; then
    echo "Reviewed Observer Job failed after canary cleanup for a reason other than stale canary admission state" >&2
    cat "$canary_err" >&2
    exit 1
  fi
  sleep 1
done
if [[ "$canary_cleared" != true ]]; then
  echo "VAP canary removal did not propagate to API-server admission within 10 attempts" >&2
  cat "$canary_err" >&2
  exit 1
fi

# Prove the policy rejects a mutable process and an image that differs from
# the operator-pinned digest. Neither negative fixture is persisted.
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

# Regression: the business app label is mutable, so removing it must NOT
# switch off the API-server admission policy for this dedicated namespace.
# The immutable Kubernetes-managed namespace-name label remains the scope.
namespace_component_removed=true
kubectl label namespace verifiedops-observer app.kubernetes.io/component- >/dev/null
namespace_name="$(kubectl get namespace verifiedops-observer -o jsonpath='{.metadata.labels.kubernetes\.io/metadata\.name}')"
if [[ "$namespace_name" != "verifiedops-observer" ]]; then
  echo "Observer namespace has no Kubernetes-managed name label: $namespace_name" >&2
  exit 1
fi
# Verify the mutable selector label really is absent before probing the
# admission cache. Namespace writes are synchronous, but API-server admission
# matching can briefly retain the old binding selector after a label change.
namespace_component="$(kubectl get namespace verifiedops-observer -o jsonpath='{.metadata.labels.app\\.kubernetes\\.io/component}')"
if [[ -n "$namespace_component" ]]; then
  echo "Observer namespace app label was not removed: $namespace_component" >&2
  exit 1
fi

# The refusal must come from the operator policy scoped by Kubernetes' immutable
# namespace-name label. While admission caches converge, the example policy's
# mutable-label binding may still reject this same malicious Job; retry only
# that exact expected stale denial. Successful admission and every unrelated
# rejection remain hard failures rather than being mistaken for scope evidence.
scope_denied=false
for attempt in $(seq 1 60); do
  if kubectl create --dry-run=server -f "$unsafe" >/dev/null 2>"$unsafe.err"; then
    echo 'Observer Job command override admitted after removing mutable namespace app label' >&2
    exit 1
  fi
  if grep -Fq "ValidatingAdmissionPolicy 'verifiedops-observer-job-envelope' with binding 'verifiedops-observer-job-envelope'" "$unsafe.err"; then
    scope_denied=true
    break
  fi
  if grep -Fq "ValidatingAdmissionPolicy 'verifiedops-observer-job-guard' with binding 'verifiedops-observer-job-guard' denied request: VerifiedOps Job must have exactly one pinned, non-overridable Observer process" "$unsafe.err"; then
    sleep 1
    continue
  fi
  echo 'Observer scope probe was rejected for an unexpected reason' >&2
  cat "$unsafe.err" >&2
  exit 1
done
if [[ "$scope_denied" != true ]]; then
  echo 'Observer admission scope was not proven after 60s of policy-cache convergence' >&2
  cat "$unsafe.err" >&2
  exit 1
fi
kubectl label namespace verifiedops-observer app.kubernetes.io/component=verifiedops-observer --overwrite >/dev/null
namespace_component_removed=false
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

# The one-shot contract of an Observer Job: exactly one completion, one
# parallel Pod, the NonIndexed completion mode, no caller-owned selector and
# a Never restart policy. backoffLimit=0 alone does not provide that, because
# completions/parallelism decide how many Pods run, completionMode decides
# whether the Job fans out, manualSelector lets the caller own the selector
# instead of the controller, and restartPolicy=OnFailure lets the kubelet
# re-run the Observer process in place.
#
# Positive baselines first: the reviewed example must stay admitted both when
# it states the one-shot shape explicitly and when it relies on API
# defaulting for completions, parallelism and completionMode.
grep -v -E '^  (completions|parallelism|completionMode):' "$manifest" > "$one_shot"
if ! kubectl create --dry-run=server -f "$one_shot" >/dev/null 2>"$one_shot_err"; then
  echo 'Admission policy rejected the reviewed Observer Job that relies on API defaulting for the one-shot fields' >&2
  cat "$one_shot_err" >&2
  exit 1
fi
if ! kubectl create --dry-run=server -f "$manifest" >/dev/null 2>"$one_shot_err"; then
  echo 'Admission policy rejected the reviewed Observer Job that states the one-shot shape explicitly' >&2
  cat "$one_shot_err" >&2
  exit 1
fi

# Each negative probe changes exactly one field of the reviewed example, so
# the denial is attributable to that field and to the one-shot clause only.
sed 's/^  completions: 1$/  completions: 3/' "$manifest" > "$one_shot"
assert_one_shot_denied 'completions=3' "$one_shot" "$one_shot_denial_re"

sed 's/^  parallelism: 1$/  parallelism: 3/' "$manifest" > "$one_shot"
assert_one_shot_denied 'parallelism=3' "$one_shot" "$one_shot_denial_re"

# Indexed completion mode is API-valid on its own: the API server defaults
# completions and parallelism to 1 before validating it.
sed 's/^  completionMode: NonIndexed$/  completionMode: Indexed/' "$manifest" > "$one_shot"
assert_one_shot_denied 'completionMode=Indexed' "$one_shot" "$one_shot_denial_re"

sed 's/^      restartPolicy: Never$/      restartPolicy: OnFailure/' "$manifest" > "$one_shot"
assert_one_shot_denied 'restartPolicy=OnFailure' "$one_shot" "$one_shot_denial_re"

# manualSelector cannot be probed as a single field: once the caller owns the
# selector, the API schema requires an explicit spec.selector that matches
# the Pod template labels, otherwise the request is rejected by schema
# validation before admission ever runs. This probe is therefore a documented
# TWO-field change (manualSelector plus the required selector) and it proves
# the one-shot clause rejects a caller-owned selector; it does not isolate the
# manualSelector clause on its own.
awk '
  /^  completions: 1$/ && !inserted {
    print "  manualSelector: true"
    print "  selector:"
    print "    matchLabels:"
    print "      app.kubernetes.io/component: verifiedops-observer"
    inserted=1
  }
  { print }
' "$manifest" > "$one_shot"
assert_one_shot_denied 'manualSelector=true with a required matching selector' "$one_shot" "$one_shot_denial_re"

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
[[ "$(readfield '{.spec.completions}')" == 1 ]]
[[ "$(readfield '{.spec.parallelism}')" == 1 ]]
[[ "$(readfield '{.spec.completionMode}')" == NonIndexed ]]
[[ "$(readfield '{.spec.template.spec.restartPolicy}')" == Never ]]

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

# Init containers execute arbitrary code before the pinned Observer process
# and could read the signer/ticket volumes. Keep the Job otherwise unchanged.
awk '
  /^      containers:$/ && !inserted {
    print "      initContainers:"
    print "        - name: injected-init"
    print "          image: busybox:1.36"
    print "          command: [\"sh\", \"-c\", \"id\"]"
    inserted=1
  }
  { print }
' "$manifest" > "$mutated"
if kubectl apply --dry-run=server -f "$mutated" >/dev/null 2>&1; then
  echo 'Admission policy accepted an injected Observer init container' >&2
  exit 1
fi

# A user must not bypass the Job policy by directly creating a Pod that selects
# the signer-bearing ServiceAccount and overrides the process, host namespace,
# privilege or capability envelope. The probe base is the reviewed Job's Pod
# template projected verbatim to a direct Pod (same identity, security context,
# env, volumes and mounts; only restartPolicy is Pod-native and already Never),
# so a single-field mutation isolates exactly one policy constraint (the
# privileged probe below is the documented exception).
cat <<'POD' > "$pod_base"
apiVersion: v1
kind: Pod
metadata:
  name: verifiedops-observer-direct-pod-probe
  namespace: verifiedops-observer
spec:
  serviceAccountName: verifiedops-observer
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: observer
      image: example.invalid/verifiedops-observer@sha256:0000000000000000000000000000000000000000000000000000000000000000
      env:
        - name: VERIFIEDOPS_TICKET_FILE
          value: /run/verifiedops/ticket/ticket.dsse.json
        - name: VERIFIEDOPS_DISPATCHER_PUBKEY_FILE
          value: /run/verifiedops/root/dispatcher.pub.b64
        - name: VERIFIEDOPS_DISPATCHER_KEY_ID
          value: operator-provisioned-dispatcher-key-id
        - name: VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE
          value: /run/verifiedops/identity/observer.pk8.pem
        - name: VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID
          value: operator-provisioned-observer-key-id
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      volumeMounts:
        - name: kubernetes-api-access
          mountPath: /var/run/secrets/kubernetes.io/serviceaccount
          readOnly: true
        - name: signed-work-ticket
          mountPath: /run/verifiedops/ticket
          readOnly: true
        - name: pinned-dispatcher-root
          mountPath: /run/verifiedops/root
          readOnly: true
        - name: external-signer
          mountPath: /run/verifiedops/identity
          readOnly: true
  volumes:
    - name: kubernetes-api-access
      projected:
        defaultMode: 0444
        sources:
          - serviceAccountToken:
              path: token
              expirationSeconds: 600
          - configMap:
              name: kube-root-ca.crt
              items:
                - key: ca.crt
                  path: ca.crt
          - downwardAPI:
              items:
                - path: namespace
                  fieldRef:
                    fieldPath: metadata.namespace
    - name: signed-work-ticket
      secret:
        secretName: verifiedops-observer-once-ticket
        optional: false
    - name: pinned-dispatcher-root
      configMap:
        name: verifiedops-observer-dispatcher-pinned-root
        optional: false
    - name: external-signer
      csi:
        driver: secrets-store.csi.k8s.io
        readOnly: true
        volumeAttributes:
          secretProviderClass: verifiedops-observer-external-signing-key
POD

# Baseline: the unmutated projected Pod must be admitted by server dry-run, or
# every downstream single-field denial is unattributable to that field.
if ! kubectl apply --dry-run=server -f "$pod_base" >/dev/null 2>"$unsafe.err"; then
  echo 'Baseline projected direct Observer Pod was rejected before any mutation' >&2
  cat "$unsafe.err" >&2
  exit 1
fi

# Each negative asserts the policy's own denial message (not merely any kubectl
# failure) so an unrelated error can never be mistaken for the intended
# constraint. The host-flag and capabilities.add negatives mutate exactly one
# projection field; the privileged negative is a documented two-field change
# (see below) and is labeled as such.
assert_pod_denied() {
  local label="$1" expected="$2"
  if kubectl apply --dry-run=server -f "$pod_negative" >/dev/null 2>"$pod_negative.err"; then
    echo "Admission policy accepted a direct Observer Pod with $label" >&2
    cat "$pod_negative" >&2
    exit 1
  fi
  if ! grep -Fq "$expected" "$pod_negative.err"; then
    echo "Direct Observer Pod $label was not denied by the expected policy message" >&2
    cat "$pod_negative.err" >&2
    exit 1
  fi
}
pod_sc_message='VerifiedOps Pods must use the dedicated identity, bounded token and reviewed Pod security context, without init or ephemeral containers'
pod_container_message='Direct Observer Pods must run only the pinned, non-overridable Observer process'

# Each host-namespace flag is a distinct Pod-level field; mutate exactly one.
for host_field in hostNetwork hostPID hostIPC; do
  sed "s/^  restartPolicy: Never$/  restartPolicy: Never\\n  ${host_field}: true/" "$pod_base" > "$pod_negative"
  assert_pod_denied "${host_field}: true" "$pod_sc_message"
done

# Kubernetes schema itself rejects privileged:true together with
# allowPrivilegeEscalation:false, so the realistic privileged bypass replaces
# the (policy-mandated) privilege-escalation guard. This is therefore a
# TWO-field change (privileged added, allowPrivilegeEscalation removed), so it
# proves the container validation as a whole rejects a privileged Pod; it does
# NOT isolate the newly added privileged clause. The decisive clause here is the
# pre-existing allowPrivilegeEscalation == false. The new privileged clause is
# deliberately redundant defense-in-depth: no Pod can satisfy
# allowPrivilegeEscalation == false together with privileged: true without first
# failing API schema validation, so the privileged clause is unreachable as the
# decisive deny.
sed 's/^        allowPrivilegeEscalation: false$/        privileged: true/' "$pod_base" > "$pod_negative"
assert_pod_denied 'privileged: true (combined container validation)' "$pod_container_message"

sed 's/^          drop: \["ALL"\]$/          drop: ["ALL"]\n          add: ["NET_RAW"]/' "$pod_base" > "$pod_negative"
assert_pod_denied 'capabilities add override' "$pod_container_message"

# The original minimal bypass probe must also remain denied.
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
