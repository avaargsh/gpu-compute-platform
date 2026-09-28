import pytest

from app.control_plane.controller import ResourceController
from app.control_plane.provider_events import ProviderEventBridge
from app.control_plane.reconcile import Reconciler
from app.control_plane.reconcile_queue import InMemoryReconcileQueue
from app.control_plane.resource_store import InMemoryResourceStore
from app.control_plane.retry import RetryPolicy


class Provider:
    def __init__(self, fail=False):
        self.fail = fail
        self.deleted = []

    async def apply(self, desired):
        if self.fail:
            raise RuntimeError("temporary provider failure")
        return "ns/item"

    async def observe(self, ref):
        return {"phase": "ready"}

    async def delete(self, ref):
        self.deleted.append(ref)


@pytest.mark.asyncio
async def test_failure_requeues_with_bounded_backoff():
    store, queue = InMemoryResourceStore(), InMemoryReconcileQueue()
    controller = ResourceController(
        store, queue, Reconciler(Provider(fail=True)),
        RetryPolicy(base_seconds=2, max_seconds=5),
    )
    await controller.submit("w/1", {"name": "item"})
    first = await controller.reconcile_one()
    second = await controller.reconcile_one()
    third = await controller.reconcile_one()
    assert [first.retry_after, second.retry_after, third.retry_after] == [2, 4, 5]


@pytest.mark.asyncio
async def test_delete_waits_for_provider_cleanup_then_removes_record():
    store, queue, provider = InMemoryResourceStore(), InMemoryReconcileQueue(), Provider()
    controller = ResourceController(store, queue, Reconciler(provider))
    await controller.submit("w/1", {"name": "item"})
    await controller.reconcile_one()
    await controller.request_delete("w/1")
    await controller.reconcile_one()
    assert provider.deleted == ["ns/item"]
    assert await store.get("w/1") is None


@pytest.mark.asyncio
async def test_provider_event_only_requeues_resource():
    queue = InMemoryReconcileQueue()
    bridge = ProviderEventBridge(queue)
    await bridge.on_event("w/1")
    await bridge.on_event("w/1")
    assert await queue.dequeue() == "w/1"
    assert await queue.dequeue() is None
