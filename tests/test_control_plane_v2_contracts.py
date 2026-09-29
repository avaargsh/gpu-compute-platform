from app.control_plane.compute_pool import (
    AcceleratorBinding,
    DeviceBinding,
    PlacementBinding,
    QuotaBinding,
)
from app.control_plane.status import AllocationObservation, ObservedState, Phase


def test_accelerator_binding_separates_portable_intent_from_provider_details():
    binding = AcceleratorBinding(
        quota=QuotaBinding(resource="gpu-memory", flavor="a100"),
        device=DeviceBinding(
            mode="hami",
            hami_memory_resource_name="nvidia.com/gpumem",
            memory_mb=40960,
        ),
        placement=PlacementBinding(scheduler="default"),
    )

    assert binding.quota.resource == "gpu-memory"
    assert binding.device.mode == "hami"
    assert binding.device.memory_mb == 40960
    assert binding.placement.scheduler == "default"


def test_observed_state_keeps_allocation_out_of_core_conditions():
    state = ObservedState(
        phase=Phase.PROGRESSING,
        observed_generation=3,
        reconcile_revision=11,
        allocation=AllocationObservation(
            state="Allocated",
            path="dra",
            isolation="exclusive",
            claims=["claim-a"],
        ),
    )

    assert state.observed_generation == 3
    assert state.reconcile_revision == 11
    assert state.allocation.path == "dra"
    assert state.conditions == []
