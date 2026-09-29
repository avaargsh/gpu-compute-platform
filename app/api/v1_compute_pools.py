"""ComputePool desired-state REST resource."""

import uuid

from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.v2_tenancy import require_project_member
from app.control_plane.compute_pool import ComputePool
from app.control_plane.reconcile_signal import enqueue_reconcile
from app.control_plane.resource_store_sqlalchemy import SQLAlchemyResourceStore
from app.core.auth import current_active_user
from app.core.database import get_async_session
from app.models.user import User

router = APIRouter()


def compute_pool_key(project_id: uuid.UUID, name: str) -> str:
    return f"project/{project_id}/computepool/{name}"


async def _write(tenant_id, project_id, pool, session, user):
    await require_project_member(tenant_id, project_id, user, session)
    store = SQLAlchemyResourceStore(session, project_id=project_id)
    key = compute_pool_key(project_id, pool.name)
    record = await store.put_desired(key, pool)
    enqueue_reconcile(key)
    return {"kind": "ComputePool", "name": pool.name, "tenant_id": tenant_id, "project_id": project_id, "generation": record.generation, "desired": record.desired, "status": record.observed}


@router.post("/tenants/{tenant_id}/projects/{project_id}/compute-pools", status_code=status.HTTP_202_ACCEPTED)
async def create_compute_pool(tenant_id: uuid.UUID, project_id: uuid.UUID, pool: ComputePool, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    return await _write(tenant_id, project_id, pool, session, user)


@router.put("/tenants/{tenant_id}/projects/{project_id}/compute-pools/{name}", status_code=status.HTTP_202_ACCEPTED)
async def replace_compute_pool(tenant_id: uuid.UUID, project_id: uuid.UUID, name: str, pool: ComputePool, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    if pool.name != name:
        raise HTTPException(status_code=409, detail="Resource name does not match request path")
    return await _write(tenant_id, project_id, pool, session, user)


@router.get("/tenants/{tenant_id}/projects/{project_id}/compute-pools/{name}")
async def get_compute_pool(tenant_id: uuid.UUID, project_id: uuid.UUID, name: str, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    await require_project_member(tenant_id, project_id, user, session)
    record = await SQLAlchemyResourceStore(session, project_id=project_id).get(compute_pool_key(project_id, name))
    if record is None:
        raise HTTPException(status_code=404, detail="ComputePool not found")
    return {"kind": "ComputePool", "name": name, "tenant_id": tenant_id, "project_id": project_id, "generation": record.generation, "desired": record.desired, "status": record.observed.model_dump(mode="json") if record.observed else None}
