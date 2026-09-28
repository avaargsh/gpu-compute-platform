"""HAMi compatibility binding.

HAMi remains an implementation-specific device provider. Its resource names and
annotations intentionally stay outside the portable AcceleratorClass model.
"""

from dataclasses import dataclass
from typing import Any

from app.control_plane.domain import AcceleratorClass


@dataclass(frozen=True)
class HAMiDeviceBinding:
    resource_name: str = "nvidia.com/gpu"
    memory_resource_name: str | None = None


class HAMiResourceBuilder:
    def __init__(self, binding: HAMiDeviceBinding):
        self.binding = binding

    def container_resources(
        self, accelerator: AcceleratorClass, memory_mb: int | None = None
    ) -> dict[str, Any]:
        values: dict[str, str] = {
            self.binding.resource_name: str(accelerator.count),
        }
        if memory_mb is not None:
            if not self.binding.memory_resource_name:
                raise ValueError("memory_resource_name is required for memory slicing")
            values[self.binding.memory_resource_name] = str(memory_mb)
        return {"requests": values, "limits": dict(values)}
