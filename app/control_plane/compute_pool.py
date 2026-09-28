"""ComputePool scheduling bindings and Kueue resource manifests."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class ResourceQuota(BaseModel):
    resource: str
    nominal_quota: int = Field(ge=0)


class ResourceFlavorBinding(BaseModel):
    name: str
    accelerator_class: str
    resource_name: str = "nvidia.com/gpu"
    node_labels: dict[str, str] = Field(default_factory=dict)


class KueuePoolBinding(BaseModel):
    namespace: str
    local_queue: str
    cluster_queue: str
    cohort: str | None = None
    flavors: list[ResourceFlavorBinding] = Field(default_factory=list)
    quotas: list[ResourceQuota] = Field(default_factory=list)


class ComputePool(BaseModel):
    name: str
    scheduler: str = "kueue"
    binding: KueuePoolBinding


class KueuePoolManifestBuilder:
    def build(self, pool: ComputePool) -> list[dict[str, Any]]:
        binding = pool.binding
        manifests: list[dict[str, Any]] = []

        for flavor in binding.flavors:
            manifests.append(
                {
                    "apiVersion": "kueue.x-k8s.io/v1beta1",
                    "kind": "ResourceFlavor",
                    "metadata": {"name": flavor.name},
                    "spec": {"nodeLabels": flavor.node_labels},
                }
            )

        flavor_name = binding.flavors[0].name if binding.flavors else "default-flavor"
        resource_groups = []
        if binding.quotas:
            resource_groups.append(
                {
                    "coveredResources": [quota.resource for quota in binding.quotas],
                    "flavors": [
                        {
                            "name": flavor_name,
                            "resources": [
                                {
                                    "name": quota.resource,
                                    "nominalQuota": quota.nominal_quota,
                                }
                                for quota in binding.quotas
                            ],
                        }
                    ],
                }
            )

        cluster_queue_spec: dict[str, Any] = {
            "namespaceSelector": {},
            "resourceGroups": resource_groups,
        }
        if binding.cohort:
            cluster_queue_spec["cohort"] = binding.cohort

        manifests.append(
            {
                "apiVersion": "kueue.x-k8s.io/v1beta1",
                "kind": "ClusterQueue",
                "metadata": {"name": binding.cluster_queue},
                "spec": cluster_queue_spec,
            }
        )
        manifests.append(
            {
                "apiVersion": "kueue.x-k8s.io/v1beta1",
                "kind": "LocalQueue",
                "metadata": {
                    "name": binding.local_queue,
                    "namespace": binding.namespace,
                },
                "spec": {"clusterQueue": binding.cluster_queue},
            }
        )
        return manifests
