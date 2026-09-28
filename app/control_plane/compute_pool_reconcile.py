"""ComputePool reconciler backed by Kubernetes custom resources."""

import asyncio
from typing import Any, Protocol

from app.control_plane.compute_pool import ComputePool, KueuePoolManifestBuilder
from app.control_plane.status import Condition, ObservedState, Phase


class CustomObjectsClient(Protocol):
    def get_cluster_custom_object(self, group: str, version: str, plural: str, name: str) -> Any: ...
    def create_cluster_custom_object(self, group: str, version: str, plural: str, body: dict[str, Any]) -> Any: ...
    def get_namespaced_custom_object(self, group: str, version: str, namespace: str, plural: str, name: str) -> Any: ...
    def create_namespaced_custom_object(self, group: str, version: str, namespace: str, plural: str, body: dict[str, Any]) -> Any: ...


class ComputePoolReconciler:
    GROUP = "kueue.x-k8s.io"
    VERSION = "v1beta1"

    def __init__(self, client: CustomObjectsClient):
        self.client = client
        self.builder = KueuePoolManifestBuilder()

    async def reconcile(self, pool: ComputePool, generation: int) -> ObservedState:
        manifests = self.builder.build(pool)
        for manifest in manifests:
            await self._ensure(manifest)
        return ObservedState(
            phase=Phase.READY,
            conditions=[Condition(type="Ready", status=True, reason="ResourcesApplied", observed_generation=generation)],
            provider_ref=f"kueue/{pool.binding.cluster_queue}",
        )

    async def _ensure(self, manifest: dict[str, Any]) -> None:
        kind = manifest["kind"]
        meta = manifest["metadata"]
        plural = {"ResourceFlavor": "resourceflavors", "ClusterQueue": "clusterqueues", "LocalQueue": "localqueues"}[kind]
        try:
            if "namespace" in meta:
                await asyncio.to_thread(self.client.get_namespaced_custom_object, self.GROUP, self.VERSION, meta["namespace"], plural, meta["name"])
            else:
                await asyncio.to_thread(self.client.get_cluster_custom_object, self.GROUP, self.VERSION, plural, meta["name"])
            return
        except Exception as exc:
            if getattr(exc, "status", None) != 404:
                raise
        if "namespace" in meta:
            await asyncio.to_thread(self.client.create_namespaced_custom_object, self.GROUP, self.VERSION, meta["namespace"], plural, manifest)
        else:
            await asyncio.to_thread(self.client.create_cluster_custom_object, self.GROUP, self.VERSION, plural, manifest)
