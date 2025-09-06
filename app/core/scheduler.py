import logging
from datetime import datetime, timezone
from typing import Dict, List, Optional, Tuple, Any
from dataclasses import dataclass
from enum import Enum
import asyncio

logger = logging.getLogger(__name__)


@dataclass
class TaskRequirement:
    """任务需求"""
    gpu_type: str
    gpu_count: int = 1
    memory_gb: int = 0
    vcpus: int = 4
    estimated_duration_minutes: int = 60
    priority: int = 5  # 1-10, 10为最高优先级
    
    def __post_init__(self):
        if self.gpu_count <= 0:
            raise ValueError("GPU count must be greater than 0")
        
        # 确保priority是整数
        if hasattr(self.priority, 'value'):
            # TaskPriority 枚举类型
            from app.models.task import TaskPriority
            priority_map = {
                TaskPriority.LOW: 2,
                TaskPriority.NORMAL: 5,
                TaskPriority.HIGH: 8,
                TaskPriority.URGENT: 10
            }
            self.priority = priority_map.get(self.priority, 5)
        elif isinstance(self.priority, str):
            # 字符串类型
            priority_map = {
                'low': 2,
                'normal': 5, 
                'high': 8,
                'urgent': 10
            }
            self.priority = priority_map.get(self.priority.lower(), 5)
        elif not isinstance(self.priority, int):
            # 其他类型，设为默认值
            self.priority = 5
        
        if not (1 <= self.priority <= 10):
            raise ValueError("Priority must be between 1 and 10")


@dataclass
class ProviderMetrics:
    """Provider指标"""
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


class IntelligentScheduler:
    """智能任务调度器"""
    
    def __init__(self):
        self.provider_metrics = {
            "runpod": ProviderMetrics(
                provider_name="runpod",
                availability_score=0.95,
                avg_cost_per_hour=2.89,
                avg_queue_time_minutes=2.0,
                success_rate=0.98,
                current_load=0.3,
                supported_gpu_types=["A100", "RTX4090", "A6000", "T4"]
            ),
            "tencent": ProviderMetrics(
                provider_name="tencent",
                availability_score=0.92,
                avg_cost_per_hour=3.2,
                avg_queue_time_minutes=3.0,
                success_rate=0.96,
                current_load=0.4,
                supported_gpu_types=["T4", "V100", "A100"]
            ),
            "alibaba": ProviderMetrics(
                provider_name="alibaba",
                availability_score=0.90,
                avg_cost_per_hour=2.95,
                avg_queue_time_minutes=4.0,
                success_rate=0.94,
                current_load=0.5,
                supported_gpu_types=["T4", "V100", "A100"]
            )
        }
    
    def update_provider_metrics(self, provider_name: str, metrics: ProviderMetrics):
        """更新Provider指标"""
        self.provider_metrics[provider_name] = metrics
    
    async def estimate_task_duration(self, task_req: TaskRequirement) -> float:
        """估算任务持续时间"""
        base_duration = task_req.estimated_duration_minutes
        
        # GPU类型因子
        gpu_factors = {
            "A100": 1.0,
            "RTX4090": 1.1,
            "V100": 1.3,
            "T4": 1.5,
            "A6000": 1.1
        }
        
        gpu_factor = gpu_factors.get(task_req.gpu_type, 1.2)
        gpu_count_factor = 1.0 + (task_req.gpu_count - 1) * 0.1
        
        estimated = base_duration * gpu_factor * gpu_count_factor
        return max(estimated, 5)  # 最少5分钟
    
    async def calculate_provider_score(
        self, 
        provider_name: str, 
        task_req: TaskRequirement, 
        strategy: str = "balanced"
    ) -> float:
        """计算Provider评分"""
        if provider_name not in self.provider_metrics:
            return 0.0
        
        metrics = self.provider_metrics[provider_name]
        
        # 检查GPU类型支持
        if not await self._provider_supports_gpu_type(provider_name, task_req.gpu_type):
            return 0.0
        
        # 基础评分
        cost_score = 1.0 / (1.0 + metrics.avg_cost_per_hour / 10.0)
        performance_score = metrics.success_rate * (1.0 - metrics.current_load)
        availability_score = metrics.availability_score
        queue_score = 1.0 / (1.0 + metrics.avg_queue_time_minutes / 10.0)
        
        # 根据策略调整权重
        if strategy == "cost":
            return cost_score * 0.6 + performance_score * 0.2 + availability_score * 0.2
        elif strategy == "performance":
            return performance_score * 0.5 + queue_score * 0.3 + availability_score * 0.2
        elif strategy == "availability":
            return availability_score * 0.5 + performance_score * 0.3 + queue_score * 0.2
        else:  # balanced
            return (cost_score + performance_score + availability_score + queue_score) / 4.0
    
    async def select_optimal_provider(
        self, 
        task_req: TaskRequirement, 
        strategy: str = "balanced",
        preferred_provider: Optional[str] = None
    ) -> Tuple[Optional[str], Optional[str]]:
        """选择最优Provider"""
        
        # 如果指定了首选Provider且支持该GPU类型，优先考虑
        if preferred_provider and preferred_provider in self.provider_metrics:
            if await self._provider_supports_gpu_type(preferred_provider, task_req.gpu_type):
                score = await self.calculate_provider_score(preferred_provider, task_req, strategy)
                if score > 0.3:  # 最低可接受分数
                    routing_key = self._get_routing_key(preferred_provider, task_req.gpu_type, task_req.gpu_count, task_req.priority)
                    return preferred_provider, routing_key
        
        # 计算所有Provider的评分
        provider_scores = {}
        for provider_name in self.provider_metrics:
            score = await self.calculate_provider_score(provider_name, task_req, strategy)
            if score > 0:
                provider_scores[provider_name] = score
        
        if not provider_scores:
            return None, None
        
        # 选择最佳Provider
        best_provider = max(provider_scores.items(), key=lambda x: x[1])[0]
        routing_key = self._get_routing_key(best_provider, task_req.gpu_type, task_req.gpu_count, task_req.priority)
        
        return best_provider, routing_key
    
    async def _provider_supports_gpu_type(self, provider_name: str, gpu_type: str) -> bool:
        """检查Provider是否支持指定GPU类型"""
        if provider_name not in self.provider_metrics:
            return False
        
        metrics = self.provider_metrics[provider_name]
        return gpu_type in metrics.supported_gpu_types
    
    def _get_routing_key(self, provider: str, gpu_type: str, gpu_count: int, priority: int = 5) -> str:
        """生成Celery路由键"""
        # 确保priority是整数
        if isinstance(priority, str):
            try:
                priority = int(priority)
            except (ValueError, TypeError):
                priority = 5
        
        priority_suffix = ""
        if priority >= 9:
            priority_suffix = "_urgent"
        elif priority >= 8 or gpu_count > 4:
            priority_suffix = "_high"
        elif priority <= 2:
            priority_suffix = "_low"
        
        return f"{provider}_{gpu_type}_{gpu_count}{priority_suffix}"
    
    def get_all_provider_metrics(self) -> Dict[str, Dict]:
        """获取所有Provider指标"""
        return {
            name: {
                "provider_name": metrics.provider_name,
                "availability_score": metrics.availability_score,
                "avg_cost_per_hour": metrics.avg_cost_per_hour,
                "avg_queue_time_minutes": metrics.avg_queue_time_minutes,
                "success_rate": metrics.success_rate,
                "current_load": metrics.current_load,
                "supported_gpu_types": metrics.supported_gpu_types
            }
            for name, metrics in self.provider_metrics.items()
        }


# 全局调度器实例
intelligent_scheduler = IntelligentScheduler()


def get_intelligent_scheduler() -> IntelligentScheduler:
    """获取调度器实例"""
    return intelligent_scheduler
