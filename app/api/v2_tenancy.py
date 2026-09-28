"""Tenant and Project APIs for the canonical v2 ownership model."""

import uuid

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, Field
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.auth import current_active_user
from app.core.database import get_async_session
from app.models.tenancy import Project, ProjectMember, Tenant
from app.models.user import User

router = APIRouter()


class TenantCreate(BaseModel):
    name: str = Field(min_length=1, max_length=128)


class ProjectCreate(BaseModel):
    name: str = Field(min_length=1, max_length=128)


async def require_project_member(project_id: uuid.UUID, user: User, session: AsyncSession) -> Project:
    membership = (
        await session.execute(
            select(ProjectMember).where(
                ProjectMember.project_id == project_id,
                ProjectMember.user_id == user.id,
            )
        )
    ).scalar_one_or_none()
    if membership is None:
        raise HTTPException(status_code=404, detail="Project not found")
    project = await session.get(Project, project_id)
    if project is None:
        raise HTTPException(status_code=404, detail="Project not found")
    return project


@router.post("/tenants", status_code=status.HTTP_201_CREATED)
async def create_tenant(payload: TenantCreate, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    tenant = Tenant(name=payload.name)
    session.add(tenant)
    await session.flush()
    project = Project(tenant_id=tenant.id, name="default")
    session.add(project)
    await session.flush()
    session.add(ProjectMember(project_id=project.id, user_id=user.id, role="owner"))
    await session.commit()
    return {"id": tenant.id, "name": tenant.name, "default_project_id": project.id}


@router.post("/tenants/{tenant_id}/projects", status_code=status.HTTP_201_CREATED)
async def create_project(tenant_id: uuid.UUID, payload: ProjectCreate, session: AsyncSession = Depends(get_async_session), user: User = Depends(current_active_user)):
    owner_membership = (
        await session.execute(
            select(ProjectMember)
            .join(Project, Project.id == ProjectMember.project_id)
            .where(Project.tenant_id == tenant_id, ProjectMember.user_id == user.id, ProjectMember.role == "owner")
        )
    ).scalar_one_or_none()
    if owner_membership is None:
        raise HTTPException(status_code=404, detail="Tenant not found")
    project = Project(tenant_id=tenant_id, name=payload.name)
    session.add(project)
    await session.flush()
    session.add(ProjectMember(project_id=project.id, user_id=user.id, role="owner"))
    await session.commit()
    return {"id": project.id, "tenant_id": tenant_id, "name": project.name}
