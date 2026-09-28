"""Kubernetes-backed Kueue SchedulerProvider."""

from __future__ import annotations

from typing import Any, Protocol

from app.control_plane.adapters.kueue import KueueBinding, KueueManifestBuilder
from app.control_plane.domain import WorkloadSpec
from app.control_plane.providers import ProviderStatus, SchedulerProvider


class BatchClient(Protocol):
    def create_namespaced_job(self, namespace: str, body: dict[str, Any]) -> Any: ...
    def read_namespaced_job_status(self, name: str, namespace: str) -> Any: ...
    def read_namespaced_job(self, name: str, namespace: str) -> Any: ...
    def delete_namespaced_job(self, name: str, namespace: str, **kwargs: Any) -> Any: ...


class KueueSchedulerProvider(SchedulerProvider):
    """Submit standard Kubernetes Jobs that are admitted by Kueue."""

    def __init__(self, batch_client: BatchClient, binding: KueueBinding):
        self._client = batch_client
        self._binding = binding
        self._builder = KueueManifestBuilder(binding)

    async def submit(self, workload: WorkloadSpec, generation: int | None = None) -> str:
        manifest = self._builder.build_job(workload, generation=generation)
        job_name = manifest["metadata"]["name"]
        try:
            self._client.read_namespaced_job(
                name=job_name,
                namespace=self._binding.namespace,
            )
        except Exception as exc:
            if getattr(exc, "status", None) != 404:
                raise
            self._client.create_namespaced_job(
                namespace=self._binding.namespace,
                body=manifest,
            )
        return self._binding_id(job_name)

    async def status(self, binding_id: str) -> ProviderStatus:
        namespace, name = self._parse_binding_id(binding_id)
        job = self._client.read_namespaced_job_status(name=name, namespace=namespace)
        status = getattr(job, "status", None)
        metadata = getattr(job, "metadata", None)
        conditions = list(getattr(status, "conditions", None) or [])
        condition_map = {getattr(item, "type", ""): getattr(item, "status", "") for item in conditions}

        active = int(getattr(status, "active", 0) or 0)
        succeeded = int(getattr(status, "succeeded", 0) or 0)
        failed = int(getattr(status, "failed", 0) or 0)

        if succeeded:
            phase = "completed"
        elif failed:
            phase = "failed"
        elif active:
            phase = "running"
        else:
            phase = "pending"

        admitted = condition_map.get("QuotaReserved") == "True" or condition_map.get("Admitted") == "True"
        evicted = condition_map.get("Evicted") == "True"

        return ProviderStatus(
            provider="kueue",
            binding_id=binding_id,
            phase=phase,
            active=active,
            succeeded=succeeded,
            failed=failed,
            resource_version=getattr(metadata, "resource_version", None),
            admitted=admitted,
            evicted=evicted,
            conditions=condition_map,
        )

    async def cancel(self, binding_id: str) -> None:
        namespace, name = self._parse_binding_id(binding_id)
        self._client.delete_namespaced_job(
            name=name,
            namespace=namespace,
            propagation_policy="Foreground",
        )

    def _binding_id(self, name: str) -> str:
        return f"{self._binding.namespace}/{name}"

    @staticmethod
    def _parse_binding_id(binding_id: str) -> tuple[str, str]:
        namespace, separator, name = binding_id.partition("/")
        if not separator or not namespace or not name or "/" in name:
            raise ValueError("binding_id must be '<namespace>/<job-name>'")
        return namespace, name
