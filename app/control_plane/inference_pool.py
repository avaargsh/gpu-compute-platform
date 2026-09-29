"""Gateway API Inference Extension adapter.

Targets the stable inference.networking.k8s.io/v1 InferencePool API.
"""

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class InferencePoolBinding:
    namespace: str
    endpoint_picker_service: str | None = None
    endpoint_picker_port: int = 9002
    failure_mode: str = "FailOpen"


class InferencePoolBuilder:
    def __init__(self, binding: InferencePoolBinding):
        self.binding = binding

    def build(
        self,
        name: str,
        pod_labels: dict[str, str],
        target_port: int = 8000,
    ) -> dict[str, Any]:
        spec: dict[str, Any] = {
            "selector": {"matchLabels": pod_labels},
            "targetPorts": [{"number": target_port}],
        }
        if self.binding.endpoint_picker_service:
            spec["endpointPickerRef"] = {
                "name": self.binding.endpoint_picker_service,
                "port": {"number": self.binding.endpoint_picker_port},
                "failureMode": self.binding.failure_mode,
            }
        return {
            "apiVersion": "inference.networking.k8s.io/v1",
            "kind": "InferencePool",
            "metadata": {"name": name, "namespace": self.binding.namespace},
            "spec": spec,
        }


def inference_pool_backend_ref(name: str, weight: int = 1) -> dict[str, Any]:
    return {
        "group": "inference.networking.k8s.io",
        "kind": "InferencePool",
        "name": name,
        "weight": weight,
    }
