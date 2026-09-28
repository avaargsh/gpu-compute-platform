"""v2 Workload desired-state API."""

from fastapi import APIRouter, Depends, HTTPException, Response, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.control_plane.domain import WorkloadSpec
from app.control_plane.resource_store_sqlalchemy import SQLAlchemyResourceStore
from app.control_plane.reconcile_signal import enqueue_reconcile
from app.core.database import get_async_session
from app.core.auth import current_active_user
from app.models.user import User

router = APIRouter()


def workload_key(name: str) -> str:
    return f"workload/{name}"


@router.post("/workloads", status_code=status.HTTP_202_ACCEPTED)
async def submit_workload(spec: WorkloadSpec, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    store = SQLAlchemyResourceStore(session, owner_id=str(user.id))
    key = workload_key(spec.name)
    record = await store.put_desired(key, spec)
    enqueue_reconcile(key)
    return {
        "name": spec.name,
        "generation": record.generation,
        "desired": record.desired,
        "status": record.observed,
    }


@router.get("/workloads/{name}")
async def get_workload(name: str, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    record = await SQLAlchemyResourceStore(session, owner_id=str(user.id)).get(workload_key(name))
    if record is None:
        raise HTTPException(status_code=404, detail="Workload not found")
    return {
        "name": name,
        "generation": record.generation,
        "desired": record.desired,
        "status": record.observed.model_dump(mode="json") if record.observed else None,
        "deletion_timestamp": record.lifecycle.deletion_timestamp,
    }


@router.delete("/workloads/{name}", status_code=status.HTTP_202_ACCEPTED)
async def delete_workload(name: str, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    from datetime import datetime, timezone
    store = SQLAlchemyResourceStore(session, owner_id=str(user.id))
    record = await store.get(workload_key(name))
    if record is None:
        raise HTTPException(status_code=404, detail="Workload not found")
    key = workload_key(name)
    await store.mark_deleting(key, datetime.now(timezone.utc))
    enqueue_reconcile(key)
    return Response(status_code=status.HTTP_202_ACCEPTED)
