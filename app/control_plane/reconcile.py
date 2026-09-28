"""Small provider-neutral reconciliation kernel."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Protocol

from app.control_plane.status import Condition, EvidenceRef, ObservedState, Phase


class ReconcileProvider(Protocol):
    async def apply(self, desired: Any) -> str: ...
    async def observe(self, provider_ref: str) -> dict[str, Any]: ...


@dataclass(frozen=True)
class ReconcileResult:
    provider_ref: str
    state: ObservedState


class Reconciler:
    def __init__(self, provider: ReconcileProvider):
        self.provider = provider

    async def reconcile(self, desired: Any, generation: int | None = None) -> ReconcileResult:
        try:
            provider_ref = await self.provider.apply(desired, generation=generation)
        except TypeError:
            provider_ref = await self.provider.apply(desired)
        raw = await self.provider.observe(provider_ref)
        phase = Phase(raw.get("phase", Phase.PENDING))
        ready = phase == Phase.READY
        condition = Condition(
            type="Ready",
            status=ready,
            reason=raw.get("reason", phase.value),
            message=raw.get("message", ""),
            observed_generation=generation,
        )
        evidence = [
            EvidenceRef.model_validate(item)
            for item in raw.get("evidence_refs", [])
        ]
        state = ObservedState(
            phase=phase,
            conditions=[condition],
            provider_ref=provider_ref,
            endpoint=raw.get("endpoint"),
            replicas_ready=raw.get("replicas_ready"),
            admitted=raw.get("admitted"),
            evidence_refs=evidence,
            provider_status=raw.get("provider_status", {}),
        )
        return ReconcileResult(provider_ref=provider_ref, state=state)
