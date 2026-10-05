# Workload Accelerator Lifecycle

The current accepted workload path is:

```text
Portable Workload Intent
        |
        v
Kueue quota/admission
        |
        v
Kubernetes Job
        |
        v
extended-resource request
        |
        v
Pod scheduled / ready
        |
        v
provider observation + evidence
        |
        v
delete Job / observe gone
        |
        v
final observation
        |
        v
finalize + tombstone
```

## Current ownership boundary

The control plane owns portable intent, desired generation, provider binding,
lifecycle/finalization and durable evidence references.

The Cluster Agent owns provider side effects and independent observation.

Kueue owns quota reservation/admission. Kubernetes and the accelerator stack own
concrete scheduling/device assignment.

## Current allocation mode

Today the accepted path uses extended resources.

Example:

```text
portable class: h100-80g
    -> AcceleratorBinding
       resourceName: nvidia.com/gpu
       flavor: kueue-h100
    -> Job resources.requests[nvidia.com/gpu]
```

The public Workload does not request a physical GPU ID.

## Evidence contract

Current extended-resource evidence includes stable provider pointers such as:

```text
k8s://<cluster>/namespaces/<ns>/jobs/<job>
kueue://<cluster>/namespaces/<ns>/workloads/<workload>
```

Evidence is an audit pointer. It does not mean the provider object still exists.

## Lifecycle invariants

1. Workload execution intent is immutable while active.
2. Identical same-generation requests are idempotent.
3. Changed active workloads use Delete -> Cleanup -> Finalize -> Recreate.
4. Provider object identity is deterministic.
5. Provider-created execution objects carry desired generation.
6. Same-generation replay adopts owned objects.
7. Conflicting generations fail closed.
8. Deletion is replay-safe and observe-until-gone.
9. Final deletion evidence is reported before finalization.
10. Failed report/finalize operations replay safely.
11. ComputePool cleanup waits for dependent Workloads.
12. Shared pool resources are not garbage-collected without explicit ownership.
13. Observation/finalization are fenced by current reconcile-lease ownership.

## Future DRA path

DRA is **not** part of the current accepted workload lifecycle.

A future adapter may translate portable accelerator intent into:

```text
DeviceClass
  -> ResourceClaim
  -> Pod resourceClaim reference
```

If implemented, ResourceClaim must remain provider-owned lifecycle state and fit
the existing contract:

- deterministic identity;
- generation marker;
- create/adopt;
- independent observation;
- lease-fenced publication;
- Job cleanup before claim cleanup where required;
- observe all workload-owned provider resources gone before finalization.

DRA must not create a second public workload model or a second recovery state
machine.

HAMi/fractional allocation has the same rule: implementation may change provider
projection, not management-plane lifecycle semantics.
