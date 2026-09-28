"""Kubernetes Dynamic Resource Allocation (DRA) device provider contracts."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import AcceleratorClass


@dataclass(frozen=True)
class DRADeviceBinding:
    namespace: str
    device_class_name: str


class DRAResourceClaimBuilder:
    """Translate portable accelerator intent into resource.k8s.io/v1."""

    def __init__(self, binding: DRADeviceBinding):
        self.binding = binding

    def build_claim(self, name: str, accelerator: AcceleratorClass) -> dict[str, Any]:
        return {
            "apiVersion": "resource.k8s.io/v1",
            "kind": "ResourceClaim",
            "metadata": {
                "name": name,
                "namespace": self.binding.namespace,
                "labels": {
                    "compute.platform/accelerator-class": accelerator.name,
                },
            },
            "spec": {
                "devices": {
                    "requests": [
                        {
                            "name": "accelerator",
                            "exactly": {
                                "deviceClassName": self.binding.device_class_name,
                                "allocationMode": "ExactCount",
                                "count": accelerator.count,
                            },
                        }
                    ]
                }
            },
        }

    def pod_resource_claim(self, claim_name: str) -> tuple[dict[str, Any], dict[str, Any]]:
        """Return Pod spec resourceClaims entry and container claim reference."""
        return (
            {
                "name": "accelerator",
                "resourceClaimName": claim_name,
            },
            {
                "name": "accelerator",
            },
        )
