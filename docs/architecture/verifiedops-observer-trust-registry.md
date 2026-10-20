# Cluster-scoped Observer trust roots

The v2 PostgreSQL receipt path no longer accepts a caller-supplied public key.
It takes only a key ID and resolves that ID inside the same SERIALIZABLE
transaction against `verified_observer_signing_keys`, scoped to the target
cluster. The selected row is locked `FOR SHARE` through commit, so a concurrent
revocation/validity update cannot race a receipt admission.

Each row contains a 32-byte Ed25519 public key, a unique per-cluster key ID,
`valid_from`, optional `valid_until`, and optional `revoked_at`. Unknown,
foreign-cluster, not-yet-valid, expired and revoked keys fail closed before
nonce consumption. Multiple keys may be active during a controlled rotation;
retiring the old key must be an explicit DBA action. Existing receipts remain
historical observations and are not retroactively promoted to authorization.

## Provisioning boundary

This is a trust *lookup* table, not a key management API. The migration creates
the table; registration and rotation require an external DBA-controlled process
that verifies the public key out of band and writes the cluster/key ID/key bytes
and validity window. Do not expose INSERT/UPDATE to the Management runtime
role. The included `runtime_role.sql` explicitly revokes its privileges.

No HTTP route is added. The caller must still derive scope from trusted
orchestration. The v2 receipt remains observational only: no production
signer deployment, real GPU Ready proof, or `AUTHORIZED_AND_VERIFIED` verdict.
