"""Persistent immutable revision history for control-plane resources."""

from datetime import datetime, timezone

from sqlalchemy import DateTime, Integer, JSON, String, UniqueConstraint
from sqlalchemy.orm import Mapped, mapped_column

from app.core.database import Base


class ControlPlaneResourceRevision(Base):
    __tablename__ = "control_plane_resource_revisions"
    __table_args__ = (
        UniqueConstraint("resource_key", "generation", name="uq_cp_resource_revision"),
    )

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    resource_key: Mapped[str] = mapped_column(String(512), nullable=False, index=True)
    generation: Mapped[int] = mapped_column(Integer, nullable=False)
    provider_ref: Mapped[str | None] = mapped_column(String(512), nullable=True)
    phase: Mapped[str] = mapped_column(String(32), nullable=False)
    evidence_refs: Mapped[list] = mapped_column(JSON, nullable=False, default=list)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc))
    terminal_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    garbage_collected_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
