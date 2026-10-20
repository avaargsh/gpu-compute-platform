# Verified Operations dispatcher delivery contract

Status: internal integration contract for issue #169; this documents the trusted adapter boundary and does not claim production delivery is deployed. This document defines a narrow integration boundary; it does not claim a production dispatcher, KMS/CSI key custody, GPU-ready evidence, or a final authorization verdict exists.

## Purpose

Connect the existing internal primitives into one fail-closed workflow:

1. trusted server-side orchestration derives the scope from durable records;
2. PostgreSQL issues a short-lived Challenge and IssueSignedObserverWork produces a signed, domain-separated ticket;
3. a reviewed isolated execution adapter delivers only that ticket and the pinned trust/configuration references to the independent Observer;
4. the Observer emits one bounded DSSE observation envelope;
5. a trusted receipt adapter resolves the original Challenge scope and passes only the Observer key ID to RecordSignedObservedJobProof; PostgreSQL resolves the Ed25519 key from the DBA-controlled cluster-scoped trust registry in the same SERIALIZABLE transaction, with bounded full-transaction retries for SQLSTATE 40001/40P01; the server-issued Challenge row is locked FOR UPDATE as the v1/v2 nonce serialization point;
6. the caller receives a receipt identity and evidence-only observation state.

This is a control-plane integration contract, not a new public API or a new execution engine.

## Authority boundaries

| Component | May do | Must not do |
| --- | --- | --- |
| Management API / browser | Request an operation through existing authenticated APIs | Choose dispatcher signer, Observer key, cluster/project binding, original CREATE UID, nonce or trusted scope |
| Cluster Agent | Report durable PREPARED / CREATE-ACK facts under existing lease and identity checks | Issue Observer tickets, mint trust roots, or certify ownership from same-generation adoption |
| Trusted dispatcher adapter | Resolve trusted operation scope, request a ticket, deliver it to the reviewed Observer target | Accept caller-supplied scope as authority, export private keys to Management/Agent, or launch arbitrary images/commands |
| Observer Pod | Verify the pinned dispatcher ticket and exact ServiceAccount identity; read Job/Pod/Node; emit one signed DSSE envelope | Connect to PostgreSQL, mutate Kubernetes, expose an HTTP listener, mint keys, or self-select a trust root |
| Receipt verifier | Resolve challenge and Observer trust independently; bound and verify DSSE; persist through the existing PostgreSQL receipt method | Trust scope claims from the envelope, accept unknown keys, bypass the challenge, or emit AUTHORIZED_AND_VERIFIED |
| PostgreSQL | Source of truth for desired state, audited CREATE/ACK bindings, challenges and idempotent receipts | Treat signature validity alone as proof of production signer isolation or GPU execution |

## Proposed internal contracts

Names below are conceptual until implemented; they are not existing exported APIs.

### Dispatch request

A caller supplies only a durable operation reference or an already-authorized internal work handle. The server resolves:

- operation / cluster / project / workload / generation;
- original CREATE response Job UID and namespace/name binding;
- current non-deleting Desired state and pinned image digest;
- the trusted dispatcher signer identity and key ID;
- the target Observer workload profile.

Do not accept a caller-created ObserverChallengeScope from a browser, Agent or Observer-facing endpoint. The trusted service must derive and validate the scope against the database before calling Store.IssueSignedObserverWork.

### Delivery adapter

The adapter should expose a narrow operation such as “run this approved one-shot Observer job with this ticket reference” and return a bounded result reference. It must not accept arbitrary image names, command overrides, namespaces, ServiceAccounts, mounts or RBAC settings from a request.

Production ticket and signer delivery must use a separately reviewed trust mechanism. The ephemeral Secret + initContainer flow in the kind test is test-only and is not an acceptable production default. Prefer the reviewed secret provider / CSI or KMS-backed design once its identity, rotation, revocation and audit contracts are demonstrated.

### Evidence return

The Observer emits exactly one DSSE envelope on its evidence channel. The receiver must enforce:

- a small explicit maximum envelope size and bounded read/deadline;
- no more than one envelope per execution attempt;
- a known predicate type and supported schema version;
- a trusted Observer key ID mapped to an active row in the DBA-controlled `verified_observer_signing_keys` registry for the exact cluster; the caller must not provide a public key, and unknown/foreign/not-yet-valid/expired/revoked keys fail closed;
- challenge/nonce/scope reconstructed from durable server records, not accepted from untrusted request fields;
- exact retry is idempotent; changed bytes for the same (cluster_id, nonce) fail closed;
- retries never widen scope, extend Challenge TTL, or replace the original Job UID.

The receiver calls Store.RecordSignedObservedJobProof with the trusted key ID, trusted scope and issued nonce. PostgreSQL locks the active key, original CREATE/ACK identity and project binding while verifying and storing the receipt. The receipt remains evidence-only.

## State and failure handling

- Ticket issuance committed but signing fails: leave the short-lived Challenge to expire; do not return an unsigned ticket.
- Delivery fails before Observer start: record operational failure separately; do not fabricate observation evidence.
- Observer timeout / crash / no envelope: no receipt is created; retry must use an explicitly authorized attempt policy and must not silently extend the original Challenge.
- Envelope malformed, oversized, wrong key, wrong nonce, wrong Job UID, wrong generation or expired Challenge: reject without consuming a legitimate Challenge.
- Database transient failure after valid observation: retry the exact envelope and nonce; rely on existing idempotency.
- Existing nonce with a different envelope or protocol-version replay: reject.
- Unknown signer identity or unavailable trust configuration: fail closed; do not fall back to a key supplied by the request.
- Workload deletion or desired generation change before receipt admission: existing store checks remain authoritative and should reject stale evidence.

## Required tests

1. Unit contract tests: untrusted caller cannot select signer, scope, image, Job UID or trust root.
2. PostgreSQL integration: valid signed Pending envelope persists one receipt; exact retry returns the same receipt; modified envelope, cross-version replay, expired challenge, changed UID/generation/image and wrong key are rejected.
3. Delivery adapter tests: timeout, duplicate result, oversized/multiple envelopes, cancellation and cleanup after partial failure.
4. kind test: actual isolated Observer Pod and projected short-lived ServiceAccount token remain required; test-only credential staging must be labelled as a fixture.
5. Security negative tests: no public issuance/receipt route, no Kubernetes write API, no database credentials in Observer, no private keys in Management/Agent, and no fallback to caller-supplied trust.
6. Exact-head CI: go vet ./..., Go tests, build, release acceptance contracts, real PostgreSQL, kind/Kueue UID races and Observer RBAC / Pod execution all pass.

## Explicit release boundary

The integration may record POD_READY_OBSERVED, OUTCOME_PENDING, OUTCOME_MISMATCH or OWNERSHIP_UNPROVEN as verified evidence states. It must not enable AUTHORIZED_AND_VERIFIED.

A real GPU claim additionally requires a real workload Pod, independently observed Ready state, verified container ImageID/digest and node binding, and production signer isolation / lifecycle evidence. A successful kind test using a suspended Job is not that proof.

## Implementation order

1. Add the trusted server-side dispatcher interface and scope derivation tests.
2. Add a narrow delivery adapter interface with no arbitrary execution inputs.
3. Add a bounded receipt-return path that resolves Challenge and Observer trust server-side.
4. Wire it to RecordSignedObservedJobProof and prove PostgreSQL idempotency/replay behavior.
5. Add real-cluster key-provider / identity acceptance as a separate release gate.
6. Keep final authorization disabled until a separately reviewed policy gate consumes the required evidence.
