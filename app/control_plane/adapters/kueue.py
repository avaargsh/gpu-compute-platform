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
    accelerator_resources: dict[str, str]
    priority_class: str | None = None


class KueueManifestBuilder:
    def __init__(self, binding: KueueBinding):
        self.binding = binding

    def build_job(self, workload: WorkloadSpec, generation: int | None = None) -> dict[str, Any]:
        job_name = workload.name if generation is None else f"{workload.name}-g{generation}"
        try:
            resource_name = self.binding.accelerator_resources[workload.accelerator.class_name]
        except KeyError as exc:
            raise ValueError(f"accelerator class {workload.accelerator.class_name!r} is not available in compute pool") from exc
        resources = {
            "requests": {resource_name: str(workload.accelerator.count)},
            "limits": {resource_name: str(workload.accelerator.count)},
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

        manifest = {
            "apiVersion": "batch/v1",
            "kind": "Job",
            "metadata": {
                "name": job_name,
                "namespace": self.binding.namespace,
                "labels": {
                    KUEUE_QUEUE_LABEL: self.binding.local_queue,
                    "compute.platform/workload-kind": workload.kind.value,
                    "compute.platform/compute-pool": workload.compute_pool.name,
                    "compute.platform/workload": workload.name,
                    **({"compute.platform/generation": str(generation)} if generation is not None else {}),
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
