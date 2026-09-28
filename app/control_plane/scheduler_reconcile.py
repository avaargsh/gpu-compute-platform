"""Adapter from SchedulerProvider to the generic reconciliation contract."""

from app.control_plane.domain import WorkloadSpec
from app.control_plane.providers import SchedulerProvider


class SchedulerReconcileProvider:
    def __init__(self, scheduler: SchedulerProvider):
        self.scheduler = scheduler
        self._refs: dict[str, str] = {}

    async def apply(self, desired: WorkloadSpec, generation: int | None = None) -> str:
        key = f"{desired.name}:g{generation}" if generation is not None else desired.name
        if key not in self._refs:
            try:
                self._refs[key] = await self.scheduler.submit(desired, generation=generation)
            except TypeError:
                self._refs[key] = await self.scheduler.submit(desired)
        return self._refs[key]

    async def observe(self, provider_ref: str) -> dict:
        status = await self.scheduler.status(provider_ref)
        phase = {
            "pending": "pending",
            "running": "progressing",
            "completed": "ready",
            "failed": "failed",
        }.get(status.get("phase", "pending"), "pending")
        return {
            "phase": phase,
            "admitted": status.get("phase") != "pending",
            "provider_status": dict(status),
        }
