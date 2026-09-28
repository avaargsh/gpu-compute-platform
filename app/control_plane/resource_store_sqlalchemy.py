"""SQLAlchemy implementation of the ResourceStore contract."""

from datetime import datetime
from typing import Any
import uuid

from sqlalchemy.ext.asyncio import AsyncSession

from app.control_plane.lifecycle import ResourceLifecycle
from app.control_plane.resource_store import ResourceRecord
from app.control_plane.status import ObservedState
from app.models.control_plane_resource import ControlPlaneResource


class SQLAlchemyResourceStore:
    def __init__(self, session: AsyncSession, project_id: uuid.UUID | None = None):
        self.session = session
        self.project_id = project_id

    def _owned(self, row: ControlPlaneResource | None) -> bool:
        return row is not None and (self.project_id is None or row.project_id == self.project_id)

    async def get(self, key: str) -> ResourceRecord | None:
        row = await self.session.get(ControlPlaneResource, key)
        if not self._owned(row):
            return None
        return ResourceRecord(
            key=row.key,
            generation=row.generation,
            desired=row.desired,
            observed=ObservedState.model_validate(row.observed) if row.observed else None,
            lifecycle=ResourceLifecycle(
                deletion_timestamp=datetime.fromisoformat(row.lifecycle["deletion_timestamp"]) if row.lifecycle.get("deletion_timestamp") else None,
                finalizers=list(row.lifecycle.get("finalizers", [])),
            ),
        )

    async def put_desired(self, key: str, desired: Any) -> ResourceRecord:
        payload = desired.model_dump(mode="json") if hasattr(desired, "model_dump") else desired
        row = await self.session.get(ControlPlaneResource, key)
        if row is None:
            if self.project_id is None:
                raise ValueError("project_id is required when creating control-plane resources")
            row = ControlPlaneResource(
                key=key, kind=key.split("/")[-2], project_id=self.project_id,
                generation=1, desired=payload, lifecycle={"finalizers": []},
            )
            self.session.add(row)
        else:
            if not self._owned(row):
                raise PermissionError("resource belongs to another project")
            row.generation += 1
            row.desired = payload
            row.observed = None
            row.observed_generation = None
        await self.session.commit()
        return await self.get(key)

    async def put_observed(self, key: str, generation: int, observed: ObservedState) -> None:
        row = await self.session.get(ControlPlaneResource, key)
        if row is None or row.generation != generation:
            return
        row.observed = observed.model_dump(mode="json")
        row.observed_generation = generation
        await self.session.commit()

    async def mark_deleting(self, key: str, deletion_timestamp: datetime) -> None:
        row = await self.session.get(ControlPlaneResource, key)
        if not self._owned(row):
            return
        lifecycle = dict(row.lifecycle or {})
        lifecycle["deletion_timestamp"] = deletion_timestamp.isoformat()
        row.lifecycle = lifecycle
        await self.session.commit()

    async def delete(self, key: str) -> None:
        row = await self.session.get(ControlPlaneResource, key)
        if self._owned(row):
            await self.session.delete(row)
            await self.session.commit()
