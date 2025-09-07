from fastapi import APIRouter, Depends, HTTPException, status, Request
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from fastapi_users.authentication import JWTAuthentication
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select, update
from datetime import datetime, timezone
from typing import Optional

from app.core.database import get_async_session
from app.core.auth import current_active_user, get_user_manager, UserManager
from app.models.user import User, UserRole
from app.schemas.user import UserRead, UserCreate, UserProfile, ChangePasswordRequest
from app.schemas.task import ApiResponse
from fastapi_users.password import PasswordHelper
import uuid

router = APIRouter()

# HTTP Bearer for token authentication
bearer_scheme = HTTPBearer()


@router.post("/login", response_model=dict)
async def login_for_access_token(
    request: Request,
    session: AsyncSession = Depends(get_async_session),
    user_manager: UserManager = Depends(get_user_manager)
):
    """
    用户登录接口
    兼容前端LoginForm格式
    """
    body = await request.json()
    username = body.get("username")
    password = body.get("password")
    remember = body.get("remember", False)
    
    if not username or not password:
        return {
            "success": False,
            "error": "用户名和密码不能为空",
            "message": "用户名和密码不能为空"
        }
    
    try:
        # 查找用户（支持用户名或邮箱登录）
        stmt = select(User).where(
            (User.email == username) | (User.email == username.lower())
        )
        result = await session.execute(stmt)
        user = result.scalar_one_or_none()
        
        if not user:
            return {
                "success": False,
                "error": "用户名或密码错误",
                "message": "用户名或密码错误"
            }
        
        # 验证密码
        password_helper = PasswordHelper()
        is_valid, updated_password_hash = password_helper.verify_and_update(
            password, user.hashed_password
        )
        
        if not is_valid:
            return {
                "success": False,
                "error": "用户名或密码错误", 
                "message": "用户名或密码错误"
            }
        
        # 检查用户是否激活
        if not user.is_active:
            return {
                "success": False,
                "error": "用户账户已被禁用",
                "message": "用户账户已被禁用"
            }
        
        # 更新最后登录时间
        await session.execute(
            update(User).where(User.id == user.id).values(
                last_login=datetime.now(timezone.utc)
            )
        )
        await session.commit()
        
        # 生成JWT token
        from app.core.auth import get_jwt_strategy
        jwt_strategy = get_jwt_strategy()
        token = await jwt_strategy.write_token(user)
        
        # 构造返回的用户数据
        user_data = {
            "id": str(user.id),
            "username": user.email,  # 前端期望的username字段
            "email": user.email,
            "nickname": user.nickname or user.email.split("@")[0],
            "avatar": user.avatar,
            "role": user.role.value,
            "token": token,
            "created_at": user.created_at.isoformat() if user.created_at else None,
            "last_login": user.last_login.isoformat() if user.last_login else None,
            "is_active": user.is_active
        }
        
        return {
            "success": True,
            "data": user_data,
            "message": "登录成功"
        }
        
    except Exception as e:
        print(f"Login error: {e}")
        return {
            "success": False,
            "error": "登录过程中发生错误",
            "message": "登录过程中发生错误"
        }


@router.post("/register", response_model=dict)
async def register_user(
    request: Request,
    session: AsyncSession = Depends(get_async_session),
    user_manager: UserManager = Depends(get_user_manager)
):
    """
    用户注册接口
    兼容前端RegisterForm格式
    """
    body = await request.json()
    
    # 提取注册信息
    username = body.get("username")
    email = body.get("email") 
    password = body.get("password")
    confirm_password = body.get("confirmPassword")
    nickname = body.get("nickname")
    agreement = body.get("agreement", False)
    
    # 验证必填字段
    if not all([username, email, password, confirm_password, nickname]):
        return {
            "success": False,
            "error": "请填写所有必填字段",
            "message": "请填写所有必填字段"
        }
    
    # 验证协议同意
    if not agreement:
        return {
            "success": False,
            "error": "请先阅读并同意用户协议",
            "message": "请先阅读并同意用户协议"
        }
    
    # 验证密码一致性
    if password != confirm_password:
        return {
            "success": False,
            "error": "密码和确认密码不一致",
            "message": "密码和确认密码不一致"
        }
    
    try:
        # 检查邮箱是否已存在
        stmt = select(User).where(User.email == email.lower())
        result = await session.execute(stmt)
        existing_user = result.scalar_one_or_none()
        
        if existing_user:
            return {
                "success": False,
                "error": "该邮箱已被注册",
                "message": "该邮箱已被注册"
            }
        
        # 创建用户数据
        user_create_data = {
            "email": email.lower(),
            "password": password,
            "nickname": nickname,
            "is_active": True,
            "is_verified": True  # 简化流程，直接验证
        }
        
        # 使用FastAPI Users创建用户
        user_create_schema = UserCreate(**user_create_data)
        created_user = await user_manager.create(user_create_schema, safe=True, request=request)
        
        # 生成JWT token
        from app.core.auth import get_jwt_strategy
        jwt_strategy = get_jwt_strategy()
        token = await jwt_strategy.write_token(created_user)
        
        # 构造返回的用户数据
        user_data = {
            "id": str(created_user.id),
            "username": created_user.email,
            "email": created_user.email,
            "nickname": created_user.nickname,
            "avatar": created_user.avatar,
            "role": created_user.role.value,
            "token": token,
            "created_at": created_user.created_at.isoformat() if created_user.created_at else None,
            "last_login": None,
            "is_active": created_user.is_active
        }
        
        return {
            "success": True,
            "data": user_data,
            "message": "注册成功"
        }
        
    except Exception as e:
        print(f"Register error: {e}")
        return {
            "success": False,
            "error": "注册过程中发生错误",
            "message": str(e) if "already exists" in str(e) else "注册过程中发生错误"
        }


@router.post("/logout", response_model=dict)
async def logout_user(current_user: User = Depends(current_active_user)):
    """
    用户登出接口
    """
    return {
        "success": True,
        "message": "登出成功"
    }


@router.get("/user", response_model=dict)
async def get_current_user(current_user: User = Depends(current_active_user)):
    """
    获取当前用户信息接口
    """
    user_data = {
        "id": str(current_user.id),
        "username": current_user.email,
        "email": current_user.email,
        "nickname": current_user.nickname or current_user.email.split("@")[0],
        "avatar": current_user.avatar,
        "role": current_user.role.value,
        "created_at": current_user.created_at.isoformat() if current_user.created_at else None,
        "last_login": current_user.last_login.isoformat() if current_user.last_login else None,
        "is_active": current_user.is_active
    }
    
    return {
        "success": True,
        "data": user_data
    }


@router.put("/profile", response_model=dict)
async def update_user_profile(
    profile_data: UserProfile,
    session: AsyncSession = Depends(get_async_session),
    current_user: User = Depends(current_active_user)
):
    """
    更新用户资料接口
    """
    try:
        # 更新用户资料
        update_data = {}
        if profile_data.nickname is not None:
            update_data["nickname"] = profile_data.nickname
        if profile_data.email is not None:
            # 检查新邮箱是否已被使用
            if profile_data.email != current_user.email:
                stmt = select(User).where(User.email == profile_data.email.lower())
                result = await session.execute(stmt)
                existing_user = result.scalar_one_or_none()
                if existing_user:
                    return {
                        "success": False,
                        "error": "该邮箱已被使用",
                        "message": "该邮箱已被使用"
                    }
            update_data["email"] = profile_data.email.lower()
        if profile_data.avatar is not None:
            update_data["avatar"] = profile_data.avatar
        
        if update_data:
            update_data["updated_at"] = datetime.now(timezone.utc)
            await session.execute(
                update(User).where(User.id == current_user.id).values(**update_data)
            )
            await session.commit()
        
        # 重新查询更新后的用户数据
        stmt = select(User).where(User.id == current_user.id)
        result = await session.execute(stmt)
        updated_user = result.scalar_one()
        
        user_data = {
            "id": str(updated_user.id),
            "username": updated_user.email,
            "email": updated_user.email,
            "nickname": updated_user.nickname or updated_user.email.split("@")[0],
            "avatar": updated_user.avatar,
            "role": updated_user.role.value,
            "created_at": updated_user.created_at.isoformat() if updated_user.created_at else None,
            "last_login": updated_user.last_login.isoformat() if updated_user.last_login else None,
            "is_active": updated_user.is_active
        }
        
        return {
            "success": True,
            "data": user_data,
            "message": "资料更新成功"
        }
        
    except Exception as e:
        print(f"Update profile error: {e}")
        return {
            "success": False,
            "error": "更新资料时发生错误",
            "message": "更新资料时发生错误"
        }


@router.put("/password", response_model=dict)
async def change_user_password(
    password_data: ChangePasswordRequest,
    session: AsyncSession = Depends(get_async_session),
    current_user: User = Depends(current_active_user)
):
    """
    修改用户密码接口
    """
    try:
        # 验证新密码和确认密码是否一致
        if password_data.new_password != password_data.confirm_password:
            return {
                "success": False,
                "error": "新密码和确认密码不一致",
                "message": "新密码和确认密码不一致"
            }
        
        # 验证旧密码
        password_helper = PasswordHelper()
        is_valid, _ = password_helper.verify_and_update(
            password_data.old_password, current_user.hashed_password
        )
        
        if not is_valid:
            return {
                "success": False,
                "error": "当前密码错误",
                "message": "当前密码错误"
            }
        
        # 验证新密码强度
        if len(password_data.new_password) < 6:
            return {
                "success": False,
                "error": "新密码至少需要6个字符",
                "message": "新密码至少需要6个字符"
            }
        
        # 更新密码
        new_hashed_password = password_helper.hash(password_data.new_password)
        await session.execute(
            update(User).where(User.id == current_user.id).values(
                hashed_password=new_hashed_password,
                updated_at=datetime.now(timezone.utc)
            )
        )
        await session.commit()
        
        return {
            "success": True,
            "message": "密码修改成功"
        }
        
    except Exception as e:
        print(f"Change password error: {e}")
        return {
            "success": False,
            "error": "修改密码时发生错误",
            "message": "修改密码时发生错误"
        }


@router.post("/refresh", response_model=dict)
async def refresh_access_token(current_user: User = Depends(current_active_user)):
    """
    刷新访问令牌接口
    """
    try:
        # 生成新的JWT token
        from app.core.auth import get_jwt_strategy
        jwt_strategy = get_jwt_strategy()
        new_token = await jwt_strategy.write_token(current_user)
        
        return {
            "success": True,
            "data": {
                "token": new_token
            },
            "message": "令牌刷新成功"
        }
        
    except Exception as e:
        print(f"Refresh token error: {e}")
        return {
            "success": False,
            "error": "刷新令牌时发生错误",
            "message": "刷新令牌时发生错误"
        }
