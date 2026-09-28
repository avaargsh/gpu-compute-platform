import pytest

from app.control_plane.controller import ResourceController
from app.control_plane.reconcile import Reconciler
from app.control_plane.reconcile_queue import InMemoryReconcileQueue
from app.control_plane.resource_store import InMemoryResourceStore
from app.control_plane.status import Phase


class FakeProvider:
    async def apply(self, desired):
        return f"jobs/{desired['name']}"

    async def observe(self, provider_ref):
        return {"phase": "ready", "admitted": True, "provider_status": {"ref": provider_ref}}


@pytest.mark.asyncio
async def test_golden_control_loop_persists_observed_state():
    store = InMemoryResourceStore()
    queue = InMemoryReconcileQueue()
    controller = ResourceController(store, queue, Reconciler(FakeProvider()))

    record = await controller.submit("workload/team-a/train-1", {"name": "train-1"})
    assert record.generation == 1
    assert await controller.reconcile_one() is True

    current = await store.get("workload/team-a/train-1")
    assert current.observed.phase == Phase.READY
    assert current.observed.admitted is True
    assert current.observed.conditions[0].observed_generation == 1


@pytest.mark.asyncio
async def test_new_desired_state_increments_generation_and_requeues():
    store = InMemoryResourceStore()
    queue = InMemoryReconcileQueue()
    controller = ResourceController(store, queue, Reconciler(FakeProvider()))
    await controller.submit("w/1", {"name": "a", "replicas": 1})
    updated = await controller.submit("w/1", {"name": "a", "replicas": 2})
    assert updated.generation == 2
    assert await queue.dequeue() == "w/1"
    assert await queue.dequeue() is None


@pytest.mark.asyncio
async def test_stale_observed_write_is_ignored():
    store = InMemoryResourceStore()
    first = await store.put_desired("w/1", {"v": 1})
    from app.control_plane.status import ObservedState
    await store.put_desired("w/1", {"v": 2})
    await store.put_observed("w/1", first.generation, ObservedState(phase=Phase.READY))
    current = await store.get("w/1")
    assert current.generation == 2
    assert current.observed is None
