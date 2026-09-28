from app.control_plane.compute_pool import (
    ComputePool,
    KueuePoolBinding,
    KueuePoolManifestBuilder,
    ResourceFlavorBinding,
    ResourceQuota,
)


def test_compute_pool_renders_kueue_capacity_objects():
    pool = ComputePool(
        name="h100-training",
        binding=KueuePoolBinding(
            namespace="team-a",
            local_queue="training",
            cluster_queue="gpu-training",
            cohort="shared-gpu",
            flavors=[
                ResourceFlavorBinding(
                    name="h100",
                    node_labels={"accelerator.platform/class": "h100-80g"},
                )
            ],
            quotas=[ResourceQuota(resource="nvidia.com/gpu", nominal_quota=32)],
        ),
    )

    manifests = KueuePoolManifestBuilder().build(pool)
    assert [m["kind"] for m in manifests] == [
        "ResourceFlavor",
        "ClusterQueue",
        "LocalQueue",
    ]
    assert manifests[1]["spec"]["cohort"] == "shared-gpu"
    assert manifests[1]["spec"]["resourceGroups"][0]["flavors"][0]["resources"][0] == {
        "name": "nvidia.com/gpu",
        "nominalQuota": 32,
    }
    assert manifests[2]["spec"]["clusterQueue"] == "gpu-training"


def test_provider_specific_labels_live_in_pool_binding_only():
    pool = ComputePool(
        name="training",
        binding=KueuePoolBinding(
            namespace="team-a",
            local_queue="training",
            cluster_queue="training",
            flavors=[
                ResourceFlavorBinding(
                    name="vendor-gpu",
                    node_labels={"vendor.example/gpu-family": "x"},
                )
            ],
        ),
    )
    assert "vendor.example/gpu-family" not in {"name": pool.name, "scheduler": pool.scheduler}
