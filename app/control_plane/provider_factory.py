"""Provider factories for canonical control-plane resources."""

from kubernetes import client, config

from app.control_plane.adapters.kueue import KueueBinding
from app.control_plane.compute_pool import ComputePool
from app.control_plane.config import ControlPlaneSettings
from app.control_plane.providers_kueue import KueueSchedulerProvider
from app.control_plane.scheduler_reconcile import SchedulerReconcileProvider


def _load_kubernetes(settings: ControlPlaneSettings) -> None:
    if settings.kubeconfig:
        config.load_kube_config(config_file=settings.kubeconfig)
    else:
        config.load_incluster_config()


def build_scheduler_reconcile_provider(settings: ControlPlaneSettings, pool: ComputePool):
    if pool.scheduler != "kueue" or settings.scheduler_provider != "kueue":
        raise RuntimeError("requested ComputePool scheduler is not enabled")

    _load_kubernetes(settings)
    accelerator_resources = {
        flavor.accelerator_class: flavor.resource_name
        for flavor in pool.binding.flavors
    }
    scheduler = KueueSchedulerProvider(
        batch_client=client.BatchV1Api(),
        binding=KueueBinding(
            namespace=pool.binding.namespace,
            local_queue=pool.binding.local_queue,
            accelerator_resources=accelerator_resources,
        ),
    )
    return SchedulerReconcileProvider(scheduler)
