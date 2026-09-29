"""Generic desired-state controller with deletion and retry semantics."""

from dataclasses import dataclass
from datetime import datetime, timezone

from app.control_plane.reconcile import Reconciler
from app.control_plane.resource_store import ResourceStore
from app.control_plane.reconcile_queue import ReconcileQueue
from app.control_plane.retry import RetryPolicy


@dataclass(frozen=True)
class ControllerResult:
    processed: bool
    retry_after: float | None = None
    error: str | None = None


class ResourceController:
    FINALIZER = "compute.platform/provider-cleanup"

    def __init__(self, store: ResourceStore, queue: ReconcileQueue, reconciler: Reconciler, retry_policy: RetryPolicy | None = None):
        self.store = store
        self.queue = queue
        self.reconciler = reconciler
        self.retry_policy = retry_policy or RetryPolicy()
        self.attempts: dict[str, int] = {}

    async def submit(self, key: str, desired):
        record = await self.store.put_desired(key, desired)
        if self.FINALIZER not in record.lifecycle.finalizers:
            record.lifecycle.finalizers.append(self.FINALIZER)
        await self.queue.enqueue(key)
        return record

    async def request_delete(self, key: str) -> None:
        record = await self.store.get(key)
        if record is None:
            return
        await self.store.mark_deleting(key, datetime.now(timezone.utc))
        await self.queue.enqueue(key)

    async def reconcile_one(self) -> ControllerResult:
        key = await self.queue.dequeue()
        if key is None:
            return ControllerResult(processed=False)
        record = await self.store.get(key)
        if record is None:
            return ControllerResult(processed=True)
        try:
            if record.lifecycle.deleting:
                provider_ref = record.observed.provider_ref if record.observed else None
                if provider_ref and hasattr(self.reconciler.provider, "delete"):
                    await self.reconciler.provider.delete(provider_ref)
                record.lifecycle.finalizers = [f for f in record.lifecycle.finalizers if f != self.FINALIZER]
                if not record.lifecycle.finalizers:
                    await self.store.delete(key)
                self.attempts.pop(key, None)
                return ControllerResult(processed=True)

            result = await self.reconciler.reconcile(record.desired, generation=record.generation)
            await self.store.put_observed(key, record.generation, result.state)
            self.attempts.pop(key, None)
            return ControllerResult(processed=True)
        except Exception as exc:
            attempts = self.attempts.get(key, 0) + 1
            self.attempts[key] = attempts
            await self.queue.enqueue(key)
            return ControllerResult(
                processed=True,
                retry_after=self.retry_policy.delay(attempts),
                error=str(exc),
            )
