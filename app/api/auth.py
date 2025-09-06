from fastapi import APIRouter, Depends
from fastapi_users import FastAPIUsers

from app.core.auth import auth_backend, fastapi_users
from app.models.user import User
from app.schemas.user import UserRead, UserCreate, UserUpdate
import uuid

router = APIRouter()

# Include auth routes
router.include_router(
    fastapi_users.get_auth_router(auth_backend), 
    prefix="/jwt", 
    tags=["auth"]
)

router.include_router(
    fastapi_users.get_register_router(UserRead, UserCreate),
    prefix="",
    tags=["auth"],
)

router.include_router(
    fastapi_users.get_reset_password_router(),
    prefix="",
    tags=["auth"],
)

router.include_router(
    fastapi_users.get_verify_router(UserRead),
    prefix="",
    tags=["auth"],
)

router.include_router(
    fastapi_users.get_users_router(UserRead, UserUpdate),
    prefix="/users",
    tags=["users"],
)
