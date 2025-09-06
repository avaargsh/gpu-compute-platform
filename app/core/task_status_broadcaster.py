import json
import logging
from typing import Dict, Any, Optional
from datetime import datetime, timezone
from enum import Enum

from app.core.websocket_manager import websocket_manager
from app.models.task import TaskStatus

logger = logging.getLogger(__name__)


class MessageType(str, Enum):
    """WebSocket消息类型"""
    TASK_STATUS_UPDATE = "task_status_update"
    TASK_PROGRESS = "task_progress"  
    TASK_LOGS = "task_logs"
    TASK_ERROR = "task_error"
    TASK_COMPLETED = "task_completed"
    TASK_CANCELLED = "task_cancelled"
    HEARTBEAT = "heartbeat"


class TaskStatusBroadcaster:
    """任务状态广播服务
    
    负责将任务状态变化实时推送到WebSocket客户端
    """
    
    def __init__(self):
        self.ws_manager = websocket_manager
    
    async def broadcast_status_update(
        self,
        task_id: str,
        status: TaskStatus,
        message: Optional[str] = None,
        progress: Optional[float] = None,
        additional_data: Optional[Dict[str, Any]] = None
    ):
        """广播任务状态更新
        
        Args:
            task_id: 任务ID
            status: 任务状态
            message: 状态消息
            progress: 进度百分比 (0-100)
            additional_data: 额外数据
        """
        try:
            # 构建状态更新消息
            status_message = {
                "type": MessageType.TASK_STATUS_UPDATE,
                "task_id": task_id,
                "status": status.value if hasattr(status, 'value') else str(status),
                "message": message or f"Task status updated to {status}",
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            if progress is not None:
                status_message["progress"] = max(0, min(100, progress))
            
            if additional_data:
                status_message["data"] = additional_data
            
            # 广播到所有订阅该任务的连接
            await self.ws_manager.broadcast_to_task(task_id, status_message)
            
            logger.info(f"Broadcasted status update for task {task_id}: {status}")
            
        except Exception as e:
            logger.error(f"Failed to broadcast status update for task {task_id}: {e}")
    
    async def broadcast_progress_update(
        self,
        task_id: str,
        progress: float,
        message: Optional[str] = None,
        step_info: Optional[Dict[str, Any]] = None
    ):
        """广播任务进度更新
        
        Args:
            task_id: 任务ID
            progress: 进度百分比 (0-100)
            message: 进度消息
            step_info: 步骤信息
        """
        try:
            progress_message = {
                "type": MessageType.TASK_PROGRESS,
                "task_id": task_id,
                "progress": max(0, min(100, progress)),
                "message": message or f"Task progress: {progress:.1f}%",
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            if step_info:
                progress_message["step_info"] = step_info
            
            await self.ws_manager.broadcast_to_task(task_id, progress_message)
            
            logger.debug(f"Broadcasted progress update for task {task_id}: {progress}%")
            
        except Exception as e:
            logger.error(f"Failed to broadcast progress update for task {task_id}: {e}")
    
    async def broadcast_logs(
        self,
        task_id: str,
        logs: str,
        level: str = "INFO",
        source: str = "worker"
    ):
        """广播任务日志
        
        Args:
            task_id: 任务ID
            logs: 日志内容
            level: 日志级别
            source: 日志来源
        """
        try:
            log_message = {
                "type": MessageType.TASK_LOGS,
                "task_id": task_id,
                "logs": logs,
                "level": level.upper(),
                "source": source,
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            await self.ws_manager.broadcast_to_task(task_id, log_message)
            
            logger.debug(f"Broadcasted logs for task {task_id} ({len(logs)} chars)")
            
        except Exception as e:
            logger.error(f"Failed to broadcast logs for task {task_id}: {e}")
    
    async def broadcast_error(
        self,
        task_id: str,
        error_message: str,
        error_code: Optional[str] = None,
        error_details: Optional[Dict[str, Any]] = None
    ):
        """广播任务错误
        
        Args:
            task_id: 任务ID
            error_message: 错误消息
            error_code: 错误代码
            error_details: 错误详情
        """
        try:
            error_msg = {
                "type": MessageType.TASK_ERROR,
                "task_id": task_id,
                "error_message": error_message,
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            if error_code:
                error_msg["error_code"] = error_code
            
            if error_details:
                error_msg["error_details"] = error_details
            
            await self.ws_manager.broadcast_to_task(task_id, error_msg)
            
            logger.info(f"Broadcasted error for task {task_id}: {error_message}")
            
        except Exception as e:
            logger.error(f"Failed to broadcast error for task {task_id}: {e}")
    
    async def broadcast_task_completed(
        self,
        task_id: str,
        success: bool,
        result_data: Optional[Dict[str, Any]] = None,
        execution_time: Optional[float] = None,
        cost_info: Optional[Dict[str, Any]] = None
    ):
        """广播任务完成
        
        Args:
            task_id: 任务ID
            success: 是否成功
            result_data: 结果数据
            execution_time: 执行时间（秒）
            cost_info: 成本信息
        """
        try:
            completion_message = {
                "type": MessageType.TASK_COMPLETED,
                "task_id": task_id,
                "success": success,
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            if result_data:
                completion_message["result_data"] = result_data
            
            if execution_time is not None:
                completion_message["execution_time"] = execution_time
            
            if cost_info:
                completion_message["cost_info"] = cost_info
            
            await self.ws_manager.broadcast_to_task(task_id, completion_message)
            
            logger.info(f"Broadcasted completion for task {task_id}: success={success}")
            
        except Exception as e:
            logger.error(f"Failed to broadcast completion for task {task_id}: {e}")
    
    async def broadcast_task_cancelled(
        self,
        task_id: str,
        reason: Optional[str] = None
    ):
        """广播任务取消
        
        Args:
            task_id: 任务ID
            reason: 取消原因
        """
        try:
            cancellation_message = {
                "type": MessageType.TASK_CANCELLED,
                "task_id": task_id,
                "reason": reason or "Task was cancelled by user",
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            await self.ws_manager.broadcast_to_task(task_id, cancellation_message)
            
            logger.info(f"Broadcasted cancellation for task {task_id}")
            
        except Exception as e:
            logger.error(f"Failed to broadcast cancellation for task {task_id}: {e}")
    
    async def send_heartbeat(self, task_id: str):
        """发送心跳消息
        
        Args:
            task_id: 任务ID
        """
        try:
            heartbeat_message = {
                "type": MessageType.HEARTBEAT,
                "task_id": task_id,
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "message": "heartbeat"
            }
            
            await self.ws_manager.broadcast_to_task(task_id, heartbeat_message)
            
        except Exception as e:
            logger.error(f"Failed to send heartbeat for task {task_id}: {e}")
    
    async def broadcast_custom_message(
        self,
        task_id: str,
        message_type: str,
        data: Dict[str, Any]
    ):
        """广播自定义消息
        
        Args:
            task_id: 任务ID
            message_type: 消息类型
            data: 消息数据
        """
        try:
            custom_message = {
                "type": message_type,
                "task_id": task_id,
                "timestamp": datetime.now(timezone.utc).isoformat(),
                **data
            }
            
            await self.ws_manager.broadcast_to_task(task_id, custom_message)
            
            logger.debug(f"Broadcasted custom message for task {task_id}: {message_type}")
            
        except Exception as e:
            logger.error(f"Failed to broadcast custom message for task {task_id}: {e}")
    
    def get_connection_count(self, task_id: str) -> int:
        """获取指定任务的连接数量
        
        Args:
            task_id: 任务ID
            
        Returns:
            连接数量
        """
        return self.ws_manager.get_connection_count(task_id)
    
    def has_active_connections(self, task_id: str) -> bool:
        """检查指定任务是否有活跃连接
        
        Args:
            task_id: 任务ID
            
        Returns:
            是否有活跃连接
        """
        return self.get_connection_count(task_id) > 0


# 全局任务状态广播器实例
task_broadcaster = TaskStatusBroadcaster()


# 便捷函数
import inspect

async def _maybe_await(result):
    """在需要时等待协程结果，兼容被MagicMock替换的情况（非异步）"""
    if inspect.isawaitable(result):
        return await result
    return result


async def broadcast_task_status(
    task_id: str,
    status: TaskStatus,
    message: Optional[str] = None,
    progress: Optional[float] = None,
    **kwargs
):
    """便捷函数：广播任务状态更新"""
    result = task_broadcaster.broadcast_status_update(
        task_id, status, message, progress, kwargs
    )
    return await _maybe_await(result)


async def broadcast_task_progress(
    task_id: str,
    progress: float,
    message: Optional[str] = None,
    **kwargs
):
    """便捷函数：广播任务进度更新"""
    result = task_broadcaster.broadcast_progress_update(
        task_id, progress, message, kwargs
    )
    return await _maybe_await(result)


async def broadcast_task_logs(
    task_id: str,
    logs: str,
    level: str = "INFO",
    source: str = "worker"
):
    """便捷函数：广播任务日志"""
    result = task_broadcaster.broadcast_logs(task_id, logs, level, source)
    return await _maybe_await(result)


async def broadcast_task_error(
    task_id: str,
    error_message: str,
    error_code: Optional[str] = None,
    **kwargs
):
    """便捷函数：广播任务错误"""
    result = task_broadcaster.broadcast_error(
        task_id, error_message, error_code, kwargs
    )
    return await _maybe_await(result)
