# Verified Operations v0 — read-only decision gate

Status: **source-level diagnostic; NOT signed evidence or Kubernetes acceptance**.

## Existing facts (do not rebuild)

- The Management API already stores scoped OIDC/RBAC operation receipts and a generation-specific Desired readback.
- The isolated Management frontend already reads historical Agent/Kueue observations. These reads are not an atomic snapshot.
- Kueue and Volcano provider implementations already enforce Kubernetes UID/resourceVersion deletion preconditions; their existing tests exercise replacement races.
- Agent Control Plane v0.1 has an independently verifiable execution proof, but its trusted out-of-band SHA-256 file is **not a producer signature**. It is a separate Python project, not a Go library.

## This slice

The current Management evidence panel shows a **read-only, conservative verdict** next to the existing receipt, Desired and observation chain. It does not create a new Tasks/Session dashboard or additional API endpoint.

| Local verdict | Meaning |
| --- | --- |
| `OWNERSHIP_UNPROVEN` | Observed state cannot be safely attributed to the audited operation, even if Job is RUNNING/SUCCEEDED |
| `OUTCOME_MISMATCH` | Desired/observation contradict the expected positive outcome |
| `VERIFICATION_UNAVAILABLE` | Missing, stale or insufficient observation; cannot make an outcome claim |
| `OUTCOME_PENDING` | Queue, readiness or deletion cleanup still pending |

There is deliberately **no `AUTHORIZED_AND_VERIFIED` output**. The existing APIs do not provide signed proof, independently read Kubernetes object UIDs, image digest comparison, or authoritative ownership binding. A UI classification must not manufacture these.

A committed delete-request, a 404, a successful Job Condition or matching Desired Generation does **not** independently prove the intended resource was deleted or that the original operation owned a side effect. Existing error handling continues to require original-ID recovery before retry.

## Next server-side contract (not implemented here)

1. Persist exact authorized intent and execution attempt, including operation ID, request digest, principal, policy version, cluster/project and expected generation; preserve UNKNOWN.
2. Bind successful Kubernetes CREATE to the API-server-returned UID; fail closed for AlreadyExists instead of adopting same-name objects.
3. Observer with separate read-only service account: fresh API-server GET of Job, Pod/Node and imageID/digest; match immutable UID and per-workload outcome rules. Keep Kueue as the sole batch-admission path.
4. Evidence: immutable operation snapshot subject, in-toto Statement v1 with a GPUPanel/Compute operation predicate, DSSE/Cosign signatures and independently configured verifier trust root. Keyless public transparency and private KMS verification are distinct trust profiles.
5. Durable scoped replay defense binds operation, nonce/TTL and proof digest. Legitimate re-verification of the *same* operation is allowed; cross-operation proof reuse fails.
6. Backend-enforced state machine, never trust a browser-provided verdict. Only this service may eventually return `AUTHORIZED_AND_VERIFIED`, after independent signature, context and outcome checks.

## Required negative integration experiments (NOT RUN in this slice)

- Create race: external Controller creates the deterministic name first → `OWNERSHIP_UNPROVEN`; no automatic adoption.
- Delete/recreate: UID-precondition prevents killing replacement; success of deleting the old UID must not fail simply because a new same-name object exists.
- Replay: reuse an old valid signed statement against another operation → `EVIDENCE_REPLAYED`.
- Tamper: change a signed payload/digest → `EVIDENCE_TAMPERED`.
- Signer mismatch, Observer outage, policy-version drift, queued Job, legal idempotent retries, finalizer delay.

The local unit tests in this PR validate only display-level fail-closed classification. They **do not** substitute for API-server races, signature verification, real OIDC/PostgreSQL acceptance, or production GPUPanel integration.

## Delivery split

- `gpu-compute-platform`: provider execution, Kueue observation, backend scoped receipt/Desired/execution proof.
- `agent-control-plane`: state transition / proof semantics; retain authority and persistence boundaries, reuse adapters, do not SDK-ify the whole service.
- `gpupanel`: eventual authenticated user entrypoint into an existing Workload operation detail, once real scoped Kubernetes workload APIs are integrated. Current GPUPanel Host/App Operations Center is **not** that integration.
