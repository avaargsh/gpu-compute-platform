"""Celery is a reconcile wake-up transport, not control-plane state."""

from celery import Celery
from kombu import Exchange, Queue

from app.core.config import settings

celery_app = Celery(
    "ai_compute_control_plane",
    broker=settings.redis_url,
    backend=settings.redis_url,
    include=["app.tasks.control_plane"],
)

celery_app.conf.update(
    task_serializer="json",
    accept_content=["json"],
    result_serializer="json",
    timezone="UTC",
    enable_utc=True,
    task_track_started=True,
    task_acks_late=True,
    worker_prefetch_multiplier=1,
    task_routes={
        "app.tasks.control_plane.*": {
            "queue": "control_plane",
            "routing_key": "control_plane",
        }
    },
)

celery_app.conf.task_queues = (
    Queue("control_plane", Exchange("control_plane"), routing_key="control_plane"),
)


@celery_app.on_after_configure.connect
def setup_periodic_tasks(sender, **kwargs):
    sender.add_periodic_task(
        60.0,
        celery_app.signature("app.tasks.control_plane.sweep_reconcile_candidates"),
        name="control-plane-convergence-sweep",
    )


def get_celery_app() -> Celery:
    return celery_app
