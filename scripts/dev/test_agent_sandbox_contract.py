import unittest
from dataclasses import replace
from datetime import datetime, timezone

from agent_sandbox_contract import (
    DesiredRuntime,
    SandboxAction,
    SandboxContractError,
    SandboxObservation,
    SandboxState,
    WorkspaceBinding,
    plan_transition,
)


NOW = datetime(2026, 10, 18, 8, 0, tzinfo=timezone.utc)


def binding():
    return WorkspaceBinding(
        owner_scope="project-a/user-1",
        workspace_id="ws-001",
        storage_ref="pvc://project-a/persistent-workspace-1",
        provider="daytona",
        generation=7,
        authority_digest="sha256:policy-7",
        runtime_spec_digest="sha256:image-7",
    )


def observation(state=SandboxState.RUNNING, **changes):
    base = binding()
    values = dict(
        state=state,
        observed_at=NOW,
        verified=True,
        owner_scope=base.owner_scope,
        workspace_id=base.workspace_id,
        storage_ref=base.storage_ref,
        provider=base.provider,
        generation=base.generation,
        authority_digest=base.authority_digest,
        runtime_spec_digest=base.runtime_spec_digest,
        sandbox_ref="sandbox://daytona/sb-001",
    )
    values.update(changes)
    return SandboxObservation(**values)


class SandboxPlannerTests(unittest.TestCase):
    def test_absent_workspace_allocates_on_demand(self):
        absent = observation(SandboxState.ABSENT, sandbox_ref=None)
        result = plan_transition(binding(), DesiredRuntime.RUNNING, absent)
        self.assertEqual(result.action, SandboxAction.CREATE)
        self.assertTrue(result.requires_authorization)

    def test_absent_paused_user_keeps_workspace_without_vm(self):
        absent = observation(SandboxState.ABSENT, sandbox_ref=None)
        result = plan_transition(binding(), DesiredRuntime.PAUSED, absent)
        self.assertEqual(result.action, SandboxAction.NOOP)
        self.assertFalse(result.requires_authorization)

    def test_paused_resumes_same_sandbox_identity(self):
        result = plan_transition(
            binding(), DesiredRuntime.RUNNING, observation(SandboxState.PAUSED)
        )
        self.assertEqual(result.action, SandboxAction.RESUME)
        self.assertTrue(result.requires_authorization)

    def test_running_pauses_execution_without_deleting_storage(self):
        result = plan_transition(
            binding(), DesiredRuntime.PAUSED, observation()
        )
        self.assertEqual(result.action, SandboxAction.PAUSE)
        self.assertEqual(result.reason, "SUSPEND_EXECUTION_NOT_STORAGE")

    def test_same_running_or_paused_state_is_noop(self):
        for desired, state in [
            (DesiredRuntime.RUNNING, SandboxState.RUNNING),
            (DesiredRuntime.PAUSED, SandboxState.PAUSED),
        ]:
            with self.subTest(state=state):
                self.assertEqual(
                    plan_transition(binding(), desired, observation(state)).action,
                    SandboxAction.NOOP,
                )

    def test_unknown_and_unverified_never_create_another_vm(self):
        for observed in [
            SandboxObservation(SandboxState.UNKNOWN, NOW, True),
            SandboxObservation(SandboxState.ABSENT, NOW, False),
            observation(verified=False),
        ]:
            with self.subTest(observed=observed):
                self.assertEqual(
                    plan_transition(binding(), DesiredRuntime.RUNNING, observed).action,
                    SandboxAction.OBSERVE_ONLY,
                )

    def test_unscoped_absence_cannot_create_or_claim_noop(self):
        cases = (
            SandboxObservation(SandboxState.ABSENT, NOW, True),
            observation(SandboxState.ABSENT, sandbox_ref="sandbox://stale"),
            observation(SandboxState.ABSENT, sandbox_ref=None, owner_scope=None),
            observation(SandboxState.ABSENT, sandbox_ref=None, storage_ref=None),
            observation(SandboxState.ABSENT, sandbox_ref=None, generation=None),
            observation(SandboxState.ABSENT, sandbox_ref=None, generation=True),
            observation(SandboxState.ABSENT, sandbox_ref=None, authority_digest=""),
        )
        for desired in (DesiredRuntime.RUNNING, DesiredRuntime.PAUSED):
            for absent in cases:
                with self.subTest(desired=desired, absent=absent):
                    result = plan_transition(binding(), desired, absent)
                    self.assertEqual(result.action, SandboxAction.OBSERVE_ONLY)
                    self.assertEqual(result.reason, "UNSCOPED_ABSENCE")
                    self.assertFalse(result.requires_authorization)

    def test_verified_absence_with_foreign_identity_is_conflict(self):
        changes = (
            {"owner_scope": "project-b/user-1"},
            {"workspace_id": "ws-other"},
            {"storage_ref": "pvc://project-b/shared"},
            {"provider": "other-provider"},
            {"generation": 8},
            {"authority_digest": "sha256:other-policy"},
            {"runtime_spec_digest": "sha256:other-image"},
        )
        for change in changes:
            with self.subTest(change=change):
                absent = observation(SandboxState.ABSENT, sandbox_ref=None, **change)
                result = plan_transition(binding(), DesiredRuntime.RUNNING, absent)
                self.assertEqual(result.action, SandboxAction.CONFLICT)
                self.assertFalse(result.requires_authorization)

    def test_provider_and_workspace_identity_drift_is_conflict(self):
        changes = (
            {"owner_scope": "project-b/user-1"},
            {"workspace_id": "ws-other"},
            {"storage_ref": "pvc://project-b/shared"},
            {"provider": "firecracker"},
            {"generation": 8},
            {"authority_digest": "sha256:other-policy"},
            {"runtime_spec_digest": "sha256:other-image"},
        )
        for change in changes:
            with self.subTest(change=change):
                result = plan_transition(
                    binding(), DesiredRuntime.RUNNING, observation(**change)
                )
                self.assertEqual(result.action, SandboxAction.CONFLICT)
                self.assertFalse(result.requires_authorization)

    def test_input_shape_must_be_complete(self):
        with self.assertRaises(SandboxContractError):
            replace(binding(), owner_scope="")
        with self.assertRaises(SandboxContractError):
            replace(binding(), generation=0)
        with self.assertRaises(SandboxContractError):
            replace(binding(), generation=True)
        with self.assertRaises(SandboxContractError):
            observation(generation=True)
        with self.assertRaises(SandboxContractError):
            observation(verified="yes")
        with self.assertRaises(SandboxContractError):
            observation(sandbox_ref="")
        with self.assertRaises(SandboxContractError):
            SandboxObservation(
                SandboxState.UNKNOWN, datetime(2026, 10, 18), True
            )
        with self.assertRaises(SandboxContractError):
            plan_transition(binding(), "RUNNING", observation())

    def test_observed_unknown_does_not_accept_fabricated_replacement(self):
        unknown = SandboxObservation(SandboxState.UNKNOWN, NOW, True)
        first = plan_transition(binding(), DesiredRuntime.RUNNING, unknown)
        second = plan_transition(binding(), DesiredRuntime.RUNNING, unknown)
        self.assertEqual(first, second)
        self.assertEqual(first.action, SandboxAction.OBSERVE_ONLY)


if __name__ == "__main__":
    unittest.main()
