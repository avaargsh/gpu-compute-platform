# Kueue Job CREATE response identity seam

`KubeClient.ApplyJobWithCreateResponse` separates a successful Kubernetes
API-server CREATE response from ordinary same-generation convergence.
Only `CREATE_ACKNOWLEDGED` can include a UID and resourceVersion captured
directly from the CREATE return. `EXISTING_UNBOUND` and
`RACE_ADOPTED_UNBOUND` return no identity even when GET exposes UID/RV.
Timeout / lost ACK surfaces an error; re-GET remains unbound.

`ApplyJob` retains the existing Kueue provider's at-least-once recovery
contract. Existing Agent/Provider interfaces are unchanged; the new method
is deliberately opt-in and is **not yet connected** to operation attribution.

Do not map this result to `AUTHORIZED_AND_VERIFIED`. The Agent must
first obtain a durable Operation ID from the original Management audit,
prove authentic Agent connection, record the API-server CREATE response with
lease fencing via `Store.RecordJobCreateUID` (candidate PR #143), and have
a separately credentialed Observer independently compare the resource
identity and signed evidence. A lost ACK without a stored response stays
`UNKNOWN / OWNERSHIP_UNPROVEN`.

Tests use Kubernetes client-go fakes only, not a real API Server. They
cover acknowledged CREATE, same-name existing, AlreadyExists race, lost ACK,
missing UID/RV, forbidden GET, and stale generation. Live kind/Kueue UID
races and isolated RBAC verification remain release blockers.
