from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select, update, delete, and_, or_, desc, asc, func
from sqlalchemy.orm import selectinload
from typing import Optional, List
from datetime import datetime, timezone
import json
import math

from app.core.database import get_async_session
from app.core.auth import current_active_user
from app.models.user import User, UserRole
from app.models.task import GpuTask, TaskStatus, TaskPriority
from app.schemas.task import (
    TaskCreate, TaskUpdate, TaskRead, TaskList, TaskResponse, 
    TaskListResponse, ApiResponse, TaskStats
)

router = APIRouter()


def require_admin(current_user: User = Depends(current_active_user)):
    """管理员权限验证"""
    if current_user.role != UserRole.ADMIN:
        raise HTTPException(status_code=403, detail="需要管理员权限")
    return current_user


async def get_user_name(user_id: str, session: AsyncSession) -> str:
    """获取用户昵称或邮箱"""
    stmt = select(User).where(User.id == user_id)
    result = await session.execute(stmt)
    user = result.scalar_one_or_none()
    if user:
        return user.nickname or user.email.split("@")[0]
    return "Unknown"


@router.get("/tasks", response_model=dict)
async def get_tasks(
    page: int = Query(1, ge=1, description="页码"),
    per_page: int = Query(20, ge=1, le=100, description="每页数量"),
    status: Optional[str] = Query(None, description="任务状态过滤"),
    provider: Optional[str] = Query(None, description="云服务商过滤"),
    search: Optional[str] = Query(None, description="搜索关键词"),
    user_id: Optional[str] = Query(None, description="用户ID过滤（管理员可用）"),
    sort_by: str = Query("created_at", description="排序字段"),
    sort_order: str = Query("desc", description="排序方向"),
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    获取任务列表
    支持分页、过滤、搜索和排序
    """
    try:
        # 构建基础查询
        stmt = select(GpuTask).options(selectinload(GpuTask.user))
        
        # 权限控制：普通用户只能看到自己的任务
        if current_user.role != UserRole.ADMIN:
            stmt = stmt.where(GpuTask.user_id == str(current_user.id))
        else:
            # 管理员可以过滤特定用户的任务
            if user_id:
                stmt = stmt.where(GpuTask.user_id == user_id)
        
        # 状态过滤
        if status:
            try:
                status_enum = TaskStatus(status)
                stmt = stmt.where(GpuTask.status == status_enum)
            except ValueError:
                return {
                    "success": False,
                    "error": f"无效的任务状态: {status}"
                }
        
        # 云服务商过滤
        if provider:
            stmt = stmt.where(GpuTask.provider_name == provider)
        
        # 搜索功能
        if search:
            search_term = f"%{search}%"
            stmt = stmt.where(
                or_(
                    GpuTask.name.ilike(search_term),
                    GpuTask.id.ilike(search_term),
                    GpuTask.description.ilike(search_term)
                )
            )
        
        # 排序
        sort_field = getattr(GpuTask, sort_by, GpuTask.created_at)
        if sort_order.lower() == "asc":
            stmt = stmt.order_by(asc(sort_field))
        else:
            stmt = stmt.order_by(desc(sort_field))
        
        # 计算总数
        count_stmt = select(func.count()).select_from(stmt.subquery())
        total_result = await session.execute(count_stmt)
        total = total_result.scalar()
        
        # 分页
        offset = (page - 1) * per_page
        stmt = stmt.offset(offset).limit(per_page)
        
        # 执行查询
        result = await session.execute(stmt)
        tasks = result.scalars().all()
        
        # 转换为Schema
        task_items = []
        for task in tasks:
            user_name = await get_user_name(task.user_id, session)
            task_read = TaskRead.from_db_model(task, user_name)
            task_items.append(task_read)
        
        # 计算分页信息
        pages = math.ceil(total / per_page)
        
        task_list = TaskList(
            items=task_items,
            total=total,
            page=page,
            per_page=per_page,
            pages=pages
        )
        
        return {
            "success": True,
            "data": task_list.dict()
        }
        
    except Exception as e:
        print(f"Get tasks error: {e}")
        return {
            "success": False,
            "error": "获取任务列表失败",
            "message": str(e)
        }


@router.get("/tasks/{task_id}", response_model=dict)
async def get_task(
    task_id: str,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    获取单个任务详情
    """
    try:
        # 查询任务
        stmt = select(GpuTask).options(selectinload(GpuTask.user)).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            return {
                "success": False,
                "error": "任务不存在",
                "message": "任务不存在"
            }
        
        # 权限检查：普通用户只能查看自己的任务
        if current_user.role != UserRole.ADMIN and task.user_id != str(current_user.id):
            return {
                "success": False,
                "error": "没有权限查看此任务",
                "message": "没有权限查看此任务"
            }
        
        # 转换为Schema
        user_name = await get_user_name(task.user_id, session)
        task_read = TaskRead.from_db_model(task, user_name)
        
        return {
            "success": True,
            "data": task_read.dict()
        }
        
    except Exception as e:
        print(f"Get task error: {e}")
        return {
            "success": False,
            "error": "获取任务详情失败",
            "message": str(e)
        }


@router.post("/tasks", response_model=dict)
async def create_task(
    task_data: TaskCreate,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    创建新任务
    """
    try:
        # 构建作业配置
        job_config = {
            "provider": task_data.provider,
            "gpu_model": task_data.gpu_model,
            "image": task_data.image,
            "script": task_data.script,
            "dataset_path": task_data.dataset_path,
            "scheduling_strategy": task_data.scheduling_strategy,
            "max_duration": task_data.max_duration,
            "budget_limit": task_data.budget_limit,
            "environment_vars": task_data.environment_vars or {},
            "requirements": task_data.requirements or []
        }
        
        # 创建任务实例
        new_task = GpuTask(
            name=task_data.name,
            description=task_data.description,
            user_id=str(current_user.id),
            provider_name=task_data.provider,
            job_config=json.dumps(job_config),
            priority=task_data.priority,
            status=TaskStatus.PENDING,
            estimated_cost=task_data.budget_limit,
            currency="USD",
            created_at=datetime.now(timezone.utc),
            updated_at=datetime.now(timezone.utc)
        )
        
        session.add(new_task)
        await session.commit()
        await session.refresh(new_task)
        
        # 这里可以添加异步任务调度逻辑
        # 例如：发送到Celery队列进行处理
        
        # 转换为Schema
        user_name = await get_user_name(new_task.user_id, session)
        task_read = TaskRead.from_db_model(new_task, user_name)
        
        return {
            "success": True,
            "data": task_read.dict(),
            "message": "任务创建成功"
        }
        
    except Exception as e:
        print(f"Create task error: {e}")
        return {
            "success": False,
            "error": "创建任务失败",
            "message": str(e)
        }


@router.put("/tasks/{task_id}", response_model=dict)
async def update_task(
    task_id: str,
    task_data: TaskUpdate,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    更新任务信息
    """
    try:
        # 查询任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            return {
                "success": False,
                "error": "任务不存在",
                "message": "任务不存在"
            }
        
        # 权限检查
        if current_user.role != UserRole.ADMIN and task.user_id != str(current_user.id):
            return {
                "success": False,
                "error": "没有权限修改此任务",
                "message": "没有权限修改此任务"
            }
        
        # 更新任务信息
        update_data = {"updated_at": datetime.now(timezone.utc)}
        
        if task_data.name is not None:
            update_data["name"] = task_data.name
        if task_data.description is not None:
            update_data["description"] = task_data.description
        if task_data.priority is not None:
            update_data["priority"] = task_data.priority
        # 只有管理员可以修改任务状态
        if task_data.status is not None and current_user.role == UserRole.ADMIN:
            update_data["status"] = task_data.status
        
        await session.execute(
            update(GpuTask).where(GpuTask.id == task_id).values(**update_data)
        )
        await session.commit()
        
        # 重新查询更新后的任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        updated_task = result.scalar_one()
        
        user_name = await get_user_name(updated_task.user_id, session)
        task_read = TaskRead.from_db_model(updated_task, user_name)
        
        return {
            "success": True,
            "data": task_read.dict(),
            "message": "任务更新成功"
        }
        
    except Exception as e:
        print(f"Update task error: {e}")
        return {
            "success": False,
            "error": "更新任务失败",
            "message": str(e)
        }


@router.post("/tasks/{task_id}/cancel", response_model=dict)
async def cancel_task(
    task_id: str,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    取消任务
    """
    try:
        # 查询任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            return {
                "success": False,
                "error": "任务不存在",
                "message": "任务不存在"
            }
        
        # 权限检查
        if current_user.role != UserRole.ADMIN and task.user_id != str(current_user.id):
            return {
                "success": False,
                "error": "没有权限取消此任务",
                "message": "没有权限取消此任务"
            }
        
        # 检查任务状态是否可以取消
        if task.status in [TaskStatus.COMPLETED, TaskStatus.FAILED, TaskStatus.CANCELLED]:
            return {
                "success": False,
                "error": "任务已完成或已取消，无法取消",
                "message": "任务已完成或已取消，无法取消"
            }
        
        # 更新任务状态为已取消
        await session.execute(
            update(GpuTask).where(GpuTask.id == task_id).values(
                status=TaskStatus.CANCELLED,
                completed_at=datetime.now(timezone.utc),
                updated_at=datetime.now(timezone.utc)
            )
        )
        await session.commit()
        
        # 这里可以添加取消实际运行任务的逻辑
        # 例如：通知Celery取消任务，或调用云服务商API取消任务
        
        return {
            "success": True,
            "message": "任务已取消"
        }
        
    except Exception as e:
        print(f"Cancel task error: {e}")
        return {
            "success": False,
            "error": "取消任务失败",
            "message": str(e)
        }


@router.post("/tasks/{task_id}/restart", response_model=dict)
async def restart_task(
    task_id: str,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    重启任务
    """
    try:
        # 查询任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            return {
                "success": False,
                "error": "任务不存在",
                "message": "任务不存在"
            }
        
        # 权限检查
        if current_user.role != UserRole.ADMIN and task.user_id != str(current_user.id):
            return {
                "success": False,
                "error": "没有权限重启此任务",
                "message": "没有权限重启此任务"
            }
        
        # 检查任务状态是否可以重启
        if task.status in [TaskStatus.PENDING, TaskStatus.QUEUED, TaskStatus.RUNNING]:
            return {
                "success": False,
                "error": "任务正在运行中，无法重启",
                "message": "任务正在运行中，无法重启"
            }
        
        # 重置任务状态
        await session.execute(
            update(GpuTask).where(GpuTask.id == task_id).values(
                status=TaskStatus.PENDING,
                started_at=None,
                completed_at=None,
                exit_code=None,
                error_message=None,
                logs=None,
                external_job_id=None,
                celery_task_id=None,
                updated_at=datetime.now(timezone.utc)
            )
        )
        await session.commit()
        
        # 重新查询任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        restarted_task = result.scalar_one()
        
        # 这里可以添加重新提交任务的逻辑
        
        user_name = await get_user_name(restarted_task.user_id, session)
        task_read = TaskRead.from_db_model(restarted_task, user_name)
        
        return {
            "success": True,
            "data": task_read.dict(),
            "message": "任务已重启"
        }
        
    except Exception as e:
        print(f"Restart task error: {e}")
        return {
            "success": False,
            "error": "重启任务失败",
            "message": str(e)
        }


@router.delete("/tasks/{task_id}", response_model=dict)
async def delete_task(
    task_id: str,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    删除任务
    """
    try:
        # 查询任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            return {
                "success": False,
                "error": "任务不存在",
                "message": "任务不存在"
            }
        
        # 权限检查
        if current_user.role != UserRole.ADMIN and task.user_id != str(current_user.id):
            return {
                "success": False,
                "error": "没有权限删除此任务",
                "message": "没有权限删除此任务"
            }
        
        # 检查任务是否正在运行
        if task.status == TaskStatus.RUNNING:
            return {
                "success": False,
                "error": "无法删除正在运行的任务，请先取消任务",
                "message": "无法删除正在运行的任务，请先取消任务"
            }
        
        # 删除任务
        await session.execute(delete(GpuTask).where(GpuTask.id == task_id))
        await session.commit()
        
        return {
            "success": True,
            "message": "任务已删除"
        }
        
    except Exception as e:
        print(f"Delete task error: {e}")
        return {
            "success": False,
            "error": "删除任务失败",
            "message": str(e)
        }


@router.get("/tasks/{task_id}/logs", response_model=dict)
async def get_task_logs(
    task_id: str,
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    获取任务日志
    """
    try:
        # 查询任务
        stmt = select(GpuTask).where(GpuTask.id == task_id)
        result = await session.execute(stmt)
        task = result.scalar_one_or_none()
        
        if not task:
            return {
                "success": False,
                "error": "任务不存在",
                "message": "任务不存在"
            }
        
        # 权限检查
        if current_user.role != UserRole.ADMIN and task.user_id != str(current_user.id):
            return {
                "success": False,
                "error": "没有权限查看此任务的日志",
                "message": "没有权限查看此任务的日志"
            }
        
        # 返回日志内容
        logs = task.logs or "暂无日志信息"
        
        return {
            "success": True,
            "data": {
                "task_id": task_id,
                "logs": logs,
                "last_updated": task.updated_at.isoformat() if task.updated_at else None
            }
        }
        
    except Exception as e:
        print(f"Get task logs error: {e}")
        return {
            "success": False,
            "error": "获取任务日志失败",
            "message": str(e)
        }


@router.get("/tasks/stats", response_model=dict)
async def get_task_stats(
    user_id: Optional[str] = Query(None, description="用户ID（管理员可指定）"),
    current_user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """
    获取任务统计信息
    """
    try:
        # 构建基础查询
        base_query = select(GpuTask)
        
        # 权限控制
        if current_user.role != UserRole.ADMIN:
            base_query = base_query.where(GpuTask.user_id == str(current_user.id))
        elif user_id:
            base_query = base_query.where(GpuTask.user_id == user_id)
        
        # 统计各种状态的任务数量
        stats_queries = {
            "total_tasks": select(func.count()).select_from(base_query.subquery()),
            "running_tasks": select(func.count()).select_from(
                base_query.where(GpuTask.status == TaskStatus.RUNNING).subquery()
            ),
            "completed_tasks": select(func.count()).select_from(
                base_query.where(GpuTask.status == TaskStatus.COMPLETED).subquery()
            ),
            "failed_tasks": select(func.count()).select_from(
                base_query.where(GpuTask.status == TaskStatus.FAILED).subquery()
            ),
            "pending_tasks": select(func.count()).select_from(
                base_query.where(GpuTask.status == TaskStatus.PENDING).subquery()
            ),
            "cancelled_tasks": select(func.count()).select_from(
                base_query.where(GpuTask.status == TaskStatus.CANCELLED).subquery()
            )
        }
        
        # 执行所有统计查询
        stats = {}
        for key, query in stats_queries.items():
            result = await session.execute(query)
            stats[key] = result.scalar()
        
        # 计算成本和计算时长（简化处理）
        cost_query = select(func.sum(GpuTask.actual_cost)).select_from(
            base_query.where(GpuTask.actual_cost.isnot(None)).subquery()
        )
        cost_result = await session.execute(cost_query)
        total_cost = float(cost_result.scalar() or 0)
        
        # 简单计算总计算时长（假设每个已完成任务运行1小时）
        total_compute_hours = float(stats["completed_tasks"])
        
        task_stats = TaskStats(
            total_tasks=stats["total_tasks"],
            running_tasks=stats["running_tasks"],
            completed_tasks=stats["completed_tasks"],
            failed_tasks=stats["failed_tasks"],
            pending_tasks=stats["pending_tasks"],
            cancelled_tasks=stats["cancelled_tasks"],
            total_compute_hours=total_compute_hours,
            total_cost=total_cost,
            currency="USD"
        )
        
        return {
            "success": True,
            "data": task_stats.dict()
        }
        
    except Exception as e:
        print(f"Get task stats error: {e}")
        return {
            "success": False,
            "error": "获取任务统计失败",
            "message": str(e)
        }
