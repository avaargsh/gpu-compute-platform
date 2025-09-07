from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
import os
import sys

# 添加项目根目录到Python路径
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

app = FastAPI(
    title="GPU Compute Platform Test",
    version="0.1.0",
    debug=True,
)

# CORS设置
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

@app.get("/")
async def root():
    """根端点"""
    return {
        "message": "GPU Compute Platform Test API",
        "version": "0.1.0",
        "status": "running"
    }

@app.get("/health")
async def health_check():
    """健康检查"""
    return {"status": "healthy", "service": "gpu-compute-platform"}

# 测试认证端点
@app.post("/test/auth")
async def test_auth():
    """测试认证功能"""
    return {
        "message": "Auth endpoint test",
        "available": True
    }

# 测试任务端点
@app.get("/test/tasks")
async def test_tasks():
    """测试任务功能"""
    return {
        "message": "Tasks endpoint test",
        "tasks": [
            {
                "id": "test-1",
                "name": "Test Task",
                "status": "completed",
                "provider": "alibaba"
            }
        ]
    }

# 测试服务商端点
@app.get("/test/providers")
async def test_providers():
    """测试服务商功能"""
    return {
        "message": "Providers endpoint test",
        "providers": [
            {"name": "alibaba", "display_name": "阿里云", "available": True},
            {"name": "tencent", "display_name": "腾讯云", "available": True},
            {"name": "runpod", "display_name": "RunPod", "available": True}
        ]
    }

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="127.0.0.1", port=8000)
