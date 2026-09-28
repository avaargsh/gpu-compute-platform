"""Phase 0 workload lifecycle contract.

Locks the portable projection independently of Kueue implementation details:
PendingAdmission -> Admitted -> PodsReady -> Completed/Ready.
"""

import pytest

from app.control_plane.domain import AcceleratorRequest, ComputePoolRef, WorkloadKind, WorkloadSpec
from app.control_plane.providers import ProviderStatus
from app.control_plane.reconcile import Reconciler
from app.control_plane.scheduler_reconcile import SchedulerReconcileProvider
from app.control_plane.status import Phase


class SequenceScheduler:
    def __init__(self, statuses):
        self.statuses = iter(statuses)

    async def submit(self, workload, generation=None):
        return "golden/hello-g7"

    async def status(self, binding_id):
        return ProviderStatus(next(self.statuses))

    async def cancel(self, binding_id):
        pass


def condition(state, name):
    return next(item for item in state.conditions if item.type == name)


@pytest.mark.asyncio
async def test_phase0_workload_lifecycle_projection_is_generation_scoped():
    scheduler = SequenceScheduler([
        dict(provider="kueue", phase="pending", admitted=False, pods_ready=0),
        dict(provider="kueue", phase="pending", admitted=True, pods_ready=0),
        dict(provider="kueue", phase="running", admitted=True, pods_ready=1),
        dict(provider="kueue", phase="completed", admitted=True, pods_ready=0, succeeded=1),
    ])
    reconciler = Reconciler(SchedulerReconcileProvider(scheduler))

    desired = WorkloadSpec(
        name="hello",
        kind=WorkloadKind.BATCH,
        compute_pool=ComputePoolRef(name="cpu-golden"),
        accelerator=AcceleratorRequest(class_name="cpu", count=1),
        image="busybox:1.36",
        command=["sh", "-c", "echo golden-path"],
    )

    states = []
    for _ in range(4):
        result = await reconciler.reconcile(desired, generation=7)
        states.append(result.state)

    pending, admitted, pods_ready, completed = states

    assert pending.phase == Phase.PENDING
    assert condition(pending, "Admitted").status is False
    assert condition(pending, "Admitted").reason == "PendingAdmission"
    assert condition(pending, "PodsReady").status is False

    assert admitted.phase == Phase.PENDING
    assert condition(admitted, "Admitted").status is True
    assert condition(admitted, "PodsReady").status is False

    assert pods_ready.phase == Phase.PROGRESSING
    assert condition(pods_ready, "Admitted").status is True
    assert condition(pods_ready, "PodsReady").status is True
    assert pods_ready.replicas_ready == 1

    assert completed.phase == Phase.READY
    assert condition(completed, "Ready").status is True
    assert condition(completed, "Admitted").status is True
    # A completed short Job no longer needs a currently Ready Pod.
    assert condition(completed, "PodsReady").status is False
    assert completed.provider_status["succeeded"] == 1

    for state in states:
        assert state.observed_generation == 7
        assert all(item.observed_generation == 7 for item in state.conditions)
