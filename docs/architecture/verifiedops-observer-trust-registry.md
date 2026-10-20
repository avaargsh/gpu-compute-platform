# Cluster-scoped Observer trust roots

The v2 PostgreSQL receipt path accepts an Observer key ID, not a caller-
supplied public key. It resolves that ID inside the same SERIALIZABLE
transaction against `verified_observer_signing_keys`, scoped to the target
cluster. The selected row is locked `FOR SHARE` through commit, so concurrent
revocation or validity changes cannot race admission.

Each row contains a 32-byte Ed25519 public key, a unique per-cluster key ID,
`valid_from`, optional `valid_until`, and optional `revoked_at`. Unknown,
foreign-cluster, not-yet-valid, expired and revoked keys fail closed before
nonce consumption. Multiple keys may be active during a controlled rotation;
retiring the old key must be an explicit DBA action. Existing receipts remain
historical observations and are not retroactively promoted to authorization.

## Provisioning boundary

This is a trust lookup table, not a key-management API. The schema creates the
table; registration and rotation require an external DBA-controlled process
that verifies the public key out of band and writes the cluster, key ID, key
bytes and validity window. The Management runtime role has no privileges on
this table. No HTTP route for key registration is added.

The trusted dispatcher passes only the key ID. PostgreSQL derives the actual
verification key from the registry and locks it through receipt commit. The
same transaction validates the server-issued Challenge, original audited
CREATE/ACK UID binding, current non-deleting Desired generation and pinned
image digest. Original CREATE/ACK, attempt and project binding rows are locked
while their identity is checked.

The nonce ledger remains shared across v1 and v2. A successful receipt records
signed observation evidence—including Pending or negative states—not an
authorization verdict. This does not claim production signer deployment,
real GPU Ready proof, key custody/KMS, or `AUTHORIZED_AND_VERIFIED`.
