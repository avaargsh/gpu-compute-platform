from pydantic import BaseModel, Field
from typing import Optional, Dict, List, Any
from datetime import datetime
from decimal import Decimal
from app.models.task import TaskStatus, TaskPriority


class TaskBase(BaseModel):
    """任务基础Schema"""
    name: str = Field(..., description="任务名称", min_length=2, max_length=255)
    description: Optional[str] = Field(None, description="任务描述")
    provider_name: str = Field(..., description="GPU提供商名称")
    priority: TaskPriority = Field(default=TaskPriority.NORMAL, description="任务优先级")


class TaskCreate(TaskBase):
    """创建任务Schema"""
    provider: str = Field(..., description="云服务商", pattern="^(alibaba|tencent|runpod)$")
    gpu_model: str = Field(..., description="GPU型号")
    image: str = Field(..., description="Docker镜像")
    script: str = Field(..., description="执行脚本", min_length=10)
    dataset_path: Optional[str] = Field(None, description="数据集路径")
    scheduling_strategy: str = Field("cost", description="调度策略", pattern="^(cost|performance|availability)$")
    max_duration: Optional[int] = Field(2, description="最大执行时间(小时)", ge=1, le=168)
    budget_limit: Optional[float] = Field(50.0, description="预算限制", ge=0)
    environment_vars: Optional[Dict[str, str]] = Field(default_factory=dict, description="环境变量")
    requirements: Optional[List[str]] = Field(default_factory=list, description="Python依赖包")


class TaskUpdate(BaseModel):
    """更新任务Schema"""
    name: Optional[str] = Field(None, description="任务名称")
    description: Optional[str] = Field(None, description="任务描述")
    priority: Optional[TaskPriority] = Field(None, description="任务优先级")
    status: Optional[TaskStatus] = Field(None, description="任务状态")


class TaskRead(TaskBase):
    """读取任务Schema"""
    id: str
    user_id: str
    user_name: Optional[str] = None
    provider: str
    gpu_model: str
    image: str
    script: str
    dataset_path: Optional[str] = None
    scheduling_strategy: str
    
    # 外部ID和状态
    external_job_id: Optional[str] = None
    celery_task_id: Optional[str] = None
    status: TaskStatus
    
    # 时间信息
    created_at: datetime
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    updated_at: datetime
    submitted_at: datetime  # 前端使用的提交时间
    
    # 执行结果
    exit_code: Optional[int] = None
    error_message: Optional[str] = None
    logs: Optional[str] = None
    
    # 成本信息
    estimated_cost: Optional[Decimal] = None
    actual_cost: Optional[Decimal] = None
    currency: str = "USD"
    
    # 进度和资源使用
    progress: int = 0
    gpu_usage: int = 0
    memory_usage: int = 0
    
    # MLflow信息
    mlflow_run_id: Optional[str] = None
    mlflow_experiment_name: Optional[str] = None
    
    # 元数据
    tags: Optional[Dict[str, Any]] = None
    task_metadata: Optional[Dict[str, Any]] = None
    
    class Config:
        from_attributes = True
        
    @classmethod
    def from_db_model(cls, db_task: Any, user_name: str = None):
        """从数据库模型创建Schema实例"""
        import json
        
        return cls(
            id=db_task.id,
            name=db_task.name,
            description=db_task.description,
            user_id=db_task.user_id,
            user_name=user_name or "Unknown",
            provider_name=db_task.provider_name,
            provider=db_task.provider_name,  # 兼容前端
            
            # 从job_config解析配置信息
            gpu_model=cls._get_from_job_config(db_task.job_config, 'gpu_model', 'Unknown'),
            image=cls._get_from_job_config(db_task.job_config, 'image', 'Unknown'),
            script=cls._get_from_job_config(db_task.job_config, 'script', ''),
            dataset_path=cls._get_from_job_config(db_task.job_config, 'dataset_path'),
            scheduling_strategy=cls._get_from_job_config(db_task.job_config, 'scheduling_strategy', 'cost'),
            
            external_job_id=db_task.external_job_id,
            celery_task_id=db_task.celery_task_id,
            status=db_task.status,
            priority=db_task.priority,
            
            created_at=db_task.created_at,
            started_at=db_task.started_at,
            completed_at=db_task.completed_at,
            updated_at=db_task.updated_at,
            submitted_at=db_task.created_at,  # 使用创建时间作为提交时间
            
            exit_code=db_task.exit_code,
            error_message=db_task.error_message,
            logs=db_task.logs,
            
            estimated_cost=db_task.estimated_cost,
            actual_cost=db_task.actual_cost,
            currency=db_task.currency,
            
            # 模拟进度和资源使用（实际项目中应该从实时数据获取）
            progress=cls._calculate_progress(db_task.status, db_task.started_at, db_task.completed_at),
            gpu_usage=cls._get_from_job_config(db_task.job_config, 'gpu_usage', 0),
            memory_usage=cls._get_from_job_config(db_task.job_config, 'memory_usage', 0),
            
            mlflow_run_id=db_task.mlflow_run_id,
            mlflow_experiment_name=db_task.mlflow_experiment_name,
            
            tags=cls._parse_json_field(db_task.tags),
            task_metadata=cls._parse_json_field(db_task.task_metadata)
        )
    
    @staticmethod
    def _get_from_job_config(job_config: str, key: str, default: Any = None):
        """从job_config JSON字符串中获取值"""
        try:
            import json
            config = json.loads(job_config) if job_config else {}
            return config.get(key, default)
        except:
            return default
    
    @staticmethod
    def _calculate_progress(status: TaskStatus, started_at: Optional[datetime], completed_at: Optional[datetime]) -> int:
        """根据任务状态计算进度"""
        if status == TaskStatus.COMPLETED:
            return 100
        elif status == TaskStatus.FAILED or status == TaskStatus.CANCELLED:
            return 0
        elif status == TaskStatus.RUNNING:
            if started_at and completed_at is None:
                # 根据运行时间估算进度（这里简化处理）
                elapsed = (datetime.now() - started_at.replace(tzinfo=None)).total_seconds()
                # 假设任务平均需要1小时，按比例计算进度，最多90%
                progress = min(int(elapsed / 3600 * 100), 90)
                return progress
            return 10
        elif status == TaskStatus.QUEUED:
            return 5
        else:  # PENDING
            return 0
    
    @staticmethod
    def _parse_json_field(json_str: Optional[str]) -> Optional[Dict[str, Any]]:
        """解析JSON字段"""
        if not json_str:
            return None
        try:
            import json
            return json.loads(json_str)
        except:
            return None


class TaskList(BaseModel):
    """任务列表Schema"""
    items: List[TaskRead]
    total: int
    page: int
    per_page: int
    pages: int


class TaskLogEntry(BaseModel):
    """任务日志条目Schema"""
    timestamp: str
    level: str = Field(..., pattern="^(info|warning|error)$")
    message: str


class TaskStats(BaseModel):
    """任务统计Schema"""
    total_tasks: int
    running_tasks: int
    completed_tasks: int
    failed_tasks: int
    pending_tasks: int
    cancelled_tasks: int
    total_compute_hours: float
    total_cost: float
    currency: str = "USD"


# Provider 相关 Schema
class Provider(BaseModel):
    """云服务商Schema"""
    id: str
    name: str
    display_name: str
    is_available: bool = True
    regions: List[str] = []


class GPUModel(BaseModel):
    """GPU型号Schema"""
    id: str
    name: str
    provider: str
    memory_gb: int
    compute_capability: Optional[str] = None
    cost_per_hour: float
    availability: str = Field("medium", pattern="^(high|medium|low)$")


class DockerImage(BaseModel):
    """Docker镜像Schema"""
    name: str
    tag: str = "latest"
    description: Optional[str] = None


# API 响应Schema
class ApiResponse(BaseModel):
    """通用API响应Schema"""
    success: bool = True
    message: Optional[str] = None
    data: Optional[Any] = None
    error: Optional[str] = None


class TaskResponse(ApiResponse):
    """任务API响应Schema"""
    data: Optional[TaskRead] = None


class TaskListResponse(ApiResponse):
    """任务列表API响应Schema"""
    data: Optional[TaskList] = None
