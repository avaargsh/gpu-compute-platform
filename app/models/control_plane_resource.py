"""Persistent resource envelope for control-plane desired/observed state."""

from datetime import datetime, timezone

from sqlalchemy import DateTime, ForeignKey, Integer, JSON, String
from sqlalchemy.dialects.postgresql import UUID
import uuid
from sqlalchemy.orm import Mapped, mapped_column

from app.core.database import Base


class ControlPlaneResource(Base):
    __tablename__ = "control_plane_resources"

    key: Mapped[str] = mapped_column(String(512), primary_key=True)
    kind: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    project_id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), ForeignKey("projects.id", ondelete="CASCADE"), nullable=False, index=True)
    generation: Mapped[int] = mapped_column(Integer, nullable=False, default=1)
    desired: Mapped[dict] = mapped_column(JSON, nullable=False)
    observed: Mapped[dict | None] = mapped_column(JSON, nullable=True)
    lifecycle: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    lease_owner: Mapped[str | None] = mapped_column(String(128), nullable=True, index=True)
    lease_until: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True, index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc))
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc))
