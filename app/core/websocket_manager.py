import json
import logging
from typing import Dict, List, Set, Any
from datetime import datetime, timezone
from fastapi import WebSocket, WebSocketDisconnect
import asyncio
from contextlib import asynccontextmanager

logger = logging.getLogger(__name__)


class WebSocketManager:
    """WebSocket连接管理器
    
    管理客户端WebSocket连接，支持按任务ID分组连接，
    提供实时状态推送功能。
    """
    
    def __init__(self):
        # 存储活跃连接 {task_id: {connection_id: websocket}}
        self.active_connections: Dict[str, Dict[str, WebSocket]] = {}
        # 存储连接元数据 {connection_id: metadata}
        self.connection_metadata: Dict[str, Dict[str, Any]] = {}
        # 全局连接映射 {connection_id: task_id}
        self.connection_task_mapping: Dict[str, str] = {}
        # 连接计数器
        self._connection_counter = 0
        # 锁保护并发操作
        self._lock = asyncio.Lock()
    
    def _generate_connection_id(self) -> str:
        """生成唯一连接ID"""
        self._connection_counter += 1
        return f"conn_{self._connection_counter}_{int(datetime.now().timestamp())}"
    
    async def connect(
        self, 
        websocket: WebSocket, 
        task_id: str, 
        user_id: str
    ) -> str:
        """建立WebSocket连接
        
        Args:
            websocket: WebSocket连接对象
            task_id: 任务ID
            user_id: 用户ID
            
        Returns:
            connection_id: 连接ID
        """
        await websocket.accept()
        
        async with self._lock:
            connection_id = self._generate_connection_id()
            
            # 初始化任务连接组
            if task_id not in self.active_connections:
                self.active_connections[task_id] = {}
            
            # 添加连接
            self.active_connections[task_id][connection_id] = websocket
            
            # 存储连接元数据
            self.connection_metadata[connection_id] = {
                "task_id": task_id,
                "user_id": user_id,
                "connected_at": datetime.now(timezone.utc),
                "last_ping": datetime.now(timezone.utc)
            }
            
            # 更新连接映射
            self.connection_task_mapping[connection_id] = task_id
            
            logger.info(f"WebSocket connected: {connection_id} for task {task_id}")
            
            # 发送连接确认消息
            await self._send_to_connection(connection_id, {
                "type": "connection_established",
                "connection_id": connection_id,
                "task_id": task_id,
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "message": "WebSocket connection established"
            })
            
            return connection_id
    
    async def disconnect(self, connection_id: str):
        """断开WebSocket连接
        
        Args:
            connection_id: 连接ID
        """
        async with self._lock:
            if connection_id in self.connection_metadata:
                task_id = self.connection_metadata[connection_id]["task_id"]
                
                # 移除连接
                if task_id in self.active_connections:
                    self.active_connections[task_id].pop(connection_id, None)
                    
                    # 如果任务组没有连接了，清理任务组
                    if not self.active_connections[task_id]:
                        del self.active_connections[task_id]
                
                # 清理元数据
                del self.connection_metadata[connection_id]
                
                # 清理连接映射
                self.connection_task_mapping.pop(connection_id, None)
                
                logger.info(f"WebSocket disconnected: {connection_id} for task {task_id}")
    
    async def broadcast_to_task(self, task_id: str, message: Dict[str, Any]):
        """向指定任务的所有连接广播消息
        
        Args:
            task_id: 任务ID
            message: 要发送的消息
        """
        if task_id not in self.active_connections:
            logger.debug(f"No active connections for task {task_id}")
            return
        
        # 添加时间戳
        message_with_timestamp = {
            **message,
            "timestamp": datetime.now(timezone.utc).isoformat()
        }
        
        # 获取所有连接
        connections = list(self.active_connections[task_id].items())
        
        # 并发发送给所有连接
        send_tasks = []
        for connection_id, websocket in connections:
            send_tasks.append(
                self._send_to_websocket(websocket, connection_id, message_with_timestamp)
            )
        
        if send_tasks:
            await asyncio.gather(*send_tasks, return_exceptions=True)
            logger.info(f"Broadcasted message to {len(send_tasks)} connections for task {task_id}")
    
    async def send_to_connection(self, connection_id: str, message: Dict[str, Any]):
        """向指定连接发送消息
        
        Args:
            connection_id: 连接ID
            message: 要发送的消息
        """
        await self._send_to_connection(connection_id, message)
    
    async def _send_to_connection(self, connection_id: str, message: Dict[str, Any]):
        """内部方法：向指定连接发送消息"""
        if connection_id not in self.connection_metadata:
            logger.warning(f"Connection {connection_id} not found")
            return
        
        task_id = self.connection_metadata[connection_id]["task_id"]
        
        if (task_id in self.active_connections and 
            connection_id in self.active_connections[task_id]):
            websocket = self.active_connections[task_id][connection_id]
            await self._send_to_websocket(websocket, connection_id, message)
    
    async def _send_to_websocket(
        self, 
        websocket: WebSocket, 
        connection_id: str, 
        message: Dict[str, Any]
    ):
        """内部方法：向WebSocket发送消息"""
        try:
            await websocket.send_text(json.dumps(message, default=str))
        except WebSocketDisconnect:
            logger.info(f"WebSocket {connection_id} disconnected during send")
            await self.disconnect(connection_id)
        except Exception as e:
            logger.error(f"Failed to send message to WebSocket {connection_id}: {e}")
            await self.disconnect(connection_id)
    
    async def handle_ping(self, connection_id: str):
        """处理连接心跳
        
        Args:
            connection_id: 连接ID
        """
        if connection_id in self.connection_metadata:
            self.connection_metadata[connection_id]["last_ping"] = datetime.now(timezone.utc)
            
            await self._send_to_connection(connection_id, {
                "type": "pong",
                "timestamp": datetime.now(timezone.utc).isoformat()
            })
    
    def get_task_connections(self, task_id: str) -> List[str]:
        """获取指定任务的所有连接ID
        
        Args:
            task_id: 任务ID
            
        Returns:
            连接ID列表
        """
        if task_id in self.active_connections:
            return list(self.active_connections[task_id].keys())
        return []
    
    def get_connection_count(self, task_id: str = None) -> int:
        """获取连接数量
        
        Args:
            task_id: 任务ID，如果为None则返回总连接数
            
        Returns:
            连接数量
        """
        if task_id:
            return len(self.active_connections.get(task_id, {}))
        return len(self.connection_metadata)
    
    def get_active_tasks(self) -> List[str]:
        """获取有活跃连接的任务ID列表
        
        Returns:
            任务ID列表
        """
        return list(self.active_connections.keys())
    
    def get_connection_info(self, connection_id: str) -> Dict[str, Any]:
        """获取连接信息
        
        Args:
            connection_id: 连接ID
            
        Returns:
            连接信息
        """
        return self.connection_metadata.get(connection_id, {})
    
    async def cleanup_stale_connections(self, timeout_minutes: int = 30):
        """清理过期连接
        
        Args:
            timeout_minutes: 超时时间（分钟）
        """
        from datetime import timedelta
        
        cutoff_time = datetime.now(timezone.utc) - timedelta(minutes=timeout_minutes)
        stale_connections = []
        
        for connection_id, metadata in self.connection_metadata.items():
            if metadata["last_ping"] < cutoff_time:
                stale_connections.append(connection_id)
        
        for connection_id in stale_connections:
            logger.info(f"Cleaning up stale connection: {connection_id}")
            await self.disconnect(connection_id)
        
        if stale_connections:
            logger.info(f"Cleaned up {len(stale_connections)} stale connections")
    
    def get_statistics(self) -> Dict[str, Any]:
        """获取连接统计信息
        
        Returns:
            统计信息
        """
        total_connections = len(self.connection_metadata)
        active_tasks = len(self.active_connections)
        
        task_connection_counts = {
            task_id: len(connections) 
            for task_id, connections in self.active_connections.items()
        }
        
        return {
            "total_connections": total_connections,
            "active_tasks": active_tasks,
            "task_connection_counts": task_connection_counts,
            "timestamp": datetime.now(timezone.utc).isoformat()
        }


# 全局WebSocket管理器实例
websocket_manager = WebSocketManager()


@asynccontextmanager
async def get_websocket_manager():
    """获取WebSocket管理器实例的上下文管理器"""
    yield websocket_manager


# 定期清理任务
async def cleanup_stale_connections_periodic():
    """定期清理过期连接的后台任务"""
    while True:
        try:
            await websocket_manager.cleanup_stale_connections()
            # 每10分钟清理一次
            await asyncio.sleep(600)
        except Exception as e:
            logger.error(f"Error in periodic connection cleanup: {e}")
            await asyncio.sleep(600)
