"""Control-plane provider contracts.

Providers translate portable desired state into scheduler/device/serving/runtime
specific resources. They must not redefine the public domain model.
"""

from abc import ABC, abstractmethod
from typing import Any

from app.control_plane.domain import DeploymentSpec, WorkloadSpec


class ProviderStatus(dict):
    """Normalized provider status returned to the control plane."""


class SchedulerProvider(ABC):
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
