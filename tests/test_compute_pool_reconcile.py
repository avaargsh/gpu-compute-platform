import pytest

from app.control_plane.compute_pool import ComputePool, KueuePoolBinding, ResourceFlavorBinding, ResourceQuota
from app.control_plane.compute_pool_reconcile import ComputePoolReconciler


class NotFound(Exception):
    status = 404


class FakeCustomObjectsClient:
    def __init__(self):
        self.cluster = {}
        self.namespaced = {}

    def get_cluster_custom_object(self, group, version, plural, name):
        key = (plural, name)
        if key not in self.cluster:
            raise NotFound()
        return self.cluster[key]

    def create_cluster_custom_object(self, group, version, plural, body):
        key = (plural, body["metadata"]["name"])
        self.cluster[key] = body
        return body

    def get_namespaced_custom_object(self, group, version, namespace, plural, name):
        key = (namespace, plural, name)
        if key not in self.namespaced:
            raise NotFound()
        return self.namespaced[key]

    def create_namespaced_custom_object(self, group, version, namespace, plural, body):
        key = (namespace, plural, body["metadata"]["name"])
        self.namespaced[key] = body
        return body


def pool():
    return ComputePool(
        name="h100-training",
        binding=KueuePoolBinding(
            namespace="team-a",
            local_queue="training",
            cluster_queue="gpu-training",
            flavors=[ResourceFlavorBinding(
                name="h100",
                accelerator_class="h100-80gb",
                resource_name="nvidia.com/gpu",
                node_labels={"accelerator.platform/class": "h100-80gb"},
            )],
            quotas=[ResourceQuota(resource="nvidia.com/gpu", nominal_quota=32)],
        ),
    )


@pytest.mark.asyncio
async def test_compute_pool_reconcile_is_idempotent_and_ready():
    client = FakeCustomObjectsClient()
    reconciler = ComputePoolReconciler(client)
    first = await reconciler.reconcile(pool(), generation=1)
    second = await reconciler.reconcile(pool(), generation=1)

    assert first.phase.value == "ready"
    assert second.phase.value == "ready"
    assert ("resourceflavors", "h100") in client.cluster
    assert ("clusterqueues", "gpu-training") in client.cluster
    assert ("team-a", "localqueues", "training") in client.namespaced
