import os
from celery import Celery
from app.core.config import settings

# 创建Celery实例
celery_app = Celery(
    "gpu-compute-platform",
    broker=settings.celery_broker_url,
    backend=settings.celery_result_backend,
    include=[
        "app.tasks.gpu_tasks",  # GPU任务模块
        "app.core.dag_engine",  # DAG任务模块，确保worker加载
    ]
)

# Celery配置
celery_app.conf.update(
    # 任务序列化
    task_serializer="json",
    accept_content=["json"],
    result_serializer="json",
    timezone="UTC",
    enable_utc=True,
    
    # 任务路由
    task_routes={
        "app.tasks.gpu_tasks.*": {"queue": "gpu_tasks"},
    },
    
    # 任务结果过期时间
    result_expires=3600,
    
    # Worker配置
    worker_prefetch_multiplier=1,
    task_acks_late=True,
    worker_max_tasks_per_child=1000,
    
    # 任务重试配置
    task_reject_on_worker_lost=True,
    task_ignore_result=False,
    
    # 任务时间限制
    task_time_limit=3600,  # 1小时
    task_soft_time_limit=3300,  # 55分钟
    
    # 监控配置
    worker_send_task_events=True,
    task_send_sent_event=True,
    
    # 队列配置
    task_default_queue="default",
    task_default_exchange="default",
    task_default_routing_key="default",
)

# 定义队列
from kombu import Queue, Exchange

celery_app.conf.task_queues = (
    Queue("default", Exchange("default"), routing_key="default"),
    Queue("gpu_tasks", Exchange("gpu_tasks"), routing_key="gpu_tasks"),
    Queue("priority_high", Exchange("priority"), routing_key="priority.high"),
    Queue("priority_low", Exchange("priority"), routing_key="priority.low"),
)

# 任务优先级路由
celery_app.conf.task_routes.update({
    "app.tasks.gpu_tasks.execute_gpu_task": {
        "queue": "gpu_tasks",
        "routing_key": "gpu_tasks",
    },
    "app.tasks.gpu_tasks.monitor_task_status": {
        "queue": "default",
        "routing_key": "default",
    },
})


def get_celery_app() -> Celery:
    """获取Celery应用实例"""
    return celery_app


# 启动时配置
@celery_app.on_after_configure.connect
def setup_periodic_tasks(sender, **kwargs):
    """设置定期任务"""
    # 每30秒检查一次任务状态
    sender.add_periodic_task(
        30.0, 
        "app.tasks.gpu_tasks.check_running_tasks",
        name="check-running-tasks"
    )
    
    # 每小时清理过期任务
    sender.add_periodic_task(
        3600.0,
        "app.tasks.gpu_tasks.cleanup_expired_tasks", 
        name="cleanup-expired-tasks"
    )
