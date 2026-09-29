from types import SimpleNamespace

import pytest

from app.control_plane.scheduler_reconcile import SchedulerReconcileProvider


class Scheduler:
    def __init__(self):
        self.cancelled = []

    async def cancel(self, ref):
        self.cancelled.append(ref)


@pytest.mark.asyncio
async def test_scheduler_reconcile_delete_delegates_to_cancel():
    scheduler = Scheduler()
    provider = SchedulerReconcileProvider(scheduler)
    await provider.delete("team-a/train-g1")
    assert scheduler.cancelled == ["team-a/train-g1"]
