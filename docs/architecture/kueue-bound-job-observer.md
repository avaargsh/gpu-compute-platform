# GPU Verified Operations — read-only bound Job observer (candidate)

This package observes **one single-Pod Kubernetes Job**. It is not an
authorization service, scheduler, evidence signer or automatic mutator.

The caller must supply a **durably authorized, API-server-returned Job UID**
bound to the original operation, a generation, and a pinned image digest. The
observer never infers operation ownership from a same-name Job or matching
`ai.compute/generation` alone.

The `JobObserver` must use an independently configured Kubernetes service
account with only scoped GET/LIST for Jobs, Pods and GET for Nodes. No
Kubernetes write methods are called. Runtime RBAC and the identity boundary
still require cluster integration tests; an in-process fake does not prove them.

Readback checks: Job UID/RV, expected generation, batch/v1 controller
OwnerReference identity, fresh Pod GET and Pod UID/RV, Pod Ready and bound Node,
Node Ready/UID/RV, pinned container image and resolved ImageID digest, followed
by second Job/Pod GET identity confirmation and a final Pod LIST confirming
the original Pod is still the only matching controller-owned Pod. A Pod must have exactly one
controller OwnerReference matching `apiVersion=batch/v1`, `kind=Job`, the exact
Job name and UID. Labels, a matching UID with a different name, or duplicate
controller references are insufficient. Multiple owned Pods and mismatched or
absent evidence fail closed.

`POD_READY_OBSERVED` is **not** `AUTHORIZED_AND_VERIFIED`, a proof that
Kueue admitted the workload, the Job completed, or that a user was authorized.
It is also not a snapshot transaction: Kubernetes objects can change after
the last GET. A signature, signer trust root, replay store, authorization record,
operation-owned CREATE binding, deletion/finalizer observer and real API-server
negative experiments remain mandatory before promotion.

The historical Kueue `ApplyJob` same-generation adoption remains unchanged
for the provider's lost-ACK recovery contract. This read-only layer must never
convert that convergence into independent operation-ownership proof.

Test: `go test ./internal/verifiedops -count=1`
Tracking: #141, #116, GPUPanel #153, agent-control-plane #191.
