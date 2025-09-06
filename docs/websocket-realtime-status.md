# WebSocket 实时任务状态推送

GPU 计算平台支持通过 WebSocket 实时推送任务状态更新，让客户端能够即时了解任务的执行进度、状态变化、日志输出和错误信息。

## 功能概述

### 核心特性
- **实时状态更新**：任务状态变化时立即推送到所有订阅的客户端
- **进度跟踪**：详细的任务执行进度信息（0-100%）
- **日志推送**：实时推送任务执行日志
- **错误通知**：即时推送错误信息和诊断数据
- **完成通知**：任务完成时推送最终结果和成本信息
- **连接管理**：自动心跳检测和连接清理
- **多客户端支持**：同一任务可被多个客户端同时监听

### 支持的消息类型
- `task_status_update` - 任务状态更新
- `task_progress` - 任务进度更新
- `task_logs` - 任务日志消息
- `task_error` - 任务错误信息
- `task_completed` - 任务完成通知
- `task_cancelled` - 任务取消通知
- `heartbeat` - 心跳消息

## WebSocket 端点

### 连接端点
```
ws://localhost:8000/api/gpu/tasks/{task_id}/ws
```

**参数：**
- `task_id` - 要监听的任务ID

**认证：**
需要在请求头中包含有效的认证令牌：
```
Authorization: Bearer <access_token>
```

## 消息格式

### 基础消息结构
所有 WebSocket 消息都采用 JSON 格式：
```json
{
  "type": "消息类型",
  "task_id": "任务ID", 
  "timestamp": "ISO格式时间戳",
  "其他字段": "根据消息类型变化"
}
```

### 连接建立消息
客户端连接成功后会收到确认消息：
```json
{
  "type": "connection_established",
  "connection_id": "conn_123_1640123456",
  "task_id": "task-uuid",
  "timestamp": "2024-01-01T10:00:00Z",
  "message": "WebSocket connection established"
}
```

### 当前状态消息
连接建立后会立即收到任务当前状态：
```json
{
  "type": "current_status",
  "task_id": "task-uuid",
  "status": "running",
  "message": "Current task status: running",
  "progress": 45,
  "created_at": "2024-01-01T09:00:00Z",
  "started_at": "2024-01-01T09:05:00Z",
  "provider": "runpod",
  "priority": "normal",
  "timestamp": "2024-01-01T10:00:00Z"
}
```

### 状态更新消息
任务状态发生变化时推送：
```json
{
  "type": "task_status_update",
  "task_id": "task-uuid",
  "status": "running",
  "message": "Task status updated to running",
  "progress": 25,
  "timestamp": "2024-01-01T10:00:00Z",
  "data": {
    "started_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:00:00Z"
  }
}
```

### 进度更新消息
任务执行进度更新时推送：
```json
{
  "type": "task_progress", 
  "task_id": "task-uuid",
  "progress": 65.5,
  "message": "Processing data batch 3/4",
  "timestamp": "2024-01-01T10:05:00Z",
  "step_info": {
    "step": "data_processing",
    "current_batch": 3,
    "total_batches": 4,
    "external_job_id": "runpod-job-456"
  }
}
```

### 日志消息
实时推送任务执行日志：
```json
{
  "type": "task_logs",
  "task_id": "task-uuid", 
  "logs": "Model training started with batch size 32",
  "level": "INFO",
  "source": "worker",
  "timestamp": "2024-01-01T10:05:30Z"
}
```

### 错误消息
任务执行错误时推送：
```json
{
  "type": "task_error",
  "task_id": "task-uuid",
  "error_message": "GPU memory allocation failed",
  "error_code": "CUDA_OUT_OF_MEMORY", 
  "timestamp": "2024-01-01T10:07:00Z",
  "error_details": {
    "requested_memory": "16GB",
    "available_memory": "8GB"
  }
}
```

### 完成通知
任务完成时推送最终结果：
```json
{
  "type": "task_completed",
  "task_id": "task-uuid",
  "success": true,
  "timestamp": "2024-01-01T10:15:00Z",
  "execution_time": 900.5,
  "cost_info": {
    "total_cost": 2.45,
    "currency": "USD"
  },
  "result_data": {
    "exit_code": 0,
    "external_job_id": "runpod-job-456",
    "provider": "runpod"
  }
}
```

### 取消通知
任务被取消时推送：
```json
{
  "type": "task_cancelled",
  "task_id": "task-uuid",
  "reason": "Task was cancelled by user request",
  "timestamp": "2024-01-01T10:12:00Z"
}
```

## 客户端交互

### 发送心跳
客户端可以发送心跳消息保持连接活跃：
```json
{
  "type": "ping",
  "timestamp": "2024-01-01T10:05:00Z"
}
```

服务器会响应：
```json
{
  "type": "pong", 
  "timestamp": "2024-01-01T10:05:00Z"
}
```

### 请求状态
客户端可以主动请求当前状态：
```json
{
  "type": "get_status",
  "timestamp": "2024-01-01T10:05:00Z" 
}
```

服务器会响应：
```json
{
  "type": "status_response",
  "task_id": "task-uuid",
  "status": "running", 
  "timestamp": "2024-01-01T10:05:01Z"
}
```

## 使用示例

### Python 客户端示例
```python
import asyncio
import json
import websockets

async def listen_to_task(task_id: str, auth_token: str):
    """监听指定任务的状态更新"""
    
    uri = f"ws://localhost:8000/api/gpu/tasks/{task_id}/ws"
    headers = {"Authorization": f"Bearer {auth_token}"}
    
    async with websockets.connect(uri, extra_headers=headers) as websocket:
        print(f"Connected to task {task_id}")
        
        # 监听消息
        async for message in websocket:
            data = json.loads(message)
            message_type = data.get("type")
            
            if message_type == "task_status_update":
                status = data.get("status")
                progress = data.get("progress")
                print(f"Status: {status}, Progress: {progress}%")
                
            elif message_type == "task_completed":
                success = data.get("success")
                print(f"Task completed: {'Success' if success else 'Failed'}")
                break
                
            elif message_type == "task_logs":
                logs = data.get("logs") 
                level = data.get("level")
                print(f"[{level}] {logs}")

# 使用示例
task_id = "your-task-id"
auth_token = "your-auth-token" 
asyncio.run(listen_to_task(task_id, auth_token))
```

### JavaScript 客户端示例
```javascript
class TaskWebSocketClient {
    constructor(taskId, authToken) {
        this.taskId = taskId;
        this.authToken = authToken;
        this.websocket = null;
    }
    
    connect() {
        const wsUrl = `ws://localhost:8000/api/gpu/tasks/${this.taskId}/ws`;
        this.websocket = new WebSocket(wsUrl, [], {
            headers: {
                'Authorization': `Bearer ${this.authToken}`
            }
        });
        
        this.websocket.onopen = () => {
            console.log(`Connected to task ${this.taskId}`);
        };
        
        this.websocket.onmessage = (event) => {
            const data = JSON.parse(event.data);
            this.handleMessage(data);
        };
        
        this.websocket.onclose = () => {
            console.log('WebSocket connection closed');
        };
        
        this.websocket.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }
    
    handleMessage(data) {
        const messageType = data.type;
        
        switch (messageType) {
            case 'task_status_update':
                console.log(`Status: ${data.status}, Progress: ${data.progress}%`);
                break;
                
            case 'task_progress':
                console.log(`Progress: ${data.progress}% - ${data.message}`);
                break;
                
            case 'task_logs':
                console.log(`[${data.level}] ${data.logs}`);
                break;
                
            case 'task_completed':
                console.log(`Task completed: ${data.success ? 'Success' : 'Failed'}`);
                break;
                
            case 'task_error':
                console.error(`Error: ${data.error_message}`);
                break;
        }
    }
    
    sendPing() {
        if (this.websocket && this.websocket.readyState === WebSocket.OPEN) {
            this.websocket.send(JSON.stringify({
                type: 'ping',
                timestamp: new Date().toISOString()
            }));
        }
    }
}

// 使用示例
const client = new TaskWebSocketClient('your-task-id', 'your-auth-token');
client.connect();

// 定期发送心跳
setInterval(() => client.sendPing(), 30000);
```

## REST API 接口

### 获取 WebSocket 统计信息
```http
GET /api/gpu/websocket/stats
Authorization: Bearer <token>
```

响应：
```json
{
  "websocket_statistics": {
    "total_connections": 5,
    "active_tasks": 3,
    "task_connection_counts": {
      "task-1": 2,
      "task-2": 1, 
      "task-3": 2
    },
    "timestamp": "2024-01-01T10:00:00Z"
  },
  "timestamp": "2024-01-01T10:00:00Z"
}
```

### 获取任务连接信息
```http
GET /api/gpu/tasks/{task_id}/connections
Authorization: Bearer <token>
```

响应：
```json
{
  "task_id": "task-uuid",
  "connection_count": 2,
  "has_active_connections": true,
  "connections": [
    {
      "task_id": "task-uuid",
      "user_id": "user-123",
      "connected_at": "2024-01-01T09:55:00Z",
      "last_ping": "2024-01-01T10:00:00Z"
    }
  ],
  "timestamp": "2024-01-01T10:00:00Z"
}
```

## 进度阶段说明

任务执行过程中，进度会按以下阶段更新：

- **0-10%**: 任务初始化
- **10-20%**: 提交任务到GPU提供商
- **20-30%**: 任务提交成功，准备执行
- **30-40%**: 开始监控任务执行
- **40-80%**: 任务执行中（根据监控轮询更新）
- **80-85%**: 任务执行完成，收集成本信息
- **85-95%**: 处理结果和日志
- **95-100%**: 任务最终状态确认

## 错误处理

### 连接错误
- `4004`: 任务不存在或无权访问
- `4003`: 认证失败
- `4000`: 通用WebSocket错误

### 消息错误
客户端发送无效消息时，服务器会返回错误消息：
```json
{
  "type": "error",
  "message": "Error processing message: Invalid JSON format",
  "timestamp": "2024-01-01T10:00:00Z"
}
```

### 连接管理
- 客户端超过30分钟无活动会被自动断开
- 服务器重启时所有WebSocket连接会被关闭
- 建议客户端实现自动重连机制

## 最佳实践

### 连接管理
1. **实现重连机制**：网络中断时自动重连
2. **发送心跳**：定期发送ping消息保持连接
3. **优雅断开**：程序退出时主动关闭WebSocket连接

### 消息处理
1. **JSON验证**：接收消息后验证JSON格式
2. **类型检查**：检查消息类型并相应处理
3. **错误处理**：妥善处理错误消息和异常情况

### 性能考虑
1. **避免频繁连接**：复用WebSocket连接
2. **限制并发**：避免同时监听过多任务
3. **缓冲处理**：对于高频消息考虑缓冲机制

## 完整示例

参考 `examples/websocket_client_example.py` 获取完整的Python客户端实现示例，包含：

- 连接管理和重连逻辑
- 所有消息类型的处理
- 心跳机制
- 错误处理
- 命令行界面

运行示例：
```bash
# 监听现有任务
python examples/websocket_client_example.py --task-id <task-id> --token <auth-token>

# 提交测试任务并监听
python examples/websocket_client_example.py --submit-test-task --token <auth-token>
```
