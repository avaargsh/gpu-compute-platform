"""Desired/observed resource persistence contracts."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Protocol

from app.control_plane.status import ObservedState
from app.control_plane.lifecycle import ResourceLifecycle


@dataclass
class ResourceRecord:
    key: str
    generation: int
    desired: Any
    observed: ObservedState | None = None
    lifecycle: ResourceLifecycle = None


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
        lifecycle = current.lifecycle if current and current.lifecycle else ResourceLifecycle()
        record = ResourceRecord(key=key, generation=generation, desired=desired, lifecycle=lifecycle)
        self.records[key] = record
        return record

    async def mark_deleting(self, key: str, deletion_timestamp) -> None:
        self.records[key].lifecycle.deletion_timestamp = deletion_timestamp

    async def delete(self, key: str) -> None:
        self.records.pop(key, None)

    async def put_observed(self, key: str, generation: int, observed: ObservedState) -> None:
        current = self.records[key]
        if generation != current.generation:
            return
        current.observed = observed
