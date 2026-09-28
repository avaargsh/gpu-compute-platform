"""Small provider-neutral reconciliation kernel."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Protocol

from app.control_plane.status import Condition, EvidenceRef, ObservedState, Phase


class ReconcileProvider(Protocol):
    async def apply(self, desired: Any, generation: int | None = None) -> str: ...
    async def observe(self, provider_ref: str) -> dict[str, Any]: ...


@dataclass(frozen=True)
class ReconcileResult:
    provider_ref: str
    state: ObservedState


class Reconciler:
    def __init__(self, provider: ReconcileProvider):
        self.provider = provider

    async def reconcile(self, desired: Any, generation: int | None = None) -> ReconcileResult:
        provider_ref = await self.provider.apply(desired, generation=generation)
        try:
            raw = await self.provider.observe(provider_ref)
        except Exception as exc:
            state = ObservedState(
                phase=Phase.FAILED,
                observed_generation=generation,
                conditions=[
                    Condition(
                        type="ReconcileFailed",
                        status=True,
                        reason=type(exc).__name__,
                        message=str(exc),
                        observed_generation=generation,
                    ),
                    Condition(
                        type="Ready",
                        status=False,
                        reason="ReconcileFailed",
                        observed_generation=generation,
                    ),
                ],
                provider_ref=provider_ref,
                evidence_refs=[
                    EvidenceRef(
                        kind="reconcile-error",
                        uri=f"control-plane://reconcile/{provider_ref}",
                    )
                ],
                provider_status={"error_type": type(exc).__name__},
            )
            return ReconcileResult(provider_ref=provider_ref, state=state)

        phase = Phase(raw.get("phase", Phase.PENDING))
        conditions = [
            Condition(
                type="Ready",
                status=phase == Phase.READY,
                reason=raw.get("reason", phase.value),
                message=raw.get("message", ""),
                observed_generation=generation,
            )
        ]
        if raw.get("admitted") is not None:
            admitted = bool(raw["admitted"])
            conditions.append(
                Condition(
                    type="Admitted",
                    status=admitted,
                    reason="Admitted" if admitted else "PendingAdmission",
                    observed_generation=generation,
                )
            )
        if raw.get("replicas_ready") is not None:
            pods_ready = int(raw["replicas_ready"] or 0)
            conditions.append(
                Condition(
                    type="PodsReady",
                    status=pods_ready > 0,
                    reason="PodsReady" if pods_ready > 0 else "WaitingForPods",
                    observed_generation=generation,
                )
            )

        state = ObservedState(
            phase=phase,
            observed_generation=generation,
            conditions=conditions,
            provider_ref=provider_ref,
            endpoint=raw.get("endpoint"),
            replicas_ready=raw.get("replicas_ready"),
            admitted=raw.get("admitted"),
            evidence_refs=[
                EvidenceRef.model_validate(item)
                for item in raw.get("evidence_refs", [])
            ],
            provider_status=raw.get("provider_status", {}),
        )
        return ReconcileResult(provider_ref=provider_ref, state=state)
