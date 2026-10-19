# Operation → provider CREATE UID candidate binding

Status: **additive persistence boundary only**. This does not claim production
identity, real API-server acceptance, or cryptographic ownership proof.

`Store.RecordJobCreateUID` requires the same original `management_operation_audit`
Workload CREATE request ID and non-legacy request digest, same cluster/project,
resource/generation, current nondeleting Desired and Kueue pool, matching
project namespace, and an unexpired lease owned by the submitting Agent.

The transaction reads audit but **never changes it**; the new row is immutable
under normal operation. Exact replay is accepted, mismatched UID, Job name,
resourceVersion, lease owner, operation ID or generation is rejected.
`GetJobCreateUID` is scoped but not user-authorized; only trusted server
callers may use it. The Management runtime must not receive INSERT/UPDATE/
DELETE for the new table; grant any write permission only to a separately
authenticated Agent persistence identity.

A Job UID submitted to this method must come from the Kubernetes API-server
CREATE response. The existing Kueue `ApplyJob` GET/AlreadyExists same-name
recovery is still allowed for convergence, but **cannot call this method and
cannot infer the CREATE response**. The current `Runner` has no original
operation_id and no such call path: wiring Agent mTLS transport, operation
projection, create-response metadata and an independent verifier remains P0.

This slice does not solve lost CREATE ACK: no recorded UID means UNKNOWN /
OWNERSHIP_UNPROVEN, even if an object with the expected name exists. Do not
fabricate a historical CREATE return by reading the object. A future
record-before-ack durable attempt protocol must make recovery explicit.

Tests cover source-level contracts and PostgreSQL when
`TEST_POSTGRES_DSN`/`TEST_POSTGRES_STORE_DSN` are configured. It does not
replace real K8s UID-race, account separation, or DSSE/replay negative tests.

Tracking: gpu-compute-platform #141; GPUPanel #153; agent-control-plane #191.
