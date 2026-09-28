"""KServe serving adapter behind the portable DeploymentSpec contract."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import DeploymentSpec
from app.control_plane.runtime_openai import OpenAICompatibleRuntimeProvider


@dataclass(frozen=True)
class KServeBinding:
    namespace: str
    runtime_image_vllm: str
    runtime_image_sglang: str


class KServeManifestBuilder:
    def __init__(self, binding: KServeBinding):
        self.binding = binding
        self.runtime = OpenAICompatibleRuntimeProvider()

    def build(self, deployment: DeploymentSpec) -> dict[str, Any]:
        runtime = self.runtime.build_runtime(deployment)
        image = (
            self.binding.runtime_image_vllm
            if deployment.serving.runtime.value == "vllm"
            else self.binding.runtime_image_sglang
        )
        return {
            "apiVersion": "serving.kserve.io/v1beta1",
            "kind": "InferenceService",
            "metadata": {
                "name": deployment.name,
                "namespace": self.binding.namespace,
                "labels": {
                    "compute.platform/compute-pool": deployment.compute_pool.name,
                },
            },
            "spec": {
                "predictor": {
                    "minReplicas": deployment.replicas,
                    "containers": [
                        {
                            "name": "runtime",
                            "image": image,
                            "command": runtime["command"],
                            "ports": [{"containerPort": runtime["port"]}],
                        }
                    ],
                }
            },
        }
