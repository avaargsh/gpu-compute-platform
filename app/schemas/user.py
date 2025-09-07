from fastapi_users import schemas
from pydantic import BaseModel, EmailStr
from typing import Optional
from datetime import datetime
from app.models.user import UserRole
import uuid


class UserRead(schemas.BaseUser[uuid.UUID]):
    """Schema for reading user data (what gets returned to client)."""
    first_name: Optional[str] = None
    last_name: Optional[str] = None
    nickname: Optional[str] = None
    avatar: Optional[str] = None
    role: UserRole = UserRole.USER
    phone: Optional[str] = None
    organization: Optional[str] = None
    total_compute_hours: str = "0.0"
    total_cost: str = "0.0"
    created_at: Optional[datetime] = None
    last_login: Optional[datetime] = None


class UserCreate(schemas.BaseUserCreate):
    """Schema for creating a new user."""
    first_name: Optional[str] = None
    last_name: Optional[str] = None
    nickname: Optional[str] = None
    avatar: Optional[str] = None
    phone: Optional[str] = None
    organization: Optional[str] = None


class UserUpdate(schemas.BaseUserUpdate):
    """Schema for updating user data."""
    first_name: Optional[str] = None
    last_name: Optional[str] = None
    nickname: Optional[str] = None
    avatar: Optional[str] = None
    phone: Optional[str] = None
    organization: Optional[str] = None


class UserProfile(BaseModel):
    """用户资料更新Schema"""
    nickname: Optional[str] = None
    email: Optional[EmailStr] = None
    avatar: Optional[str] = None


class ChangePasswordRequest(BaseModel):
    """修改密码请求Schema"""
    old_password: str
    new_password: str
    confirm_password: str
