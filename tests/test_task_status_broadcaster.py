import pytest
import asyncio
import json
from unittest.mock import AsyncMock, MagicMock, patch
from datetime import datetime, timezone

from app.core.task_status_broadcaster import (
    TaskStatusBroadcaster, 
    MessageType,
    task_broadcaster,
    broadcast_task_status,
    broadcast_task_progress,
    broadcast_task_logs,
    broadcast_task_error
)
from app.models.task import TaskStatus


@pytest.fixture
def mock_ws_manager():
    """创建模拟WebSocket管理器"""
    mock_manager = AsyncMock()
    mock_manager.broadcast_to_task = AsyncMock()
    mock_manager.get_connection_count = MagicMock(return_value=2)
    return mock_manager


@pytest.fixture
def broadcaster(mock_ws_manager):
    """创建任务状态广播器实例"""
    broadcaster = TaskStatusBroadcaster()
    broadcaster.ws_manager = mock_ws_manager
    return broadcaster


@pytest.mark.asyncio
class TestTaskStatusBroadcaster:
    """任务状态广播器测试"""
    
    async def test_broadcast_status_update(self, broadcaster, mock_ws_manager):
        """测试广播状态更新"""
        task_id = "test-task-123"
        status = TaskStatus.RUNNING
        message = "Task is now running"
        progress = 50.0
        additional_data = {"step": "processing"}
        
        await broadcaster.broadcast_status_update(
            task_id, status, message, progress, additional_data
        )
        
        # 验证调用WebSocket管理器
        mock_ws_manager.broadcast_to_task.assert_called_once()
        call_args = mock_ws_manager.broadcast_to_task.call_args
        
        assert call_args[0][0] == task_id  # task_id参数
        
        # 验证消息内容
        message_data = call_args[0][1]
        assert message_data["type"] == MessageType.TASK_STATUS_UPDATE
        assert message_data["task_id"] == task_id
        assert message_data["status"] == status.value
        assert message_data["message"] == message
        assert message_data["progress"] == progress
        assert message_data["data"] == additional_data
        assert "timestamp" in message_data
    
    async def test_broadcast_status_update_without_optional_params(self, broadcaster, mock_ws_manager):
        """测试广播状态更新（不包含可选参数）"""
        task_id = "test-task-123"
        status = TaskStatus.COMPLETED
        
        await broadcaster.broadcast_status_update(task_id, status)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.TASK_STATUS_UPDATE
        assert message_data["task_id"] == task_id
        assert message_data["status"] == status.value
        assert message_data["message"] == f"Task status updated to {status}"
        assert "progress" not in message_data
        assert "data" not in message_data
    
    async def test_broadcast_progress_update(self, broadcaster, mock_ws_manager):
        """测试广播进度更新"""
        task_id = "test-task-123"
        progress = 75.5
        message = "Processing data"
        step_info = {"current_step": 3, "total_steps": 4}
        
        await broadcaster.broadcast_progress_update(
            task_id, progress, message, step_info
        )
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.TASK_PROGRESS
        assert message_data["task_id"] == task_id
        assert message_data["progress"] == progress
        assert message_data["message"] == message
        assert message_data["step_info"] == step_info
    
    async def test_broadcast_progress_update_bounds(self, broadcaster, mock_ws_manager):
        """测试进度更新边界值处理"""
        task_id = "test-task-123"
        
        # 测试超出上限
        await broadcaster.broadcast_progress_update(task_id, 150.0)
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        assert message_data["progress"] == 100.0
        
        # 测试低于下限
        await broadcaster.broadcast_progress_update(task_id, -10.0)
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        assert message_data["progress"] == 0.0
    
    async def test_broadcast_logs(self, broadcaster, mock_ws_manager):
        """测试广播日志"""
        task_id = "test-task-123"
        logs = "Processing started successfully"
        level = "INFO"
        source = "worker"
        
        await broadcaster.broadcast_logs(task_id, logs, level, source)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.TASK_LOGS
        assert message_data["task_id"] == task_id
        assert message_data["logs"] == logs
        assert message_data["level"] == level.upper()
        assert message_data["source"] == source
    
    async def test_broadcast_logs_default_params(self, broadcaster, mock_ws_manager):
        """测试广播日志（使用默认参数）"""
        task_id = "test-task-123"
        logs = "Default log message"
        
        await broadcaster.broadcast_logs(task_id, logs)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["level"] == "INFO"
        assert message_data["source"] == "worker"
    
    async def test_broadcast_error(self, broadcaster, mock_ws_manager):
        """测试广播错误"""
        task_id = "test-task-123"
        error_message = "Connection failed"
        error_code = "CONNECTION_ERROR"
        error_details = {"retry_count": 3, "last_attempt": "2024-01-01T10:00:00Z"}
        
        await broadcaster.broadcast_error(
            task_id, error_message, error_code, error_details
        )
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.TASK_ERROR
        assert message_data["task_id"] == task_id
        assert message_data["error_message"] == error_message
        assert message_data["error_code"] == error_code
        assert message_data["error_details"] == error_details
    
    async def test_broadcast_task_completed(self, broadcaster, mock_ws_manager):
        """测试广播任务完成"""
        task_id = "test-task-123"
        success = True
        result_data = {"output_file": "/path/to/output.txt", "rows_processed": 1000}
        execution_time = 120.5
        cost_info = {"total_cost": 2.50, "currency": "USD"}
        
        await broadcaster.broadcast_task_completed(
            task_id, success, result_data, execution_time, cost_info
        )
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.TASK_COMPLETED
        assert message_data["task_id"] == task_id
        assert message_data["success"] == success
        assert message_data["result_data"] == result_data
        assert message_data["execution_time"] == execution_time
        assert message_data["cost_info"] == cost_info
    
    async def test_broadcast_task_cancelled(self, broadcaster, mock_ws_manager):
        """测试广播任务取消"""
        task_id = "test-task-123"
        reason = "User requested cancellation"
        
        await broadcaster.broadcast_task_cancelled(task_id, reason)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.TASK_CANCELLED
        assert message_data["task_id"] == task_id
        assert message_data["reason"] == reason
    
    async def test_broadcast_task_cancelled_default_reason(self, broadcaster, mock_ws_manager):
        """测试广播任务取消（使用默认原因）"""
        task_id = "test-task-123"
        
        await broadcaster.broadcast_task_cancelled(task_id)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["reason"] == "Task was cancelled by user"
    
    async def test_send_heartbeat(self, broadcaster, mock_ws_manager):
        """测试发送心跳"""
        task_id = "test-task-123"
        
        await broadcaster.send_heartbeat(task_id)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == MessageType.HEARTBEAT
        assert message_data["task_id"] == task_id
        assert message_data["message"] == "heartbeat"
    
    async def test_broadcast_custom_message(self, broadcaster, mock_ws_manager):
        """测试广播自定义消息"""
        task_id = "test-task-123"
        message_type = "custom_event"
        data = {"custom_field": "custom_value", "number": 42}
        
        await broadcaster.broadcast_custom_message(task_id, message_type, data)
        
        call_args = mock_ws_manager.broadcast_to_task.call_args
        message_data = call_args[0][1]
        
        assert message_data["type"] == message_type
        assert message_data["task_id"] == task_id
        assert message_data["custom_field"] == "custom_value"
        assert message_data["number"] == 42
        assert "timestamp" in message_data
    
    def test_get_connection_count(self, broadcaster, mock_ws_manager):
        """测试获取连接数量"""
        task_id = "test-task-123"
        
        count = broadcaster.get_connection_count(task_id)
        
        mock_ws_manager.get_connection_count.assert_called_once_with(task_id)
        assert count == 2
    
    def test_has_active_connections(self, broadcaster, mock_ws_manager):
        """测试检查活跃连接"""
        task_id = "test-task-123"
        
        # 有连接的情况
        mock_ws_manager.get_connection_count.return_value = 2
        assert broadcaster.has_active_connections(task_id) == True
        
        # 无连接的情况
        mock_ws_manager.get_connection_count.return_value = 0
        assert broadcaster.has_active_connections(task_id) == False
    
    async def test_broadcast_error_handling(self, broadcaster):
        """测试广播错误处理"""
        task_id = "test-task-123"
        
        # 模拟WebSocket管理器抛出异常
        mock_manager = AsyncMock()
        mock_manager.broadcast_to_task.side_effect = Exception("WebSocket error")
        broadcaster.ws_manager = mock_manager
        
        # 广播不应该抛出异常
        await broadcaster.broadcast_status_update(task_id, TaskStatus.RUNNING)
        
        # 验证方法被调用（即使失败）
        mock_manager.broadcast_to_task.assert_called_once()


@pytest.mark.asyncio
class TestConvenienceFunctions:
    """便捷函数测试"""
    
    @patch('app.core.task_status_broadcaster.task_broadcaster')
    async def test_broadcast_task_status(self, mock_broadcaster):
        """测试便捷函数：广播任务状态"""
        task_id = "test-task-123"
        status = TaskStatus.RUNNING
        message = "Task running"
        progress = 50.0
        extra_data = {"key": "value"}
        
        await broadcast_task_status(
            task_id, status, message, progress, extra_key="extra_value"
        )
        
        mock_broadcaster.broadcast_status_update.assert_called_once_with(
            task_id, status, message, progress, {"extra_key": "extra_value"}
        )
    
    @patch('app.core.task_status_broadcaster.task_broadcaster')
    async def test_broadcast_task_progress(self, mock_broadcaster):
        """测试便捷函数：广播任务进度"""
        task_id = "test-task-123"
        progress = 75.0
        message = "Processing"
        extra_data = {"step": "data_processing"}
        
        await broadcast_task_progress(
            task_id, progress, message, step="data_processing"
        )
        
        mock_broadcaster.broadcast_progress_update.assert_called_once_with(
            task_id, progress, message, {"step": "data_processing"}
        )
    
    @patch('app.core.task_status_broadcaster.task_broadcaster')
    async def test_broadcast_task_logs(self, mock_broadcaster):
        """测试便捷函数：广播任务日志"""
        task_id = "test-task-123"
        logs = "Log message"
        level = "ERROR"
        source = "scheduler"
        
        await broadcast_task_logs(task_id, logs, level, source)
        
        mock_broadcaster.broadcast_logs.assert_called_once_with(
            task_id, logs, level, source
        )
    
    @patch('app.core.task_status_broadcaster.task_broadcaster')
    async def test_broadcast_task_error(self, mock_broadcaster):
        """测试便捷函数：广播任务错误"""
        task_id = "test-task-123"
        error_message = "Error occurred"
        error_code = "SYSTEM_ERROR"
        extra_data = {"details": "detailed info"}
        
        await broadcast_task_error(
            task_id, error_message, error_code, details="detailed info"
        )
        
        mock_broadcaster.broadcast_error.assert_called_once_with(
            task_id, error_message, error_code, {"details": "detailed info"}
        )


@pytest.mark.asyncio 
class TestGlobalBroadcaster:
    """全局广播器测试"""
    
    def test_global_broadcaster_instance(self):
        """测试全局广播器实例"""
        assert task_broadcaster is not None
        assert isinstance(task_broadcaster, TaskStatusBroadcaster)
    
    def test_message_type_enum(self):
        """测试消息类型枚举"""
        assert MessageType.TASK_STATUS_UPDATE == "task_status_update"
        assert MessageType.TASK_PROGRESS == "task_progress"
        assert MessageType.TASK_LOGS == "task_logs"
        assert MessageType.TASK_ERROR == "task_error"
        assert MessageType.TASK_COMPLETED == "task_completed"
        assert MessageType.TASK_CANCELLED == "task_cancelled"
        assert MessageType.HEARTBEAT == "heartbeat"
