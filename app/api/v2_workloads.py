"""Canonical project-scoped Workload REST resource."""

from datetime import datetime, timezone
import uuid

from fastapi import APIRouter, Depends, HTTPException, Response, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.v2_tenancy import require_project_member
from app.control_plane.domain import WorkloadSpec
from app.control_plane.reconcile_signal import enqueue_reconcile
from app.control_plane.resource_store_sqlalchemy import SQLAlchemyResourceStore
from app.core.auth import current_active_user
from app.core.database import get_async_session
from app.models.user import User

router = APIRouter()


def workload_key(project_id: uuid.UUID, name: str) -> str:
    return f"project/{project_id}/workload/{name}"


async def _write(tenant_id, project_id, spec, session, user):
    await require_project_member(tenant_id, project_id, user, session)
    store = SQLAlchemyResourceStore(session, project_id=project_id)
    key = workload_key(project_id, spec.name)
    record = await store.put_desired(key, spec)
    enqueue_reconcile(key)
    return {
        "kind": "Workload",
        "name": spec.name,
        "tenant_id": tenant_id,
        "project_id": project_id,
        "generation": record.generation,
        "desired": record.desired,
        "status": record.observed,
    }


@router.post("/tenants/{tenant_id}/projects/{project_id}/workloads", status_code=status.HTTP_202_ACCEPTED)
async def create_workload(tenant_id: uuid.UUID, project_id: uuid.UUID, spec: WorkloadSpec, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    return await _write(tenant_id, project_id, spec, session, user)


@router.put("/tenants/{tenant_id}/projects/{project_id}/workloads/{name}", status_code=status.HTTP_202_ACCEPTED)
async def replace_workload(tenant_id: uuid.UUID, project_id: uuid.UUID, name: str, spec: WorkloadSpec, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    if spec.name != name:
        raise HTTPException(status_code=409, detail="Resource name does not match request path")
    return await _write(tenant_id, project_id, spec, session, user)


@router.get("/tenants/{tenant_id}/projects/{project_id}/workloads/{name}")
async def get_workload(tenant_id: uuid.UUID, project_id: uuid.UUID, name: str, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    await require_project_member(tenant_id, project_id, user, session)
    record = await SQLAlchemyResourceStore(session, project_id=project_id).get(workload_key(project_id, name))
    if record is None:
        raise HTTPException(status_code=404, detail="Workload not found")
    return {
        "kind": "Workload",
        "name": name,
        "tenant_id": tenant_id,
        "project_id": project_id,
        "generation": record.generation,
        "desired": record.desired,
        "status": record.observed.model_dump(mode="json") if record.observed else None,
        "deletion_timestamp": record.lifecycle.deletion_timestamp,
    }


@router.delete("/tenants/{tenant_id}/projects/{project_id}/workloads/{name}", status_code=status.HTTP_202_ACCEPTED)
async def delete_workload(tenant_id: uuid.UUID, project_id: uuid.UUID, name: str, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    await require_project_member(tenant_id, project_id, user, session)
    store = SQLAlchemyResourceStore(session, project_id=project_id)
    key = workload_key(project_id, name)
    if await store.get(key) is None:
        raise HTTPException(status_code=404, detail="Workload not found")
    await store.mark_deleting(key, datetime.now(timezone.utc))
    enqueue_reconcile(key)
    return Response(status_code=status.HTTP_202_ACCEPTED)
