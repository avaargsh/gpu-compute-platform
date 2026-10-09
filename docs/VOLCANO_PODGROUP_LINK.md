# Stage B: UID-bound Volcano PodGroup linkage (unregistered)

Status: **experimental read-only evidence; NOT scheduler admission, quota application or production support**.

## Why this exists

A Pod carrying `scheduling.k8s.io/group-name=<job>-<job-uid>` is only a
claim. It does not prove that the named PodGroup exists, belongs to that Job,
or has not been replaced under the same name. The v1.15.3 controller uses
`NewControllerRef(job,...)` to create the PodGroup and copies the Job's
`minAvailable`/Queue into `spec.minMember`/`spec.queue`.

Pinned upstream: Volcano v1.15.3 commit
`d2dd7024f4af3070f9d99d68a78973893908a743`,
`pkg/controllers/job/job_controller_actions.go` lines 806–823:
https://github.com/volcano-sh/volcano/blob/d2dd7024f4af3070f9d99d68a78973893908a743/pkg/controllers/job/job_controller_actions.go#L806-L823

## Narrow observation contract

Only after independent `PodsReady=True`, the *unregistered* Volcano provider
reads `scheduling.volcano.sh/v1beta1` PodGroup by
`Namespace(job)/Name(job.Name + "-" + job.UID)`. The observer verifies:

- exact PodGroup API group/version, kind, namespace, and generated name;
- nonempty server-issued PodGroup UID and resourceVersion, no deletion;
- exactly one Controller OwnerReference to the observed Job GVK, name and UID;
- `spec.queue == job.spec.queue` and
  `spec.minMember == job.spec.minAvailable == 1` for the current 1/1 projector;
- a **second, independent GET** of that exact name with unchanged UID,
  resourceVersion and all identity/spec comparisons.

Only then is a separate `PodGroupLinked=True` condition emitted. Missing,
foreign-owned, deleting, changed, unversioned or unverifiable groups produce
`PodGroupLinked=Unknown`; transient read errors fail closed with retryability.

This is an observation of a stable controller-owned API object, **not**
cryptographic evidence, an atomic cross-object snapshot, or a guarantee that
the scheduler actually admitted/scheduled all gang members. No
`PodGroupLinked` condition is emitted when `PodsReady` has not been proved.
The canonical `Ready`, `Succeeded`, `QuotaApplied`, provider registration,
lease ownership and Kueue Golden Path are **unchanged**.

## Checks

```bash
go test ./internal/provider/volcano -run 'PodGroup|Pod.*Wire|PodReadiness' -count=1
go test -race ./internal/provider/volcano -count=1
make fmt-check
go vet ./...
make stage-b-volcano-contract
```

Adversarial tests cover wrong/missing UID or RV, Controller mismatch,
non-Controller references, wrong scope/queue/minMember, terminating group,
replacement with the same name, stale RV, readback NotFound, transport timeout
and read-only GET/GET ordering. Wire-level tests run against an HTTP
simulator; **they do not replace live kind+Volcano validation**.

Historical exact-SHA real kind+Volcano v1.15.3 proof:
https://github.com/avaargsh/temp-runner/actions/runs/37919203307
That run predates this slice and cannot establish that this changed observer
passed a live integration test. Require a newly pinned proof and exact-HEAD
Go race and Kueue regression gates before considering merge.

No real GPU, digest+cosign promotion, scheduler-applied quota or production
Volcano behavior has been proved by this observer.
