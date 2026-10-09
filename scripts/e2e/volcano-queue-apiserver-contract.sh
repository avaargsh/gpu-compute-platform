#!/usr/bin/env bash
# CPU-only, live Kubernetes API-server falsification for the unregistered Queue CAS.
# Does NOT prove Volcano scheduler quota application or real GPU execution.
set -euo pipefail

die() { printf 'VOLCANO API CONTRACT: BLOCKED: %s\n' "$*" >&2; exit 2; }
for tool in git kubectl kind jq sha256sum; do
  command -v "$tool" >/dev/null || die "missing $tool"
done

context="${STAGE_B_KIND_CONTEXT:-}"
[[ "$context" == kind-* ]] || die "STAGE_B_KIND_CONTEXT must explicitly name a disposable kind-* context"
[[ "${STAGE_B_KIND_MUTATION_ACK:-}" == "1" ]] || die "set STAGE_B_KIND_MUTATION_ACK=1 to authorize an isolated test Queue"
expected_sha="${STAGE_B_EXPECTED_SHA:-}"
[[ "$expected_sha" =~ ^[0-9a-f]{40}$ ]] || die "pin the exact PR SHA in STAGE_B_EXPECTED_SHA"
actual_sha="$(git rev-parse HEAD 2>/dev/null)" || die "run from the reviewed git checkout"
[[ "$expected_sha" == "$actual_sha" ]] || die "HEAD differs from frozen PR SHA"
expected_crd_spec_sha="${STAGE_B_EXPECTED_QUEUE_CRD_SPEC_SHA256:-}"
[[ "$expected_crd_spec_sha" =~ ^[0-9a-f]{64}$ ]] ||
  die "pin SHA256 of independently reviewed Queue CRD .spec in STAGE_B_EXPECTED_QUEUE_CRD_SPEC_SHA256"
[[ -z "$(git status --porcelain --untracked-files=normal)" ]] || die "working tree must be clean"

# A context called kind-* can be an arbitrary kubeconfig alias. Before any
# cluster mutation, require a locally enumerated kind cluster and the exact
# API-server endpoint + CA supplied by kind itself. Never persist kubeconfig
# credentials or weaken this fence into a name-prefix-only check.
kind_cluster="${context#kind-}"
[[ -n "$kind_cluster" ]] || die "missing kind cluster name"
if ! kind get clusters | grep -Fx -- "$kind_cluster" >/dev/null; then
  die "the authorized context is not a local kind cluster"
fi
cluster_identity_jq='
  .clusters |
  if length == 1 then .[0].cluster else empty end |
  select((.server | type) == "string" and (.server | length) > 0 and
         (."certificate-authority-data" | type) == "string" and
         (."certificate-authority-data" | length) > 0) |
  [.server, ."certificate-authority-data"] | @json
'
kind_identity="$(kind get kubeconfig --name "$kind_cluster" |
  kubectl config view --kubeconfig=/dev/stdin --raw -o json |
  jq -er "$cluster_identity_jq")" ||
  die "cannot obtain trusted kind API-server/CA identity"
context_identity="$(kubectl --context "$context" config view --minify --raw -o json |
  jq -er "$cluster_identity_jq")" ||
  die "cannot obtain selected context API-server/CA identity"
[[ "$kind_identity" == "$context_identity" ]] ||
  die "kubeconfig context endpoint/CA differs from local kind cluster; refuse mutation"
unset kind_identity context_identity

kubectl --context "$context" get crd queues.scheduling.volcano.sh >/dev/null ||
  die "Volcano Queue CRD is absent"
kubectl --context "$context" api-resources --api-group=scheduling.volcano.sh -o name |
  grep -Eqx 'queues(\.scheduling\.volcano\.sh)?' || die "Queue GVR is not served"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence="${STAGE_B_VOLCANO_API_EVIDENCE_DIR:-${TMPDIR:-/tmp}/volcano-api-${actual_sha:0:12}-$timestamp}"
mkdir -p "$evidence"
evidence="$(cd "$evidence" && pwd)"
# The observed CRD/defaults must be traceable to a concrete API-server build.
kubectl --context "$context" get crd queues.scheduling.volcano.sh -o json \
  >"$evidence/queue-crd.json" || die "cannot snapshot Queue CRD"
jq -Se '.spec' "$evidence/queue-crd.json" >"$evidence/queue-crd-spec.json" ||
  die "cannot canonicalize installed Queue CRD spec"
actual_crd_spec_sha="$(sha256sum "$evidence/queue-crd-spec.json")"
actual_crd_spec_sha="${actual_crd_spec_sha%% *}"
[[ "$actual_crd_spec_sha" == "$expected_crd_spec_sha" ]] ||
  die "installed Queue CRD schema differs from the independently pinned digest"
kubectl --context "$context" version -o json \
  >"$evidence/kubernetes-version.json" || die "cannot snapshot Kubernetes version"
name="vq-stageb-probe-$(date -u +%s)-$$"
printf 'head_sha\t%s\ncontext\t%s\nresource\t%s\nstarted_utc\t%s\n' \
  "$actual_sha" "$context" "$name" "$timestamp" >"$evidence/run.tsv"
printf 'queue_crd_spec_sha256\t%s\n' "$actual_crd_spec_sha" >>"$evidence/run.tsv"

created=0
created_uid=""
cleanup() {
  if [[ "$created" != "1" ]]; then
    return 0
  fi

  # The test Queue is cluster-scoped. Do not trust the name after a separate
  # GET: it might have been deleted/recreated between the two operations.
  if ! kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
    >"$evidence/pre-cleanup-get.json" 2>"$evidence/cleanup-get.stderr"; then
    if grep -Eqi 'NotFound|not found' "$evidence/cleanup-get.stderr"; then
      printf 'ALREADY_GONE\n' >"$evidence/cleanup.log"
      return 0
    fi
    printf 'BLOCKED: cleanup GET was ambiguous\n' >"$evidence/cleanup.log"
    return 1
  fi

  # A lost CREATE ACK provides no authoritative UID. Refuse cleanup in
  # that case: token/name alone cannot disprove a foreign replacement.
  if [[ -z "$created_uid" ]]; then
    printf 'BLOCKED: missing CREATE receipt UID; manual cleanup review required\n' >"$evidence/cleanup.log"
    return 1
  fi
  if ! jq -e --arg token "$name" --arg expected_uid "$created_uid" '
    .metadata.annotations["stageb.volcano.probe/token"] == $token and
    .metadata.uid == $expected_uid and
    .metadata.resourceVersion != null and .metadata.resourceVersion != ""
  ' "$evidence/pre-cleanup-get.json" >/dev/null; then
    printf 'BLOCKED: cleanup ownership/UID/resourceVersion is unproven\n' >"$evidence/cleanup.log"
    return 1
  fi

  # kubectl delete NAME does NOT enforce UID/RV preconditions. Use the raw
  # Kubernetes DeleteOptions request, which the API server evaluates
  # atomically. A stale UID/RV must return 409, never delete a replacement.
  jq '{
    apiVersion: "meta.k8s.io/v1",
    kind: "DeleteOptions",
    preconditions: {
      uid: .metadata.uid,
      resourceVersion: .metadata.resourceVersion
    }
  }' "$evidence/pre-cleanup-get.json" >"$evidence/delete-options.json" || return 1

  if ! kubectl --context "$context" delete \
    --raw "/apis/scheduling.volcano.sh/v1beta1/queues/$name" \
    -f "$evidence/delete-options.json" \
    >"$evidence/cleanup.log" 2>"$evidence/cleanup.stderr"; then
    printf 'BLOCKED: UID/RV conditional DELETE failed or raced\n' >>"$evidence/cleanup.log"
    return 1
  fi
  # Waiting observes deletion; it performs no second destructive request.
  if ! kubectl --context "$context" wait \
    --for=delete "queues.scheduling.volcano.sh/$name" --timeout=45s \
    >"$evidence/cleanup-wait.log" 2>&1; then
    return 1
  fi
  return 0
}
trap cleanup EXIT

cat >"$evidence/request.json" <<EOF
{
  "apiVersion": "scheduling.volcano.sh/v1beta1",
  "kind": "Queue",
  "metadata": {
    "name": "$name",
    "annotations": {
      "ai.compute/provider": "volcano",
      "ai.compute/pool-id": "stageb-api-probe",
      "ai.compute/accelerator-class": "whole-gpu",
      "ai.compute/generation": "4",
      "stageb.volcano.probe/token": "$name"
    }
  },
  "spec": {"capability": {"nvidia.com/gpu": "8"}}
}
EOF

# If CREATE times out, the side effect is ambiguous. Cleanup must verify
# the nonce on an independently observed object before a name-based delete.
created=1
kubectl --context "$context" create -f "$evidence/request.json" -o json \
  >"$evidence/create-response.json" 2>"$evidence/create.stderr" ||
  die "Queue CREATE failed or ACK was ambiguous; refuse automatic cleanup without CREATE UID"
created_uid="$(jq -r '.metadata.uid // empty' "$evidence/create-response.json")"
[[ -n "$created_uid" ]] || die "CREATE acknowledgement lacks server UID; refuse destructive cleanup"
kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
  >"$evidence/initial-get.json" || die "fresh Queue GET failed"

# CREATE's server-issued UID is the identity boundary. Matching name and
# quota alone could be an unrelated replacement Queue.
jq -e --arg uid "$created_uid" --arg token "$name" '
  .metadata.uid == $uid and
  .metadata.annotations["stageb.volcano.probe/token"] == $token and
  .metadata.annotations["ai.compute/pool-id"] == "stageb-api-probe" and
  .metadata.annotations["ai.compute/generation"] == "4" and
  .metadata.resourceVersion != null and .metadata.resourceVersion != "" and
  (.spec.capability | keys | length) == 1 and
  .spec.capability["nvidia.com/gpu"] == "8"
' "$evidence/initial-get.json" >/dev/null ||
  die "CREATE/readback UID, nonce, owner, generation or quota differs; refuse mutation"

# Inject concurrency through resourceVersion-fenced UPDATE, not a name-only
# kubectl annotate. A replacement between GET and UPDATE must yield conflict
# rather than writing to an unrelated Queue.
jq '.metadata.annotations["stageb.volcano.probe/rv-bump"] = "concurrent" |
    del(.status)' "$evidence/initial-get.json" >"$evidence/concurrent-annotate-request.json" ||
  die "cannot prepare fenced concurrent UPDATE"
kubectl --context "$context" replace -f "$evidence/concurrent-annotate-request.json" -o json \
  >"$evidence/concurrent-update-response.json" 2>"$evidence/concurrent-update.stderr" ||
  die "concurrent UPDATE failed or Queue identity changed; refuse name-only retry"
old_rv="$(jq -r '.metadata.resourceVersion' "$evidence/initial-get.json")"
jq -e --arg uid "$created_uid" --arg old_rv "$old_rv" '
  .metadata.uid == $uid and
  .metadata.resourceVersion != null and .metadata.resourceVersion != $old_rv and
  .metadata.annotations["stageb.volcano.probe/rv-bump"] == "concurrent"
' "$evidence/concurrent-update-response.json" >/dev/null ||
  die "concurrent UPDATE ACK lost Queue UID or did not advance resourceVersion"

# The same concurrent write must also fence a stale conditional DELETE.
# This intentionally uses the PRE-annotation UID/RV and must return 409;
# deleting the object would be a hard safety failure, not a passing probe.
jq '{
  apiVersion: "meta.k8s.io/v1",
  kind: "DeleteOptions",
  preconditions: {
    uid: .metadata.uid,
    resourceVersion: .metadata.resourceVersion
  }
}' "$evidence/initial-get.json" >"$evidence/stale-delete-options.json"
if kubectl --context "$context" delete \
  --raw "/apis/scheduling.volcano.sh/v1beta1/queues/$name" \
  -f "$evidence/stale-delete-options.json" \
  >"$evidence/stale-delete.stdout" 2>"$evidence/stale-delete.stderr"; then
  die "stale UID/RV DELETE unexpectedly succeeded; API-server fencing is unsafe"
fi
if ! grep -Eq 'Conflict|the object has been modified|ResourceVersion' "$evidence/stale-delete.stderr"; then
  die "stale conditional DELETE failed for a reason other than Kubernetes Conflict"
fi
kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
  >"$evidence/after-stale-delete-get.json" ||
  die "cannot prove Queue survived stale conditional DELETE"
jq -e --arg uid "$(jq -r '.metadata.uid' "$evidence/initial-get.json")" \
  '.metadata.uid == $uid and .metadata.annotations["stageb.volcano.probe/rv-bump"] == "concurrent"' \
  "$evidence/after-stale-delete-get.json" >/dev/null ||
  die "stale conditional DELETE modified or replaced the Queue"

if kubectl --context "$context" replace -f "$evidence/initial-get.json" \
  >"$evidence/stale-update.stdout" 2>"$evidence/stale-update.stderr"; then
  die "stale resourceVersion UPDATE unexpectedly succeeded"
fi
if ! grep -Eq 'Conflict|the object has been modified' "$evidence/stale-update.stderr"; then
  die "stale UPDATE failed for a reason other than Kubernetes Conflict"
fi
kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
  >"$evidence/post-conflict-get.json" || die "GET after 409 failed"
jq -e --arg uid "$(jq -r '.metadata.uid' "$evidence/initial-get.json")" \
  '.metadata.uid == $uid and .metadata.annotations["stageb.volcano.probe/rv-bump"] == "concurrent"' \
  "$evidence/post-conflict-get.json" >/dev/null ||
  die "409 caused unexpected object replacement or metadata loss"

# A fresh GET is the only allowed source for another CAS attempt.
jq '.spec.capability["nvidia.com/gpu"] = "16" |
    .metadata.annotations["ai.compute/generation"] = "5" |
    del(.status)' "$evidence/post-conflict-get.json" >"$evidence/fresh-cas-request.json"
kubectl --context "$context" replace -f "$evidence/fresh-cas-request.json" -o json \
  >"$evidence/fresh-update-response.json" ||
  die "resourceVersion-fenced fresh CAS failed"
kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
  >"$evidence/final-get.json" || die "fresh independent readback failed"
jq -e --arg uid "$(jq -r '.metadata.uid' "$evidence/initial-get.json")" \
  '.metadata.uid == $uid and .metadata.annotations["ai.compute/generation"] == "5" and
   (.spec.capability | keys | length) == 1 and
   .spec.capability["nvidia.com/gpu"] == "16"' "$evidence/final-get.json" >/dev/null ||
  die "fresh CAS reply did not survive independent GET"

# These are the exact defaults currently admitted by the v0.1 Stage B
# classifier, not a universal Volcano schema assertion.
if jq -e '
  ((.spec | keys) - ["capability", "parent", "reclaimable", "weight"] | length) == 0 and
  (.spec.parent == null or .spec.parent == "root") and
  (.spec.reclaimable == null or .spec.reclaimable == false) and
  (.spec.weight == null or .spec.weight == 1)
' "$evidence/initial-get.json" >/dev/null; then
  default_gate=PASS
else
  default_gate=BLOCKED
fi
jq -S '.spec' "$evidence/initial-get.json" >"$evidence/observed-defaults.json"
sha256sum "$evidence/"*.json >"$evidence/artifact-sha256.txt"
printf 'kubernetes_stale_update_cas\tPASS\nkubernetes_stale_delete_precondition\tPASS\ndefault_comparator\t%s\nquota_applied\tUNPROVEN\n' \
  "$default_gate" >"$evidence/gates.tsv"
printf 'Evidence: %s\n' "$evidence"
if [[ "$default_gate" != "PASS" ]]; then
  die "server-defaulted Queue differs from the fail-closed comparator; see observed-defaults.json"
fi
# Passing the real API gate also requires independent cleanup convergence.
cleanup
created=0
if kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
  >"$evidence/post-cleanup-get.json" 2>"$evidence/post-cleanup-get.stderr"; then
  die "cleanup is incomplete: test Queue still exists"
fi
if ! grep -Eqi 'NotFound|not found' "$evidence/post-cleanup-get.stderr"; then
  die "cannot independently prove cleanup NotFound"
fi
printf 'cleanup_observed_not_found\tPASS\n' >>"$evidence/gates.tsv"
# Recompute after the conditional DELETE, including identity and cleanup receipts.
sha256sum "$evidence/"*.json >"$evidence/artifact-sha256.txt"
printf 'VOLCANO API CONTRACT: API SERVER PASS (scheduler application NOT PROVEN)\n'
