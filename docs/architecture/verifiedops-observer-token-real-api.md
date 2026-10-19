# Real Observer ServiceAccount bearer-token integration

The dedicated kind RBAC workflow now also creates a Kubernetes **TokenRequest**
for `system:serviceaccount:verifiedops-observer:verifiedops-observer` with a
bounded 10-minute lifetime. The Go acceptance constructs a fresh REST client
with **only** the cluster URL/CA and this bearer token. It never copies kind's
admin x509 certificate or impersonates the subject with admin credentials.

Using that actual token, the `verifiedops.JobObserver` GETs a newly created
real Job bound to the API-server-returned UID and confirms an unsigned
`OUTCOME_PENDING` result; a mismatched/replaced UID becomes
`OWNERSHIP_UNPROVEN`. It verifies its own Kubernetes username and exercises
actual API-server rejection for forbidden cross-namespace Job/Pod reads,
Secret/Job/Node LIST, Pod CREATE and Job DELETE. The admin checks that
the forbidden DELETE did not remove the Job.

The Job deliberately points to an unavailable pinned image digest: the
test does not pretend it observed a running GPU workload, a trusted ImageID
or a completed Job. Actual Ready/Node/ImageID attestation against live Kueue
GPU workloads remains a distinct acceptance gate.

A successful SA TokenRequest test is **NOT** production deployment, an
immutable operation ownership proof, DSSE provenance or release authorization.
Production requires a reviewed Pod identity/admission policy, projected
short-lived tokens (no long-lived bearer Secret), independent trust-root,
nonce/replay validation and durable UNKNOWN recovery.
