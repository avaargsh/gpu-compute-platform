"""Lifecycle metadata for desired-state resources."""

from dataclasses import dataclass, field
from datetime import datetime


@dataclass
class ResourceLifecycle:
    deletion_timestamp: datetime | None = None
    finalizers: list[str] = field(default_factory=list)

    @property
    def deleting(self) -> bool:
        return self.deletion_timestamp is not None
