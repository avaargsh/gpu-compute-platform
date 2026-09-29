"""Reconcile queue contracts."""

from collections import deque
from typing import Protocol


class ReconcileQueue(Protocol):
    async def enqueue(self, key: str) -> None: ...
    async def dequeue(self) -> str | None: ...


class InMemoryReconcileQueue:
    def __init__(self):
        self.items: deque[str] = deque()
        self.pending: set[str] = set()

    async def enqueue(self, key: str) -> None:
        if key not in self.pending:
            self.items.append(key)
            self.pending.add(key)

    async def dequeue(self) -> str | None:
        if not self.items:
            return None
        key = self.items.popleft()
        self.pending.remove(key)
        return key
