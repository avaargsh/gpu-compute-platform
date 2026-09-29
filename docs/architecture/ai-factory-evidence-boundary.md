# AI Factory Evidence Boundary

The compute control plane owns desired state, scheduling, reconciliation, and provider observations. It does **not** own commissioning or acceptance semantics.

Cross-repository evidence uses a stable reference:

```text
GPU Compute Platform Observation
  evidenceRefs:
    - aifactory://evidence/<bundleId>
                     |
                     v
AI Factory Engineering
  EvidenceBundle.metadata.bundleId
  provenance.workloadRef
  provenance.sourceRefs
                     |
                     v
AcceptanceDecision / AcceptanceArtifact
```

## Ownership

- GPU Compute Platform may attach an `aifactory://evidence/<bundleId>` reference to an Observation.
- AI Factory Engineering remains the owner of the EvidenceBundle schema, measurements, raw artifacts, acceptance tests, gate DAG, decision, attestation, and replay.
- The compute control plane must not copy AI Factory threshold or acceptance logic into reconciliation.
- AI Factory may use `provenance.workloadRef` for the portable workload identity and `sourceRefs` for provider objects such as Kubernetes Job, Pod, or Kueue Workload.

This boundary keeps scheduling truth and acceptance truth independently evolvable while giving both systems a replayable join key.
