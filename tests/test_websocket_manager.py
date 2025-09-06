import pytest
import asyncio
import json
from datetime import datetime, timezone, timedelta
from unittest.mock import AsyncMock, MagicMock, patch
from fastapi import WebSocket

from app.core.websocket_manager import WebSocketManager, websocket_manager


class MockWebSocket:
    """模拟WebSocket连接"""
    
    def __init__(self):
        self.accepted = False
        self.closed = False
        self.sent_messages = []
        self.received_messages = []
        self.close_code = None
        self.close_reason = None
    
    async def accept(self):
        self.accepted = True
    
    async def send_text(self, text: str):
        self.sent_messages.append(text)
    
    async def receive_text(self):
        if self.received_messages:
            return self.received_messages.pop(0)
        # 模拟等待消息
        await asyncio.sleep(0.1)
        return '{"type": "ping"}'
    
    async def close(self, code: int = 1000, reason: str = ""):
        self.closed = True
        self.close_code = code
        self.close_reason = reason
    
    def add_message(self, message: str):
        self.received_messages.append(message)


@pytest.fixture
def ws_manager():
    """创建WebSocket管理器实例"""
    return WebSocketManager()


@pytest.fixture
def mock_websocket():
    """创建模拟WebSocket连接"""
    return MockWebSocket()


@pytest.mark.asyncio
class TestWebSocketManager:
    """WebSocket管理器测试"""
    
    async def test_connect(self, ws_manager, mock_websocket):
        """测试WebSocket连接建立"""
        task_id = "test-task-123"
        user_id = "user-456"
        
        connection_id = await ws_manager.connect(mock_websocket, task_id, user_id)
        
        # 验证连接建立
        assert connection_id is not None
        assert connection_id.startswith("conn_")
        assert mock_websocket.accepted
        
        # 验证连接被记录
        assert task_id in ws_manager.active_connections
        assert connection_id in ws_manager.active_connections[task_id]
        assert connection_id in ws_manager.connection_metadata
        
        # 验证连接元数据
        metadata = ws_manager.connection_metadata[connection_id]
        assert metadata["task_id"] == task_id
        assert metadata["user_id"] == user_id
        assert "connected_at" in metadata
        assert "last_ping" in metadata
        
        # 验证发送了连接确认消息
        assert len(mock_websocket.sent_messages) == 1
        sent_message = json.loads(mock_websocket.sent_messages[0])
        assert sent_message["type"] == "connection_established"
        assert sent_message["task_id"] == task_id
        assert sent_message["connection_id"] == connection_id
    
    async def test_disconnect(self, ws_manager, mock_websocket):
        """测试WebSocket连接断开"""
        task_id = "test-task-123"
        user_id = "user-456"
        
        # 建立连接
        connection_id = await ws_manager.connect(mock_websocket, task_id, user_id)
        
        # 断开连接
        await ws_manager.disconnect(connection_id)
        
        # 验证连接被清理
        assert task_id not in ws_manager.active_connections
        assert connection_id not in ws_manager.connection_metadata
        assert connection_id not in ws_manager.connection_task_mapping
    
    async def test_broadcast_to_task(self, ws_manager):
        """测试向任务广播消息"""
        task_id = "test-task-123"
        user_id = "user-456"
        
        # 建立多个连接到同一任务
        mock_ws1 = MockWebSocket()
        mock_ws2 = MockWebSocket()
        
        conn_id1 = await ws_manager.connect(mock_ws1, task_id, user_id)
        conn_id2 = await ws_manager.connect(mock_ws2, task_id, user_id)
        
        # 广播消息
        test_message = {
            "type": "test_message",
            "data": "Hello World"
        }
        
        await ws_manager.broadcast_to_task(task_id, test_message)
        
        # 验证所有连接都收到消息
        assert len(mock_ws1.sent_messages) == 2  # 连接确认 + 广播消息
        assert len(mock_ws2.sent_messages) == 2  # 连接确认 + 广播消息
        
        # 验证广播消息内容
        broadcast_msg1 = json.loads(mock_ws1.sent_messages[1])
        broadcast_msg2 = json.loads(mock_ws2.sent_messages[1])
        
        assert broadcast_msg1["type"] == "test_message"
        assert broadcast_msg1["data"] == "Hello World"
        assert "timestamp" in broadcast_msg1
        
        assert broadcast_msg2["type"] == "test_message"
        assert broadcast_msg2["data"] == "Hello World"
    
    async def test_send_to_connection(self, ws_manager, mock_websocket):
        """测试向指定连接发送消息"""
        task_id = "test-task-123"
        user_id = "user-456"
        
        connection_id = await ws_manager.connect(mock_websocket, task_id, user_id)
        
        test_message = {
            "type": "direct_message",
            "content": "Direct message"
        }
        
        await ws_manager.send_to_connection(connection_id, test_message)
        
        # 验证消息被发送
        assert len(mock_websocket.sent_messages) == 2  # 连接确认 + 直接消息
        direct_msg = json.loads(mock_websocket.sent_messages[1])
        assert direct_msg["type"] == "direct_message"
        assert direct_msg["content"] == "Direct message"
    
    async def test_handle_ping(self, ws_manager, mock_websocket):
        """测试心跳处理"""
        task_id = "test-task-123"
        user_id = "user-456"
        
        connection_id = await ws_manager.connect(mock_websocket, task_id, user_id)
        
        # 记录原始ping时间
        original_ping = ws_manager.connection_metadata[connection_id]["last_ping"]
        
        # 等待一小段时间确保时间戳不同
        await asyncio.sleep(0.001)
        
        # 处理ping
        await ws_manager.handle_ping(connection_id)
        
        # 验证ping时间被更新
        new_ping = ws_manager.connection_metadata[connection_id]["last_ping"]
        assert new_ping > original_ping
        
        # 验证发送了pong响应
        assert len(mock_websocket.sent_messages) == 2  # 连接确认 + pong
        pong_msg = json.loads(mock_websocket.sent_messages[1])
        assert pong_msg["type"] == "pong"
    
    def test_get_task_connections(self, ws_manager):
        """测试获取任务连接"""
        # 测试不存在的任务
        connections = ws_manager.get_task_connections("nonexistent-task")
        assert connections == []
    
    def test_get_connection_count(self, ws_manager):
        """测试获取连接数量"""
        # 测试总连接数
        total_count = ws_manager.get_connection_count()
        assert total_count == 0
        
        # 测试特定任务连接数
        task_count = ws_manager.get_connection_count("nonexistent-task")
        assert task_count == 0
    
    def test_get_active_tasks(self, ws_manager):
        """测试获取活跃任务"""
        active_tasks = ws_manager.get_active_tasks()
        assert active_tasks == []
    
    def test_get_connection_info(self, ws_manager):
        """测试获取连接信息"""
        info = ws_manager.get_connection_info("nonexistent-connection")
        assert info == {}
    
    async def test_cleanup_stale_connections(self, ws_manager):
        """测试清理过期连接"""
        task_id = "test-task-123"
        user_id = "user-456"
        
        mock_ws = MockWebSocket()
        connection_id = await ws_manager.connect(mock_ws, task_id, user_id)
        
        # 手动设置过期时间
        past_time = datetime.now(timezone.utc) - timedelta(minutes=31)
        ws_manager.connection_metadata[connection_id]["last_ping"] = past_time
        
        # 清理过期连接
        await ws_manager.cleanup_stale_connections(timeout_minutes=30)
        
        # 验证连接被清理
        assert connection_id not in ws_manager.connection_metadata
        assert task_id not in ws_manager.active_connections
    
    def test_get_statistics(self, ws_manager):
        """测试获取统计信息"""
        stats = ws_manager.get_statistics()
        
        assert "total_connections" in stats
        assert "active_tasks" in stats
        assert "task_connection_counts" in stats
        assert "timestamp" in stats
        
        assert stats["total_connections"] == 0
        assert stats["active_tasks"] == 0
        assert stats["task_connection_counts"] == {}
    
    async def test_multiple_tasks_multiple_connections(self, ws_manager):
        """测试多任务多连接场景"""
        task1 = "task-1"
        task2 = "task-2"
        user_id = "user-456"
        
        # 为任务1建立2个连接
        mock_ws1_1 = MockWebSocket()
        mock_ws1_2 = MockWebSocket()
        conn1_1 = await ws_manager.connect(mock_ws1_1, task1, user_id)
        conn1_2 = await ws_manager.connect(mock_ws1_2, task1, user_id)
        
        # 为任务2建立1个连接
        mock_ws2_1 = MockWebSocket()
        conn2_1 = await ws_manager.connect(mock_ws2_1, task2, user_id)
        
        # 验证连接统计
        assert ws_manager.get_connection_count() == 3
        assert ws_manager.get_connection_count(task1) == 2
        assert ws_manager.get_connection_count(task2) == 1
        
        # 验证活跃任务
        active_tasks = ws_manager.get_active_tasks()
        assert len(active_tasks) == 2
        assert task1 in active_tasks
        assert task2 in active_tasks
        
        # 广播到任务1
        message1 = {"type": "task1_message", "data": "Task 1"}
        await ws_manager.broadcast_to_task(task1, message1)
        
        # 验证只有任务1的连接收到消息
        assert len(mock_ws1_1.sent_messages) == 2  # 连接确认 + 广播
        assert len(mock_ws1_2.sent_messages) == 2  # 连接确认 + 广播
        assert len(mock_ws2_1.sent_messages) == 1  # 只有连接确认
        
        # 断开任务1的一个连接
        await ws_manager.disconnect(conn1_1)
        
        # 验证统计更新
        assert ws_manager.get_connection_count() == 2
        assert ws_manager.get_connection_count(task1) == 1
        assert ws_manager.get_connection_count(task2) == 1
        
        # 断开任务1的剩余连接
        await ws_manager.disconnect(conn1_2)
        
        # 验证任务1组被清理
        assert task1 not in ws_manager.active_connections
        assert ws_manager.get_connection_count(task1) == 0
        
        # 任务2仍然存在
        active_tasks = ws_manager.get_active_tasks()
        assert len(active_tasks) == 1
        assert task2 in active_tasks


@pytest.mark.asyncio
class TestGlobalWebSocketManager:
    """全局WebSocket管理器测试"""
    
    async def test_global_manager_instance(self):
        """测试全局管理器实例"""
        # 验证全局实例存在
        assert websocket_manager is not None
        assert isinstance(websocket_manager, WebSocketManager)
    
    async def test_context_manager(self):
        """测试上下文管理器"""
        from app.core.websocket_manager import get_websocket_manager
        
        async with get_websocket_manager() as manager:
            assert manager is websocket_manager
