"""Control-plane provider contracts.

Providers translate portable desired state into scheduler/device/serving/runtime
specific resources. They must not redefine the public domain model.

Kueue owns admission/quota, schedulers own placement, and device backends own
allocation. ComputeProvider is the stable control-plane SPI above those mechanisms.
"""

from abc import ABC, abstractmethod
from typing import Any

from app.control_plane.domain import DeploymentSpec, WorkloadSpec


class ProviderStatus(dict):
    """Normalized provider status returned to the control plane."""


class ComputeProvider(ABC):
    """Stable provider SPI for pool/workload materialization and observation."""

    @abstractmethod
    async def ensure_pool(self, pool: Any, generation: int | None = None) -> str:
        """Ensure provider-side pool resources and return a provider reference."""

    @abstractmethod
    async def delete_pool(self, provider_ref: str) -> None:
        """Delete provider-side pool resources."""

    @abstractmethod
    async def resolve_binding(self, accelerator_class: str, pool: Any) -> dict[str, Any]:
        """Resolve portable accelerator intent to provider-private binding details."""

    @abstractmethod
    async def materialize_workload(
        self, workload: WorkloadSpec, generation: int | None = None
    ) -> str:
        """Materialize workload intent; admission remains owned by Kueue."""

    @abstractmethod
    async def observe_workload(self, provider_ref: str) -> ProviderStatus:
        """Project runtime reality into provider-neutral status."""

    @abstractmethod
    async def report_capacity(self) -> dict[str, Any]:
        """Return normalized pool/provider capacity observations."""


class SchedulerProvider(ABC):
    """Compatibility interface used by the current single-cluster Kueue adapter."""

    @abstractmethod
    async def submit(self, workload: WorkloadSpec, generation: int | None = None) -> str:
        """Submit portable workload intent and return a provider binding ID."""

    @abstractmethod
    async def status(self, binding_id: str) -> ProviderStatus:
        """Return normalized admission/placement status."""

    @abstractmethod
    async def cancel(self, binding_id: str) -> None:
        """Cancel or release the provider-side workload."""


class ServingProvider(ABC):
    @abstractmethod
    async def apply(self, deployment: DeploymentSpec) -> str:
        """Reconcile desired serving state and return a provider binding ID."""

    @abstractmethod
    async def status(self, binding_id: str) -> ProviderStatus:
        """Return normalized deployment status."""

    @abstractmethod
    async def delete(self, binding_id: str) -> None:
        """Delete provider-side serving resources."""


class DeviceProvider(ABC):
    @abstractmethod
    async def capabilities(self) -> dict[str, Any]:
        """Return normalized accelerator/device capabilities."""


class RuntimeProvider(ABC):
    @abstractmethod
    def build_runtime(self, deployment: DeploymentSpec) -> dict[str, Any]:
        """Translate serving intent into runtime configuration."""
