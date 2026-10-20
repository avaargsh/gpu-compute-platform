# Signed Observer nonce replay ledger — internal contract

`RecordSignedObserverNonce` enforces **first seen per (cluster, nonce)**
persistence after verifying the pinned Ed25519 DSSE envelope and exact
operation context. It additionally checks the immutable original Management
CREATE audit and Agent-published UID binding; Kubernetes same-name GET cannot
supply this binding. First proof is inserted once; identical proof retries
are idempotent, but a different signer or envelope reusing the nonce is
rejected. Signed timestamps are bounded to a five-minute past freshness
window and 30 seconds future clock skew.

The nonce is constrained to 64 lowercase hex characters to support 256-bit
random challenges; its syntax does **not** prove it was issued by a trusted
party. This milestone does **not** yet implement server-issued challenges,
challenge storage, signer lifecycle/revocation, a separately deployed signed
Observer, digest-of-observation semantics, or positive authorization/outcome
verdicts. This method is an internal-only primitive with no HTTP handler.

The Management DB runtime is explicitly barred from SELECT/INSERT/UPDATE/
DELETE on the nonce ledger; schema is created by the trusted Agent-store
migration path. No portal evidence card changes. Before rollout: design a
server-issued nonce challenge bound to the original Operation and UID, pass
real PostgreSQL and Observer/Kind acceptance, and integrate signature
verification with independently read Kubernetes Job/Pod/Node observations.
Never expose `AUTHORIZED_AND_VERIFIED` based only on this method.
