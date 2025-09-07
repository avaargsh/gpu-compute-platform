"""
WebSocket 实时推送功能
用于实时推送任务状态更新、系统通知等
"""
import json
import uuid
from datetime import datetime
from typing import Dict, List, Set, Optional, Any
from fastapi import WebSocket, WebSocketDisconnect, Depends, HTTPException
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.user import User, UserRole
from app.core.auth import get_current_user_websocket
from app.core.database import get_async_session


class ConnectionManager:
    """WebSocket连接管理器"""
    
    def __init__(self):
        # 存储所有活跃连接: {connection_id: {'websocket': WebSocket, 'user': User}}
        self.active_connections: Dict[str, Dict[str, Any]] = {}
        # 按用户ID分组连接: {user_id: set(connection_ids)}
        self.user_connections: Dict[str, Set[str]] = {}
        # 管理员连接单独管理
        self.admin_connections: Set[str] = set()
    
    async def connect(self, websocket: WebSocket, user: User) -> str:
        """接受WebSocket连接"""
        await websocket.accept()
        
        # 生成连接ID
        connection_id = str(uuid.uuid4())
        user_id = str(user.id)
        
        # 存储连接
        self.active_connections[connection_id] = {
            'websocket': websocket,
            'user': user,
            'connected_at': datetime.utcnow()
        }
        
        # 按用户分组
        if user_id not in self.user_connections:
            self.user_connections[user_id] = set()
        self.user_connections[user_id].add(connection_id)
        
        # 如果是管理员，加入管理员连接组
        if user.role == UserRole.ADMIN:
            self.admin_connections.add(connection_id)
        
        print(f"🔗 WebSocket连接建立: {user.nickname} ({user.email}) - {connection_id}")
        return connection_id
    
    def disconnect(self, connection_id: str):
        """断开WebSocket连接"""
        if connection_id in self.active_connections:
            connection_info = self.active_connections[connection_id]
            user = connection_info['user']
            user_id = str(user.id)
            
            # 从连接列表中移除
            del self.active_connections[connection_id]
            
            # 从用户连接组中移除
            if user_id in self.user_connections:
                self.user_connections[user_id].discard(connection_id)
                if not self.user_connections[user_id]:
                    del self.user_connections[user_id]
            
            # 从管理员连接组中移除
            self.admin_connections.discard(connection_id)
            
            print(f"❌ WebSocket连接断开: {user.nickname} ({user.email}) - {connection_id}")
    
    async def send_personal_message(self, message: dict, user_id: str):
        """发送个人消息给特定用户的所有连接"""
        if user_id in self.user_connections:
            disconnected_connections = []
            
            for connection_id in self.user_connections[user_id].copy():
                if connection_id in self.active_connections:
                    websocket = self.active_connections[connection_id]['websocket']
                    try:
                        await websocket.send_text(json.dumps(message, ensure_ascii=False))
                    except Exception as e:
                        print(f"❌ 发送消息失败: {e}")
                        disconnected_connections.append(connection_id)
            
            # 清理断开的连接
            for conn_id in disconnected_connections:
                self.disconnect(conn_id)
    
    async def send_to_admins(self, message: dict):
        """发送消息给所有管理员"""
        disconnected_connections = []
        
        for connection_id in self.admin_connections.copy():
            if connection_id in self.active_connections:
                websocket = self.active_connections[connection_id]['websocket']
                try:
                    await websocket.send_text(json.dumps(message, ensure_ascii=False))
                except Exception as e:
                    print(f"❌ 发送管理员消息失败: {e}")
                    disconnected_connections.append(connection_id)
        
        # 清理断开的连接
        for conn_id in disconnected_connections:
            self.disconnect(conn_id)
    
    async def broadcast_to_all(self, message: dict):
        """广播消息给所有连接"""
        disconnected_connections = []
        
        for connection_id, connection_info in self.active_connections.items():
            websocket = connection_info['websocket']
            try:
                await websocket.send_text(json.dumps(message, ensure_ascii=False))
            except Exception as e:
                print(f"❌ 广播消息失败: {e}")
                disconnected_connections.append(connection_id)
        
        # 清理断开的连接
        for conn_id in disconnected_connections:
            self.disconnect(conn_id)
    
    def get_connection_stats(self) -> dict:
        """获取连接统计信息"""
        return {
            "total_connections": len(self.active_connections),
            "total_users": len(self.user_connections),
            "admin_connections": len(self.admin_connections),
            "users_online": list(self.user_connections.keys())
        }


# 全局连接管理器实例
manager = ConnectionManager()


class WebSocketNotificationService:
    """WebSocket通知服务"""
    
    @staticmethod
    async def notify_task_status_changed(task_id: str, user_id: str, status: str, message: str = None):
        """通知任务状态变更"""
        notification = {
            "type": "task_status_update",
            "data": {
                "task_id": task_id,
                "status": status,
                "message": message or f"任务状态更新为: {status}",
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        await manager.send_personal_message(notification, user_id)
    
    @staticmethod
    async def notify_task_log_update(task_id: str, user_id: str, log_content: str):
        """通知任务日志更新"""
        notification = {
            "type": "task_log_update",
            "data": {
                "task_id": task_id,
                "log_content": log_content,
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        await manager.send_personal_message(notification, user_id)
    
    @staticmethod
    async def notify_system_alert(message: str, alert_type: str = "info", target_admins_only: bool = True):
        """发送系统警告"""
        notification = {
            "type": "system_alert",
            "data": {
                "message": message,
                "alert_type": alert_type,  # info, warning, error, success
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        
        if target_admins_only:
            await manager.send_to_admins(notification)
        else:
            await manager.broadcast_to_all(notification)
    
    @staticmethod
    async def notify_user_message(user_id: str, title: str, content: str, message_type: str = "info"):
        """发送用户消息"""
        notification = {
            "type": "user_message",
            "data": {
                "title": title,
                "content": content,
                "message_type": message_type,
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        await manager.send_personal_message(notification, user_id)
    
    @staticmethod
    async def broadcast_system_maintenance(message: str, start_time: str = None):
        """广播系统维护通知"""
        notification = {
            "type": "system_maintenance",
            "data": {
                "message": message,
                "start_time": start_time,
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        await manager.broadcast_to_all(notification)
    
    @staticmethod
    async def send_realtime_stats(stats: dict):
        """发送实时统计数据给管理员"""
        notification = {
            "type": "realtime_stats",
            "data": {
                "stats": stats,
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        await manager.send_to_admins(notification)


# WebSocket路由依赖
async def websocket_auth_dependency(
    websocket: WebSocket,
    token: Optional[str] = None
) -> User:
    """WebSocket认证依赖"""
    if not token:
        await websocket.close(code=4001, reason="未提供认证令牌")
        raise HTTPException(status_code=401, detail="未提供认证令牌")
    
    try:
        user = await get_current_user_websocket(token)
        return user
    except Exception as e:
        await websocket.close(code=4003, reason="认证失败")
        raise HTTPException(status_code=403, detail="认证失败")


async def handle_websocket_connection(
    websocket: WebSocket,
    user: User,
    session: AsyncSession
):
    """处理WebSocket连接的通用逻辑"""
    connection_id = await manager.connect(websocket, user)
    
    try:
        # 发送连接成功消息
        welcome_message = {
            "type": "connection_established",
            "data": {
                "message": f"欢迎, {user.nickname}！WebSocket连接已建立。",
                "connection_id": connection_id,
                "timestamp": datetime.utcnow().isoformat()
            }
        }
        await websocket.send_text(json.dumps(welcome_message, ensure_ascii=False))
        
        # 发送连接统计信息给管理员
        if user.role == UserRole.ADMIN:
            stats = manager.get_connection_stats()
            await WebSocketNotificationService.send_realtime_stats(stats)
        
        # 保持连接并处理消息
        while True:
            # 接收客户端消息
            try:
                data = await websocket.receive_text()
                message = json.loads(data)
                
                # 处理心跳检测
                if message.get("type") == "ping":
                    pong_message = {
                        "type": "pong",
                        "data": {"timestamp": datetime.utcnow().isoformat()}
                    }
                    await websocket.send_text(json.dumps(pong_message))
                    continue
                
                # 处理其他消息类型
                await handle_client_message(message, user, session)
                
            except json.JSONDecodeError:
                error_message = {
                    "type": "error",
                    "data": {"message": "无效的JSON消息格式"}
                }
                await websocket.send_text(json.dumps(error_message))
                
    except WebSocketDisconnect:
        print(f"🔌 WebSocket客户端断开连接: {user.nickname}")
    except Exception as e:
        print(f"❌ WebSocket连接错误: {e}")
    finally:
        manager.disconnect(connection_id)


async def handle_client_message(message: dict, user: User, session: AsyncSession):
    """处理客户端发送的消息"""
    message_type = message.get("type")
    
    if message_type == "subscribe_task_updates":
        # 客户端订阅任务更新
        task_id = message.get("data", {}).get("task_id")
        if task_id:
            # 这里可以实现任务更新订阅逻辑
            print(f"📊 用户 {user.nickname} 订阅任务 {task_id} 的更新")
    
    elif message_type == "request_stats" and user.role == UserRole.ADMIN:
        # 管理员请求实时统计
        stats = manager.get_connection_stats()
        await WebSocketNotificationService.send_realtime_stats(stats)
    
    # 可以添加更多消息处理逻辑
