#!/usr/bin/env python3
"""
GPU计算平台WebSocket客户端示例

此示例展示如何连接到GPU计算平台的WebSocket端点，
实时接收任务状态更新、进度信息、日志和错误消息。

使用方法:
    python websocket_client_example.py --task-id <task_id> --token <auth_token>

依赖:
    pip install websockets httpx
"""

import asyncio
import json
import argparse
import logging
from datetime import datetime
from typing import Optional, Dict, Any
import websockets
from websockets.exceptions import ConnectionClosed, WebSocketException
import httpx

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class GPUTaskWebSocketClient:
    """GPU任务WebSocket客户端"""
    
    def __init__(self, base_url: str = "ws://localhost:8000", auth_token: Optional[str] = None):
        self.base_url = base_url.replace("http://", "ws://").replace("https://", "wss://")
        self.auth_token = auth_token
        self.websocket = None
        self.connected = False
        self.task_id = None
        self.heartbeat_task = None
    
    async def connect(self, task_id: str) -> bool:
        """连接到指定任务的WebSocket端点
        
        Args:
            task_id: 任务ID
            
        Returns:
            是否连接成功
        """
        self.task_id = task_id
        ws_url = f"{self.base_url}/api/gpu/tasks/{task_id}/ws"
        
        # 构建请求头
        headers = {}
        if self.auth_token:
            headers["Authorization"] = f"Bearer {self.auth_token}"
        
        try:
            logger.info(f"Connecting to WebSocket: {ws_url}")
            self.websocket = await websockets.connect(
                ws_url,
                extra_headers=headers,
                ping_interval=20,
                ping_timeout=10
            )
            self.connected = True
            
            # 启动心跳任务
            self.heartbeat_task = asyncio.create_task(self._heartbeat_loop())
            
            logger.info(f"Successfully connected to task {task_id}")
            return True
            
        except Exception as e:
            logger.error(f"Failed to connect: {e}")
            return False
    
    async def disconnect(self):
        """断开WebSocket连接"""
        self.connected = False
        
        if self.heartbeat_task:
            self.heartbeat_task.cancel()
            try:
                await self.heartbeat_task
            except asyncio.CancelledError:
                pass
        
        if self.websocket:
            try:
                await self.websocket.close()
            except:
                pass
        
        logger.info("WebSocket connection closed")
    
    async def listen(self):
        """监听WebSocket消息"""
        if not self.websocket or not self.connected:
            raise RuntimeError("WebSocket not connected")
        
        try:
            async for message in self.websocket:
                try:
                    data = json.loads(message)
                    await self._handle_message(data)
                except json.JSONDecodeError:
                    logger.warning(f"Received invalid JSON message: {message}")
                except Exception as e:
                    logger.error(f"Error handling message: {e}")
        
        except ConnectionClosed:
            logger.info("WebSocket connection closed by server")
            self.connected = False
        except WebSocketException as e:
            logger.error(f"WebSocket error: {e}")
            self.connected = False
        except Exception as e:
            logger.error(f"Unexpected error in message listener: {e}")
            self.connected = False
    
    async def send_ping(self):
        """发送心跳消息"""
        if self.websocket and self.connected:
            try:
                ping_message = {
                    "type": "ping",
                    "timestamp": datetime.now().isoformat()
                }
                await self.websocket.send(json.dumps(ping_message))
                logger.debug("Sent ping message")
            except Exception as e:
                logger.error(f"Failed to send ping: {e}")
    
    async def get_status(self):
        """请求当前任务状态"""
        if self.websocket and self.connected:
            try:
                status_request = {
                    "type": "get_status",
                    "timestamp": datetime.now().isoformat()
                }
                await self.websocket.send(json.dumps(status_request))
                logger.debug("Requested current status")
            except Exception as e:
                logger.error(f"Failed to request status: {e}")
    
    async def _heartbeat_loop(self):
        """心跳循环"""
        while self.connected:
            try:
                await asyncio.sleep(30)  # 每30秒发送一次心跳
                if self.connected:
                    await self.send_ping()
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Error in heartbeat loop: {e}")
    
    async def _handle_message(self, data: Dict[str, Any]):
        """处理接收到的WebSocket消息
        
        Args:
            data: 解析后的消息数据
        """
        message_type = data.get("type", "unknown")
        timestamp = data.get("timestamp", "")
        
        if message_type == "connection_established":
            logger.info(f"✅ Connection established: {data.get('message')}")
            logger.info(f"   Connection ID: {data.get('connection_id')}")
            
        elif message_type == "current_status":
            logger.info(f"📊 Current Status: {data.get('status')}")
            logger.info(f"   Message: {data.get('message')}")
            if data.get('progress') is not None:
                logger.info(f"   Progress: {data.get('progress')}%")
            logger.info(f"   Provider: {data.get('provider')}")
            logger.info(f"   Priority: {data.get('priority')}")
        
        elif message_type == "task_status_update":
            status = data.get("status", "unknown")
            message = data.get("message", "")
            progress = data.get("progress")
            
            logger.info(f"🔄 Status Update: {status}")
            logger.info(f"   Message: {message}")
            if progress is not None:
                logger.info(f"   Progress: {progress}%")
            
        elif message_type == "task_progress":
            progress = data.get("progress", 0)
            message = data.get("message", "")
            step_info = data.get("step_info", {})
            
            logger.info(f"📈 Progress: {progress}%")
            logger.info(f"   Message: {message}")
            if step_info:
                logger.info(f"   Step: {step_info.get('step', 'unknown')}")
        
        elif message_type == "task_logs":
            logs = data.get("logs", "")
            level = data.get("level", "INFO")
            source = data.get("source", "unknown")
            
            log_emoji = {"INFO": "ℹ️", "WARNING": "⚠️", "ERROR": "❌", "DEBUG": "🐛"}.get(level, "📝")
            logger.info(f"{log_emoji} Log [{level}] from {source}: {logs}")
        
        elif message_type == "task_error":
            error_message = data.get("error_message", "Unknown error")
            error_code = data.get("error_code", "")
            
            logger.error(f"❌ Task Error: {error_message}")
            if error_code:
                logger.error(f"   Error Code: {error_code}")
        
        elif message_type == "task_completed":
            success = data.get("success", False)
            execution_time = data.get("execution_time")
            cost_info = data.get("cost_info")
            result_data = data.get("result_data")
            
            if success:
                logger.info(f"✅ Task Completed Successfully!")
            else:
                logger.info(f"❌ Task Failed")
            
            if execution_time:
                logger.info(f"   Execution Time: {execution_time:.2f} seconds")
            
            if cost_info:
                total_cost = cost_info.get("total_cost", 0)
                currency = cost_info.get("currency", "USD")
                logger.info(f"   Cost: {total_cost} {currency}")
            
            if result_data:
                logger.info(f"   Result: {json.dumps(result_data, indent=2)}")
        
        elif message_type == "task_cancelled":
            reason = data.get("reason", "No reason provided")
            logger.info(f"🚫 Task Cancelled: {reason}")
        
        elif message_type == "heartbeat":
            logger.debug("💓 Received heartbeat")
        
        elif message_type == "pong":
            logger.debug("🏓 Received pong")
        
        elif message_type == "status_response":
            status = data.get("status", "unknown")
            logger.info(f"📊 Status Response: {status}")
        
        elif message_type == "error":
            error_msg = data.get("message", "Unknown error")
            logger.error(f"🚨 WebSocket Error: {error_msg}")
        
        else:
            logger.info(f"📦 Unknown Message Type: {message_type}")
            logger.info(f"   Data: {json.dumps(data, indent=2)}")


async def submit_test_task(base_url: str, auth_token: str) -> Optional[str]:
    """提交测试任务并返回任务ID
    
    Args:
        base_url: API基础URL
        auth_token: 认证令牌
        
    Returns:
        任务ID，如果失败返回None
    """
    api_url = f"{base_url}/api/gpu/jobs/submit"
    
    # 示例任务配置
    job_config = {
        "name": "WebSocket Test Task",
        "description": "Test task for WebSocket demonstration",
        "image": "python:3.9",
        "command": ["python", "-c", "import time; print('Task started'); time.sleep(60); print('Task completed')"],
        "gpu_spec": {
            "gpu_type": "RTX4090",
            "gpu_count": 1,
            "memory_gb": 8,
            "vcpus": 4
        },
        "environment": {},
        "volumes": [],
        "working_directory": "/workspace"
    }
    
    headers = {
        "Authorization": f"Bearer {auth_token}",
        "Content-Type": "application/json"
    }
    
    try:
        async with httpx.AsyncClient() as client:
            response = await client.post(
                api_url,
                json=job_config,
                headers=headers,
                timeout=30
            )
            
            if response.status_code == 200:
                result = response.json()
                task_id = result.get("task_id")
                logger.info(f"✅ Task submitted successfully: {task_id}")
                return task_id
            else:
                logger.error(f"❌ Failed to submit task: {response.status_code} - {response.text}")
                return None
                
    except Exception as e:
        logger.error(f"❌ Error submitting task: {e}")
        return None


async def main():
    """主函数"""
    parser = argparse.ArgumentParser(description="GPU计算平台WebSocket客户端示例")
    parser.add_argument("--base-url", default="http://localhost:8000", help="API基础URL")
    parser.add_argument("--task-id", help="任务ID（如果未提供，将创建测试任务）")
    parser.add_argument("--token", required=True, help="认证令牌")
    parser.add_argument("--submit-test-task", action="store_true", help="提交测试任务")
    
    args = parser.parse_args()
    
    # 获取任务ID
    task_id = args.task_id
    
    if args.submit_test_task or not task_id:
        logger.info("🚀 Submitting test task...")
        task_id = await submit_test_task(args.base_url, args.token)
        if not task_id:
            logger.error("❌ Failed to submit test task, exiting")
            return
    
    if not task_id:
        logger.error("❌ No task ID provided and failed to create test task")
        return
    
    # 创建WebSocket客户端
    client = GPUTaskWebSocketClient(args.base_url, args.token)
    
    try:
        # 连接到WebSocket
        if not await client.connect(task_id):
            logger.error("❌ Failed to connect to WebSocket")
            return
        
        # 请求当前状态
        await asyncio.sleep(1)  # 等待连接稳定
        await client.get_status()
        
        # 监听消息
        logger.info("👂 Listening for messages... (Press Ctrl+C to exit)")
        await client.listen()
        
    except KeyboardInterrupt:
        logger.info("👋 Received keyboard interrupt, closing connection...")
    except Exception as e:
        logger.error(f"❌ Unexpected error: {e}")
    finally:
        await client.disconnect()


if __name__ == "__main__":
    asyncio.run(main())
