# Observer DSSE verification primitive (draft)

This slice is an **offline Ed25519 DSSE/in-toto Statement v1 verifier**,
not a complete GPUPanel verified operation. The key is explicitly pinned
by the verifier; the envelope's keyid alone cannot select a trust root.
A valid signature binds exact Operation ID, cluster, project, Workload,
generation, Job UID, nonce and observation timestamp. It authenticates
the signed bytes only. The signer can lie or observe a stale Kubernetes
object, and an identical envelope can still be replayed for the same
operation until durable nonce tracking is implemented.

Never emit AUTHORIZED_AND_VERIFIED from this function. Required next:
independently authorized read-only Kubernetes Observer, freshness limits,
durable nonce consumption scoped to operation, trusted signer rotation,
ACK-bound ownership and operation authorization policy checks, negative
real-cluster integration tests. This PR is not a release gate.
