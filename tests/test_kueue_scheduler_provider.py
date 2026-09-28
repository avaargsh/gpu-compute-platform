from types import SimpleNamespace

import pytest

from app.control_plane.adapters.kueue import KueueBinding
from app.control_plane.domain import AcceleratorRequest, ComputePoolRef, WorkloadKind, WorkloadSpec
from app.control_plane.providers_kueue import KueueSchedulerProvider


class FakeBatchClient:
    def __init__(self):
        self.created = None
        self.exists = False
        self.deleted = None
        self.pods = []
        self.job = SimpleNamespace(
            metadata=SimpleNamespace(resource_version="42", uid="job-uid-1"),
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

    def list_namespaced_pod(self, namespace, **kwargs):
        return SimpleNamespace(items=self.pods)


class FakeCustomObjectsClient:
    def __init__(self):
        self.conditions = []

    def list_namespaced_custom_object(self, group, version, namespace, plural, **kwargs):
        assert plural == "workloads"
        assert kwargs["label_selector"] == "kueue.x-k8s.io/job-uid=job-uid-1"
        return {"items": [{"status": {"conditions": self.conditions}}]}


def workload():
    return WorkloadSpec(
        name="train-qwen",
        kind=WorkloadKind.TRAINING,
        compute_pool=ComputePoolRef(name="training"),
        accelerator=AcceleratorRequest(class_name="h100-80gb", count=2),
        image="trainer:v1",
    )


def provider(client=None, custom=None):
    return KueueSchedulerProvider(
        client or FakeBatchClient(),
        KueueBinding(
            namespace="team-a",
            local_queue="training",
            accelerator_resources={"h100-80gb": "nvidia.com/gpu"},
        ),
        custom or FakeCustomObjectsClient(),
    )


@pytest.mark.asyncio
async def test_submit_returns_stable_provider_binding():
    client = FakeBatchClient()
    p = provider(client=client)
    binding_id = await p.submit(workload())
    assert binding_id == "team-a/train-qwen"
    assert client.created[0] == "team-a"
    assert client.created[1]["spec"]["template"]["spec"]["containers"][0]["resources"]["requests"] == {"nvidia.com/gpu": "2"}


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("active", "succeeded", "failed", "phase"),
    [(0, 0, 0, "pending"), (1, 0, 0, "running"), (0, 1, 0, "completed"), (0, 0, 1, "failed")],
)
async def test_status_normalizes_kubernetes_job_state(active, succeeded, failed, phase):
    client = FakeBatchClient()
    client.job.status = SimpleNamespace(active=active, succeeded=succeeded, failed=failed)
    status = await provider(client=client).status("team-a/train-qwen")
    assert status["phase"] == phase
    assert status["provider"] == "kueue"


@pytest.mark.asyncio
async def test_status_reads_admission_from_kueue_workload_cr():
    custom = FakeCustomObjectsClient()
    custom.conditions = [
        {"type": "QuotaReserved", "status": "True"},
        {"type": "Admitted", "status": "True"},
        {"type": "Evicted", "status": "False"},
    ]
    status = await provider(custom=custom).status("team-a/train-qwen")
    assert status["admitted"] is True
    assert status["evicted"] is False
    assert status["conditions"]["QuotaReserved"] == "True"


@pytest.mark.asyncio
async def test_cancel_is_idempotent():
    client = FakeBatchClient()
    p = provider(client=client)
    await p.cancel("team-a/train-qwen")
    assert client.deleted[0:2] == ("team-a", "train-qwen")


@pytest.mark.asyncio
async def test_invalid_binding_id_is_rejected():
    with pytest.raises(ValueError):
        await provider().status("not-a-binding")


@pytest.mark.asyncio
async def test_submit_is_idempotent_when_job_already_exists():
    client = FakeBatchClient()
    client.exists = True
    binding_id = await provider(client=client).submit(workload())
    assert binding_id == "team-a/train-qwen"
    assert client.created is None


@pytest.mark.asyncio
async def test_status_reports_ready_pods():
    client = FakeBatchClient()
    client.job.status = SimpleNamespace(active=1, succeeded=0, failed=0)
    client.pods = [
        SimpleNamespace(status=SimpleNamespace(conditions=[SimpleNamespace(type="Ready", status="True")])),
        SimpleNamespace(status=SimpleNamespace(conditions=[SimpleNamespace(type="Ready", status="False")])),
    ]
    status = await provider(client=client).status("team-a/train-qwen")
    assert status["pods_ready"] == 1
