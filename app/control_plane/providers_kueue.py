"""Kubernetes-backed Kueue SchedulerProvider."""

from __future__ import annotations

import asyncio
from typing import Any, Protocol

from app.control_plane.adapters.kueue import KueueBinding, KueueManifestBuilder
from app.control_plane.domain import WorkloadSpec
from app.control_plane.providers import ProviderStatus, SchedulerProvider


class BatchClient(Protocol):
    def create_namespaced_job(self, namespace: str, body: dict[str, Any]) -> Any: ...
    def read_namespaced_job_status(self, name: str, namespace: str) -> Any: ...
    def read_namespaced_job(self, name: str, namespace: str) -> Any: ...
    def delete_namespaced_job(self, name: str, namespace: str, **kwargs: Any) -> Any: ...\n    def list_namespaced_pod(self, namespace: str, **kwargs: Any) -> Any: ...


class CustomObjectsClient(Protocol):
    def list_namespaced_custom_object(self, group: str, version: str, namespace: str, plural: str, **kwargs: Any) -> Any: ...


class KueueSchedulerProvider(SchedulerProvider):
    GROUP = "kueue.x-k8s.io"
    VERSION = "v1beta1"

    def __init__(self, batch_client: BatchClient, binding: KueueBinding, custom_client: CustomObjectsClient):
        self._client = batch_client
        self._custom = custom_client
        self._binding = binding
        self._builder = KueueManifestBuilder(binding)

    async def submit(self, workload: WorkloadSpec, generation: int | None = None) -> str:
        manifest = self._builder.build_job(workload, generation=generation)
        job_name = manifest["metadata"]["name"]
        try:
            await asyncio.to_thread(
                self._client.read_namespaced_job,
                name=job_name,
                namespace=self._binding.namespace,
            )
        except Exception as exc:
            if getattr(exc, "status", None) != 404:
                raise
            await asyncio.to_thread(
                self._client.create_namespaced_job,
                namespace=self._binding.namespace,
                body=manifest,
            )
        return self._binding_id(job_name)

    async def status(self, binding_id: str) -> ProviderStatus:
        namespace, name = self._parse_binding_id(binding_id)
        job = await asyncio.to_thread(
            self._client.read_namespaced_job_status,
            name=name,
            namespace=namespace,
        )
        status = getattr(job, "status", None)
        metadata = getattr(job, "metadata", None)

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

        conditions = await self._workload_conditions(namespace, metadata)\n        pods_ready = await self._pods_ready(namespace, name)
        admitted = conditions.get("Admitted") == "True"
        evicted = conditions.get("Evicted") == "True"

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
            conditions=conditions,\n            pods_ready=pods_ready,
        )

    async def cancel(self, binding_id: str) -> None:
        namespace, name = self._parse_binding_id(binding_id)
        try:
            await asyncio.to_thread(
                self._client.delete_namespaced_job,
                name=name,
                namespace=namespace,
                propagation_policy="Foreground",
            )
        except Exception as exc:
            if getattr(exc, "status", None) != 404:
                raise

    async def _workload_conditions(self, namespace: str, job_metadata: Any) -> dict[str, str]:
        job_uid = getattr(job_metadata, "uid", None)
        if not job_uid:
            return {}
        payload = await asyncio.to_thread(
            self._custom.list_namespaced_custom_object,
            self.GROUP,
            self.VERSION,
            namespace,
            "workloads",
            label_selector=f"kueue.x-k8s.io/job-uid={job_uid}",
        )
        items = payload.get("items", []) if isinstance(payload, dict) else []
        if not items:
            return {}
        conditions = items[0].get("status", {}).get("conditions", [])
        return {
            item.get("type", ""): str(item.get("status", ""))
            for item in conditions
            if item.get("type")
        }

    async def _pods_ready(self, namespace: str, job_name: str) -> int:\n        pods = await asyncio.to_thread(\n            self._client.list_namespaced_pod,\n            namespace=namespace,\n            label_selector=f"job-name={job_name}",\n        )\n        items = getattr(pods, "items", []) or []\n        return sum(\n            1 for pod in items\n            if any(\n                getattr(condition, "type", None) == "Ready" and getattr(condition, "status", None) == "True"\n                for condition in (getattr(getattr(pod, "status", None), "conditions", None) or [])\n            )\n        )\n\n    def _binding_id(self, name: str) -> str:
        return f"{self._binding.namespace}/{name}"

    @staticmethod
    def _parse_binding_id(binding_id: str) -> tuple[str, str]:
        namespace, separator, name = binding_id.partition("/")
        if not separator or not namespace or not name or "/" in name:
            raise ValueError("binding_id must be '<namespace>/<job-name>'")
        return namespace, name
