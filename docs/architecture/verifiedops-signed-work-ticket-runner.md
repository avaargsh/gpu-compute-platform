# Verified Operations / independently approved Observer work

## What this code enforces

The read-only JobObserver now has a narrowly scoped signed work-ticket
admission and execution method:

1. A trusted dispatcher, **after** the database has issued a matching
   original-operation/Job-UID-bound challenge, supplies the expected Job and
   challenge in a `ObserverWorkTicket`.
2. `AuthorObserverWorkTicket` signs the ticket with an injected Ed25519
   `crypto.Signer`, using domain-separated DSSE payload type
   `application/vnd.gpupanel.observer-work+json`.
3. The Observer accepts only `ApproveObserverWorkTicket` results, verified
   using a separately pinned dispatcher public key. Unknown/duplicate JSON
   keys, wrong signature/keyid, unbounded TTL (over 2 minutes), expired
   timestamps, and mismatched operation/Job UID are denied.
4. `RunApprovedObserverWork` requests a Kubernetes
   `authentication.k8s.io/v1 SelfSubjectReview` using its **own**
   Kubernetes client and checks the exact
   `system:serviceaccount:verifiedops-observer:verifiedops-observer`
   subject before any Job/Pod/Node reads. It then calls the existing
   read-only Job/Pod/Node Observer, signs its actual evidence and rechecks
   the ticket expiry before returning the result.
5. The real kind TokenRequest acceptance uses the actual short-lived
   ServiceAccount bearer identity, signs a true `OUTCOME_PENDING` Job
   under a separate test-only dispatcher ticket, and verifies the receipt
   against a separate observer test key.

## Not production deployment

There is **no public ticket-minting endpoint** or automatic dispatcher signing
key. This code does not issue PostgreSQL challenges, expose an HTTP handler,
or mint credentials; both signing keys are injected test fixtures in CI.
A separate deployment must provide short-lived projected Kubernetes
ServiceAccount tokens, isolated signer material (prefer external KMS/HSM),
pinned dispatcher/Observer trust roots, authenticated challenge transport,
key rotation/revocation and admission policy for the signer Pod.

A valid ticket proves the dispatcher authorized a *read request*, not that
a workload succeeded; a signed observation is evidence, not a positive
authorization verdict. No `AUTHORIZED_AND_VERIFIED` result is emitted.

The ticket expiry is a second freshness bound alongside the separately
enforced PostgreSQL challenge TTL and nonce-receipt uniqueness. A duplicated
work ticket may produce repeat observations; the backend remains
responsible for transactionally rejecting altered or replayed evidence.


## Admission enforcement for the Observer namespace

The example policy bundle is
`deploy/kubernetes/verifiedops-observer/observer-admission-policy.example.yaml`.
It installs Kubernetes `ValidatingAdmissionPolicy` plus bindings scoped to
the `verifiedops-observer` namespace label. The Job policy pins the image
string and constrains the ServiceAccount, token-mount behavior, process
overrides, container security context, environment-variable sources, volume
sources, and read-only mount paths. A separate Pod policy prevents bypassing
the Job policy by creating a Pod directly.

**This is a non-production template.** Before installing in production, an
operator must replace the `example.invalid/...sha256:000...000` image in both
policy expressions with the reviewed immutable Observer image digest and
review the manifest. The policy deliberately fails closed when the submitted
image does not exactly match. Only trusted cluster administrators may create
or modify these cluster-scoped policies and bindings.

Run `bash scripts/e2e/verifiedops-observer-job-admission.sh` against a real
Kubernetes API server to verify policy installation, acceptance of the
suspended template, and rejection of an unsafe Job and a direct-Pod process
override. This acceptance does not prove production key isolation: RBAC must
also prevent untrusted users from changing the pinned trust-root ConfigMap,
ticket Secret, CSI provider configuration, policies, or bindings. The
operator must review those controls separately. The sample image remains
unusable and the Job remains suspended until an explicitly reviewed deployment.

The policy constrains admission-time workload shape; it is not a production
Dispatcher delivery adapter, does not establish trusted challenge transport,
and does not enable `AUTHORIZED_AND_VERIFIED`.
