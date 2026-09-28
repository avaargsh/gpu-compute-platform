"""add control-plane v2 resource and revision tables

Revision ID: 20260928_cp_v2
Revises:
Create Date: 2026-09-28
"""

from alembic import op
import sqlalchemy as sa

revision = "20260928_cp_v2"
down_revision = None
branch_labels = ("control_plane_v2",)
depends_on = None


def upgrade() -> None:
    op.create_table(
        "control_plane_resources",
        sa.Column("key", sa.String(length=512), primary_key=True),
        sa.Column("kind", sa.String(length=64), nullable=False),
        sa.Column("project_id", sa.String(length=36), nullable=False),
        sa.Column("generation", sa.Integer(), nullable=False, server_default="1"),
        sa.Column("desired", sa.JSON(), nullable=False),
        sa.Column("observed", sa.JSON(), nullable=True),
        sa.Column("lifecycle", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_control_plane_resources_kind", "control_plane_resources", ["kind"])
    op.create_index("ix_control_plane_resources_project_id", "control_plane_resources", ["project_id"])
    op.create_table(
        "control_plane_resource_revisions",
        sa.Column("id", sa.Integer(), primary_key=True, autoincrement=True),
        sa.Column("resource_key", sa.String(length=512), nullable=False),
        sa.Column("generation", sa.Integer(), nullable=False),
        sa.Column("provider_ref", sa.String(length=512), nullable=True),
        sa.Column("phase", sa.String(length=32), nullable=False),
        sa.Column("evidence_refs", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("terminal_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("garbage_collected_at", sa.DateTime(timezone=True), nullable=True),
        sa.UniqueConstraint("resource_key", "generation", name="uq_cp_resource_revision"),
    )
    op.create_index("ix_control_plane_resource_revisions_resource_key", "control_plane_resource_revisions", ["resource_key"])


def downgrade() -> None:
    op.drop_index("ix_control_plane_resource_revisions_resource_key", table_name="control_plane_resource_revisions")
    op.drop_table("control_plane_resource_revisions")
    op.drop_index("ix_control_plane_resources_owner_id", table_name="control_plane_resources")
    op.drop_index("ix_control_plane_resources_kind", table_name="control_plane_resources")
    op.drop_table("control_plane_resources")
