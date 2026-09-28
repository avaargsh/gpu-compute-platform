import pytest

from app.control_plane.domain import AcceleratorClass, ComputePoolRef, WorkloadKind, WorkloadSpec
from app.control_plane.providers import ProviderStatus
from app.control_plane.scheduler_reconcile import SchedulerReconcileProvider


class FakeScheduler:
    def __init__(self):
        self.submits = 0

    async def submit(self, workload):
        self.submits += 1
        return "team-a/train"

    async def status(self, binding_id):
        return ProviderStatus(provider="kueue", binding_id=binding_id, phase="running")

    async def cancel(self, binding_id):
        pass


def workload():
    return WorkloadSpec(
        name="train",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="gpu"),
        accelerator=AcceleratorClass(name="h100", count=8),
        image="registry.example/train:1.0.0",
        command=["python", "train.py"],
    )


@pytest.mark.asyncio
async def test_scheduler_adapter_normalizes_running_to_progressing():
    scheduler = FakeScheduler()
    provider = SchedulerReconcileProvider(scheduler)
    ref = await provider.apply(workload())
    observed = await provider.observe(ref)
    assert observed["phase"] == "progressing"
    assert observed["admitted"] is True


@pytest.mark.asyncio
async def test_scheduler_apply_is_idempotent_within_provider_instance():
    scheduler = FakeScheduler()
    provider = SchedulerReconcileProvider(scheduler)
    await provider.apply(workload())
    await provider.apply(workload())
    assert scheduler.submits == 1
