"""Optional Agent Sandbox provider *planning* contract.

No Kubernetes, Kueue, Firecracker or VM operations happen in this module.
This is deliberately separate from the Stage B ComputeProvider path.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import Enum


class SandboxContractError(ValueError):
    pass


class SandboxState(str, Enum):
    ABSENT = "ABSENT"
    RUNNING = "RUNNING"
    PAUSED = "PAUSED"
    UNKNOWN = "UNKNOWN"


class DesiredRuntime(str, Enum):
    RUNNING = "RUNNING"
    PAUSED = "PAUSED"


class SandboxAction(str, Enum):
    CREATE = "CREATE"
    RESUME = "RESUME"
    PAUSE = "PAUSE"
    NOOP = "NOOP"
    OBSERVE_ONLY = "OBSERVE_ONLY"
    CONFLICT = "CONFLICT"


@dataclass(frozen=True)
class WorkspaceBinding:
    """Persistent identity: survives all ephemeral execution sessions."""

    owner_scope: str
    workspace_id: str
    storage_ref: str
    provider: str
    generation: int
    authority_digest: str
    runtime_spec_digest: str

    def __post_init__(self) -> None:
        if not all(
            (
                self.owner_scope,
                self.workspace_id,
                self.storage_ref,
                self.provider,
                self.authority_digest,
                self.runtime_spec_digest,
            )
        ) or self.generation < 1:
            raise SandboxContractError("incomplete persistent workspace binding")


@dataclass(frozen=True)
class SandboxObservation:
    state: SandboxState
    observed_at: datetime
    verified: bool
    owner_scope: str | None = None
    workspace_id: str | None = None
    storage_ref: str | None = None
    provider: str | None = None
    generation: int | None = None
    authority_digest: str | None = None
    runtime_spec_digest: str | None = None
    sandbox_ref: str | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.state, SandboxState):
            raise SandboxContractError("invalid provider state")
        if self.observed_at.tzinfo is None or self.observed_at.utcoffset() is None:
            raise SandboxContractError("observation requires timezone-aware time")
        if self.state in (SandboxState.RUNNING, SandboxState.PAUSED):
            if not all(
                (
                    self.owner_scope,
                    self.workspace_id,
                    self.storage_ref,
                    self.provider,
                    self.generation,
                    self.authority_digest,
                    self.runtime_spec_digest,
                    self.sandbox_ref,
                )
            ):
                raise SandboxContractError("active sandbox lacks complete identity")


@dataclass(frozen=True)
class SandboxPlan:
    action: SandboxAction
    reason: str
    # Planning is NOT authority to execute. Caller must validate a current
    # operation-specific grant, lease/fence, and exact provider observation.
    requires_authorization: bool


def plan_transition(
    binding: WorkspaceBinding,
    desired: DesiredRuntime,
    observed: SandboxObservation,
) -> SandboxPlan:
    """Fail-closed plan with stable identity and no hidden replacement VM."""
    if not isinstance(desired, DesiredRuntime):
        raise SandboxContractError("invalid desired runtime state")
    if not observed.verified:
        return SandboxPlan(SandboxAction.OBSERVE_ONLY, "UNVERIFIED_OBSERVATION", False)
    if observed.state is SandboxState.UNKNOWN:
        return SandboxPlan(SandboxAction.OBSERVE_ONLY, "UNKNOWN_PROVIDER_OUTCOME", False)
    if observed.state is SandboxState.ABSENT:
        # Absence is meaningful only for an exact, verified lookup scope.
        # An unscoped provider 404 (or a contradictory physical reference)
        # must never authorize allocation or even assert a scoped NOOP.
        if observed.sandbox_ref is not None or not all((
            observed.owner_scope, observed.workspace_id, observed.storage_ref,
            observed.provider, observed.generation, observed.authority_digest,
            observed.runtime_spec_digest,
        )):
            return SandboxPlan(SandboxAction.OBSERVE_ONLY, "UNSCOPED_ABSENCE", False)

    if (
        observed.owner_scope != binding.owner_scope
        or observed.workspace_id != binding.workspace_id
        or observed.storage_ref != binding.storage_ref
        or observed.provider != binding.provider
        or observed.generation != binding.generation
        or observed.authority_digest != binding.authority_digest
        or observed.runtime_spec_digest != binding.runtime_spec_digest
    ):
        return SandboxPlan(SandboxAction.CONFLICT, "IMMUTABLE_BINDING_DRIFT", False)

    if observed.state is SandboxState.ABSENT:
        if desired is DesiredRuntime.PAUSED:
            return SandboxPlan(SandboxAction.NOOP, "NO_SANDBOX_ALLOCATED", False)
        return SandboxPlan(SandboxAction.CREATE, "ALLOCATE_ON_DEMAND", True)

    if observed.state is SandboxState.RUNNING:
        if desired is DesiredRuntime.PAUSED:
            return SandboxPlan(SandboxAction.PAUSE, "SUSPEND_EXECUTION_NOT_STORAGE", True)
        return SandboxPlan(SandboxAction.NOOP, "ALREADY_RUNNING", False)
    if observed.state is SandboxState.PAUSED:
        if desired is DesiredRuntime.RUNNING:
            return SandboxPlan(SandboxAction.RESUME, "RESUME_SAME_SANDBOX", True)
        return SandboxPlan(SandboxAction.NOOP, "ALREADY_PAUSED", False)
    raise SandboxContractError("unsupported sandbox observation state")
