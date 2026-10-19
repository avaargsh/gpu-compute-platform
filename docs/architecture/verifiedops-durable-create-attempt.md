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

**Integrated path:** the Agent creates the attempt via the same mTLS-only
cluster-scoped protocol. The Kueue adapter calls this endpoint after a Job
GET returns NotFound, immediately before actual CREATE; only then can a
returned UID/RV flow into existing candidate persistence. A durable PREPARED
failure stops the API-server CREATE. Any non-Management Workload remains
unbound; a claimed Management operation missing the pre-CREATE or ACK
publisher fails closed, without falling back to legacy direct CREATE.

The real kind acceptance now also exercises a guarded CREATE, an existing
Job that must not prepare again, and a rejected PREPARED that must never
write a Job to the API server. Crash-before/after-Kubernetes remains
conservatively UNKNOWN after restart; that ambiguity is not magically
resolved. Do not deploy before CI/DB grants and Agent mTLS validation pass.
No backwards-compatible relaxation of the pre-CREATE invariant is allowed.
