"""Kueue scheduler adapter.

The first slice deliberately renders a standard Kubernetes Job instead of
depending on the Kubernetes Python client. Execution/reconciliation can be
added behind this adapter without changing the public WorkloadSpec contract.
"""

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import WorkloadSpec

KUEUE_QUEUE_LABEL = "kueue.x-k8s.io/queue-name"


@dataclass(frozen=True)
class KueueBinding:
    namespace: str
    local_queue: str
    accelerator_resource: str = "nvidia.com/gpu"


class KueueManifestBuilder:
    def __init__(self, binding: KueueBinding):
        self.binding = binding

    def build_job(self, workload: WorkloadSpec) -> dict[str, Any]:
        resources = {
            "requests": {
                self.binding.accelerator_resource: str(workload.accelerator.count),
            },
            "limits": {
                self.binding.accelerator_resource: str(workload.accelerator.count),
            },
        }
        container: dict[str, Any] = {
            "name": "workload",
            "image": workload.image,
            "resources": resources,
        }
        if workload.command:
            container["command"] = workload.command
        if workload.env:
            container["env"] = [
                {"name": name, "value": value}
                for name, value in sorted(workload.env.items())
            ]

        return {
            "apiVersion": "batch/v1",
            "kind": "Job",
            "metadata": {
                "name": workload.name,
                "namespace": self.binding.namespace,
                "labels": {
                    KUEUE_QUEUE_LABEL: self.binding.local_queue,
                    "compute.platform/workload-kind": workload.kind.value,
                    "compute.platform/compute-pool": workload.compute_pool.name,
                },
            },
            "spec": {
                "template": {
                    "spec": {
                        "restartPolicy": "Never",
                        "containers": [container],
                    }
                }
            },
        }
