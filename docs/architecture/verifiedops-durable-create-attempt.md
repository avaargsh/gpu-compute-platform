# Durable pre-CREATE intent (fail-closed first slice)

Before a Workload Job CREATE the authenticated Agent must durably prepare an
immutable attempt scoped to the original Management CREATE audit, current
nondeleting Desired generation, project namespace/Kueue binding, and active
lease owner. Exactly one (operation, cluster, Workload, generation) can be
prepared. A second attempt returns `ErrCreateAttemptUnresolved` even if
the original Agent repeats it. This is deliberate: it is impossible to
distinguish crash-before-Kubernetes from crash-after-CREATE without external
evidence. Do NOT retry external CREATE or infer ownership from a Job GET.

`RecordJobCreateUID` now requires that exact prepared operation/lease owner
in the same transaction before writing the actual API-server CREATE response
UID/RV. Existing candidate binding remains immutable. Status lookup is
`UNKNOWN` when PREPARED has no ACK-bound UID and `ACK_BOUND` only with an
actual matching candidate record. No recovery step may turn UNKNOWN into
ACK_BOUND via a GET. In particular, the operation status is not a signed proof
and does not establish actual Job outcome or authorization.

**Integration boundary:** this PR only introduces and tests the PostgreSQL
journal and privilege-denial contract. It does not wire the Agent/Kueue
pre-CREATE callback or publish endpoint. The existing write path is not yet
compatible with requiring a prepared attempt: no rollout/merge until the
Agent mTLS Prepare endpoint and pre-CREATE guard arrive in a stacked PR,
pass full CI, and real kind race/restart tests are green. No backwards-compatible
relaxation of the pre-create invariant is permissible.
