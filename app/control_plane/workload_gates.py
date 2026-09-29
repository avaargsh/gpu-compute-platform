"""Provider-neutral workload admission gates."""

from app.control_plane.status import Condition, ObservedState, Phase


def waiting_for_compute_pool(
    pool_name: str,
    pool_key: str,
    generation: int,
) -> ObservedState:
    """Return observable state while a workload is blocked by its ComputePool."""
    return ObservedState(
        phase=Phase.PROGRESSING,
        observed_generation=generation,
        conditions=[
            Condition(
                type="Admitted",
                status=False,
                reason="ComputePoolNotReady",
                message=f"waiting for ComputePool {pool_name!r} to become Ready",
                observed_generation=generation,
            ),
            Condition(
                type="Ready",
                status=False,
                reason="ComputePoolNotReady",
                observed_generation=generation,
            ),
        ],
        provider_status={"blocked_by": pool_key},
    )
