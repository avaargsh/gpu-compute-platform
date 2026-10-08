#!/usr/bin/env bash
# CPU-only, live Kubernetes API-server falsification for the unregistered Queue CAS.
# Does NOT prove Volcano scheduler quota application or real GPU execution.
set -euo pipefail

die() { printf 'VOLCANO API CONTRACT: BLOCKED: %s\n' "$*" >&2; exit 2; }
for tool in git kubectl jq sha256sum; do
  command -v "$tool" >/dev/null || die "missing $tool"
done

context="${STAGE_B_KIND_CONTEXT:-}"
[[ "$context" == kind-* ]] || die "STAGE_B_KIND_CONTEXT must explicitly name a disposable kind-* context"
[[ "${STAGE_B_KIND_MUTATION_ACK:-}" == "1" ]] || die "set STAGE_B_KIND_MUTATION_ACK=1 to authorize an isolated test Queue"
expected_sha="${STAGE_B_EXPECTED_SHA:-}"
[[ "$expected_sha" =~ ^[0-9a-f]{40}$ ]] || die "pin the exact PR SHA in STAGE_B_EXPECTED_SHA"
actual_sha="$(git rev-parse HEAD 2>/dev/null)" || die "run from the reviewed git checkout"
[[ "$expected_sha" == "$actual_sha" ]] || die "HEAD differs from frozen PR SHA"
[[ -z "$(git status --porcelain --untracked-files=normal)" ]] || die "working tree must be clean"

kubectl --context "$context" get crd queues.scheduling.volcano.sh >/dev/null ||
  die "Volcano Queue CRD is absent"
kubectl --context "$context" api-resources --api-group=scheduling.volcano.sh -o name |
  grep -Eqx 'queues(\\.scheduling\\.volcano\\.sh)?' || die "Queue GVR is not served"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence="${STAGE_B_VOLCANO_API_EVIDENCE_DIR:-${TMPDIR:-/tmp}/volcano-api-${actual_sha:0:12}-$timestamp}"
mkdir -p "$evidence"
evidence="$(cd "$evidence" && pwd)"
name="vq-stageb-probe-$(date -u +%s)-$$"
printf 'head_sha\t%s\ncontext\t%s\nresource\t%s\nstarted_utc\t%s\n' \
  "$actual_sha" "$context" "$name" "$timestamp" >"$evidence/run.tsv"

created=0
cleanup() {
  if [[ "$created" == "1" ]]; then
    # A failed/ambiguous create may have hit an existing Queue name. Never
    # delete by name unless the isolated request's nonce still owns it.
    if kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
      >"$evidence/pre-cleanup-get.json" 2>"$evidence/cleanup-get.stderr"; then
      if jq -e --arg token "$name" \
        '.metadata.annotations["stageb.volcano.probe/token"] == $token' \
        "$evidence/pre-cleanup-get.json" >/dev/null; then
        kubectl --context "$context" delete queues.scheduling.volcano.sh "$name" \
          --ignore-not-found=true --wait=true --timeout=45s >"$evidence/cleanup.log" 2>&1 || true
      else
        printf 'REFUSED: cleanup sees another owner\\n' >"$evidence/cleanup.log"
      fi
    fi
  fi
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
  die "Queue CREATE failed or ACK was ambiguous; inspect evidence and cleanup.log"
kubectl --context "$context" get queues.scheduling.volcano.sh "$name" -o json \
  >"$evidence/initial-get.json" || die "fresh Queue GET failed"

jq -e '.metadata.uid != null and .metadata.uid != "" and
       .metadata.resourceVersion != null and
       .spec.capability["nvidia.com/gpu"] == "8"' "$evidence/initial-get.json" >/dev/null ||
  die "stored Queue UID/RV/capability does not match expected projection"

# A metadata-only concurrent actor advances resourceVersion. Replay of the
# frozen old GET must return a real Kubernetes 409; any other failure is blocked.
kubectl --context "$context" annotate queues.scheduling.volcano.sh "$name" \
  stageb.volcano.probe/rv-bump=concurrent --overwrite \
  >"$evidence/concurrent-update.log" 2>&1 ||
  die "cannot inject a concurrent resourceVersion update"

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
printf 'kubernetes_stale_cas\tPASS\ndefault_comparator\t%s\nquota_applied\tUNPROVEN\n' \
  "$default_gate" >"$evidence/gates.tsv"
printf 'Evidence: %s\n' "$evidence"
if [[ "$default_gate" != "PASS" ]]; then
  die "server-defaulted Queue differs from the fail-closed comparator; see observed-defaults.json"
fi
printf 'VOLCANO API CONTRACT: API SERVER PASS (scheduler application NOT PROVEN)\n'
