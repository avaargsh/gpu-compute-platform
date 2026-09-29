"""Retry/backoff policy for reconcile failures."""

from dataclasses import dataclass


@dataclass(frozen=True)
class RetryPolicy:
    base_seconds: float = 1.0
    max_seconds: float = 60.0

    def delay(self, attempts: int) -> float:
        if attempts < 1:
            return 0.0
        return min(self.max_seconds, self.base_seconds * (2 ** (attempts - 1)))
