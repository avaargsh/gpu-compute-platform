"""Reconcile scheduling policy for desired-state convergence."""

from app.control_plane.status import Phase


TERMINAL_PHASES = frozenset({Phase.READY, Phase.FAILED})


def needs_follow_up(phase: Phase) -> bool:
    """Non-terminal resources must be observed again without waiting for sweeps."""
    return phase not in TERMINAL_PHASES
