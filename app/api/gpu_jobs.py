import json
from datetime import datetime, timezone
from fastapi import APIRouter, Depends, HTTPException, status, WebSocket, WebSocketDisconnect
from typing import List, Optional
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select

from app.core.auth import current_active_user
from app.core.database import get_async_session
from app.core.scheduler import IntelligentScheduler, TaskRequirement
from app.core.websocket_manager import websocket_manager
from app.core.task_status_broadcaster import task_broadcaster
from app.models.user import User
from app.models.task import GpuTask, TaskStatus, TaskPriority
from app.gpu.interface import GpuSpec, JobConfig, JobResult, CostInfo
from app.tasks.gpu_tasks import execute_gpu_task, cancel_gpu_task
import os

router = APIRouter()

# Provider configurations (在实际环境中这些应该来自环境变量或配置文件)
PROVIDERS = {
    "runpod": {
        "api_key": os.getenv("RUNPOD_API_KEY", "demo-api-key"),
        "endpoint_id": os.getenv("RUNPOD_ENDPOINT_ID", "demo-endpoint"),
    },
    "tencent": {
        "secret_id": os.getenv("TENCENT_SECRET_ID", "demo-secret-id"),
        "secret_key": os.getenv("TENCENT_SECRET_KEY", "demo-secret-key"),
        "region": os.getenv("TENCENT_REGION", "ap-shanghai"),
        "cluster_id": os.getenv("TENCENT_CLUSTER_ID", "cls-demo"),
    },
    "alibaba": {
        "access_key_id": os.getenv("ALIBABA_ACCESS_KEY_ID", "demo-access-key"),
        "access_key_secret": os.getenv("ALIBABA_ACCESS_KEY_SECRET", "demo-secret"),
        "region_id": os.getenv("ALIBABA_REGION_ID", "cn-hangzhou"),
    }
}


def get_provider_adapter(provider_name: str):
    """获取指定提供商的适配器实例"""
    if provider_name not in PROVIDERS:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Unsupported provider: {provider_name}"
        )
    
    config = PROVIDERS[provider_name]

    # 实际适配器实现
    from app.gpu.providers.tencent import TencentCloudAdapter
    from app.gpu.providers.alibaba import AlibabaCloudAdapter
    from app.gpu.providers.runpod import RunPodAdapter
    
    if provider_name == "tencent":
        return TencentCloudAdapter(config)
    elif provider_name == "alibaba":
        return AlibabaCloudAdapter(config)
    elif provider_name == "runpod":
        return RunPodAdapter(config)
    else:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Unknown provider: {provider_name}"
        )


@router.get("/providers")
async def list_providers(user: User = Depends(current_active_user)):
    """列出所有支持的GPU提供商"""
    return {
        "providers": [
            {
                "name": "runpod",
                "display_name": "RunPod",
                "description": "RunPod GPU cloud computing platform"
            },
            {
                "name": "tencent",
                "display_name": "Tencent Cloud",
                "description": "Tencent Cloud TKE GPU computing"
            },
            {
                "name": "alibaba",
                "display_name": "Alibaba Cloud",
                "description": "Alibaba Cloud ECS GPU instances"
            }
        ]
    }


@router.get("/providers/{provider_name}/gpus")
async def list_available_gpus(
    provider_name: str,
    user: User = Depends(current_active_user)
):
    """列出指定提供商的可用GPU规格"""
    try:
        adapter = get_provider_adapter(provider_name)
        gpus = await adapter.list_available_gpus()
        return {"provider": provider_name, "available_gpus": gpus}
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to list GPUs: {str(e)}"
        )


@router.get("/providers/{provider_name}/health")
async def check_provider_health(
    provider_name: str,
    user: User = Depends(current_active_user)
):
    """检查指定提供商的健康状态"""
    try:
        adapter = get_provider_adapter(provider_name)
        health = await adapter.health_check()
        return {"provider": provider_name, "health": health}
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Health check failed: {str(e)}"
        )


@router.get("/scheduling/recommendations")
async def get_scheduling_recommendations(
    gpu_type: str,
    gpu_count: int = 1,
    memory_gb: Optional[int] = None,
    vcpus: int = 4,
    estimated_duration_minutes: int = 60,
    priority: int = 5,
    user: User = Depends(current_active_user)
):
    """获取针对特定任务的调度建议"""
    try:
        scheduler = IntelligentScheduler()
        
        task_requirement = TaskRequirement(
            gpu_type=gpu_type,
            gpu_count=gpu_count,
            memory_gb=memory_gb or 0,
            vcpus=vcpus,
            estimated_duration_minutes=estimated_duration_minutes,
            priority=priority
        )
        
        recommendations = {}
        strategies = ["cost", "performance", "availability", "balanced"]
        
        for strategy in strategies:
            provider, routing_key = await scheduler.select_optimal_provider(
                task_requirement, strategy
            )
            if provider:
                score = await scheduler.calculate_provider_score(
                    provider, task_requirement, strategy
                )
                recommendations[strategy] = {
                    "provider": provider,
                    "routing_key": routing_key,
                    "score": score
                }
        
        return {
            "task_requirement": {
                "gpu_type": task_requirement.gpu_type,
                "gpu_count": task_requirement.gpu_count,
                "memory_gb": task_requirement.memory_gb,
                "vcpus": task_requirement.vcpus,
                "estimated_duration_minutes": task_requirement.estimated_duration_minutes,
                "priority": task_requirement.priority
            },
            "recommendations": recommendations,
            "provider_metrics": scheduler.get_all_provider_metrics()
        }
        
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get recommendations: {str(e)}"
        )


@router.get("/scheduling/metrics")
async def get_scheduling_metrics(
    user: User = Depends(current_active_user)
):
    """获取调度器指标和提供商统计"""
    try:
        scheduler = IntelligentScheduler()
        metrics = scheduler.get_all_provider_metrics()
        
        return {
            "provider_metrics": metrics,
            "total_providers": len(metrics),
            "healthy_providers": len([m for m in metrics.values() if m.get("health_score", 0) > 0.7]),
            "timestamp": datetime.now(timezone.utc).isoformat()
        }
        
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get scheduling metrics: {str(e)}"
        )


@router.post("/jobs/submit")
async def submit_job(
    job_config: JobConfig,
    priority: TaskPriority = TaskPriority.NORMAL,
    preferred_provider: Optional[str] = None,
    scheduling_strategy: str = "balanced",  # cost, performance, availability, balanced
    session: AsyncSession = Depends(get_async_session),
    user: User = Depends(current_active_user)
):
    """智能提交GPU作业到最优提供商"""
    try:
        # 创建智能调度器
        scheduler = IntelligentScheduler()
        
        # 构建任务需求
        task_requirement = TaskRequirement(
            gpu_type=job_config.gpu_spec.gpu_type,
            gpu_count=job_config.gpu_spec.gpu_count,
            memory_gb=job_config.gpu_spec.memory_gb or 0,
            vcpus=job_config.gpu_spec.vcpus,
            estimated_duration_minutes=60,  # 默认估计1小时，可以从job_config中提取
            priority=_map_priority_to_int(priority)
        )
        
        # 如果指定了首选提供商，验证其存在
        if preferred_provider:
            if preferred_provider not in PROVIDERS:
                raise HTTPException(
                    status_code=status.HTTP_400_BAD_REQUEST,
                    detail=f"Unsupported provider: {preferred_provider}"
                )
        
        # 选择最优提供商和路由
        selected_provider, routing_key = await scheduler.select_optimal_provider(
            task_requirement, 
            scheduling_strategy,
            preferred_provider
        )
        
        if not selected_provider:
            raise HTTPException(
                status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
                detail="No available providers for the requested GPU specification"
            )
        
        # 创建任务记录
        task = GpuTask(
            name=job_config.name,
            description=f"{job_config.name} on {selected_provider} (智能调度)",
            user_id=str(user.id),
            provider_name=selected_provider,
            job_config=job_config.model_dump_json(),
            status=TaskStatus.PENDING,
            priority=priority,
            gpu_spec=job_config.gpu_spec.model_dump_json() if job_config.gpu_spec else None,
            created_at=datetime.now(timezone.utc)
        )
        
        session.add(task)
        await session.commit()
        await session.refresh(task)
        
        # 提交异步任务到选定的队列
        provider_config = PROVIDERS[selected_provider]
        
        # 根据优先级选择队列
        queue_name = "gpu_tasks"
        if task_requirement.priority >= 8:
            queue_name = "priority_high"
        elif task_requirement.priority <= 2:
            queue_name = "priority_low"
        
        celery_task = execute_gpu_task.apply_async(
            args=[task.id, provider_config],
            queue=queue_name
        )
        
        # 更新Celery任务ID
        task.celery_task_id = celery_task.id
        task.status = TaskStatus.QUEUED
        await session.commit()
        
        # 获取调度决策的详细信息
        provider_score = await scheduler.calculate_provider_score(
            selected_provider, task_requirement, scheduling_strategy
        )
        
        return {
            "task_id": task.id,
            "celery_task_id": celery_task.id,
            "provider": selected_provider,
            "routing_key": routing_key,
            "queue": queue_name,
            "status": "queued",
            "message": f"Job intelligently scheduled to {selected_provider}",
            "created_at": task.created_at.isoformat(),
            "scheduling_info": {
                "strategy": scheduling_strategy,
                "provider_score": provider_score,
                "estimated_duration": task_requirement.estimated_duration_minutes,
                "gpu_spec": {
                    "gpu_type": task_requirement.gpu_type,
                    "gpu_count": task_requirement.gpu_count,
                    "memory_gb": task_requirement.memory_gb,
                    "vcpus": task_requirement.vcpus,
                    "priority": task_requirement.priority
                }
            }
        }
        
    except Exception as e:
        await session.rollback()
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Job submission failed: {str(e)}"
        )


@router.get("/jobs/{provider_name}/{job_id}/status")
async def get_job_status(
    provider_name: str,
    job_id: str,
    user: User = Depends(current_active_user)
):
    """获取作业状态"""
    try:
        adapter = get_provider_adapter(provider_name)
        result = await adapter.get_job_status(job_id)
        return {
            "provider": provider_name,
            "result": result
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get job status: {str(e)}"
        )


@router.get("/jobs/{provider_name}/{job_id}/logs")
async def get_job_logs(
    provider_name: str,
    job_id: str,
    lines: Optional[int] = None,
    user: User = Depends(current_active_user)
):
    """获取作业日志"""
    try:
        adapter = get_provider_adapter(provider_name)
        logs = await adapter.get_job_logs(job_id, lines)
        return {
            "provider": provider_name,
            "job_id": job_id,
            "logs": logs
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get job logs: {str(e)}"
        )


@router.get("/jobs/{provider_name}/{job_id}/cost")
async def get_job_cost(
    provider_name: str,
    job_id: str,
    user: User = Depends(current_active_user)
):
    """获取作业成本信息"""
    try:
        adapter = get_provider_adapter(provider_name)
        cost_info = await adapter.get_cost_info(job_id)
        return {
            "provider": provider_name,
            "cost_info": cost_info
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get cost info: {str(e)}"
        )


@router.get("/tasks")
async def list_user_tasks(
    skip: int = 0,
    limit: int = 100,
    status_filter: Optional[TaskStatus] = None,
    provider_filter: Optional[str] = None,
    session: AsyncSession = Depends(get_async_session),
    user: User = Depends(current_active_user)
):
    """列出用户的任务"""
    try:
        query = select(GpuTask).where(GpuTask.user_id == str(user.id))
        
        if status_filter:
            query = query.where(GpuTask.status == status_filter)
        if provider_filter:
            query = query.where(GpuTask.provider_name == provider_filter)
            
        query = query.order_by(GpuTask.created_at.desc()).offset(skip).limit(limit)
        
        result = await session.execute(query)
        tasks = result.scalars().all()
        
        return {
            "tasks": [
                {
                    "task_id": task.id,
                    "name": task.name,
                    "provider": task.provider_name,
                    "status": task.status,
                    "priority": task.priority,
                    "created_at": task.created_at,
                    "started_at": task.started_at,
                    "completed_at": task.completed_at,
                    "duration_seconds": task.duration_seconds,
                    "actual_cost": float(task.actual_cost) if task.actual_cost else None,
                    "currency": task.currency,
                    "mlflow_run_id": task.mlflow_run_id
                }
                for task in tasks
            ],
            "total": len(tasks)
        }
        
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to list tasks: {str(e)}"
        )


@router.get("/tasks/{task_id}")
async def get_task_details(
    task_id: str,
    session: AsyncSession = Depends(get_async_session),
    user: User = Depends(current_active_user)
):
    """获取任务详细信息"""
    try:
        stmt = select(GpuTask).where(
            GpuTask.id == task_id,
            GpuTask.user_id == str(user.id)
        )
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail="Task not found"
            )
        
        return {
            "task_id": task.id,
            "name": task.name,
            "description": task.description,
            "provider": task.provider_name,
            "status": task.status,
            "priority": task.priority,
            "created_at": task.created_at,
            "started_at": task.started_at,
            "completed_at": task.completed_at,
            "updated_at": task.updated_at,
            "duration_seconds": task.duration_seconds,
            "exit_code": task.exit_code,
            "error_message": task.error_message,
            "logs": task.logs,
            "job_config": json.loads(task.job_config) if task.job_config else None,
            "gpu_spec": json.loads(task.gpu_spec) if task.gpu_spec else None,
            "estimated_cost": float(task.estimated_cost) if task.estimated_cost else None,
            "actual_cost": float(task.actual_cost) if task.actual_cost else None,
            "currency": task.currency,
            "external_job_id": task.external_job_id,
            "celery_task_id": task.celery_task_id,
            "mlflow_run_id": task.mlflow_run_id,
            "mlflow_experiment_name": task.mlflow_experiment_name
        }
        
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get task details: {str(e)}"
        )


@router.post("/tasks/{task_id}/cancel")
async def cancel_task(
    task_id: str,
    session: AsyncSession = Depends(get_async_session),
    user: User = Depends(current_active_user)
):
    """取消任务"""
    try:
        stmt = select(GpuTask).where(
            GpuTask.id == task_id,
            GpuTask.user_id == str(user.id)
        )
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail="Task not found"
            )
        
        if task.is_terminal_state:
            return {
                "task_id": task_id,
                "status": task.status,
                "message": f"Task is already in terminal state: {task.status}"
            }
        
        # 取消Celery任务
        if task.celery_task_id:
            from app.core.celery_app import celery_app
            celery_app.control.revoke(task.celery_task_id, terminate=True)
        
        # 如果有外部任务ID，也要取消外部任务
        if task.external_job_id:
            provider_config = PROVIDERS[task.provider_name]
            cancel_gpu_task.delay(task_id, provider_config)
        
        # 更新任务状态
        task.status = TaskStatus.CANCELLED
        task.completed_at = datetime.now(timezone.utc)
        task.updated_at = datetime.now(timezone.utc)
        await session.commit()
        
        return {
            "task_id": task_id,
            "status": "cancelled",
            "message": "Task cancellation request sent"
        }
        
    except Exception as e:
        await session.rollback()
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to cancel task: {str(e)}"
        )


@router.websocket("/tasks/{task_id}/ws")
async def websocket_task_status(
    websocket: WebSocket,
    task_id: str,
    user: User = Depends(current_active_user)
):
    """WebSocket端点：实时获取任务状态更新
    
    客户端可以连接到此端点获取指定任务的实时状态更新。
    消息格式：
    {
        "type": "task_status_update|task_progress|task_logs|task_error|task_completed",
        "task_id": "任务ID",
        "timestamp": "ISO格式时间戳",
        "data": {...}
    }
    """
    connection_id = None
    
    try:
        # 验证任务存在且属于当前用户
        session_gen = get_async_session()
        session = await anext(session_gen)
        
        try:
            stmt = select(GpuTask).where(
                GpuTask.id == task_id,
                GpuTask.user_id == str(user.id)
            )
            result = await session.execute(stmt)
            task = result.scalar_one_or_none()
            
            if not task:
                await websocket.close(code=4004, reason="Task not found")
                return
            
            # 建立WebSocket连接
            connection_id = await websocket_manager.connect(
                websocket, task_id, str(user.id)
            )
            
            # 发送当前任务状态
            current_status_message = {
                "type": "current_status",
                "task_id": task_id,
                "status": task.status.value if hasattr(task.status, 'value') else str(task.status),
                "message": f"Current task status: {task.status}",
                "progress": 100 if task.is_terminal_state else None,
                "created_at": task.created_at.isoformat() if task.created_at else None,
                "started_at": task.started_at.isoformat() if task.started_at else None,
                "completed_at": task.completed_at.isoformat() if task.completed_at else None,
                "provider": task.provider_name,
                "priority": task.priority.value if hasattr(task.priority, 'value') else str(task.priority),
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
            await websocket_manager.send_to_connection(connection_id, current_status_message)
            
            # 监听客户端消息（主要用于心跳）
            while True:
                try:
                    # 等待客户端消息
                    message = await websocket.receive_text()
                    data = json.loads(message)
                    
                    # 处理心跳消息
                    if data.get("type") == "ping":
                        await websocket_manager.handle_ping(connection_id)
                    
                    # 处理客户端状态查询请求
                    elif data.get("type") == "get_status":
                        # 重新查询最新状态
                        await session.refresh(task)
                        await websocket_manager.send_to_connection(connection_id, {
                            "type": "status_response",
                            "task_id": task_id,
                            "status": task.status.value if hasattr(task.status, 'value') else str(task.status),
                            "timestamp": datetime.now(timezone.utc).isoformat()
                        })
                    
                except WebSocketDisconnect:
                    break
                except json.JSONDecodeError:
                    # 忽略无效的JSON消息
                    continue
                except Exception as e:
                    # 发送错误消息
                    await websocket_manager.send_to_connection(connection_id, {
                        "type": "error",
                        "message": f"Error processing message: {str(e)}",
                        "timestamp": datetime.now(timezone.utc).isoformat()
                    })
        
        finally:
            await session.close()
            
    except WebSocketDisconnect:
        pass
    except Exception as e:
        if connection_id:
            try:
                await websocket_manager.send_to_connection(connection_id, {
                    "type": "error",
                    "message": f"Connection error: {str(e)}",
                    "timestamp": datetime.now(timezone.utc).isoformat()
                })
            except:
                pass
    finally:
        if connection_id:
            await websocket_manager.disconnect(connection_id)


def _map_priority_to_int(priority) -> int:
    """将TaskPriority映射为整数值"""
    from app.models.task import TaskPriority
    
    if isinstance(priority, TaskPriority):
        mapping = {
            TaskPriority.LOW: 2,
            TaskPriority.NORMAL: 5,
            TaskPriority.HIGH: 8,
            TaskPriority.URGENT: 10
        }
        return mapping.get(priority, 5)
    elif isinstance(priority, str):
        mapping = {
            'low': 2,
            'normal': 5,
            'high': 8,
            'urgent': 10
        }
        return mapping.get(priority.lower(), 5)
    elif isinstance(priority, int):
        return max(1, min(10, priority))  # 确保在1-10范围内
    else:
        return 5  # 默认值


@router.get("/websocket/stats")
async def get_websocket_stats(
    user: User = Depends(current_active_user)
):
    """获取WebSocket连接统计信息"""
    try:
        stats = websocket_manager.get_statistics()
        return {
            "websocket_statistics": stats,
            "timestamp": datetime.now(timezone.utc).isoformat()
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get WebSocket stats: {str(e)}"
        )


@router.get("/tasks/{task_id}/connections")
async def get_task_connections(
    task_id: str,
    session: AsyncSession = Depends(get_async_session),
    user: User = Depends(current_active_user)
):
    """获取指定任务的WebSocket连接信息"""
    try:
        # 验证任务存在且属于当前用户
        stmt = select(GpuTask).where(
            GpuTask.id == task_id,
            GpuTask.user_id == str(user.id)
        )
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail="Task not found"
            )
        
        # 获取连接信息
        connections = websocket_manager.get_task_connections(task_id)
        connection_count = websocket_manager.get_connection_count(task_id)
        
        return {
            "task_id": task_id,
            "connection_count": connection_count,
            "has_active_connections": connection_count > 0,
            "connections": [
                websocket_manager.get_connection_info(conn_id)
                for conn_id in connections
            ],
            "timestamp": datetime.now(timezone.utc).isoformat()
        }
        
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get task connections: {str(e)}"
        )
