"""Provider-neutral observed state for control-plane resources."""

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


class ObservedState(BaseModel):
    phase: Phase
    conditions: list[Condition] = Field(default_factory=list)
    provider_ref: str | None = None
    endpoint: str | None = None
    replicas_ready: int | None = None
    admitted: bool | None = None
    evidence_refs: list[EvidenceRef] = Field(default_factory=list)
    provider_status: dict[str, Any] = Field(default_factory=dict)
