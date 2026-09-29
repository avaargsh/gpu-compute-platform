"""Revision retention policy for immutable provider resources."""

from dataclasses import dataclass


@dataclass(frozen=True)
class Revision:
    generation: int
    provider_ref: str
    phase: str


@dataclass(frozen=True)
class RevisionRetentionPolicy:
    successful: int = 3
    failed: int = 5

    def garbage_collect(self, revisions: list[Revision], active_generation: int) -> list[Revision]:
        terminal = [r for r in revisions if r.generation != active_generation and r.phase in {"ready", "failed"}]
        successful = sorted((r for r in terminal if r.phase == "ready"), key=lambda r: r.generation, reverse=True)
        failed = sorted((r for r in terminal if r.phase == "failed"), key=lambda r: r.generation, reverse=True)
        keep = {r.provider_ref for r in successful[: self.successful] + failed[: self.failed]}
        return [r for r in terminal if r.provider_ref not in keep]
