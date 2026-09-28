"""Provider-neutral observed state for control-plane resources.

PostgreSQL stores the platform projection of runtime reality. Conditions model
portable lifecycle facts; provider/device details stay in allocation/provider_status.
"""

from datetime import datetime, timezone
from enum import Enum
from typing import Any

from pydantic import BaseModel, Field


class Phase(str, Enum):
    PENDING = "pending"
    PROGRESSING = "progressing"
    READY = "ready"
    DEGRADED = "degraded"
    FAILED = "failed"
    TERMINATING = "terminating"


class Condition(BaseModel):
    type: str
    status: bool
    reason: str
    message: str = ""
    observed_generation: int | None = None
    last_transition_time: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc)
    )


class EvidenceRef(BaseModel):
    kind: str
    uri: str
    digest: str | None = None


class AllocationObservation(BaseModel):
    """Provider-neutral summary of an optional device allocation."""

    state: str | None = None
    path: str | None = None
    isolation: str | None = None
    claims: list[str] = Field(default_factory=list)
    details: dict[str, Any] = Field(default_factory=dict)


class ObservedState(BaseModel):
    phase: Phase
    observed_generation: int | None = None
    reconcile_revision: int | None = None
    conditions: list[Condition] = Field(default_factory=list)
    provider_ref: str | None = None
    endpoint: str | None = None
    replicas_ready: int | None = None
    admitted: bool | None = None
    allocation: AllocationObservation | None = None
    evidence_refs: list[EvidenceRef] = Field(default_factory=list)
    provider_status: dict[str, Any] = Field(default_factory=dict)
