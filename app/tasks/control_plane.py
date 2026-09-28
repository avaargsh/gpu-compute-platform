"""Control-plane reconcile worker entrypoint."""

import asyncio
import logging

from app.core.celery_app import celery_app
from app.core.database import async_session_maker
from app.control_plane.resource_store_sqlalchemy import SQLAlchemyResourceStore
from app.control_plane.config import control_plane_settings
from app.control_plane.provider_factory import build_scheduler_reconcile_provider
from app.control_plane.reconcile import Reconciler

logger = logging.getLogger(__name__)


async def _reconcile_resource(resource_key: str) -> None:
    async with async_session_maker() as session:
        store = SQLAlchemyResourceStore(session)
        record = await store.get(resource_key)
        if record is None:
            return

        kind = resource_key.split("/", 1)[0]
        if kind != "workload":
            logger.info("no reconcile provider registered for kind=%s key=%s", kind, resource_key)
            return
        if control_plane_settings.scheduler_provider == "disabled":
            logger.info("control-plane scheduler disabled; retaining desired state key=%s", resource_key)
            return

        from app.control_plane.domain import WorkloadSpec
        provider = build_scheduler_reconcile_provider(control_plane_settings)
        reconciler = Reconciler(provider)
        result = await reconciler.reconcile(
            WorkloadSpec.model_validate(record.desired),
            generation=record.generation,
        )
        await store.put_observed(resource_key, record.generation, result.state)
        logger.info(
            "reconciled key=%s generation=%s phase=%s provider_ref=%s",
            resource_key, record.generation, result.state.phase.value, result.provider_ref,
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
