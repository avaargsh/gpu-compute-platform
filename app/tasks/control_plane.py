"""Control-plane reconcile worker entrypoint."""

import asyncio
import logging

from app.core.celery_app import celery_app
from app.core.database import async_session_maker
from app.control_plane.resource_store_sqlalchemy import SQLAlchemyResourceStore

logger = logging.getLogger(__name__)


async def _reconcile_resource(resource_key: str) -> None:
    async with async_session_maker() as session:
        store = SQLAlchemyResourceStore(session)
        record = await store.get(resource_key)
        if record is None:
            return

        # Provider wiring is intentionally explicit per resource kind. The first
        # production binding (Kueue Workload) is installed by deployment config.
        # Until configured, retain desired state rather than executing implicitly.
        logger.info(
            "reconcile wake-up key=%s generation=%s kind=%s",
            resource_key,
            record.generation,
            resource_key.split("/", 1)[0],
        )


@celery_app.task(
    name="app.tasks.control_plane.reconcile_resource",
    autoretry_for=(Exception,),
    retry_backoff=True,
    retry_backoff_max=60,
    retry_jitter=True,
    max_retries=8,
)
def reconcile_resource(resource_key: str):
    asyncio.run(_reconcile_resource(resource_key))
