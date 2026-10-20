# Internal trusted Observer work-ticket issuance

`Store.IssueSignedObserverWork` bridges the original audited Management
CREATE and Kueue Agent PREPARED+ACK to an independently signed dispatcher
work ticket. No browser, Agent or Observer API may choose signer identity,
project scope or original CREATE UID. This is an internal Go primitive.

One Serializable PostgreSQL transaction:
- locks current non-deleting Desired at the expected generation and project;
- resolves a strict `@sha256:...` pinned workload image from current Desired;
- joins original immutable audit, PREPARED and CREATE-ACK with equal lease
  owner and a live project namespace binding;
- reconstructs the exact expected Job namespace/name/UID, `workload`
  container and image digest from these authoritative rows;
- generates a CSPRNG 256-bit nonce and persists an unexpired Challenge.
  A single PostgreSQL clock instant establishes both issuance and TTL.

**After** commit, a caller-provided trusted dispatcher Ed25519 signer
signs a domain-separated DSSE work-ticket for exactly this context. This
avoids holding database row locks while using an external signer. A signer
failure may strand an unused challenge for its two-minute TTL but cannot
create an unsigned accepted work ticket.

The existing independently constrained Observer runner checks a pinned
dispatcher key, ticket bounds and exact Observer SA via SelfSubjectReview
before read-only Kubernetes access. The signed evidence then requires
the original Challenge and independently pinned Observer root at the v2
PostgreSQL receipt method.

This does NOT provision a production dispatcher service, signer KMS,
external delivery protocol or trusted runtime; it does not conclude
`AUTHORIZED_AND_VERIFIED`. Production must independently pin signer
identity and ensure the signed ticket is delivered ONLY to a reviewed
isolated Observer workload; no open issuance endpoint is introduced.
