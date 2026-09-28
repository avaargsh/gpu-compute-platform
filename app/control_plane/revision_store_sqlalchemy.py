"""Persistence for immutable control-plane resource revisions."""

from datetime import datetime, timezone

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.control_plane.revision_gc import Revision
from app.control_plane.status import ObservedState
from app.models.control_plane_revision import ControlPlaneResourceRevision


class SQLAlchemyRevisionStore:
    def __init__(self, session: AsyncSession):
        self.session = session

    async def upsert(self, resource_key: str, generation: int, observed: ObservedState) -> None:
        stmt = select(ControlPlaneResourceRevision).where(
            ControlPlaneResourceRevision.resource_key == resource_key,
            ControlPlaneResourceRevision.generation == generation,
        )
        row = (await self.session.execute(stmt)).scalar_one_or_none()
        terminal = observed.phase.value in {"ready", "failed"}
        if row is None:
            row = ControlPlaneResourceRevision(
                resource_key=resource_key,
                generation=generation,
                provider_ref=observed.provider_ref,
                phase=observed.phase.value,
                evidence_refs=[item.model_dump(mode="json") for item in observed.evidence_refs],
                terminal_at=datetime.now(timezone.utc) if terminal else None,
            )
            self.session.add(row)
        else:
            row.provider_ref = observed.provider_ref
            row.phase = observed.phase.value
            row.evidence_refs = [item.model_dump(mode="json") for item in observed.evidence_refs]
            if terminal and row.terminal_at is None:
                row.terminal_at = datetime.now(timezone.utc)
        await self.session.commit()

    async def list_for_resource(self, resource_key: str) -> list[ControlPlaneResourceRevision]:
        stmt = (
            select(ControlPlaneResourceRevision)
            .where(ControlPlaneResourceRevision.resource_key == resource_key)
            .order_by(ControlPlaneResourceRevision.generation)
        )
        return list((await self.session.execute(stmt)).scalars().all())

    async def gc_candidates(self, resource_key: str, active_generation: int, policy) -> list[Revision]:
        rows = await self.list_for_resource(resource_key)
        revisions = [
            Revision(row.generation, row.provider_ref, row.phase)
            for row in rows
            if row.provider_ref and row.garbage_collected_at is None
        ]
        return policy.garbage_collect(revisions, active_generation)

    async def mark_garbage_collected(self, resource_key: str, generation: int) -> None:
        stmt = select(ControlPlaneResourceRevision).where(
            ControlPlaneResourceRevision.resource_key == resource_key,
            ControlPlaneResourceRevision.generation == generation,
        )
        row = (await self.session.execute(stmt)).scalar_one()
        row.garbage_collected_at = datetime.now(timezone.utc)
        await self.session.commit()
