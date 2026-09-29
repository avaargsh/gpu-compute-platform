"""Provider event bridge: resource events only enqueue reconcile keys."""

from app.control_plane.reconcile_queue import ReconcileQueue


class ProviderEventBridge:
    def __init__(self, queue: ReconcileQueue):
        self.queue = queue

    async def on_event(self, resource_key: str) -> None:
        await self.queue.enqueue(resource_key)
