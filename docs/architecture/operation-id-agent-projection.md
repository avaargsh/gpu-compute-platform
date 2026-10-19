# Original Operation ID propagation (unverified)

PostgreSQL `Store.Desired` now enriches a current, non-deleting Workload
from **exactly one** matching immutable original Management audit CREATE row:
(cluster, project, Workload, resulting Generation, expected Generation=0).
The audit must have a non-legacy request fingerprint; no audit table, no row,
legacy Desired, deletion or no project ID yields an **empty** Operation ID.
Duplicate matching audits are rejected, never arbitrarily selected.

Agent's `DesiredResource.operationId` and downstream
`WorkloadProjection.OperationID` are non-authorizing correlation hints.
The same field is never read from user-editable `spec` or Kubernetes labels,
nor injected by a browser. The Agent mTLS-only listener authenticates its
client certificate at the enclosing transport boundary in production; local
development's loopback HTTP is not production operation verification.

This does **not** connect the Kueue CREATE-return seam (#144) to durable
candidate UID persistence (#143). The downstream writer must authenticate
the caller's cluster identity, hold an actual lease and persist the exact
API-server CREATE ACK before release. Treat missing ID / lost ACK /
AlreadyExists GET as **UNKNOWN / OWNERSHIP_UNPROVEN**. This feature does not
authorize writes, report signed proof, or change Kueue admission semantics.

Unmerged dependencies: #143 candidate UID binding, #144 CREATE response.
Tracking: #141, GPUPanel #153.
