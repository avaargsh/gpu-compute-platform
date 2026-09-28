"""Native Kubernetes compatibility serving provider.

This is intentionally a compatibility path. Higher-level serving systems such as
KServe or llm-d can implement ServingProvider without changing DeploymentSpec.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import DeploymentSpec
from app.control_plane.providers import ProviderStatus, ServingProvider
from app.control_plane.runtime_openai import OpenAICompatibleRuntimeProvider


@dataclass(frozen=True)
class NativeKubernetesServingBinding:
    namespace: str
    accelerator_resource: str = "nvidia.com/gpu"
    image_vllm: str = "vllm/vllm-openai"
    image_sglang: str = "lmsysorg/sglang"


class NativeKubernetesManifestBuilder:
    def __init__(self, binding: NativeKubernetesServingBinding):
        self.binding = binding
        self.runtime = OpenAICompatibleRuntimeProvider()

    def build(self, deployment: DeploymentSpec) -> list[dict[str, Any]]:
        runtime = self.runtime.build_runtime(deployment)
        image = (
            self.binding.image_vllm
            if deployment.serving.runtime.value == "vllm"
            else self.binding.image_sglang
        )
        labels = {"compute.platform/deployment": deployment.name}
        resources = {
            "requests": {
                self.binding.accelerator_resource: str(deployment.serving.accelerator.count)
            },
            "limits": {
                self.binding.accelerator_resource: str(deployment.serving.accelerator.count)
            },
        }
        deploy = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {"name": deployment.name, "namespace": self.binding.namespace},
            "spec": {
                "replicas": deployment.replicas,
                "selector": {"matchLabels": labels},
                "template": {
                    "metadata": {"labels": labels},
                    "spec": {
                        "containers": [
                            {
                                "name": "runtime",
                                "image": image,
                                "command": runtime["command"],
                                "ports": [{"containerPort": runtime["port"], "name": "http"}],
                                "resources": resources,
                            }
                        ]
                    },
                },
            },
        }
        service = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {"name": deployment.name, "namespace": self.binding.namespace},
            "spec": {
                "selector": labels,
                "ports": [{"name": "http", "port": 80, "targetPort": runtime["port"]}],
            },
        }
        return [deploy, service]
