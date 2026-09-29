import pytest
from pydantic import ValidationError

from app.control_plane.domain import (
    AcceleratorClass, ComputePoolRef, DeploymentSpec, ModelRevisionRef,
    RuntimeKind, ServingConfig, ServingRole, ServingTopology, WorkerPoolSpec,
)
from app.control_plane.serving_topology import ServingTopologyResolver


def gpu(count=1):
    return AcceleratorClass(name="h100-80g", count=count)


def deployment(topology=None):
    return DeploymentSpec(
        name="qwen",
        model_revision=ModelRevisionRef(model="qwen", revision="r1"),
        compute_pool=ComputePoolRef(name="inference"),
        replicas=2,
        serving=ServingConfig(
            runtime=RuntimeKind.VLLM,
            accelerator=gpu(2),
            tensor_parallelism=2,
            topology=topology,
        ),
    )


def test_default_topology_remains_unified():
    pools = ServingTopologyResolver().resolve(deployment())
    assert len(pools) == 1
    assert pools[0].role == ServingRole.UNIFIED
    assert pools[0].replicas == 2


def test_prefill_decode_can_scale_and_size_independently():
    topology = ServingTopology(worker_pools=[
        WorkerPoolSpec(role=ServingRole.PREFILL, accelerator=gpu(4), replicas=2, tensor_parallelism=4),
        WorkerPoolSpec(role=ServingRole.DECODE, accelerator=gpu(2), replicas=8, tensor_parallelism=2),
    ])
    pools = ServingTopologyResolver().resolve(deployment(topology))
    assert [(p.role, p.replicas, p.accelerator_count) for p in pools] == [
        (ServingRole.PREFILL, 2, 4),
        (ServingRole.DECODE, 8, 2),
    ]


def test_disaggregated_topology_requires_both_roles():
    with pytest.raises(ValidationError):
        ServingTopology(worker_pools=[
            WorkerPoolSpec(role=ServingRole.PREFILL, accelerator=gpu(), replicas=1),
        ])
