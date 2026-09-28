"""KV-transfer provider capability contracts for disaggregated serving."""

from dataclasses import dataclass
from enum import Enum
from typing import Any


class KVTransportKind(str, Enum):
    NIXL = "nixl"
    MOONCAKE = "mooncake"
    CUSTOM = "custom"


@dataclass(frozen=True)
class KVTransferBinding:
    transport: KVTransportKind
    config: dict[str, Any]


class KVTransferProvider:
    """Provider extension; intentionally outside the portable domain."""

    def runtime_overrides(self, role: str, binding: KVTransferBinding) -> dict[str, Any]:
        if role not in {"prefill", "decode"}:
            raise ValueError("KV transfer only applies to prefill/decode roles")
        return {
            "transport": binding.transport.value,
            "role": role,
            "config": dict(binding.config),
        }
