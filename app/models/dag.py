from datetime import datetime, timezone
from enum import Enum
from typing import Optional, Dict, Any, List
import uuid
import json

from sqlalchemy import Column, String, Text, DateTime, Enum as SQLEnum, ForeignKey, Integer, Boolean
from sqlalchemy.dialects.postgresql import UUID
from sqlalchemy.orm import relationship
from sqlalchemy.ext.declarative import declarative_base

from app.core.database import Base


class DAGStatus(str, Enum):
    """DAG状态枚举"""
    PENDING = "pending"         # 等待中
    RUNNING = "running"         # 运行中  
    COMPLETED = "completed"     # 已完成
    FAILED = "failed"          # 失败
    CANCELLED = "cancelled"    # 已取消
    PAUSED = "paused"          # 暂停


class NodeType(str, Enum):
    """节点类型"""
    TASK = "task"              # GPU任务节点
    CONDITION = "condition"    # 条件判断节点
    PARALLEL = "parallel"      # 并行组节点
    SEQUENTIAL = "sequential"  # 顺序组节点
    WEBHOOK = "webhook"        # Webhook节点


class NodeStatus(str, Enum):
    """节点状态"""
    PENDING = "pending"
    WAITING = "waiting"        # 等待依赖
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    SKIPPED = "skipped"        # 跳过
    CANCELLED = "cancelled"


class TaskDAG(Base):
    """任务DAG（有向无环图）"""
    __tablename__ = "task_dags"

    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    name = Column(String(255), nullable=False, comment="DAG名称")
    description = Column(Text, nullable=True, comment="DAG描述")
    
    # 用户信息
    user_id = Column(UUID(as_uuid=True), ForeignKey("users.id"), nullable=False, index=True, comment="用户ID")
    user = relationship("User", back_populates="task_dags")
    
    # DAG配置
    dag_config = Column(Text, nullable=False, comment="DAG配置JSON")
    schedule_expression = Column(String(100), nullable=True, comment="调度表达式")
    is_active = Column(Boolean, default=True, comment="是否激活")
    is_template = Column(Boolean, default=False, comment="是否为模板")
    
    # 状态和时间
    status = Column(SQLEnum(DAGStatus), default=DAGStatus.PENDING, nullable=False, index=True, comment="DAG状态")
    created_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="创建时间")
    updated_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc), nullable=False, comment="更新时间")
    
    # 执行统计
    total_runs = Column(Integer, default=0, comment="总运行次数")
    success_runs = Column(Integer, default=0, comment="成功运行次数")
    failed_runs = Column(Integer, default=0, comment="失败运行次数")
    
    # 关联关系
    dag_runs = relationship("DAGRun", back_populates="dag", cascade="all, delete-orphan", lazy="noload")
    dag_nodes = relationship("DAGNode", back_populates="dag", cascade="all, delete-orphan", lazy="noload")
    
    def __repr__(self):
        return f"<TaskDAG(id='{self.id}', name='{self.name}', status='{self.status}')>"
    
    @property
    def success_rate(self) -> float:
        """成功率"""
        if self.total_runs == 0:
            return 0.0
        return self.success_runs / self.total_runs
    
    @property
    def schedule(self) -> Optional[str]:
        """API兼容性属性，返回调度表达式"""
        return self.schedule_expression
    
    @schedule.setter
    def schedule(self, value: Optional[str]):
        self.schedule_expression = value


class DAGRun(Base):
    """DAG运行实例"""
    __tablename__ = "dag_runs"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    dag_id = Column(String, ForeignKey("task_dags.id", ondelete="CASCADE"), nullable=False, index=True)
    
    # 运行信息
    run_name = Column(String(100), nullable=True, comment="运行名称")
    status = Column(SQLEnum(DAGStatus), default=DAGStatus.PENDING, nullable=False, index=True, comment="运行状态")
    
    # 时间信息
    created_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="创建时间")
    started_at = Column(DateTime(timezone=True), nullable=True, comment="开始时间")
    completed_at = Column(DateTime(timezone=True), nullable=True, comment="完成时间")
    
    # 运行参数和错误
    parameters = Column(Text, nullable=True, comment="运行参数JSON")
    error_message = Column(Text, nullable=True, comment="错误信息")
    
    # 归属用户
    user_id = Column(UUID(as_uuid=True), ForeignKey("users.id"), nullable=True, index=True, comment="用户ID")
    
    # MLflow追踪
    mlflow_experiment_id = Column(String(255), nullable=True, comment="MLflow实验ID")
    mlflow_parent_run_id = Column(String(255), nullable=True, comment="MLflow父运行ID")
    
    # 关联关系
    dag = relationship("TaskDAG", back_populates="dag_runs")
    node_runs = relationship("DAGNodeRun", back_populates="dag_run", cascade="all, delete-orphan")
    
    def __repr__(self):
        return f"<DAGRun(id='{self.id}', dag_id='{self.dag_id}', status='{self.status}')>"
    
    @property
    def duration_seconds(self) -> Optional[int]:
        """运行时长（秒）"""
        if self.started_at and self.completed_at:
            return int((self.completed_at - self.started_at).total_seconds())
        return None


class DAGNode(Base):
    """DAG节点定义"""
    __tablename__ = "dag_nodes"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    dag_id = Column(String, ForeignKey("task_dags.id", ondelete="CASCADE"), nullable=False, index=True)
    
    # 节点信息
    node_id = Column(String(100), nullable=False, comment="节点ID")
    node_name = Column(String(255), nullable=False, comment="节点名称")
    node_type = Column(SQLEnum(NodeType), nullable=False, comment="节点类型")
    
    # 节点配置
    node_config = Column(Text, nullable=False, comment="节点配置JSON")
    position_x = Column(Integer, default=0, comment="X坐标")
    position_y = Column(Integer, default=0, comment="Y坐标")
    
    # 依赖关系
    depends_on = Column(Text, nullable=True, comment="依赖节点ID列表JSON")
    dependencies = Column(Text, nullable=True, comment="依赖节点ID列表JSON")  # API兼容性别名
    
    # 关联关系
    dag = relationship("TaskDAG", back_populates="dag_nodes")
    node_runs = relationship("DAGNodeRun", back_populates="node", cascade="all, delete-orphan")
    
    def __repr__(self):
        return f"<DAGNode(id='{self.id}', node_id='{self.node_id}', node_type='{self.node_type}')>"
    
    def get_dependencies(self) -> List[str]:
        """获取依赖节点ID列表。
        优先从depends_on读取；若为空则回退到dependencies。
        如果JSON无效，则抛出JSONDecodeError。
        """
        source = self.depends_on if self.depends_on not in (None, "") else self.dependencies
        if not source:
            return []
        # 让json.loads抛出异常以便上层测试捕获
        return json.loads(source)
    
    def set_dependencies(self, deps: List[str]):
        """设置依赖节点ID列表"""
        self.depends_on = json.dumps(deps)


class DAGNodeRun(Base):
    """DAG节点运行实例"""
    __tablename__ = "dag_node_runs"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    dag_run_id = Column(String, ForeignKey("dag_runs.id", ondelete="CASCADE"), nullable=False, index=True)
    node_id = Column(String, ForeignKey("dag_nodes.id", ondelete="CASCADE"), nullable=False, index=True)
    
    # 运行信息
    status = Column(SQLEnum(NodeStatus), default=NodeStatus.PENDING, nullable=False, index=True, comment="节点状态")
    attempt_number = Column(Integer, default=1, comment="尝试次数")
    
    # 时间信息
    created_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="创建时间")
    started_at = Column(DateTime(timezone=True), nullable=True, comment="开始时间")
    completed_at = Column(DateTime(timezone=True), nullable=True, comment="完成时间")
    
    # 执行结果
    exit_code = Column(Integer, nullable=True, comment="退出码")
    error_message = Column(Text, nullable=True, comment="错误信息")
    output_data = Column(Text, nullable=True, comment="输出数据JSON")
    
    # 任务关联
    gpu_task_id = Column(String, ForeignKey("gpu_tasks.id"), nullable=True, index=True, comment="关联的GPU任务ID")
    celery_task_id = Column(String(255), nullable=True, index=True, comment="Celery任务ID")
    
    # MLflow追踪
    mlflow_run_id = Column(String(255), nullable=True, comment="MLflow运行ID")
    
    # 关联关系
    dag_run = relationship("DAGRun", back_populates="node_runs")
    node = relationship("DAGNode", back_populates="node_runs")
    gpu_task = relationship("GpuTask", backref="dag_node_runs")
    
    def __repr__(self):
        return f"<DAGNodeRun(id='{self.id}', node_id='{self.node_id}', status='{self.status}')>"
    
    @property
    def duration_seconds(self) -> Optional[int]:
        """执行时长（秒）"""
        if self.started_at and self.completed_at:
            return int((self.completed_at - self.started_at).total_seconds())
        return None
    
    @property
    def is_terminal_state(self) -> bool:
        """是否为终态"""
        return self.status in [NodeStatus.COMPLETED, NodeStatus.FAILED, NodeStatus.SKIPPED, NodeStatus.CANCELLED]


class DAGEdge(Base):
    """DAG边（节点间的依赖关系）"""
    __tablename__ = "dag_edges"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    dag_id = Column(String, ForeignKey("task_dags.id", ondelete="CASCADE"), nullable=False, index=True)
    
    # 边信息
    from_node_id = Column(String, nullable=False, comment="源节点ID")
    to_node_id = Column(String, nullable=False, comment="目标节点ID")
    
    # 条件配置
    condition_config = Column(Text, nullable=True, comment="边条件配置JSON")
    
    def __repr__(self):
        return f"<DAGEdge(from='{self.from_node_id}', to='{self.to_node_id}')>"


class DAGTemplate(Base):
    """DAG模板"""
    __tablename__ = "dag_templates"
    
    id = Column(String, primary_key=True, default=lambda: str(uuid.uuid4()), index=True)
    name = Column(String(255), nullable=False, comment="模板名称")
    description = Column(Text, nullable=True, comment="模板描述")
    category = Column(String(100), nullable=True, comment="模板分类")
    
    # 模板配置
    template_config = Column(Text, nullable=False, comment="模板配置JSON")
    is_public = Column(Boolean, default=False, comment="是否公开")
    
    # 创建者
    created_by = Column(UUID(as_uuid=True), ForeignKey("users.id"), nullable=False, comment="创建者ID")
    created_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), nullable=False, comment="创建时间")
    updated_at = Column(DateTime(timezone=True), default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc), nullable=False, comment="更新时间")
    
    # 使用统计
    usage_count = Column(Integer, default=0, comment="使用次数")
    
    def __repr__(self):
        return f"<DAGTemplate(id='{self.id}', name='{self.name}', category='{self.category}')>"
