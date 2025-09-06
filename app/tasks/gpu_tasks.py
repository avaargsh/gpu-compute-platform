import json
import logging
import asyncio
from datetime import datetime, timezone
from typing import Dict, Any, Optional
from celery import Task
from celery.signals import task_prerun, task_postrun, task_failure
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select, update

from app.core.celery_app import celery_app
from app.core.database import get_async_session
from app.core.mlflow_config import MLflowTaskTracker
from app.core.task_status_broadcaster import task_broadcaster, broadcast_task_status, broadcast_task_progress, broadcast_task_logs, broadcast_task_error
from app.models.task import GpuTask, TaskStatus, TaskLog, TaskMetric
from app.models.user import User
from app.gpu.interface import JobConfig, JobResult, GpuProviderInterface
from app.gpu.providers.runpod import RunPodAdapter
from app.gpu.providers.tencent import TencentCloudAdapter
from app.gpu.providers.alibaba import AlibabaCloudAdapter

logger = logging.getLogger(__name__)


def get_provider_adapter(provider_name: str, config: Dict[str, Any]) -> GpuProviderInterface:
    """获取GPU提供商适配器"""
    if provider_name == "runpod":
        return RunPodAdapter(config)
    elif provider_name == "tencent":
        return TencentCloudAdapter(config)
    elif provider_name == "alibaba":
        return AlibabaCloudAdapter(config)
    else:
        raise ValueError(f"Unsupported provider: {provider_name}")


async def update_task_status(
    session: AsyncSession,
    task_id: str, 
    status: TaskStatus,
    broadcast: bool = True,
    **kwargs
):
    """更新任务状态并广播到WebSocket客户端"""
    try:
        update_data = {"status": status, "updated_at": datetime.now(timezone.utc)}
        update_data.update(kwargs)
        
        stmt = update(GpuTask).where(GpuTask.id == task_id).values(**update_data)
        await session.execute(stmt)
        await session.commit()
        
        logger.info(f"Updated task {task_id} status to {status}")
        
        # 广播状态更新到WebSocket客户端
        if broadcast:
            try:
                # 计算进度
                progress = None
                if status == TaskStatus.RUNNING:
                    progress = 25  # 开始执行
                elif status == TaskStatus.COMPLETED:
                    progress = 100  # 完成
                elif status == TaskStatus.FAILED:
                    progress = 100  # 失败也是终态
                elif status == TaskStatus.CANCELLED:
                    progress = 100  # 取消也是终态
                
                await broadcast_task_status(
                    task_id=task_id,
                    status=status,
                    message=f"Task status updated to {status.value}",
                    progress=progress,
                    additional_data=update_data
                )
            except Exception as broadcast_error:
                logger.warning(f"Failed to broadcast status update for task {task_id}: {broadcast_error}")
        
    except Exception as e:
        logger.error(f"Failed to update task {task_id} status: {e}")
        await session.rollback()
        raise


async def log_task_message(
    session: AsyncSession,
    task_id: str,
    level: str,
    message: str,
    source: str = "worker",
    broadcast: bool = True
):
    """记录任务日志并广播到WebSocket客户端"""
    try:
        log_entry = TaskLog(
            task_id=task_id,
            level=level,
            message=message,
            source=source,
            timestamp=datetime.now(timezone.utc)
        )
        session.add(log_entry)
        await session.commit()
        
        # 广播日志到WebSocket客户端
        if broadcast:
            try:
                await broadcast_task_logs(
                    task_id=task_id,
                    logs=message,
                    level=level,
                    source=source
                )
            except Exception as broadcast_error:
                logger.warning(f"Failed to broadcast log message for task {task_id}: {broadcast_error}")
        
    except Exception as e:
        logger.error(f"Failed to log task message: {e}")
        await session.rollback()


async def record_task_metric(
    session: AsyncSession,
    task_id: str,
    metric_name: str,
    metric_value: float,
    unit: Optional[str] = None
):
    """记录任务指标"""
    try:
        metric = TaskMetric(
            task_id=task_id,
            metric_name=metric_name,
            metric_value=metric_value,
            unit=unit,
            timestamp=datetime.now(timezone.utc)
        )
        session.add(metric)
        await session.commit()
    except Exception as e:
        logger.error(f"Failed to record task metric: {e}")
        await session.rollback()


class GPUTaskBase(Task):
    """GPU任务基类"""
    
    def on_success(self, retval, task_id, args, kwargs):
        """任务成功回调"""
        logger.info(f"Task {task_id} completed successfully")
    
    def on_failure(self, exc, task_id, args, kwargs, einfo):
        """任务失败回调"""
        logger.error(f"Task {task_id} failed: {exc}")
    
    def on_retry(self, exc, task_id, args, kwargs, einfo):
        """任务重试回调"""
        logger.warning(f"Task {task_id} retrying: {exc}")


@celery_app.task(bind=True, base=GPUTaskBase, name="app.tasks.gpu_tasks.execute_gpu_task")
def execute_gpu_task(self, task_id: str, provider_config: Dict[str, Any]):
    """执行GPU任务
    
    Args:
        task_id: 任务ID
        provider_config: 提供商配置
    """
    
    async def _execute_task():
        """异步执行任务逻辑"""
        session_gen = get_async_session()
        session = await anext(session_gen)
        
        try:
            # 获取任务信息
            stmt = select(GpuTask).where(GpuTask.id == task_id)
            result = await session.execute(stmt)
            task = result.scalar_one_or_none()
            
            if not task:
                raise ValueError(f"Task {task_id} not found")
            
            # 更新任务状态为运行中
            await update_task_status(
                session, 
                task_id, 
                TaskStatus.RUNNING,
                started_at=datetime.now(timezone.utc),
                celery_task_id=self.request.id
            )
            
            # 解析作业配置
            job_config_dict = json.loads(task.job_config)
            job_config = JobConfig(**job_config_dict)
            
            # 获取提供商适配器
            adapter = get_provider_adapter(task.provider_name, provider_config)
            
            # 启动MLflow追踪
            with MLflowTaskTracker(task_id, task.name, task.provider_name) as tracker:
                # 记录作业配置到MLflow
                tracker.log_job_config(job_config_dict)
                
                # 记录开始日志
                await log_task_message(
                    session, task_id, "INFO", 
                    f"Starting GPU task on {task.provider_name}", 
                    "worker"
                )
                
                # 广播任务开始进度
                await broadcast_task_progress(
                    task_id=task_id,
                    progress=10,
                    message="Initializing GPU task execution",
                    step_info={"step": "initialization", "provider": task.provider_name}
                )
                
                try:
                    # 提交作业到GPU提供商
                    await broadcast_task_progress(
                        task_id=task_id,
                        progress=20,
                        message=f"Submitting job to {task.provider_name}",
                        step_info={"step": "job_submission", "provider": task.provider_name}
                    )
                    
                    external_job_id = await adapter.submit_job(job_config)
                    
                    # 更新外部任务ID
                    await update_task_status(
                        session, 
                        task_id, 
                        TaskStatus.RUNNING,
                        external_job_id=external_job_id
                    )
                    
                    await log_task_message(
                        session, task_id, "INFO", 
                        f"Job submitted to {task.provider_name} with ID: {external_job_id}", 
                        "worker"
                    )
                    
                    # 广播作业提交成功
                    await broadcast_task_progress(
                        task_id=task_id,
                        progress=30,
                        message=f"Job submitted successfully. External ID: {external_job_id}",
                        step_info={"step": "job_submitted", "external_job_id": external_job_id}
                    )
                    
                    # 监控任务执行
                    await broadcast_task_progress(
                        task_id=task_id,
                        progress=40,
                        message="Monitoring job execution",
                        step_info={"step": "monitoring", "external_job_id": external_job_id}
                    )
                    
                    job_result = await monitor_job_execution(
                        session, task_id, adapter, external_job_id, tracker
                    )
                    
                    # 获取成本信息
                    cost_data = None
                    try:
                        await broadcast_task_progress(
                            task_id=task_id,
                            progress=85,
                            message="Collecting cost information",
                            step_info={"step": "cost_collection"}
                        )
                        
                        cost_info = await adapter.get_cost_info(external_job_id)
                        cost_data = {
                            "total_cost": float(cost_info.total_cost),
                            "currency": cost_info.currency
                        }
                        
                        await update_task_status(
                            session, task_id, job_result.status,
                            actual_cost=float(cost_info.total_cost),
                            currency=cost_info.currency
                        )
                        
                        # 记录成本指标
                        tracker.log_execution_metrics(cost=cost_info.total_cost)
                        await record_task_metric(
                            session, task_id, "cost", 
                            float(cost_info.total_cost), cost_info.currency
                        )
                        
                    except Exception as e:
                        logger.warning(f"Failed to get cost info for task {task_id}: {e}")
                    
                    # 记录最终状态
                    final_status = TaskStatus.COMPLETED if job_result.status.value == "completed" else TaskStatus.FAILED
                    
                    # 广播任务即将完成
                    await broadcast_task_progress(
                        task_id=task_id,
                        progress=95,
                        message=f"Task finishing with status: {final_status.value}",
                        step_info={"step": "finalizing", "final_status": final_status.value}
                    )
                    
                    await update_task_status(
                        session, 
                        task_id, 
                        final_status,
                        completed_at=datetime.now(timezone.utc),
                        exit_code=job_result.exit_code,
                        error_message=job_result.error_message,
                        logs=job_result.logs[:10000] if job_result.logs else None  # 限制日志长度
                    )
                    
                    # 记录执行指标到MLflow
                    if task.duration_seconds:
                        tracker.log_execution_metrics(
                            duration_seconds=task.duration_seconds,
                            exit_code=job_result.exit_code
                        )
                    
                    # 记录日志到MLflow
                    if job_result.logs:
                        tracker.log_task_logs(job_result.logs)
                    
                    await log_task_message(
                        session, task_id, "INFO", 
                        f"Task completed with status: {final_status}", 
                        "worker"
                    )
                    
                    # 广播任务完成
                    execution_time = task.duration_seconds if hasattr(task, 'duration_seconds') and task.duration_seconds else None
                    await task_broadcaster.broadcast_task_completed(
                        task_id=task_id,
                        success=(final_status == TaskStatus.COMPLETED),
                        result_data={
                            "exit_code": job_result.exit_code,
                            "external_job_id": external_job_id,
                            "provider": task.provider_name
                        },
                        execution_time=execution_time,
                        cost_info=cost_data
                    )
                    
                    # 更新MLflow运行ID
                    await update_task_status(
                        session, task_id, final_status,
                        mlflow_run_id=tracker.run_id
                    )
                    
                    return {
                        "task_id": task_id,
                        "status": final_status.value,
                        "external_job_id": external_job_id,
                        "mlflow_run_id": tracker.run_id
                    }
                    
                except Exception as e:
                    # 记录错误
                    error_message = str(e)
                    await update_task_status(
                        session, 
                        task_id, 
                        TaskStatus.FAILED,
                        completed_at=datetime.now(timezone.utc),
                        error_message=error_message
                    )
                    
                    await log_task_message(
                        session, task_id, "ERROR", 
                        f"Task failed: {error_message}", 
                        "worker"
                    )
                    
                    # 广播任务错误
                    await broadcast_task_error(
                        task_id=task_id,
                        error_message=error_message,
                        error_code="TASK_EXECUTION_FAILED"
                    )
                    
                    # 记录错误到MLflow
                    tracker.log_error(error_message)
                    
                    raise
        
        finally:
            await session.close()
    
    # 在Celery任务中运行异步函数
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    try:
        return loop.run_until_complete(_execute_task())
    finally:
        loop.close()


async def monitor_job_execution(
    session: AsyncSession,
    task_id: str,
    adapter: GpuProviderInterface,
    external_job_id: str,
    tracker: MLflowTaskTracker,
    poll_interval: int = 30,
    max_polls: int = 120  # 最多监控1小时
) -> JobResult:
    """监控作业执行并实时广播进度"""
    
    for poll_count in range(max_polls):
        try:
            # 获取作业状态
            job_result = await adapter.get_job_status(external_job_id)
            
            await log_task_message(
                session, task_id, "INFO", 
                f"Job status: {job_result.status}, poll: {poll_count + 1}/{max_polls}", 
                "monitor"
            )
            
            # 计算监控进度 (40% - 80%)
            monitor_progress = 40 + (poll_count / max_polls) * 40
            await broadcast_task_progress(
                task_id=task_id,
                progress=monitor_progress,
                message=f"Monitoring job execution: {job_result.status}",
                step_info={
                    "step": "monitoring",
                    "poll_count": poll_count + 1,
                    "max_polls": max_polls,
                    "job_status": job_result.status.value if hasattr(job_result.status, 'value') else str(job_result.status),
                    "external_job_id": external_job_id
                }
            )
            
            # 检查是否为终态
            if job_result.status.value in ["completed", "failed", "cancelled"]:
                # 监控完成，进度设为80%
                await broadcast_task_progress(
                    task_id=task_id,
                    progress=80,
                    message=f"Job execution finished: {job_result.status}",
                    step_info={
                        "step": "monitoring_complete",
                        "final_status": job_result.status.value if hasattr(job_result.status, 'value') else str(job_result.status)
                    }
                )
                return job_result
            
            # 等待下次轮询
            if poll_count < max_polls - 1:  # 不是最后一次
                await asyncio.sleep(poll_interval)
                
        except Exception as e:
            await log_task_message(
                session, task_id, "WARNING", 
                f"Failed to get job status: {e}", 
                "monitor"
            )
            
            # 广播监控错误但继续尝试
            await broadcast_task_progress(
                task_id=task_id,
                progress=40 + (poll_count / max_polls) * 40,
                message=f"Monitoring warning: {str(e)}",
                step_info={
                    "step": "monitoring_warning",
                    "poll_count": poll_count + 1,
                    "warning": str(e)
                }
            )
            
            if poll_count < max_polls - 1:
                await asyncio.sleep(poll_interval)
    
    # 超时
    await log_task_message(
        session, task_id, "ERROR", 
        "Job monitoring timeout", 
        "monitor"
    )
    
    # 尝试获取最后状态
    try:
        return await adapter.get_job_status(external_job_id)
    except:
        # 返回超时状态
        return JobResult(
            job_id=external_job_id,
            status="timeout",
            created_at=datetime.now(timezone.utc),
            error_message="Task monitoring timeout"
        )


@celery_app.task(name="app.tasks.gpu_tasks.check_running_tasks")
def check_running_tasks():
    """检查运行中的任务状态（定期任务）"""
    
    async def _check_tasks():
        session_gen = get_async_session()
        session = await anext(session_gen)
        
        try:
            # 查询运行中的任务
            stmt = select(GpuTask).where(
                GpuTask.status.in_([TaskStatus.RUNNING, TaskStatus.QUEUED])
            )
            result = await session.execute(stmt)
            running_tasks = result.scalars().all()
            
            logger.info(f"Checking {len(running_tasks)} running tasks")
            
            for task in running_tasks:
                try:
                    if task.external_job_id:
                        # TODO: 检查外部任务状态并更新
                        pass
                except Exception as e:
                    logger.error(f"Failed to check task {task.id}: {e}")
                    
        finally:
            await session.close()
    
    # 运行异步检查
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    try:
        loop.run_until_complete(_check_tasks())
    finally:
        loop.close()


@celery_app.task(name="app.tasks.gpu_tasks.cleanup_expired_tasks")
def cleanup_expired_tasks():
    """清理过期任务（定期任务）"""
    
    async def _cleanup_tasks():
        session_gen = get_async_session()
        session = await anext(session_gen)
        
        try:
            # 清理7天前完成的任务日志
            from datetime import timedelta
            cutoff_date = datetime.now(timezone.utc) - timedelta(days=7)
            
            # 这里可以添加清理逻辑
            logger.info("Task cleanup completed")
            
        finally:
            await session.close()
    
    # 运行异步清理
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    try:
        loop.run_until_complete(_cleanup_tasks())
    finally:
        loop.close()


@celery_app.task(bind=True, name="app.tasks.gpu_tasks.cancel_gpu_task")
def cancel_gpu_task(self, task_id: str, provider_config: Dict[str, Any]):
    """取消GPU任务"""
    
    async def _cancel_task():
        session_gen = get_async_session()
        session = await anext(session_gen)
        
        try:
            # 获取任务信息
            stmt = select(GpuTask).where(GpuTask.id == task_id)
            result = await session.execute(stmt)
            task = result.scalar_one_or_none()
            
            if not task:
                raise ValueError(f"Task {task_id} not found")
            
            if task.external_job_id:
                # 取消外部任务
                adapter = get_provider_adapter(task.provider_name, provider_config)
                await adapter.cancel_job(task.external_job_id)
            
            # 更新任务状态
            await update_task_status(
                session, 
                task_id, 
                TaskStatus.CANCELLED,
                completed_at=datetime.now(timezone.utc)
            )
            
            await log_task_message(
                session, task_id, "INFO", 
                "Task cancelled", 
                "worker"
            )
            
            # 广播任务取消
            await task_broadcaster.broadcast_task_cancelled(
                task_id=task_id,
                reason="Task was cancelled by user request"
            )
            
            return {"task_id": task_id, "status": "cancelled"}
            
        finally:
            await session.close()
    
    # 运行异步取消
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    try:
        return loop.run_until_complete(_cancel_task())
    finally:
        loop.close()


# Celery信号处理
@task_prerun.connect
def task_prerun_handler(sender=None, task_id=None, task=None, args=None, kwargs=None, **kwds):
    """任务开始前的信号处理"""
    logger.info(f"Task {task_id} starting: {task.name}")


@task_postrun.connect  
def task_postrun_handler(sender=None, task_id=None, task=None, args=None, kwargs=None, retval=None, state=None, **kwds):
    """任务完成后的信号处理"""
    logger.info(f"Task {task_id} finished: {task.name} - State: {state}")


@task_failure.connect
def task_failure_handler(sender=None, task_id=None, exception=None, traceback=None, einfo=None, **kwds):
    """任务失败的信号处理"""
    logger.error(f"Task {task_id} failed: {exception}")
    logger.error(f"Traceback: {traceback}")
