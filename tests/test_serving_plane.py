from app.control_plane.domain import (
    AcceleratorClass, ComputePoolRef, DeploymentSpec, ModelRevisionRef,
    RuntimeKind, ServingConfig, ServingRole, ServingTopology, WorkerPoolSpec,
)
from app.control_plane.runtime_openai import OpenAICompatibleRuntimeProvider
from app.control_plane.serving_native import (
    NativeKubernetesManifestBuilder, NativeKubernetesServingBinding,
)


def deployment(runtime=RuntimeKind.VLLM):
    return DeploymentSpec(
        name="qwen-prod",
        model_revision=ModelRevisionRef(model="qwen", revision="r1"),
        compute_pool=ComputePoolRef(name="inference"),
        replicas=2,
        serving=ServingConfig(
            runtime=runtime,
            accelerator=AcceleratorClass(name="h100-80g", count=2),
            tensor_parallelism=2,
        ),
    )


def test_runtime_provider_builds_vllm_command():
    runtime = OpenAICompatibleRuntimeProvider().build_runtime(deployment())
    assert runtime["command"] == [
        "vllm", "serve", "qwen:r1", "--tensor-parallel-size", "2"
    ]
    assert runtime["api"] == "openai"


def test_native_serving_builds_deployment_and_service():
    manifests = NativeKubernetesManifestBuilder(
        NativeKubernetesServingBinding(
            namespace="team-a",
            accelerator_resource="vendor.example/gpu",
            image_vllm="registry.example/vllm:0.1.0",
            image_sglang="registry.example/sglang:0.1.0",
        )
    ).build(deployment())
    deploy, service = manifests
    assert deploy["kind"] == "Deployment"
    assert service["kind"] == "Service"
    assert deploy["spec"]["replicas"] == 2
    container = deploy["spec"]["template"]["spec"]["containers"][0]
    assert container["image"] == "registry.example/vllm:0.1.0"
    assert container["resources"]["requests"]["vendor.example/gpu"] == "2"
    assert "nodeSelector" not in deploy["spec"]["template"]["spec"]


def test_sglang_runtime_is_swappable_without_changing_deployment_contract():
    spec = deployment(RuntimeKind.SGLANG)
    runtime = OpenAICompatibleRuntimeProvider().build_runtime(spec)
    assert "sglang.launch_server" in runtime["command"]
    assert spec.model_revision.model == "qwen"


def test_native_serving_expands_prefill_decode_worker_pools():
    spec = deployment()
    spec.serving.topology = ServingTopology(worker_pools=[
        WorkerPoolSpec(role=ServingRole.PREFILL, accelerator=AcceleratorClass(name="h100", count=4), replicas=2, tensor_parallelism=4),
        WorkerPoolSpec(role=ServingRole.DECODE, accelerator=AcceleratorClass(name="h100", count=2), replicas=8, tensor_parallelism=2),
    ])
    manifests = NativeKubernetesManifestBuilder(
        NativeKubernetesServingBinding(
            namespace="team-a",
            accelerator_resource="nvidia.com/gpu",
            image_vllm="registry.example/vllm:0.1.0",
            image_sglang="registry.example/sglang:0.1.0",
        )
    ).build(spec)
    deployments = [m for m in manifests if m["kind"] == "Deployment"]
    assert [(m["metadata"]["name"], m["spec"]["replicas"]) for m in deployments] == [
        ("qwen-prod-prefill", 2),
        ("qwen-prod-decode", 8),
    ]
