"""Provider factory for control-plane workers."""

from kubernetes import client, config

from app.control_plane.adapters.kueue import KueueBinding
from app.control_plane.config import ControlPlaneSettings
from app.control_plane.providers_kueue import KueueSchedulerProvider
from app.control_plane.scheduler_reconcile import SchedulerReconcileProvider


def build_scheduler_reconcile_provider(settings: ControlPlaneSettings):
    if settings.scheduler_provider != "kueue":
        raise RuntimeError(
            "control-plane scheduler is disabled; set "
            "CONTROL_PLANE_SCHEDULER_PROVIDER=kueue explicitly"
        )

    if settings.kubeconfig:
        config.load_kube_config(config_file=settings.kubeconfig)
    else:
        config.load_incluster_config()

    scheduler = KueueSchedulerProvider(
        batch_client=client.BatchV1Api(),
        binding=KueueBinding(
            namespace=settings.kueue_namespace,
            local_queue=settings.kueue_local_queue,
            accelerator_resource=settings.accelerator_resource,
        ),
    )
    return SchedulerReconcileProvider(scheduler)
