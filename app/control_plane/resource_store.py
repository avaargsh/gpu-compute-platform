"""Desired/observed resource persistence contracts."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Protocol

from app.control_plane.status import ObservedState


@dataclass
class ResourceRecord:
    key: str
    generation: int
    desired: Any
    observed: ObservedState | None = None


class ResourceStore(Protocol):
    async def get(self, key: str) -> ResourceRecord | None: ...
    async def put_desired(self, key: str, desired: Any) -> ResourceRecord: ...
    async def put_observed(self, key: str, generation: int, observed: ObservedState) -> None: ...


class InMemoryResourceStore:
    def __init__(self):
        self.records: dict[str, ResourceRecord] = {}

    async def get(self, key: str) -> ResourceRecord | None:
        return self.records.get(key)

    async def put_desired(self, key: str, desired: Any) -> ResourceRecord:
        current = self.records.get(key)
        generation = 1 if current is None else current.generation + 1
        record = ResourceRecord(key=key, generation=generation, desired=desired)
        self.records[key] = record
        return record

    async def put_observed(self, key: str, generation: int, observed: ObservedState) -> None:
        current = self.records[key]
        if generation != current.generation:
            return
        current.observed = observed
