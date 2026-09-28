"""Gateway API adapter for stable inference endpoints."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import EndpointSpec


@dataclass(frozen=True)
class GatewayBinding:
    namespace: str
    gateway_name: str
    service_port: int = 80
    inference_pool_backends: bool = False


class GatewayEndpointBuilder:
    def __init__(self, binding: GatewayBinding):
        self.binding = binding

    def build_route(self, endpoint: EndpointSpec) -> dict[str, Any]:
        backend_refs = []
        for target in endpoint.targets:
            if target.shadow:
                continue
            ref: dict[str, Any] = {
                "name": target.deployment,
                "weight": target.weight,
            }
            if self.binding.inference_pool_backends:
                ref.update({
                    "group": "inference.networking.k8s.io",
                    "kind": "InferencePool",
                })
            else:
                ref["port"] = self.binding.service_port
            backend_refs.append(ref)
        rule: dict[str, Any] = {"backendRefs": backend_refs}
        route: dict[str, Any] = {
            "apiVersion": "gateway.networking.k8s.io/v1",
            "kind": "HTTPRoute",
            "metadata": {
                "name": endpoint.name,
                "namespace": self.binding.namespace,
            },
            "spec": {
                "parentRefs": [{"name": self.binding.gateway_name}],
                "rules": [rule],
            },
        }
        if endpoint.hostname:
            route["spec"]["hostnames"] = [endpoint.hostname]
        return route

    def shadow_targets(self, endpoint: EndpointSpec) -> list[str]:
        """Expose shadow intent for a provider that supports request mirroring."""
        return [target.deployment for target in endpoint.targets if target.shadow]
