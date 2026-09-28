"""Generic desired-state controller."""

from app.control_plane.reconcile import Reconciler
from app.control_plane.resource_store import ResourceStore
from app.control_plane.reconcile_queue import ReconcileQueue


class ResourceController:
    def __init__(self, store: ResourceStore, queue: ReconcileQueue, reconciler: Reconciler):
        self.store = store
        self.queue = queue
        self.reconciler = reconciler

    async def submit(self, key: str, desired):
        record = await self.store.put_desired(key, desired)
        await self.queue.enqueue(key)
        return record

    async def reconcile_one(self) -> bool:
        key = await self.queue.dequeue()
        if key is None:
            return False
        record = await self.store.get(key)
        if record is None:
            return True
        result = await self.reconciler.reconcile(record.desired, generation=record.generation)
        await self.store.put_observed(key, record.generation, result.state)
        return True
