from datetime import datetime, timezone
from enum import Enum
from typing import Optional
import uuid
from decimal import Decimal

from sqlalchemy import Column, String, Text, DateTime, Enum as SQLEnum, ForeignKey, Numeric, Integer
from sqlalchemy.dialects.postgresql import UUID
from sqlalchemy.dialects.sqlite import TEXT
from sqlalchemy.orm import relationship
from sqlalchemy.ext.declarative import declarative_base

from app.core.database import Base


class TaskStatus(str, Enum):
    """任务状态枚举"""
    PENDING = "pending"           # 等待中
    QUEUED = "queued"            # 队列中
    RUNNING = "running"          # 运行中
    COMPLETED = "completed"      # 已完成
    FAILED = "failed"           # 失败
    CANCELLED = "cancelled"     # 已取消
    TIMEOUT = "timeout"         # 超时


class TaskPriority(str, Enum):
    """任务优先级"""
    LOW = "low"
    NORMAL = "normal"
    HIGH = "high"
    URGENT = "urgent"


class GpuTask(Base):
    """GPU任务数据库模型"""
    __tablename__ = "gpu_tasks"

    # 主键和基础信息
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    name = Column(String(255), nullable=False, comment="任务名称")
    description = Column(Text, nullable=True, comment="任务描述")
    
    # 用户信息
    user_id = Column(UUID(as_uuid=True), ForeignKey("users.id"), nullable=False, index=True, comment="用户ID")
    user = relationship("User", back_populates="gpu_tasks")
    
    # 任务配置
    provider_name = Column(String(50), nullable=False, index=True, comment="GPU提供商")
    job_config = Column(Text, nullable=False, comment="作业配置JSON")
    
    # 外部任务ID
    external_job_id = Column(String(255), nullable=True, index=True, comment="外部提供商任务ID")
    celery_task_id = Column(String(255), nullable=True, index=True, comment="Celery任务ID")
    
    # 状态和优先级
    status = Column(SQLEnum(TaskStatus), default=TaskStatus.PENDING, nullable=False, index=True, comment="任务状态")
    priority = Column(SQLEnum(TaskPriority), default=TaskPriority.NORMAL, nullable=False, comment="任务优先级")
    
    # 时间戳
    created_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="创建时间")
    started_at = Column(DateTime(timezone=True), nullable=True, comment="开始时间")
    completed_at = Column(DateTime(timezone=True), nullable=True, comment="完成时间")
    updated_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc), nullable=False, comment="更新时间")
    
    # 执行结果
    exit_code = Column(Integer, nullable=True, comment="退出码")
    error_message = Column(Text, nullable=True, comment="错误信息")
    logs = Column(Text, nullable=True, comment="任务日志")
    
    # 资源和成本
    gpu_spec = Column(Text, nullable=True, comment="GPU规格JSON")
    estimated_cost = Column(Numeric(10, 4), nullable=True, comment="预估成本")
    actual_cost = Column(Numeric(10, 4), nullable=True, comment="实际成本")
    currency = Column(String(10), default="USD", comment="货币单位")
    
    # MLflow相关
    mlflow_run_id = Column(String(255), nullable=True, index=True, comment="MLflow运行ID")
    mlflow_experiment_name = Column(String(255), nullable=True, comment="MLflow实验名称")
    
    # 元数据
    tags = Column(Text, nullable=True, comment="标签JSON")
    task_metadata = Column(Text, nullable=True, comment="元数据JSON")
    
    def __repr__(self):
        return f"<GpuTask(id='{self.id}', name='{self.name}', status='{self.status}')>"
    
    @property
    def duration_seconds(self) -> Optional[int]:
        """计算任务执行时长（秒）"""
        if self.started_at and self.completed_at:
            return int((self.completed_at - self.started_at).total_seconds())
        return None
    
    @property
    def is_terminal_state(self) -> bool:
        """检查是否为终态（不再变化的状态）"""
        return self.status in [TaskStatus.COMPLETED, TaskStatus.FAILED, TaskStatus.CANCELLED, TaskStatus.TIMEOUT]
    
    @property
    def is_active(self) -> bool:
        """检查是否为活跃状态"""
        return self.status in [TaskStatus.PENDING, TaskStatus.QUEUED, TaskStatus.RUNNING]


class TaskLog(Base):
    """任务日志模型（用于存储详细日志）"""
    __tablename__ = "task_logs"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()))
    task_id = Column(String, ForeignKey("gpu_tasks.id", ondelete="CASCADE"), nullable=False, index=True)
    level = Column(String(20), nullable=False, comment="日志级别")
    message = Column(Text, nullable=False, comment="日志消息")
    timestamp = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="时间戳")
    source = Column(String(50), nullable=True, comment="日志来源")
    
    # 关联任务
    task = relationship("GpuTask", backref="detailed_logs")
    
    def __repr__(self):
        return f"<TaskLog(task_id='{self.task_id}', level='{self.level}', timestamp='{self.timestamp}')>"


class TaskMetric(Base):
    """任务指标模型（用于存储性能指标）"""
    __tablename__ = "task_metrics"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()))
    task_id = Column(String, ForeignKey("gpu_tasks.id", ondelete="CASCADE"), nullable=False, index=True)
    metric_name = Column(String(100), nullable=False, comment="指标名称")
    metric_value = Column(Numeric(20, 6), nullable=False, comment="指标值")
    unit = Column(String(20), nullable=True, comment="单位")
    timestamp = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="时间戳")
    
    # 关联任务
    task = relationship("GpuTask", backref="metrics")
    
    def __repr__(self):
        return f"<TaskMetric(task_id='{self.task_id}', name='{self.metric_name}', value='{self.metric_value}')>"
