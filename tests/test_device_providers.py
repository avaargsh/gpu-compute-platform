from app.control_plane.adapters.dra import DRADeviceBinding, DRAResourceClaimBuilder
from app.control_plane.adapters.hami import HAMiDeviceBinding, HAMiResourceBuilder
from app.control_plane.domain import AcceleratorClass


def test_dra_claim_uses_resource_v1_and_device_class_binding():
    accelerator = AcceleratorClass(name="h100-80g", family="H100", count=4)
    claim = DRAResourceClaimBuilder(
        DRADeviceBinding(namespace="team-a", device_class_name="gpu.example/h100")
    ).build_claim("train-qwen-gpu", accelerator)

    assert claim["apiVersion"] == "resource.k8s.io/v1"
    request = claim["spec"]["devices"]["requests"][0]["exactly"]
    assert request["deviceClassName"] == "gpu.example/h100"
    assert request["allocationMode"] == "ExactCount"
    assert request["count"] == 4
    assert "gpu.example/h100" not in accelerator.model_dump_json()


def test_dra_pod_reference_is_separate_from_accelerator_intent():
    builder = DRAResourceClaimBuilder(
        DRADeviceBinding(namespace="team-a", device_class_name="gpu.example/h100")
    )
    pod_claim, container_claim = builder.pod_resource_claim("claim-a")
    assert pod_claim["resourceClaimName"] == "claim-a"
    assert container_claim == {"name": "accelerator"}


def test_hami_resource_names_remain_provider_binding():
    accelerator = AcceleratorClass(name="shared-gpu", count=1, dedicated=False)
    resources = HAMiResourceBuilder(
        HAMiDeviceBinding(
            resource_name="vendor.example/vgpu",
            memory_resource_name="vendor.example/vgpu-memory",
        )
    ).container_resources(accelerator, memory_mb=16384)

    assert resources["requests"]["vendor.example/vgpu"] == "1"
    assert resources["requests"]["vendor.example/vgpu-memory"] == "16384"
    assert "vendor.example" not in accelerator.model_dump_json()
