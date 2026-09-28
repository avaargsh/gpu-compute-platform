from types import SimpleNamespace

import pytest

from app.control_plane.adapters.kueue import KueueBinding
from app.control_plane.domain import AcceleratorClass, ComputePoolRef, WorkloadKind, WorkloadSpec
from app.control_plane.providers_kueue import KueueSchedulerProvider


class FakeBatchClient:
    def __init__(self):
        self.created = None
        self.exists = False
        self.deleted = None
        self.job = SimpleNamespace(
            metadata=SimpleNamespace(resource_version="42"),
            status=SimpleNamespace(active=0, succeeded=0, failed=0),
        )

    def create_namespaced_job(self, namespace, body):
        self.created = (namespace, body)
        self.exists = True

    def read_namespaced_job(self, name, namespace):
        if self.exists:
            return self.job
        exc = Exception("not found")
        exc.status = 404
        raise exc

    def read_namespaced_job_status(self, name, namespace):
        return self.job

    def delete_namespaced_job(self, name, namespace, **kwargs):
        self.deleted = (namespace, name, kwargs)


def workload():
    return WorkloadSpec(
        name="train-qwen",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="training"),
        accelerator=AcceleratorClass(name="h100", count=2),
        image="trainer:v1",
    )


@pytest.mark.asyncio
async def test_submit_returns_stable_provider_binding():
    client = FakeBatchClient()
    provider = KueueSchedulerProvider(
        client, KueueBinding(namespace="team-a", local_queue="training")
    )
    binding_id = await provider.submit(workload())
    assert binding_id == "team-a/train-qwen"
    assert client.created[0] == "team-a"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("active", "succeeded", "failed", "phase"),
    [(0, 0, 0, "pending"), (1, 0, 0, "running"), (0, 1, 0, "completed"), (0, 0, 1, "failed")],
)
async def test_status_normalizes_kubernetes_job_state(active, succeeded, failed, phase):
    client = FakeBatchClient()
    client.job.status = SimpleNamespace(active=active, succeeded=succeeded, failed=failed)
    provider = KueueSchedulerProvider(
        client, KueueBinding(namespace="team-a", local_queue="training")
    )
    status = await provider.status("team-a/train-qwen")
    assert status["phase"] == phase
    assert status["provider"] == "kueue"


@pytest.mark.asyncio
async def test_cancel_deletes_bound_job():
    client = FakeBatchClient()
    provider = KueueSchedulerProvider(
        client, KueueBinding(namespace="team-a", local_queue="training")
    )
    await provider.cancel("team-a/train-qwen")
    assert client.deleted[0:2] == ("team-a", "train-qwen")
    assert client.deleted[2]["propagation_policy"] == "Foreground"


@pytest.mark.asyncio
async def test_invalid_binding_id_is_rejected():
    client = FakeBatchClient()
    provider = KueueSchedulerProvider(
        client, KueueBinding(namespace="team-a", local_queue="training")
    )
    with pytest.raises(ValueError):
        await provider.status("not-a-binding")


@pytest.mark.asyncio
async def test_submit_is_idempotent_when_job_already_exists():
    client = FakeBatchClient()
    client.exists = True
    provider = KueueSchedulerProvider(
        client, KueueBinding(namespace="team-a", local_queue="training")
    )
    binding_id = await provider.submit(workload())
    assert binding_id == "team-a/train-qwen"
    assert client.created is None
