# Stage B Volcano Job Pod readiness observation (unregistered)

Status: **code experiment; no live Volcano acceptance; not production registered**.

## User-facing capability

For an independently re-observed Volcano Job with controller phase `Running`,
the unregistered adapter now emits a separate `PodsReady` condition.
This distinguishes "Volcano reached minAvailable Running Pods" from
"Kubernetes actually reports the projected Pod Ready".

The current whole-GPU Stage B projector emits **exactly one Task with one Pod**.
The provider lists Pod resources through a read-only Kubernetes dynamic client
using `volcano.sh/job-name=<job-name>` in the Job namespace. The returned
candidate must carry:

- `apiVersion=v1`, `kind=Pod`, nonempty Pod UID, matching namespace;
- `volcano.sh/job-name`, `volcano.sh/job-namespace`,
  `volcano.sh/task-spec=workload` and `volcano.sh/task-index=0`;
- the `scheduling.k8s.io/group-name` annotation exactly equal to
  `<JobName>-<JobUID>` (the v1.15.3 Job-controller-generated PodGroup
  identity); **this verifies a link, not PodGroup existence or admission**;
- a *controller* ownerReference to the observed Job API version, kind,
  name and **Kubernetes Job UID**;
- no deletion timestamp; Pod phase `Running`; nonempty `spec.nodeName`;
- exactly one Kubernetes `status.conditions[type=Ready].status=True`;
- a **fresh, independent GET of that named Pod** with the same UID and
  nonempty, unchanged server resourceVersion, unchanged owner/labels and
  Ready condition. A LIST-only ready Pod is insufficient evidence.

This double-observation fence rejects a Pod deleted/recreated under the same
name, a Pod whose Ready status changed between LIST/GET, and ambiguous
readback. It proves only a stable observed snapshot, **not** a linearizable
snapshot of the Job+Pod and never a durable operation ownership attestation.

Any missing UID, absent/extra Pod, unmatched owner, or unreviewed task shape
returns `PodsReady=Unknown` rather than claiming ready. An owned but not
Ready/terminating Pod returns `PodsReady=False`. Read errors fail the
reconciliation (retryable if the Kubernetes failure is transient).

This condition is **observational only**. The Job's existing `Ready=False`
while `Running` is not promoted; the `Succeeded=True` condition still
requires `Completed`. Neither `PodsReady=True` nor an Open Queue proves
Volcano PodGroup/gang admission, scheduler-applied Queue quota, GPU operation
or execution ownership. Kueue remains the **only** production runtime adapter.

The implementation does not mutate Pods, create PodGroups, expand the adapter
registry, change placement, enable KAI/Volcano or modify v0.1 acceptance.

## Falsification checks

```bash
go test ./internal/provider/volcano -run 'Volcano.*Pod|PodReadiness' -count=1
go test -race ./internal/provider/volcano
make fmt-check
go vet ./...
make stage-b-volcano-contract
```

The authored synthetic tests exercise a UID-owned Ready Pod, label-selector
and namespace scoping, prohibited Pod writes, foreign job UID/controller,
wrong task, incorrect/missing task-index, incorrect/missing PodGroup link, missing Pod UID, Pending/no-node/non-Ready Pod, missing Job UID,
missing Pods, immutable Job task drift, duplicate Ready condition, and
transient list failure; additionally a vanished/recreated Pod on GET,
changed resourceVersion, stale Ready, foreign owner, terminating Pod, missing
server resourceVersion and transient Pod readback failure. All synthetic tests
require execution on the exact PR head before claiming they passed.

## Kubernetes REST transport test (HTTP simulator)

`TestVolcanoPodsReadyWireListThenReadback` exercises the **real client-go
dynamic HTTP transport** against an in-process `httptest.Server`, in addition
to the `dynamic/fake` tests. It checks that two Job GETs use the namespaced
Volcano Job URL, the Pod LIST carries the exact job-name label selector, the
follow-up GET targets that one Pod name, and no mutation occurs. An HTTP
readback that keeps the Pod name but replaces the UID must yield
`PodsReady=Unknown`. This is **not a Kubernetes API server**, and cannot
prove controller defaulting, scheduler admission or atomic multi-object
consistency. The test runs in the ordinary Go test suite without GPUs.

## Pinned upstream implementation evidence (static, not live acceptance)

For the **Volcano v1.15.3** annotated tag
(`d2dd7024f4af3070f9d99d68a78973893908a743`), the checked upstream
`pkg/controllers/job/job_controller_util.go` blob
(`66fae5dc75a6b4f216388badfbd0d8bb1b6fcea2`) constructs a Pod
with the Job controller ownerReference (lines 46-55), writes the
`scheduling.k8s.io/group-name` annotation as
`job.Name + "-" + job.UID` (lines 110-115), and assigns
`volcano.sh/task-index`, `job-name`, `task-spec` and `job-namespace`
labels (lines 143-152).

Immutable source:
https://github.com/volcano-sh/volcano/blob/d2dd7024f4af3070f9d99d68a78973893908a743/pkg/controllers/job/job_controller_util.go

These source facts justify stricter **unregistered** read-only identity
fences for the one-task/one-replica fixture. They do not prove that a
deployed Volcano controller follows this source, that the PodGroup exists,
or that any workload was admitted. Actual kind+Volcano v1.15.3 is still
required before promotion.

## Promotion/compatibility gates

1. Keep this PR stacked on #56; #56 itself is stacked on #55. No independent
   merge before both parents pass their code and API-server gates.
2. Verify Go 1.24 formatting, race/full test suites, fake-client list behavior,
   frozen v0.1 contracts and supply-chain check at the exact commit.
3. Run a disposable **kind + version-pinned Volcano** Job with an observed,
   controller-owned Pod. Save Pod JSON with UID/ownerReferences/conditions and
   the matching Job UID; verify Job `Running` alone remains `Ready=False`.
4. Falsify stale foreign-owner, owner UID replacement, unrelated selected
   Pods, and Pod delete/recreate. Never use a stale Pod result as a durable
   execution/ownership attestation.
5. Do not promote to `Ready=True` until an independently reviewed
   PodGroup/scheduler contract exists; do not promote `QuotaApplied` from
   `Unknown` without separate scheduler-level evidence.

Upstream evidence:
- https://volcano.sh/docs/concepts/volcanojob/
- https://volcano.sh/docs/userguide/user_guide_how_to_use_svc_plugin/
- https://github.com/volcano-sh/volcano/blob/master/docs/design/job-api.md
