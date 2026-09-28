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
    user_role = sa.Enum("ADMIN", "USER", name="userrole")
    op.create_table(
        "users",
        sa.Column("id", sa.Uuid(), primary_key=True),
        sa.Column("email", sa.String(length=320), nullable=False),
        sa.Column("hashed_password", sa.String(length=1024), nullable=False),
        sa.Column("is_active", sa.Boolean(), nullable=False, server_default=sa.true()),
        sa.Column("is_superuser", sa.Boolean(), nullable=False, server_default=sa.false()),
        sa.Column("is_verified", sa.Boolean(), nullable=False, server_default=sa.false()),
        sa.Column("first_name", sa.String(length=50), nullable=True),
        sa.Column("last_name", sa.String(length=50), nullable=True),
        sa.Column("nickname", sa.String(length=50), nullable=True),
        sa.Column("avatar", sa.String(length=500), nullable=True),
        sa.Column("phone", sa.String(length=20), nullable=True),
        sa.Column("organization", sa.String(length=100), nullable=True),
        sa.Column("role", user_role, nullable=False, server_default="USER"),
        sa.Column("total_compute_hours", sa.String(), nullable=True, server_default="0.0"),
        sa.Column("total_cost", sa.String(), nullable=True, server_default="0.0"),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=True, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("last_login", sa.DateTime(timezone=True), nullable=True),
        sa.UniqueConstraint("email", name="uq_users_email"),
    )
    op.create_index("ix_users_email", "users", ["email"], unique=True)
    op.create_table("tenants", sa.Column("id", sa.String(length=36), primary_key=True), sa.Column("name", sa.String(length=128), nullable=False), sa.Column("created_at", sa.DateTime(timezone=True), nullable=False))
    op.create_table("projects", sa.Column("id", sa.String(length=36), primary_key=True), sa.Column("tenant_id", sa.String(length=36), sa.ForeignKey("tenants.id", ondelete="CASCADE"), nullable=False), sa.Column("name", sa.String(length=128), nullable=False), sa.Column("created_at", sa.DateTime(timezone=True), nullable=False), sa.UniqueConstraint("tenant_id", "name", name="uq_project_tenant_name"))
    op.create_index("ix_projects_tenant_id", "projects", ["tenant_id"])
    op.create_table("project_members", sa.Column("id", sa.Integer(), primary_key=True, autoincrement=True), sa.Column("project_id", sa.String(length=36), sa.ForeignKey("projects.id", ondelete="CASCADE"), nullable=False), sa.Column("user_id", sa.String(length=36), nullable=False), sa.Column("role", sa.String(length=32), nullable=False), sa.UniqueConstraint("project_id", "user_id", name="uq_project_member"))
    op.create_index("ix_project_members_project_id", "project_members", ["project_id"])
    op.create_index("ix_project_members_user_id", "project_members", ["user_id"])
    op.create_table(
        "control_plane_resources",
        sa.Column("key", sa.String(length=512), primary_key=True),
        sa.Column("kind", sa.String(length=64), nullable=False),
        sa.Column("project_id", sa.String(length=36), sa.ForeignKey("projects.id", ondelete="CASCADE"), nullable=False),
        sa.Column("generation", sa.Integer(), nullable=False, server_default="1"),
        sa.Column("desired", sa.JSON(), nullable=False),
        sa.Column("observed", sa.JSON(), nullable=True),
        sa.Column("observed_generation", sa.Integer(), nullable=True),
        sa.Column("lifecycle", sa.JSON(), nullable=False),
        sa.Column("lease_owner", sa.String(length=128), nullable=True),
        sa.Column("lease_until", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_control_plane_resources_kind", "control_plane_resources", ["kind"])
    op.create_index("ix_control_plane_resources_project_id", "control_plane_resources", ["project_id"])
    op.create_index("ix_control_plane_resources_observed_generation", "control_plane_resources", ["observed_generation"])
    op.create_index("ix_control_plane_resources_lease_owner", "control_plane_resources", ["lease_owner"])
    op.create_index("ix_control_plane_resources_lease_until", "control_plane_resources", ["lease_until"])
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
    op.drop_index("ix_control_plane_resources_lease_until", table_name="control_plane_resources")
    op.drop_index("ix_control_plane_resources_lease_owner", table_name="control_plane_resources")
    op.drop_index("ix_control_plane_resources_observed_generation", table_name="control_plane_resources")
    op.drop_index("ix_control_plane_resources_project_id", table_name="control_plane_resources")
    op.drop_index("ix_control_plane_resources_kind", table_name="control_plane_resources")
    op.drop_table("control_plane_resources")
    op.drop_index("ix_project_members_user_id", table_name="project_members")
    op.drop_index("ix_project_members_project_id", table_name="project_members")
    op.drop_table("project_members")
    op.drop_index("ix_projects_tenant_id", table_name="projects")
    op.drop_table("projects")
    op.drop_table("tenants")
    op.drop_index("ix_users_email", table_name="users")
    op.drop_table("users")
    sa.Enum(name="userrole").drop(op.get_bind(), checkfirst=True)
