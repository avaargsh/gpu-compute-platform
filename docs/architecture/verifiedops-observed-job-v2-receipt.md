# Observed Job v2 receipt admission — internal evidence, not a verdict

This change bridges the read-only Observer's `observed-job/v1` Ed25519 DSSE
statement with PostgreSQL's original Management Operation / CREATE-ACK UID /
server-issued Challenge, without introducing a public HTTP endpoint.

## Single transaction, one nonce across v1 and v2

`Store.RecordSignedObservedJobProof` runs in SERIALIZABLE PostgreSQL:
1. Lock the Desired Workload, require matching project/generation and that
   deletion has not been requested; resolve its pinned `image` / `Image` digest.
2. Reconstruct expected Job namespace/name/UID from the original audited
   Management CREATE, durable Agent PREPARED+ACK binding, and current
   project binding. No same-name Kubernetes GET may supply this identity.
3. Lock the exact server-issued Challenge row, match Operation/Cluster/
   Project/Workload/Generation/JobUID, and enforce DB-clock TTL.
4. Verify the exact v2 in-toto predicate, an independently *provided pinned*
   Ed25519 public key, challenge identity, image, signed timestamp, and
   SHA-256 digest of the canonical signed Job/Pod/Node observation.
5. Insert a receipt with predicate type + immutable observation JSON using
   the existing global `(cluster_id, nonce)` primary key. Exact retries
   remain idempotent during validity. Altered envelopes and cross-version
   nonce reuse are rejected. The additive migration retains the original v1
   rows and their predicate-type default.

The returned state is `POD_READY_OBSERVED`, `OUTCOME_PENDING`,
`OUTCOME_MISMATCH` or `OWNERSHIP_UNPROVEN`, **not** a server-issued
`AUTHORIZED_AND_VERIFIED` decision. Even a valid signed READY observation
is not yet proof that a production signer was independently deployed.

## Acceptance and release gates

Real PostgreSQL tests cover signed Pending evidence, durable idempotency,
changed project/UID, unknown nonce, opposite version replay, expiry,
Desired deletion / changed image and mismatched signer/tampered envelope.
The Pending observation uses the K8s fake client in the database test;
the separate kind suite tests an actual API-server-issued Observer SA
bearer token signing a real Pending Job. Neither is production GPU Ready.

Still absent: independently deployed and policy-restricted Observer Pod,
projected workload SA token, signer key isolation/rotation and trusted-root
lifecycle, real GPU Ready + ImageID acceptance, and independently authorized
backend outcome verdict. The Go-only API must not be exposed without a
reviewed authenticated delivery interface and trusted scope selection.
