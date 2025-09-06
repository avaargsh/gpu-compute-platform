import logging
from datetime import datetime, timezone
from typing import Dict, List, Optional, Tuple, Any
from dataclasses import dataclass
from enum import Enum
import json
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select, func

from app.models.task import GpuTask, TaskStatus
from app.gpu.interface import GpuSpec, JobConfig
from app.core.config import settings

logger = logging.getLogger(__name__)


@dataclass
class ProviderMetrics:
    """Provider指标 - 新的简化版本"""
    provider_name: str
    availability_score: float = 0.9
    avg_cost_per_hour: float = 2.5
    avg_queue_time_minutes: float = 5.0
    success_rate: float = 0.95
    current_load: float = 0.5
    supported_gpu_types: List[str] = None
    
    def __post_init__(self):
        if self.supported_gpu_types is None:
            self.supported_gpu_types = ["A100", "V100", "T4"]


class SchedulingStrategy(str, Enum):
    """调度策略"""
    COST_OPTIMIZED = "cost_optimized"           # 成本优先
    PERFORMANCE_OPTIMIZED = "performance_optimized"  # 性能优先
    BALANCED = "balanced"                       # 平衡模式
    AVAILABILITY_FIRST = "availability_first"  # 可用性优先


# 移除重复的ProviderMetrics定义
    
# 移除重复的属性定义


@dataclass
class TaskProfile:
    """任务画像"""
    gpu_type: str
    gpu_count: int
    estimated_duration_minutes: int
    memory_gb: int
    vcpus: int
    ram_gb: int
    priority_score: float  # 优先级评分 (0-1)


@dataclass
class TaskRequirement:
    """任务需求 - 用于API接口"""
    gpu_type: str
    gpu_count: int = 1
    memory_gb: int = 0
    vcpus: int = 4
    estimated_duration_minutes: int = 60
    priority: int = 5  # 1-10, 10为最高优先级
    
    def __post_init__(self):
        if self.gpu_count <= 0:
            raise ValueError("GPU count must be greater than 0")
        if not (1 <= self.priority <= 10):
            raise ValueError("Priority must be between 1 and 10")
    
    @property
    def resource_intensity(self) -> float:
        """资源强度评分"""
        gpu_weight = self.gpu_count * 0.4
        memory_weight = min(self.memory_gb / 100, 1) * 0.3
        duration_weight = min(self.estimated_duration_minutes / 120, 1) * 0.3
        return gpu_weight + memory_weight + duration_weight
    
    @property
    def size_category(self) -> str:
        """任务大小分类"""
        if self.resource_intensity < 0.3:
            return "small"
        elif self.resource_intensity < 0.7:
            return "medium"
        else:
            return "large"


class IntelligentScheduler:
    """智能任务调度器"""
    
    def __init__(self):
        self.providers = {
            "runpod": ProviderMetrics(
                name="runpod",
                availability_score=0.95,
                cost_per_hour=2.89,  # A100价格
                average_queue_time=30,
                success_rate=0.98,
                average_execution_time=1800,
                current_load=0,
                max_concurrent=100,
                supported_gpu_types=["A100", "RTX4090", "A6000", "T4"]
            ),
            "tencent": ProviderMetrics(
                name="tencent",
                availability_score=0.92,
                cost_per_hour=3.2,
                average_queue_time=45,
                success_rate=0.96,
                average_execution_time=2100,
                current_load=0,
                max_concurrent=50,
                supported_gpu_types=["T4", "V100", "A100"]
            ),
            "alibaba": ProviderMetrics(
                name="alibaba",
                availability_score=0.90,
                cost_per_hour=2.95,
                average_queue_time=60,
                success_rate=0.94,
                average_execution_time=2400,
                current_load=0,
                max_concurrent=75,
                supported_gpu_types=["T4", "V100", "A100"]
            )
        }
    
    async def update_provider_metrics(self, session: AsyncSession):
        """更新Provider实时指标"""
        try:
            for provider_name in self.providers:
                # 查询当前运行中的任务数
                stmt = select(func.count(GpuTask.id)).where(
                    GpuTask.provider_name == provider_name,
                    GpuTask.status.in_([TaskStatus.RUNNING, TaskStatus.QUEUED])
                )
                result = await session.execute(stmt)
                current_load = result.scalar() or 0
                
                self.providers[provider_name].current_load = current_load
                
                # 查询最近成功率
                recent_tasks_stmt = select(GpuTask).where(
                    GpuTask.provider_name == provider_name,
                    GpuTask.status.in_([TaskStatus.COMPLETED, TaskStatus.FAILED]),
                    GpuTask.completed_at >= datetime.now(timezone.utc).replace(hour=0, minute=0, second=0)
                ).limit(100)
                
                recent_result = await session.execute(recent_tasks_stmt)
                recent_tasks = recent_result.scalars().all()
                
                if recent_tasks:
                    success_count = len([t for t in recent_tasks if t.status == TaskStatus.COMPLETED])
                    self.providers[provider_name].success_rate = success_count / len(recent_tasks)
                    
                    # 更新平均执行时间
                    completed_tasks = [t for t in recent_tasks if t.duration_seconds]
                    if completed_tasks:
                        avg_duration = sum(t.duration_seconds for t in completed_tasks) / len(completed_tasks)
                        self.providers[provider_name].average_execution_time = avg_duration
                
            logger.info("Updated provider metrics successfully")
            
        except Exception as e:
            logger.error(f"Failed to update provider metrics: {e}")
    
    def create_task_profile(self, job_config: JobConfig) -> TaskProfile:
        """创建任务画像"""
        gpu_spec = job_config.gpu_spec
        
        # 估算执行时长
        estimated_duration = self._estimate_duration(job_config)
        
        # 计算优先级评分
        priority_map = {"low": 0.2, "normal": 0.5, "high": 0.8, "urgent": 1.0}
        priority_score = priority_map.get(getattr(job_config, 'priority', 'normal'), 0.5)
        
        return TaskProfile(
            gpu_type=gpu_spec.gpu_type,
            gpu_count=gpu_spec.gpu_count,
            estimated_duration_minutes=estimated_duration,
            memory_gb=gpu_spec.memory_gb,
            vcpus=gpu_spec.vcpus,
            ram_gb=gpu_spec.ram_gb,
            priority_score=priority_score
        )
    
    def _estimate_duration(self, job_config: JobConfig) -> int:
        """估算任务执行时长(分钟)"""
        # 基于任务配置估算时长
        base_duration = 30  # 基础时长30分钟
        
        # GPU数量影响
        gpu_factor = job_config.gpu_spec.gpu_count * 0.8
        
        # GPU类型影响
        gpu_type_factors = {
            "T4": 1.5,
            "V100": 1.2,
            "A100": 1.0,
            "RTX4090": 1.1,
            "A6000": 1.1
        }
        gpu_type_factor = gpu_type_factors.get(job_config.gpu_spec.gpu_type, 1.0)
        
        # 超时时间影响
        timeout_factor = min(job_config.timeout_minutes or 60, 180) / 60
        
        estimated = int(base_duration * gpu_factor * gpu_type_factor * timeout_factor)
        return max(estimated, 5)  # 最少5分钟
    
    def select_optimal_provider(
        self, 
        task_profile: TaskProfile, 
        strategy: SchedulingStrategy = SchedulingStrategy.BALANCED
    ) -> Tuple[str, float]:
        """选择最优Provider"""
        
        scores = {}
        
        for provider_name, metrics in self.providers.items():
            # 检查GPU类型支持
            if task_profile.gpu_type not in metrics.supported_gpu_types:
                continue
            
            # 检查负载能力
            if metrics.current_load >= metrics.max_concurrent:
                continue
            
            score = self._calculate_provider_score(task_profile, metrics, strategy)
            scores[provider_name] = score
        
        if not scores:
            raise ValueError("No suitable provider found")
        
        best_provider = max(scores.items(), key=lambda x: x[1])
        return best_provider[0], best_provider[1]
    
    def _calculate_provider_score(
        self, 
        task_profile: TaskProfile, 
        metrics: ProviderMetrics, 
        strategy: SchedulingStrategy
    ) -> float:
        """计算Provider评分"""
        
        if strategy == SchedulingStrategy.COST_OPTIMIZED:
            # 成本优先：成本权重60%，性能40%
            cost_score = 1 / (1 + metrics.cost_per_hour / 10)  # 成本越低分越高
            performance_score = metrics.efficiency_score
            return cost_score * 0.6 + performance_score * 0.4
        
        elif strategy == SchedulingStrategy.PERFORMANCE_OPTIMIZED:
            # 性能优先：性能权重70%，成本30%
            performance_score = metrics.efficiency_score
            queue_score = 1 / (1 + metrics.average_queue_time / 100)
            cost_score = 1 / (1 + metrics.cost_per_hour / 10)
            return performance_score * 0.5 + queue_score * 0.2 + cost_score * 0.3
        
        elif strategy == SchedulingStrategy.AVAILABILITY_FIRST:
            # 可用性优先
            availability_score = metrics.availability_score
            load_score = 1 - metrics.load_ratio
            success_score = metrics.success_rate
            return availability_score * 0.4 + load_score * 0.3 + success_score * 0.3
        
        else:  # BALANCED
            # 平衡模式：各因素权重相等
            cost_score = 1 / (1 + metrics.cost_per_hour / 10)
            performance_score = metrics.efficiency_score
            availability_score = metrics.availability_score
            load_score = 1 - metrics.load_ratio
            
            return (cost_score + performance_score + availability_score + load_score) / 4
    
    def get_optimal_queue(self, task_profile: TaskProfile) -> str:
        """获取最优队列"""
        # 根据任务大小和优先级选择队列
        if task_profile.priority_score > 0.8:
            return "priority_high"
        elif task_profile.size_category == "large":
            return "gpu_tasks"  # 大任务使用专用GPU队列
        elif task_profile.priority_score < 0.3:
            return "priority_low"
        else:
            return "gpu_tasks"
    
    def get_worker_routing_key(self, task_profile: TaskProfile, provider: str) -> str:
        """获取Worker路由键"""
        # 根据GPU类型和任务大小确定路由
        gpu_type = task_profile.gpu_type.lower()
        size = task_profile.size_category
        
        return f"{provider}.{gpu_type}.{size}"
    
    async def schedule_task(
        self, 
        session: AsyncSession,
        job_config: JobConfig, 
        strategy: SchedulingStrategy = SchedulingStrategy.BALANCED
    ) -> Dict[str, Any]:
        """智能调度任务"""
        
        # 更新Provider指标
        await self.update_provider_metrics(session)
        
        # 创建任务画像
        task_profile = self.create_task_profile(job_config)
        
        # 选择最优Provider
        provider, score = self.select_optimal_provider(task_profile, strategy)
        
        # 获取队列和路由信息
        queue = self.get_optimal_queue(task_profile)
        routing_key = self.get_worker_routing_key(task_profile, provider)
        
        # 预估成本
        provider_metrics = self.providers[provider]
        estimated_cost = (
            provider_metrics.cost_per_hour * 
            task_profile.estimated_duration_minutes / 60
        )
        
        logger.info(
            f"Scheduled task to {provider} (score: {score:.3f}, "
            f"queue: {queue}, estimated_cost: ${estimated_cost:.2f})"
        )
        
        return {
            "provider": provider,
            "provider_config": self._get_provider_config(provider),
            "queue": queue,
            "routing_key": routing_key,
            "estimated_cost": estimated_cost,
            "estimated_duration_minutes": task_profile.estimated_duration_minutes,
            "scheduling_score": score,
            "task_profile": {
                "size_category": task_profile.size_category,
                "resource_intensity": task_profile.resource_intensity,
                "priority_score": task_profile.priority_score
            }
        }
    
    def _get_provider_config(self, provider: str) -> Dict[str, Any]:
        """获取Provider配置"""
        # 这里应该从环境变量或配置文件读取
        configs = {
            "runpod": {
                "api_key": "demo-api-key",
                "endpoint_id": "demo-endpoint",
            },
            "tencent": {
                "secret_id": "demo-secret-id",
                "secret_key": "demo-secret-key",
                "region": "ap-shanghai",
                "cluster_id": "cls-demo",
            },
            "alibaba": {
                "access_key_id": "demo-access-key",
                "access_key_secret": "demo-secret",
                "region_id": "cn-hangzhou",
            }
        }
        return configs.get(provider, {})
    
    def get_scheduling_recommendation(
        self, 
        task_profile: TaskProfile
    ) -> Dict[str, Any]:
        """获取调度建议"""
        recommendations = []
        
        for provider_name, metrics in self.providers.items():
            if task_profile.gpu_type not in metrics.supported_gpu_types:
                continue
            
            cost_per_task = metrics.cost_per_hour * task_profile.estimated_duration_minutes / 60
            wait_time = metrics.average_queue_time + (metrics.current_load * 10)
            
            recommendations.append({
                "provider": provider_name,
                "estimated_cost": cost_per_task,
                "estimated_wait_time_seconds": wait_time,
                "success_rate": metrics.success_rate,
                "current_load": metrics.current_load,
                "load_ratio": metrics.load_ratio,
                "recommendation_score": self._calculate_provider_score(
                    task_profile, metrics, SchedulingStrategy.BALANCED
                )
            })
        
        # 按推荐分数排序
        recommendations.sort(key=lambda x: x["recommendation_score"], reverse=True)
        
        return {
            "task_profile": {
                "gpu_type": task_profile.gpu_type,
                "size_category": task_profile.size_category,
                "estimated_duration_minutes": task_profile.estimated_duration_minutes,
                "resource_intensity": task_profile.resource_intensity
            },
            "recommendations": recommendations
        }


# 全局调度器实例
scheduler = IntelligentScheduler()


def get_scheduler() -> IntelligentScheduler:
    """获取调度器实例"""
    return scheduler
