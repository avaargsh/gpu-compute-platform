"""
权限控制中间件
实现基于角色的权限控制
"""
from functools import wraps
from typing import List, Optional, Callable
from fastapi import HTTPException, status, Depends
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials

from app.models.user import User, UserRole
from app.core.auth import get_current_user


security = HTTPBearer()


class PermissionError(HTTPException):
    """权限错误异常"""
    def __init__(self, detail: str = "权限不足"):
        super().__init__(
            status_code=status.HTTP_403_FORBIDDEN,
            detail=detail
        )


def require_roles(allowed_roles: List[UserRole]):
    """
    角色权限装饰器
    只允许指定角色的用户访问
    """
    def decorator(func: Callable):
        @wraps(func)
        async def wrapper(*args, **kwargs):
            # 从参数中获取当前用户
            current_user = None
            for arg in args:
                if isinstance(arg, User):
                    current_user = arg
                    break
            
            # 如果没有找到用户参数，从kwargs中查找
            if not current_user:
                for key, value in kwargs.items():
                    if isinstance(value, User):
                        current_user = value
                        break
            
            if not current_user:
                raise PermissionError("无法获取用户信息")
            
            if current_user.role not in allowed_roles:
                raise PermissionError(
                    f"需要以下角色权限之一: {[role.value for role in allowed_roles]}"
                )
            
            return await func(*args, **kwargs)
        return wrapper
    return decorator


def require_admin(current_user: User = Depends(get_current_user)):
    """
    管理员权限依赖
    只允许管理员访问
    """
    if current_user.role != UserRole.ADMIN:
        raise PermissionError("需要管理员权限")
    return current_user


def require_user_or_admin(current_user: User = Depends(get_current_user)):
    """
    普通用户或管理员权限依赖
    允许普通用户和管理员访问
    """
    if current_user.role not in [UserRole.USER, UserRole.ADMIN]:
        raise PermissionError("需要用户或管理员权限")
    return current_user


def require_active_user(current_user: User = Depends(get_current_user)):
    """
    活跃用户权限依赖
    只允许活跃用户访问
    """
    if not current_user.is_active:
        raise PermissionError("账户已被禁用")
    if not current_user.is_verified:
        raise PermissionError("账户尚未验证")
    return current_user


def check_resource_ownership(
    resource_user_id: str,
    current_user: User,
    allow_admin_access: bool = True
) -> bool:
    """
    检查资源所有权
    
    Args:
        resource_user_id: 资源所属用户ID
        current_user: 当前用户
        allow_admin_access: 是否允许管理员访问
        
    Returns:
        bool: 是否有权限访问
    """
    # 管理员可以访问所有资源（如果允许）
    if allow_admin_access and current_user.role == UserRole.ADMIN:
        return True
    
    # 用户只能访问自己的资源
    return str(current_user.id) == str(resource_user_id)


def require_resource_access(
    resource_user_id: str,
    allow_admin_access: bool = True
):
    """
    资源访问权限装饰器
    确保用户只能访问自己的资源，管理员可访问所有资源
    """
    def dependency(current_user: User = Depends(get_current_user)):
        if not check_resource_ownership(resource_user_id, current_user, allow_admin_access):
            raise PermissionError("无权限访问此资源")
        return current_user
    
    return dependency


class PermissionChecker:
    """权限检查工具类"""
    
    @staticmethod
    def can_access_user_profile(target_user_id: str, current_user: User) -> bool:
        """检查是否可以访问用户资料"""
        return check_resource_ownership(target_user_id, current_user)
    
    @staticmethod
    def can_modify_user(target_user_id: str, current_user: User) -> bool:
        """检查是否可以修改用户信息"""
        # 只有管理员或用户本人可以修改
        if current_user.role == UserRole.ADMIN:
            return True
        return str(current_user.id) == str(target_user_id)
    
    @staticmethod
    def can_delete_user(target_user_id: str, current_user: User) -> bool:
        """检查是否可以删除用户"""
        # 只有管理员可以删除用户
        return current_user.role == UserRole.ADMIN
    
    @staticmethod
    def can_access_task(task_user_id: str, current_user: User) -> bool:
        """检查是否可以访问任务"""
        return check_resource_ownership(task_user_id, current_user)
    
    @staticmethod
    def can_modify_task(task_user_id: str, current_user: User) -> bool:
        """检查是否可以修改任务"""
        return check_resource_ownership(task_user_id, current_user)
    
    @staticmethod
    def can_delete_task(task_user_id: str, current_user: User) -> bool:
        """检查是否可以删除任务"""
        return check_resource_ownership(task_user_id, current_user)
    
    @staticmethod
    def can_view_all_tasks(current_user: User) -> bool:
        """检查是否可以查看所有任务"""
        return current_user.role == UserRole.ADMIN
    
    @staticmethod
    def can_access_system_stats(current_user: User) -> bool:
        """检查是否可以访问系统统计"""
        return current_user.role == UserRole.ADMIN
    
    @staticmethod
    def can_manage_system(current_user: User) -> bool:
        """检查是否可以管理系统"""
        return current_user.role == UserRole.ADMIN
    
    @staticmethod
    def can_view_pricing_details(current_user: User) -> bool:
        """检查是否可以查看详细价格信息"""
        return current_user.role == UserRole.ADMIN


# 常用权限依赖快捷方式
def AdminRequired():
    """管理员权限依赖快捷方式"""
    return Depends(require_admin)


def UserRequired():
    """用户权限依赖快捷方式"""
    return Depends(require_user_or_admin)


def ActiveUserRequired():
    """活跃用户权限依赖快捷方式"""
    return Depends(require_active_user)
