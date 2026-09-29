"""Durable reconcile wake-up through Celery.

Messages contain only resource keys. PostgreSQL remains the source of truth.
"""

from app.core.celery_app import celery_app


def enqueue_reconcile(resource_key: str, countdown: float | None = None) -> None:
    celery_app.send_task(
        "app.tasks.control_plane.reconcile_resource",
        args=[resource_key],
        queue="control_plane",
        countdown=countdown,
    )
