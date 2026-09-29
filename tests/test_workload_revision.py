import pytest

from app.control_plane.adapters.kueue import KueueBinding, KueueManifestBuilder
from app.control_plane.domain import AcceleratorRequest, ComputePoolRef, WorkloadKind, WorkloadSpec
from app.control_plane.resource_store import InMemoryResourceStore
from app.control_plane.status import ObservedState, Phase


def workload(image="trainer:v1"):
    return WorkloadSpec(
        name="train-qwen",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="training"),
        accelerator=AcceleratorRequest(class_name="h100", count=2),
        image=image,
    )


def test_kueue_job_name_and_labels_are_generation_scoped():
    manifest = KueueManifestBuilder(
        KueueBinding(namespace="team-a", local_queue="training", accelerator_resources={"h100": "nvidia.com/gpu"})
    ).build_job(workload(), generation=7)
    assert manifest["metadata"]["name"] == "train-qwen-g7"
    assert manifest["metadata"]["labels"]["compute.platform/workload"] == "train-qwen"
    assert manifest["metadata"]["labels"]["compute.platform/generation"] == "7"


@pytest.mark.asyncio
async def test_stale_generation_cannot_overwrite_new_desired_state():
    store = InMemoryResourceStore()
    first = await store.put_desired("workload/train-qwen", workload("trainer:v1"))
    second = await store.put_desired("workload/train-qwen", workload("trainer:v2"))
    await store.put_observed(
        "workload/train-qwen",
        first.generation,
        ObservedState(phase=Phase.READY, provider_ref="team-a/train-qwen-g1"),
    )
    current = await store.get("workload/train-qwen")
    assert current.generation == second.generation == 2
    assert current.observed is None
