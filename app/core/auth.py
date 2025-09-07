from fastapi import Depends, Request, HTTPException, status
from fastapi_users import BaseUserManager, FastAPIUsers, UUIDIDMixin
from fastapi_users.authentication import (
    AuthenticationBackend,
    BearerTransport,
    JWTStrategy,
)
from fastapi_users.db import SQLAlchemyUserDatabase
from sqlalchemy.ext.asyncio import AsyncSession
from jose import jwt, JWTError

from app.core.config import settings
from app.core.database import get_async_session
from app.models.user import User
from app.schemas.user import UserCreate
from typing import Optional, Union, Dict, Any
import uuid


class UserManager(UUIDIDMixin, BaseUserManager[User, uuid.UUID]):
    reset_password_token_secret = settings.secret_key
    verification_token_secret = settings.secret_key

    async def on_after_register(self, user: User, request: Optional[Request] = None):
        print(f"User {user.id} has registered.")

    async def on_after_forgot_password(
        self, user: User, token: str, request: Optional[Request] = None
    ):
        print(f"User {user.id} has forgot their password. Reset token: {token}")

    async def on_after_request_verify(
        self, user: User, token: str, request: Optional[Request] = None
    ):
        print(f"Verification requested for user {user.id}. Verification token: {token}")


async def get_user_db(session: AsyncSession = Depends(get_async_session)):
    yield SQLAlchemyUserDatabase(session, User)


async def get_user_manager(user_db: SQLAlchemyUserDatabase = Depends(get_user_db)):
    yield UserManager(user_db)


def get_jwt_strategy() -> JWTStrategy:
    return JWTStrategy(
        secret=settings.secret_key, 
        lifetime_seconds=settings.access_token_expire_minutes * 60
    )

# Authentication backend configuration
bearer_transport = BearerTransport(tokenUrl="auth/jwt/login")

auth_backend = AuthenticationBackend(
    name="jwt",
    transport=bearer_transport,
    get_strategy=get_jwt_strategy,
)

# FastAPI Users instance
fastapi_users = FastAPIUsers[User, uuid.UUID](get_user_manager, [auth_backend])

# Dependencies
current_active_user = fastapi_users.current_user(active=True)
current_superuser = fastapi_users.current_user(active=True, superuser=True)

# Standard dependency for routes
get_current_user = fastapi_users.current_user()


async def get_current_user_websocket(token: str) -> User:
    """从JWT令牌中获取当前用户（用于WebSocket认证）"""
    try:
        # 解码JWT令牌
        payload = jwt.decode(
            token,
            settings.secret_key,
            algorithms=[settings.algorithm]
        )
        user_id = payload.get("sub")
        if user_id is None:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="无效的认证凭据",
            )
    except JWTError:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="无效的认证令牌",
        )
    
    # 通过用户ID获取用户对象
    async for session in get_async_session():
        async for user_manager in get_user_manager(SQLAlchemyUserDatabase(session, User)):
            try:
                user = await user_manager.get(uuid.UUID(user_id))
                if not user.is_active:
                    raise HTTPException(
                        status_code=status.HTTP_401_UNAUTHORIZED,
                        detail="账户未激活",
                    )
                return user
            except Exception as e:
                raise HTTPException(
                    status_code=status.HTTP_401_UNAUTHORIZED,
                    detail=f"无法获取用户: {str(e)}",
                )
    
    raise HTTPException(
        status_code=status.HTTP_401_UNAUTHORIZED,
        detail="无法获取用户",
    )
