from app.control_plane.domain import (
    AcceleratorClass,
    AcceleratorRequest,
    ComputePoolRef,
    DeploymentSpec,
    ModelRevisionRef,
    RuntimeKind,
    ServingConfig,
    WorkloadKind,
    WorkloadSpec,
)


def test_workload_intent_contains_no_physical_gpu_identity():
    spec = WorkloadSpec(
        name="train-qwen",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="training"),
        accelerator=AcceleratorRequest(class_name="h100-80g", count=8),
        image="trainer:v1",
    )
    payload = spec.model_dump()
    assert "gpu_ids" not in payload
    assert "node_name" not in payload
    assert payload["accelerator"]["count"] == 8


def test_serving_contract_separates_model_runtime_and_capacity():
    spec = DeploymentSpec(
        name="qwen-prod",
        model_revision=ModelRevisionRef(model="qwen", revision="2026-09-28"),
        compute_pool=ComputePoolRef(name="inference"),
        serving=ServingConfig(
            runtime=RuntimeKind.VLLM,
            accelerator=AcceleratorClass(name="h100-80g", count=2),
            tensor_parallelism=2,
        ),
    )
    assert spec.model_revision.revision == "2026-09-28"
    assert spec.serving.runtime == RuntimeKind.VLLM
    assert spec.compute_pool.name == "inference"
