# Independent Kubernetes Observer identity (RBAC contract)

Verified Operations does **not** reuse the Agent's write-capable Kubernetes
client or the Management API's database principal. Its observer identity is
`system:serviceaccount:verifiedops-observer:verifiedops-observer`, with
`automountServiceAccountToken: false` by default.

## Reviewed installation

```bash
kubectl apply -f deploy/kubernetes/verifiedops-observer/rbac.yaml
bash scripts/e2e/verifiedops-observer-project-rbac.sh <existing-project-namespace>
```

Run the second command separately for every *reviewed* project namespace.
It rejects `default`, Kubernetes system namespaces and the observer namespace,
and does not create project namespaces.

The dedicated namespace SA is limited to `get nodes` (cluster-scoped, because
a Pod's assigned Node is not predetermined); each project Role grants exactly
`get jobs`, `get pods`, `list pods` **in that namespace only**. No Pod/Job
writes, Namespace reads, secrets, exec/log, Kueue writes, watch, node LIST,
role/rolebinding management, or impersonation. The binder is a privileged
administrator operation; do not give Agent or browser users RBAC mutation.

A reviewed Observer deployment must explicitly mount a projected, bounded
`serviceAccountToken` and use that credential with `JobObserver`.
The manifest intentionally creates no Observer Deployment or public API.
`automountServiceAccountToken: false` prevents accidentally attaching its
identity to an arbitrary Pod. Admission policies still must prevent untrusted
users in the observer namespace from creating Pods that select the SA.

## Actual API-server acceptance

```bash
bash scripts/e2e/verifiedops-observer-rbac-acceptance.sh
```

The dedicated GitHub workflow provisions real kind v1.34 and checks Kubernetes
SelfSubjectAccessReview authorizations through admin impersonation (it does not
generate an SA token). It asserts required Job/Pod/Node reads and negative
Secret, Pod exec/log, project B, cluster/system namespace, mutation, RBAC
escalation and role impersonation. This is a **real RBAC admission contract**,
not proof that an Observer process has deployed or independently observed the
CREATE-bound UID. Do not issue `AUTHORIZED_AND_VERIFIED` from this result.

Next: run `JobObserver` using the short-lived SA token on a real Kueue Job
and compare the server-side candidate UID; deliver a signed in-toto DSSE
statement with trust-root, nonce/replay, operation and scope verification.
Tracking: gpu-compute-platform #141; agent-control-plane #191.
