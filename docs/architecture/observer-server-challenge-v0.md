# Server-issued signed Observer challenge admission — v0

This is the **internal** issuance/consumption layer, not a public endpoint.

- A trusted Control Plane caller requests `IssueObserverChallenge` with a
  precise cluster/project/Workload/generation/Operation ID/Job UID scope.
- PostgreSQL independently requires that original immutable Management CREATE
  audit and an actual Agent-reported Job CREATE response candidate existed.
- A current nondeleting Desired Workload of the same generation and project
  is required at issuance AND at evidence consumption, fenced by row locks.
- The server generates 32 bytes with `crypto/rand` and stores a random 64-char
  lowercase hex nonce with PostgreSQL `issued_at` and `expires_at` (2 min).
- `RecordSignedObserverNonce` only accepts an independently trusted/pinned
  Ed25519-signed in-toto Statement whose scope matches that exact challenge,
  whose signed observation timestamp is fresh and not before the challenge,
  and whose challenge is still unexpired according to PostgreSQL's clock.
  It locks the challenge row and writes the nonce receipt in one transaction.
- A repeated **identical** envelope remains an idempotent acknowledgement
  during validity; a different signer/envelope or cross-operation use of the
  nonce fails closed.

**Not implemented:** external challenge delivery to an independent Observer,
signer key issuance/rotation/revocation, independent Job/Pod/Node observation
serialization or digest binding, fully atomic operation ownership proof,
network partition behavior, end-user verdict. This interface must remain
internal: exposing generic challenge issuance to an authenticated Agent would
weaken the independence of execution verification. The caller's scope must be
a trusted backend decision, not echoed from client input. Reject all attempts
to derive `AUTHORIZED_AND_VERIFIED` solely from successful challenge receipt.
