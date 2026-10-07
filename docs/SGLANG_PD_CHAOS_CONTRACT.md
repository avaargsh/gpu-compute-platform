# SGLang P/D Chaos Contract

Status: **Stage D falsification contract; no production serving-path promotion**.

This contract turns upstream SGLang prefill/decode (P/D) failure modes into platform acceptance requirements without moving SGLang protocol semantics into the gpu-compute-platform control plane.

## Boundary

The platform may observe a serving provider attempt and its terminal evidence. It must not interpret or reproduce SGLang-internal KV page layouts, transfer plans, rank/component accounting, or transport protocols.

A future serving provider exposes only portable evidence: role intent -> provider attempt identity -> provider-native transfer/runtime work -> terminal provider outcome -> semantic verification evidence -> lease-fenced platform observation.

HTTP success, Pod readiness, or transport success alone is not proof that a P/D transition is valid.

## Invariants

1. **Transfer completeness** — every provider-required transfer component is complete before Decode may consume transferred state.
2. **Semantic validity** — a successful HTTP response is compared with a deterministic/reference result so silent corruption cannot pass acceptance.
3. **Terminal ambiguity** — incomplete or contradictory transfer evidence fails closed; the platform must not infer success from liveness.
4. **Role ownership** — role/attempt/generation identity is stable across retry, process crash, and takeover.
5. **Recovery reuse** — failure maps back to the existing generation + lease + tombstone/recovery model rather than creating a second platform state machine.

## Chaos matrix

| Case | Injection | Required result |
| --- | --- | --- |
| PD-C1 | transfer component reorder | no early completion; semantic result remains valid |
| PD-C2 | delayed or missing component | fail/timeout closed; no successful semantic commit |
| PD-C3 | full-cache-hit / empty-transfer boundary | deterministic terminal outcome; no stuck role |
| PD-C4 | chunked-prefill boundary mismatch | explicit provider failure or valid result; never silent corruption |
| PD-C5 | source/destination layout mismatch | fail closed before platform success commit |
| PD-C6 | provider process death after transfer begins | retry/re-observe under existing lease/generation fencing |
| PD-C7 | role transition completes but observation commit is lost | takeover re-observes the same attempt; no duplicate authority |
| PD-C8 | HTTP 200 with intentionally corrupted/incomplete transferred state | semantic verifier rejects acceptance |

## Acceptance evidence

A P/D experiment is accepted only when the artifact records provider/runtime version and immutable image digest; workload/generation/attempt identity; requested and observed role; provider terminal outcome; semantic verification result and reference identifier; relevant provider evidence refs; lease owner/epoch used for the platform commit; and chaos case identifier.

Acceptance requires all of: transport success, runtime terminal success, semantic verification success, and current lease ownership. No individual term may substitute for another.

## Promotion rule

This contract does not promote SGLang into Stage B or Stage C. It is an executable-design target for the later serving/runtime stages.

Do not add a public TransferPlan, KV-page model, NIXL component model, or SGLang-specific role state to the management-plane API. If such data is needed for diagnosis, retain it as opaque provider evidence.

Adjust this experiment when upstream changes P/D transfer-completion semantics, role-switch transaction boundaries, provider-visible terminal failure semantics, or image/signing guarantees needed to pin the tested runtime. Otherwise upstream runtime implementation changes remain watch-only.
