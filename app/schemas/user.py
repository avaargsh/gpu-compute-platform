from fastapi_users import schemas
from pydantic import EmailStr
from typing import Optional
import uuid


class UserRead(schemas.BaseUser[uuid.UUID]):
    """Schema for reading user data (what gets returned to client)."""
    first_name: Optional[str] = None
    last_name: Optional[str] = None
    phone: Optional[str] = None
    organization: Optional[str] = None
    total_compute_hours: str = "0.0"
    total_cost: str = "0.0"


class UserCreate(schemas.BaseUserCreate):
    """Schema for creating a new user."""
    first_name: Optional[str] = None
    last_name: Optional[str] = None
    phone: Optional[str] = None
    organization: Optional[str] = None


class UserUpdate(schemas.BaseUserUpdate):
    """Schema for updating user data."""
    first_name: Optional[str] = None
    last_name: Optional[str] = None
    phone: Optional[str] = None
    organization: Optional[str] = None
