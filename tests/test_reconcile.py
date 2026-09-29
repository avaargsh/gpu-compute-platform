import pytest

from app.control_plane.reconcile import Reconciler
from app.control_plane.status import Phase


class FakeProvider:
    def __init__(self, phase="ready"):
        self.phase = phase
        self.applied = None

    async def apply(self, desired, generation=None):
        self.applied = desired
        return "team-a/qwen"

    async def observe(self, provider_ref):
        return {
            "phase": self.phase,
            "replicas_ready": 2,
            "admitted": True,
            "endpoint": "https://example.test/v1",
            "evidence_refs": [
                {"kind": "manifest", "uri": "s3://evidence/qwen.json", "digest": "sha256:abc"}
            ],
            "provider_status": {"resourceVersion": "42"},
        }


@pytest.mark.asyncio
async def test_reconcile_applies_observes_and_normalizes_status():
    provider = FakeProvider()
    result = await Reconciler(provider).reconcile({"name": "qwen"}, generation=3)
    assert provider.applied == {"name": "qwen"}
    assert result.provider_ref == "team-a/qwen"
    assert result.state.phase == Phase.READY
    assert result.state.conditions[0].type == "Ready"
    assert result.state.observed_generation == 3
    assert result.state.conditions[0].observed_generation == 3
    assert result.state.conditions[0].status is True
    assert result.state.replicas_ready == 2
    assert {condition.type for condition in result.state.conditions} == {"Ready", "Admitted", "PodsReady"}
    assert result.state.evidence_refs[0].digest == "sha256:abc"
    assert result.state.provider_status == {"resourceVersion": "42"}


@pytest.mark.asyncio
async def test_non_ready_provider_state_becomes_not_ready_condition():
    result = await Reconciler(FakeProvider("progressing")).reconcile({"name": "qwen"})
    assert result.state.phase == Phase.PROGRESSING
    assert result.state.conditions[0].status is False


class FailingObserveProvider(FakeProvider):
    async def observe(self, provider_ref):
        raise RuntimeError("kubernetes api unavailable")


@pytest.mark.asyncio
async def test_observe_failure_becomes_condition_and_evidence():
    result = await Reconciler(FailingObserveProvider()).reconcile({"name": "qwen"}, generation=9)
    assert result.state.phase == Phase.FAILED
    assert result.state.observed_generation == 9
    assert any(c.type == "ReconcileFailed" and c.status for c in result.state.conditions)
    assert result.state.evidence_refs[0].kind == "reconcile-error"
    assert result.state.provider_status["error_type"] == "RuntimeError"
