# AI Compute Control Plane v2 — Target Architecture

Status: **target architecture, not current feature inventory**.

For the implemented Go system, read
[../CONTROL_PLANE_V2_GO.md](../CONTROL_PLANE_V2_GO.md).

## Purpose

Evolve the project into a Kubernetes-native AI compute control plane without
reimplementing scheduler, device-allocation, serving-controller or runtime
mechanics.

The platform should own portable intent, lifecycle, policy, evidence and
provider selection. Specialized upstream systems should own their native
execution algorithms.

## Current implemented core

```text
Project
  -> ComputePool
  -> Workload
  -> ClusterBinding(provider=kueue)
  -> Cluster Agent
  -> Kueue adapter
```

Only Kueue is a registered execution provider today.

## Long-term domain direction

Potential future objects:

```text
Tenant / Project
      |
  ComputePool -------- AcceleratorClass
      |
   Workload
      |
    Model -------- ModelRevision
      |
 ServingConfig
      |
  Deployment -------- Endpoint
```

Model/ServingConfig/Deployment/Endpoint are not current product claims.

## Long-term provider decomposition

The architecture may eventually separate:

```text
Scheduler provider
Device-allocation provider
Serving provider
Runtime provider
```

Candidate technologies include Kueue, Volcano, KAI, Kubernetes DRA, HAMi,
KServe, llm-d, vLLM and SGLang.

This is a decomposition guide, not a list of implemented adapters.

Stage B currently has one generic execution `provider.Adapter` boundary for the
existing ComputePool + Workload lifecycle. Do not prematurely create four
independent plugin frameworks before real use cases require them.

## Ownership rule

The platform owns:

- portable desired state;
- explicit placement/provider bindings;
- lifecycle/finalization;
- normalized observation/evidence;
- policy and future economics/SLO surfaces.

The platform delegates:

- queue admission and quota borrowing;
- gang/topology scheduling;
- physical device allocation;
- workload-controller mechanics;
- inference routing/replica mechanics.

## Compatibility rule

Provider-native fields remain inside provider bindings/implementations. Public
portable intent must not contain physical GPU IDs, selected nodes or arbitrary
scheduler internals.

## DRA direction

Kubernetes DRA is a future device-allocation option, not a current execution
path.

Current Stage B only reports whether the DRA API is visible:

```text
draApiAvailable
draApiVersion
```

That fact does not imply a driver, DeviceClass or accepted allocation path.

If DRA is implemented later, ResourceClaim lifecycle must reuse the current
generation, lease, observation, evidence and deletion/finalization contract.

## Evolution rule

Use:

```text
Adopt -> Integrate -> Contribute upstream -> Build
```

A new provider is justified only when the existing supported adapter cannot
cover a validated workload requirement and the candidate can conform to the
existing recovery model.
