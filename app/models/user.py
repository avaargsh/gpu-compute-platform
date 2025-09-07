from fastapi_users.db import SQLAlchemyBaseUserTableUUID
from sqlalchemy import Column, String, Boolean, DateTime, func, Enum as SQLEnum
from sqlalchemy.dialects.postgresql import UUID
from sqlalchemy.orm import relationship
from app.core.database import Base
from enum import Enum
import uuid


class UserRole(str, Enum):
    """用户角色枚举"""
    ADMIN = "admin"
    USER = "user"


class User(SQLAlchemyBaseUserTableUUID, Base):
    __tablename__ = "users"
    
    # Additional custom fields beyond the base FastAPI Users fields
    first_name = Column(String(50), nullable=True)
    last_name = Column(String(50), nullable=True)
    nickname = Column(String(50), nullable=True, comment="昵称")
    avatar = Column(String(500), nullable=True, comment="头像URL")
    phone = Column(String(20), nullable=True)
    organization = Column(String(100), nullable=True)
    
    # Role and permissions
    role = Column(SQLEnum(UserRole), default=UserRole.USER, nullable=False, comment="用户角色")
    
    # Usage tracking
    total_compute_hours = Column(String, default="0.0")  # Store as string to avoid precision issues
    total_cost = Column(String, default="0.0")
    
    # Account status
    is_active = Column(Boolean, default=True)
    is_superuser = Column(Boolean, default=False)
    is_verified = Column(Boolean, default=False)
    
    # Timestamps
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), onupdate=func.now())
    last_login = Column(DateTime(timezone=True), nullable=True, comment="最后登录时间")
    
    # Relationships
    gpu_tasks = relationship("GpuTask", back_populates="user")
    task_dags = relationship("TaskDAG", back_populates="user")
    
    def __repr__(self):
        return f"<User(id={self.id}, email={self.email})>"
