# Signed read-only Observer material (v0, NOT deployed)

`JobObserver` uses independent Kubernetes read-only credentials and
collects Job UID/generation, Pod owner UID/RV, Node identity/RV,
pinned container ImageID and Ready conditions. Stable READY now
re-GETs **Job, Pod AND Node**; a replaced or drained Node fails closed.

`ObserveAndSign` accepts an injected `crypto.Signer` instead of storing
or provisioning private keys. It signs a DSSE in-toto Statement v1 with
versioned `observed-job/v1` predicate: Challenge operation ID, cluster,
project, Workload, generation, Job UID, nonce/timestamp, expected namespace/
name/container/image digest, and read-only actual observation. The in-toto
subject digest commits to the canonical predicate JSON. Pinned Ed25519
verification rehashes that predicate and checks exact scope, timestamp
and observed-state consistency.

The **real kind integration** signs an actual pending Job using a short-lived
ServiceAccount bearer token (no admin certificate in the read client) and
a strictly ephemeral TEST private key. A fake-client READY case tests the
canonical positive evidence path, but is not evidence of a real GPU Pod
running.

**Not production ready:** no deployed independent signer Pod, no audited
external key management, no projected Pod token, no trust-root rotation,
no challenge delivery/receipt integration for this new versioned predicate,
and no real GPU digest/Ready acceptance. Existing PostgreSQL nonce receipt
uses the prior predicate version; it MUST NOT accept this new body without
an explicit reviewed integration.

A valid signature is not a proof of Kubernetes runtime truth or operation
ownership. No AUTHORIZED_AND_VERIFIED result is produced.
