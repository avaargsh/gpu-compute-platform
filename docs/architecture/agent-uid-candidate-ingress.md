# Candidate UID publication ingress (stacked on PR #143)

A separate `POST /api/v1/agent/workload-create-uid` is available only through
`AgentMTLSRouter`, which verifies the peer's SPIFFE cluster identity against
a trusted client certificate chain and stamps a private context marker. The
handler itself requires this marker, so a dev loopback client cannot bypass
the wrapper. Body cluster scope is validated against the verified certificate
before any store mutation.

Publication is candidate-only and delegates to `Store.RecordJobCreateUID`,
which revalidates immutable Management audit, current Desired and live Agent
lease. The endpoint never returns a signed or verified outcome. Database
errors are not reflected into client responses.

**Still missing:** connection from Agent Runner and the Kueue CREATE-only
response seam, and an independent observer with production RBAC and signature
verification. A valid Agent certificate authenticates the sender, **not**
whether its reported Job UID really came from Kubernetes. Protect this API
behind mTLS, never reuse it as a browser endpoint.

Related: #141, Draft #143, Draft #144, Draft #145.
