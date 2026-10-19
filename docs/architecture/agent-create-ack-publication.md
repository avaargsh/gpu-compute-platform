# Agent CREATE ACK → candidate persistence: integrated path

This branch stacks the still-unmerged work in #143 (durable immutable binding),
#144 (Kubernetes CREATE-only response), #145 (original Management request ID),
and #146 (SPIFFE/mTLS authenticated publish ingress).

The Agent pulls a server-derived original CREATE Operation ID alongside Desired.
While it holds the Workload reconcile lease, it passes an ephemeral callback
through the existing Runtime/Kueue adapter. Only when the API server returns
a successful Job CREATE ACK with nonempty UID/resourceVersion does Kueue invoke
that callback. It synchronously POSTs the candidate under Agent mTLS. The
server rechecks the immutable audit, project namespace/provider binding,
current Generation and unexpired lease before first-writer-wins persistence.

For GET convergence or AlreadyExists recovery the Kueue seam returns no UID
and MUST NOT call the sink. Kubernetes timeouts without CREATE ACK remain
UNKNOWN. Sink failure returns retryable error before Ready reporting; this
prevents false positive verified state but does not guarantee eventual receipt.
If the first CREATE succeeds but its ACK/publish is lost, a later GET must not
create a synthetic CREATE attribution.

A candidate table row is not proof of actual workload execution, signer
identity or authority to act: independent read-only observer, separate K8s
credentials, cryptographic DSSE verification, nonce/replay, and real kind
race/lease-expiry acceptance remain mandatory.

Only the Agent transport on production mTLS may publish. The browser and
Management runtime DB login cannot access the candidate table. Legacy local
dev operation with no original Management CREATE ID remains unbound.
