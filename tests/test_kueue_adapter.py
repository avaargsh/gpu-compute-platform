from app.control_plane.domain import AcceleratorClass, ComputePoolRef, WorkloadKind, WorkloadSpec
from app.control_plane.adapters.kueue import KUEUE_QUEUE_LABEL, KueueBinding, KueueManifestBuilder


def test_kueue_job_uses_local_queue_and_gpu_request():
    workload = WorkloadSpec(
        name="train-qwen",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="training"),
        accelerator=AcceleratorClass(name="h100-80g", family="H100", count=8),
        image="trainer:v1",
        command=["python", "train.py"],
    )
    manifest = KueueManifestBuilder(
        KueueBinding(namespace="team-a", local_queue="training")
    ).build_job(workload)

    assert manifest["metadata"]["labels"][KUEUE_QUEUE_LABEL] == "training"
    assert manifest["metadata"]["namespace"] == "team-a"
    container = manifest["spec"]["template"]["spec"]["containers"][0]
    assert container["resources"]["requests"]["nvidia.com/gpu"] == "8"
    assert "nodeName" not in manifest["spec"]["template"]["spec"]
    assert "nodeSelector" not in manifest["spec"]["template"]["spec"]


def test_accelerator_resource_is_provider_binding_not_domain_field():
    workload = WorkloadSpec(
        name="train-alt",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="training"),
        accelerator=AcceleratorClass(name="accelerator", count=2),
        image="trainer:v1",
    )
    manifest = KueueManifestBuilder(
        KueueBinding(
            namespace="team-a",
            local_queue="training",
            accelerator_resource="vendor.example/accelerator",
        )
    ).build_job(workload)

    container = manifest["spec"]["template"]["spec"]["containers"][0]
    assert container["resources"]["requests"]["vendor.example/accelerator"] == "2"
    assert "vendor.example/accelerator" not in workload.model_dump_json()
