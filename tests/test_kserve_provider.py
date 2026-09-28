from app.control_plane.domain import (
    AcceleratorClass, ComputePoolRef, DeploymentSpec, ModelRevisionRef,
    RuntimeKind, ServingConfig,
)
from app.control_plane.serving_kserve import KServeBinding, KServeManifestBuilder


def test_kserve_is_provider_translation_not_public_domain():
    spec = DeploymentSpec(
        name="qwen-prod",
        model_revision=ModelRevisionRef(model="qwen", revision="r2"),
        compute_pool=ComputePoolRef(name="inference"),
        replicas=1,
        serving=ServingConfig(
            runtime=RuntimeKind.VLLM,
            accelerator=AcceleratorClass(name="h100", count=2),
            tensor_parallelism=2,
        ),
    )
    manifest = KServeManifestBuilder(
        KServeBinding(
            namespace="team-a",
            runtime_image_vllm="registry.example/vllm:0.1.0",
            runtime_image_sglang="registry.example/sglang:0.1.0",
        )
    ).build(spec)
    assert manifest["kind"] == "InferenceService"
    assert manifest["metadata"]["labels"]["compute.platform/compute-pool"] == "inference"
    assert "kserve" not in spec.model_dump_json().lower()
