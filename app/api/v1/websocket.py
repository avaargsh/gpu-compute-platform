"""
WebSocket API 路由
实现实时通信功能
"""
from fastapi import APIRouter, WebSocket, WebSocketDisconnect, Depends, Query
from sqlalchemy.ext.asyncio import AsyncSession
from typing import Optional

from app.core.database import get_async_session
from app.core.websocket import (
    handle_websocket_connection,
    websocket_auth_dependency,
    manager,
    WebSocketNotificationService
)
from app.models.user import User
from app.core.auth import get_current_user_websocket

router = APIRouter()


@router.websocket("/ws")
async def websocket_endpoint(
    websocket: WebSocket,
    token: Optional[str] = Query(None),
    session: AsyncSession = Depends(get_async_session)
):
    """
    WebSocket连接端点
    客户端需要提供JWT token进行认证
    URL: ws://localhost:8000/api/v1/ws?token=<jwt_token>
    """
    # 认证用户
    try:
        if not token:
            await websocket.close(code=4001, reason="未提供认证令牌")
            return
        
        user = await get_current_user_websocket(token)
        
        # 处理WebSocket连接
        await handle_websocket_connection(websocket, user, session)
        
    except Exception as e:
        try:
            await websocket.close(code=4003, reason=f"认证失败: {str(e)}")
        except:
            pass
        print(f"❌ WebSocket认证失败: {e}")


@router.websocket("/ws/admin")
async def admin_websocket_endpoint(
    websocket: WebSocket,
    token: Optional[str] = Query(None),
    session: AsyncSession = Depends(get_async_session)
):
    """
    管理员专用WebSocket连接端点
    只允许管理员用户连接
    """
    try:
        if not token:
            await websocket.close(code=4001, reason="未提供认证令牌")
            return
        
        user = await get_current_user_websocket(token)
        
        # 检查管理员权限
        if user.role.value != 'admin':
            await websocket.close(code=4003, reason="需要管理员权限")
            return
        
        # 处理WebSocket连接
        await handle_websocket_connection(websocket, user, session)
        
    except Exception as e:
        try:
            await websocket.close(code=4003, reason=f"认证失败: {str(e)}")
        except:
            pass
        print(f"❌ 管理员WebSocket认证失败: {e}")


# WebSocket管理API（用于调试和监控）
from fastapi import HTTPException, status
from app.core.permissions import AdminRequired


@router.get("/ws/stats")
async def get_websocket_stats(
    current_user: User = AdminRequired()
):
    """获取WebSocket连接统计信息（仅管理员）"""
    return manager.get_connection_stats()


@router.post("/ws/test/broadcast")
async def test_broadcast_message(
    message: str,
    current_user: User = AdminRequired()
):
    """测试广播消息（仅管理员）"""
    await WebSocketNotificationService.notify_system_alert(
        message=message,
        alert_type="info",
        target_admins_only=False
    )
    return {"message": "广播消息已发送"}


@router.post("/ws/test/admin-message")
async def test_admin_message(
    message: str,
    current_user: User = AdminRequired()
):
    """测试管理员消息（仅管理员）"""
    await WebSocketNotificationService.notify_system_alert(
        message=message,
        alert_type="warning",
        target_admins_only=True
    )
    return {"message": "管理员消息已发送"}


@router.post("/ws/test/user-message")
async def test_user_message(
    user_id: str,
    title: str,
    content: str,
    message_type: str = "info",
    current_user: User = AdminRequired()
):
    """测试用户消息（仅管理员）"""
    await WebSocketNotificationService.notify_user_message(
        user_id=user_id,
        title=title,
        content=content,
        message_type=message_type
    )
    return {"message": f"消息已发送给用户 {user_id}"}


@router.post("/ws/test/task-update")
async def test_task_status_update(
    task_id: str,
    user_id: str,
    status: str,
    message: str = None,
    current_user: User = AdminRequired()
):
    """测试任务状态更新通知（仅管理员）"""
    await WebSocketNotificationService.notify_task_status_changed(
        task_id=task_id,
        user_id=user_id,
        status=status,
        message=message
    )
    return {"message": f"任务状态更新通知已发送"}


@router.post("/ws/test/maintenance")
async def test_maintenance_notification(
    message: str,
    start_time: Optional[str] = None,
    current_user: User = AdminRequired()
):
    """测试系统维护通知（仅管理员）"""
    await WebSocketNotificationService.broadcast_system_maintenance(
        message=message,
        start_time=start_time
    )
    return {"message": "系统维护通知已发送"}
