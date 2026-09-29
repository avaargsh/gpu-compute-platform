"""Native Kubernetes compatibility serving provider.

This is intentionally a compatibility path. Higher-level serving systems such as
KServe or llm-d can implement ServingProvider without changing DeploymentSpec.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import DeploymentSpec
from app.control_plane.runtime_openai import OpenAICompatibleRuntimeProvider
from app.control_plane.serving_topology import ServingTopologyResolver


@dataclass(frozen=True)
class NativeKubernetesServingBinding:
    namespace: str
    image_vllm: str
    image_sglang: str
    accelerator_resource: str = "nvidia.com/gpu"


class NativeKubernetesManifestBuilder:
    def __init__(self, binding: NativeKubernetesServingBinding):
        self.binding = binding
        self.runtime = OpenAICompatibleRuntimeProvider()
        self.topology = ServingTopologyResolver()

    def build(self, deployment: DeploymentSpec) -> list[dict[str, Any]]:
        runtime = self.runtime.build_runtime(deployment)
        image = self.binding.image_vllm if deployment.serving.runtime.value == "vllm" else self.binding.image_sglang
        manifests: list[dict[str, Any]] = []
        for pool in self.topology.resolve(deployment):
            labels = {
                "compute.platform/deployment": deployment.name,
                "compute.platform/serving-role": pool.role.value,
            }
            resources = {
                "requests": {self.binding.accelerator_resource: str(pool.accelerator_count)},
                "limits": {self.binding.accelerator_resource: str(pool.accelerator_count)},
            }
            manifests.extend([
                {
                    "apiVersion": "apps/v1", "kind": "Deployment",
                    "metadata": {"name": pool.name, "namespace": self.binding.namespace},
                    "spec": {
                        "replicas": pool.replicas,
                        "selector": {"matchLabels": labels},
                        "template": {"metadata": {"labels": labels}, "spec": {"containers": [{
                            "name": "runtime", "image": image, "command": list(runtime["command"]),
                            "ports": [{"containerPort": runtime["port"], "name": "http"}],
                            "resources": resources,
                        }]}}
                    },
                },
                {
                    "apiVersion": "v1", "kind": "Service",
                    "metadata": {"name": pool.name, "namespace": self.binding.namespace},
                    "spec": {"selector": labels, "ports": [{"name": "http", "port": 80, "targetPort": runtime["port"]}]},
                },
            ])
        return manifests
