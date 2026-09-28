"""Database-backed reconcile leases.

Leases serialize provider side effects across workers while allowing recovery
after worker loss. Desired/observed generation CAS remains the correctness
boundary; the lease is an execution coordination primitive.
"""

from datetime import datetime, timedelta, timezone
import uuid

from sqlalchemy import or_, select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.control_plane_resource import ControlPlaneResource


class ReconcileLeaseStore:
    def __init__(self, session: AsyncSession):
        self.session = session

    async def claim(self, key: str, owner: str, ttl_seconds: int = 120) -> bool:
        now = datetime.now(timezone.utc)
        until = now + timedelta(seconds=ttl_seconds)
        stmt = (
            update(ControlPlaneResource)
            .where(
                ControlPlaneResource.key == key,
                or_(
                    ControlPlaneResource.lease_until.is_(None),
                    ControlPlaneResource.lease_until < now,
                    ControlPlaneResource.lease_owner == owner,
                ),
            )
            .values(lease_owner=owner, lease_until=until)
        )
        result = await self.session.execute(stmt)
        await self.session.commit()
        return result.rowcount == 1

    async def release(self, key: str, owner: str) -> None:
        stmt = (
            update(ControlPlaneResource)
            .where(ControlPlaneResource.key == key, ControlPlaneResource.lease_owner == owner)
            .values(lease_owner=None, lease_until=None)
        )
        await self.session.execute(stmt)
        await self.session.commit()

    async def sweep_candidates(self, limit: int = 100) -> list[str]:
        now = datetime.now(timezone.utc)
        stmt = (
            select(ControlPlaneResource.key)
            .where(
                or_(
                    ControlPlaneResource.observed.is_(None),
                    ControlPlaneResource.lease_until.is_(None),
                    ControlPlaneResource.lease_until < now,
                )
            )
            .order_by(ControlPlaneResource.updated_at)
            .limit(limit)
        )
        return list((await self.session.execute(stmt)).scalars().all())


def new_lease_owner() -> str:
    return str(uuid.uuid4())
