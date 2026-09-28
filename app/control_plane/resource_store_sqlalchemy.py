"""SQLAlchemy implementation of the ResourceStore contract."""

from datetime import datetime
from typing import Any

from sqlalchemy.ext.asyncio import AsyncSession

from app.control_plane.lifecycle import ResourceLifecycle
from app.control_plane.resource_store import ResourceRecord
from app.control_plane.status import ObservedState
from app.models.control_plane_resource import ControlPlaneResource


class SQLAlchemyResourceStore:
    def __init__(self, session: AsyncSession, owner_id: str | None = None):
        self.session = session
        self.owner_id = owner_id

    async def get(self, key: str) -> ResourceRecord | None:
        row = await self.session.get(ControlPlaneResource, key)
        if row is None or (self.owner_id is not None and row.owner_id != self.owner_id):
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
            if self.owner_id is None:
                raise ValueError("owner_id is required when creating control-plane resources")
            row = ControlPlaneResource(key=key, kind=key.split("/", 1)[0], owner_id=self.owner_id, generation=1, desired=payload, lifecycle={"finalizers": []})
            self.session.add(row)
        else:
            if self.owner_id is not None and row.owner_id != self.owner_id:
                raise PermissionError("resource belongs to another owner")
            row.generation += 1
            row.desired = payload
            row.observed = None
        await self.session.commit()
        return await self.get(key)

    async def put_observed(self, key: str, generation: int, observed: ObservedState) -> None:
        row = await self.session.get(ControlPlaneResource, key)
        if row is None or row.generation != generation:
            return
        row.observed = observed.model_dump(mode="json")
        await self.session.commit()

    async def mark_deleting(self, key: str, deletion_timestamp: datetime) -> None:
        row = await self.session.get(ControlPlaneResource, key)
        if row is None:
            return
        lifecycle = dict(row.lifecycle or {})
        lifecycle["deletion_timestamp"] = deletion_timestamp.isoformat()
        row.lifecycle = lifecycle
        await self.session.commit()

    async def delete(self, key: str) -> None:
        row = await self.session.get(ControlPlaneResource, key)
        if row is not None:
            await self.session.delete(row)
            await self.session.commit()
