# Stage B Volcano scheduler-status evidence: non-promoting experiment

Status: **unregistered, pure classifier only**. Production dispatch remains
Kueue; this code does not change provider output, admission, Ready or
QuotaApplied semantics.

The earlier v1.15.3 live PodGroup Controller UID/RV test proved the object
belongs to the Job. It did **not** prove the scheduler accepted the gang,
applied the specific Queue generation, or allocated GPU capacity. To avoid
collapsing these boundaries, `inspectPodGroupSchedulerEvidence` is an
isolated, read-only classifier for the Volcano `status.phase` and
`status.conditions[type=Scheduled]` vocabulary. It emits an internal
`podGroupSchedulerEvidence` structure, **not a domain Condition**.

A PodGroup in `Running` phase means enough Pods are running, and
`Scheduled=True` means an observed scheduler condition only. Even when
both are present, neither is a causal attestation of the Queue quota update
or a stable signal for the currently desired ComputePool generation.

## Fail-closed table

| Observed PodGroup | Internal classification | Production effect |
|---|---|---|
| UID/RV absent, deleting, no conditions | Unknown | None |
| Phase Running, no Scheduled condition | Unknown | None |
| Scheduled=True without conflicting Unschedulable=True | Scheduled True observed | None |
| Scheduled=True and Unschedulable=True | Unknown conflict | None |
| Scheduled=False | False observed | None |
| Duplicate/malformed Scheduled conditions | Unknown | None |
| Unknown or unsupported phase | Unknown | None |

## Evidence still needed before wiring it into the provider

1. Pin Volcano v1.15.3 and independently GET the same PodGroup twice with
   stable UID/RV, including the `status.conditions` projection.
2. Test the negative transition: PodGroup Pending/Inqueue/Unknown,
   Unschedulable=True, resource pressure, scheduling retry and terminating
   Pods. In particular, do not infer gang success from PodGroup Running alone.
3. Correlate against **the actual scheduler decision for the Job/Pod**, not
   just Controller owner annotations.
4. Separately establish a generation-bound quota application contract. No
   existing Volcano Queue status field supplies this proof automatically.
5. Do not add Volcano production registry, PodGroup writes, or loosen
   digest+cosign or GPU controlled-lab requirements.

Static pinned upstream API vocabulary:
https://github.com/volcano-sh/volcano/blob/d2dd7024f4af3070f9d99d68a78973893908a743/staging/src/volcano.sh/apis/pkg/apis/scheduling/v1beta1/types.go

Validation:

```bash
go test ./internal/provider/volcano -run 'SchedulerEvidence' -count=1
go test -race ./internal/provider/volcano -count=1
make fmt-check
make vet
```

Historical `temp-runner` live PodGroup identity proof:
https://github.com/avaargsh/temp-runner/actions/runs/38001250776 .
That receipt predates this classifier and must not be used as evidence that
it has been verified against actual `Scheduled` conditions.
