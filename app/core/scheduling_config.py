from typing import Dict, List, Optional, Any
from enum import Enum
from pydantic import BaseModel, Field
from datetime import datetime, timezone
import json


class SchedulingStrategy(str, Enum):
    """调度策略枚举"""
    COST_OPTIMIZED = "cost"
    PERFORMANCE_OPTIMIZED = "performance"
    AVAILABILITY_OPTIMIZED = "availability"
    BALANCED = "balanced"
    CUSTOM = "custom"


class ProviderPriority(BaseModel):
    """提供商优先级配置"""
    provider_name: str = Field(..., description="提供商名称")
    priority: int = Field(1, description="优先级 (1-10, 10最高)")
    enabled: bool = Field(True, description="是否启用")
    max_concurrent_tasks: Optional[int] = Field(None, description="最大并发任务数")
    cost_multiplier: float = Field(1.0, description="成本乘数")
    performance_multiplier: float = Field(1.0, description="性能乘数")


class GPUTypeMapping(BaseModel):
    """GPU类型映射配置"""
    gpu_type: str = Field(..., description="标准GPU类型名称")
    provider_mappings: Dict[str, str] = Field(..., description="提供商特定的GPU类型映射")
    performance_score: float = Field(1.0, description="性能评分")
    cost_per_hour: Optional[float] = Field(None, description="每小时成本(USD)")


class SchedulingRule(BaseModel):
    """调度规则"""
    rule_id: str = Field(..., description="规则ID")
    name: str = Field(..., description="规则名称")
    description: Optional[str] = Field(None, description="规则描述")
    enabled: bool = Field(True, description="是否启用")
    priority: int = Field(1, description="规则优先级")
    
    # 条件
    conditions: Dict[str, Any] = Field(default_factory=dict, description="触发条件")
    
    # 动作
    action: str = Field(..., description="调度动作")
    action_params: Dict[str, Any] = Field(default_factory=dict, description="动作参数")
    
    # 时间限制
    active_hours: Optional[List[int]] = Field(None, description="生效时间(小时，24小时制)")
    active_days: Optional[List[int]] = Field(None, description="生效日期(1-7，1=周一)")


class CostOptimizationConfig(BaseModel):
    """成本优化配置"""
    enable_spot_instances: bool = Field(True, description="启用Spot实例")
    max_cost_per_hour: Optional[float] = Field(None, description="每小时最大成本")
    cost_threshold_multiplier: float = Field(1.2, description="成本阈值乘数")
    prefer_cheaper_alternatives: bool = Field(True, description="优先选择更便宜的替代方案")
    
    # 成本预算控制
    daily_budget: Optional[float] = Field(None, description="每日预算")
    monthly_budget: Optional[float] = Field(None, description="每月预算")
    budget_alert_threshold: float = Field(0.8, description="预算警告阈值")


class PerformanceConfig(BaseModel):
    """性能优化配置"""
    min_performance_score: float = Field(0.0, description="最小性能评分")
    prefer_dedicated_instances: bool = Field(True, description="优先选择专用实例")
    enable_gpu_memory_optimization: bool = Field(True, description="启用GPU内存优化")
    
    # 性能阈值
    min_gpu_memory_gb: Optional[int] = Field(None, description="最小GPU内存")
    min_cpu_cores: Optional[int] = Field(None, description="最小CPU核心数")
    min_ram_gb: Optional[int] = Field(None, description="最小内存")


class AvailabilityConfig(BaseModel):
    """可用性配置"""
    min_availability_score: float = Field(0.7, description="最小可用性评分")
    max_queue_wait_minutes: int = Field(30, description="最大队列等待时间(分钟)")
    enable_multi_region: bool = Field(True, description="启用多区域")
    preferred_regions: List[str] = Field(default_factory=list, description="首选区域")
    
    # 故障转移配置
    enable_failover: bool = Field(True, description="启用故障转移")
    failover_providers: List[str] = Field(default_factory=list, description="故障转移提供商")
    max_retry_attempts: int = Field(3, description="最大重试次数")


class SchedulingPolicy(BaseModel):
    """调度策略配置"""
    policy_id: str = Field(..., description="策略ID")
    name: str = Field(..., description="策略名称")
    description: Optional[str] = Field(None, description="策略描述")
    strategy: SchedulingStrategy = Field(..., description="调度策略")
    
    # 提供商配置
    provider_priorities: List[ProviderPriority] = Field(default_factory=list, description="提供商优先级")
    
    # GPU类型映射
    gpu_type_mappings: List[GPUTypeMapping] = Field(default_factory=list, description="GPU类型映射")
    
    # 调度规则
    scheduling_rules: List[SchedulingRule] = Field(default_factory=list, description="调度规则")
    
    # 优化配置
    cost_config: CostOptimizationConfig = Field(default_factory=CostOptimizationConfig, description="成本优化配置")
    performance_config: PerformanceConfig = Field(default_factory=PerformanceConfig, description="性能优化配置")
    availability_config: AvailabilityConfig = Field(default_factory=AvailabilityConfig, description="可用性配置")
    
    # 权重配置
    cost_weight: float = Field(0.3, description="成本权重")
    performance_weight: float = Field(0.4, description="性能权重")
    availability_weight: float = Field(0.3, description="可用性权重")
    
    # 元数据
    created_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    updated_at: Optional[datetime] = Field(None)
    created_by: Optional[str] = Field(None, description="创建者")
    is_active: bool = Field(True, description="是否激活")
    is_default: bool = Field(False, description="是否为默认策略")


class SchedulingConfigManager:
    """调度配置管理器"""
    
    def __init__(self):
        self._policies: Dict[str, SchedulingPolicy] = {}
        self._default_policy_id: Optional[str] = None
        self._load_default_policies()
    
    def _load_default_policies(self):
        """加载默认策略"""
        
        # 成本优化策略
        cost_policy = SchedulingPolicy(
            policy_id="cost_optimized",
            name="成本优化",
            description="优先选择成本最低的提供商",
            strategy=SchedulingStrategy.COST_OPTIMIZED,
            provider_priorities=[
                ProviderPriority(provider_name="alibaba", priority=8, cost_multiplier=0.8),
                ProviderPriority(provider_name="tencent", priority=7, cost_multiplier=0.9),
                ProviderPriority(provider_name="runpod", priority=6, cost_multiplier=1.0),
            ],
            cost_config=CostOptimizationConfig(
                enable_spot_instances=True,
                prefer_cheaper_alternatives=True,
                cost_threshold_multiplier=1.1
            ),
            cost_weight=0.6,
            performance_weight=0.2,
            availability_weight=0.2,
            is_default=True
        )
        
        # 性能优化策略
        performance_policy = SchedulingPolicy(
            policy_id="performance_optimized",
            name="性能优化",
            description="优先选择性能最佳的提供商",
            strategy=SchedulingStrategy.PERFORMANCE_OPTIMIZED,
            provider_priorities=[
                ProviderPriority(provider_name="runpod", priority=9, performance_multiplier=1.2),
                ProviderPriority(provider_name="tencent", priority=7, performance_multiplier=1.1),
                ProviderPriority(provider_name="alibaba", priority=6, performance_multiplier=1.0),
            ],
            performance_config=PerformanceConfig(
                min_performance_score=0.8,
                prefer_dedicated_instances=True,
                enable_gpu_memory_optimization=True
            ),
            cost_weight=0.2,
            performance_weight=0.6,
            availability_weight=0.2
        )
        
        # 可用性优化策略
        availability_policy = SchedulingPolicy(
            policy_id="availability_optimized",
            name="可用性优化",
            description="优先选择可用性最高的提供商",
            strategy=SchedulingStrategy.AVAILABILITY_OPTIMIZED,
            provider_priorities=[
                ProviderPriority(provider_name="alibaba", priority=8),
                ProviderPriority(provider_name="tencent", priority=8),
                ProviderPriority(provider_name="runpod", priority=7),
            ],
            availability_config=AvailabilityConfig(
                min_availability_score=0.9,
                max_queue_wait_minutes=15,
                enable_multi_region=True,
                enable_failover=True,
                max_retry_attempts=3
            ),
            cost_weight=0.2,
            performance_weight=0.2,
            availability_weight=0.6
        )
        
        # 均衡策略
        balanced_policy = SchedulingPolicy(
            policy_id="balanced",
            name="均衡策略",
            description="在成本、性能和可用性之间保持平衡",
            strategy=SchedulingStrategy.BALANCED,
            provider_priorities=[
                ProviderPriority(provider_name="tencent", priority=8),
                ProviderPriority(provider_name="alibaba", priority=7),
                ProviderPriority(provider_name="runpod", priority=7),
            ],
            cost_weight=0.33,
            performance_weight=0.34,
            availability_weight=0.33
        )
        
        # 添加到策略字典
        for policy in [cost_policy, performance_policy, availability_policy, balanced_policy]:
            self._policies[policy.policy_id] = policy
            if policy.is_default:
                self._default_policy_id = policy.policy_id
    
    def get_policy(self, policy_id: str) -> Optional[SchedulingPolicy]:
        """获取调度策略"""
        return self._policies.get(policy_id)
    
    def get_default_policy(self) -> SchedulingPolicy:
        """获取默认策略"""
        if self._default_policy_id:
            return self._policies[self._default_policy_id]
        return list(self._policies.values())[0]  # 返回第一个策略
    
    def list_policies(self) -> List[SchedulingPolicy]:
        """列出所有策略"""
        return list(self._policies.values())
    
    def add_policy(self, policy: SchedulingPolicy) -> bool:
        """添加策略"""
        self._policies[policy.policy_id] = policy
        return True
    
    def update_policy(self, policy_id: str, policy: SchedulingPolicy) -> bool:
        """更新策略"""
        if policy_id in self._policies:
            policy.updated_at = datetime.now(timezone.utc)
            self._policies[policy_id] = policy
            return True
        return False
    
    def delete_policy(self, policy_id: str) -> bool:
        """删除策略"""
        if policy_id in self._policies and policy_id != self._default_policy_id:
            del self._policies[policy_id]
            return True
        return False
    
    def set_default_policy(self, policy_id: str) -> bool:
        """设置默认策略"""
        if policy_id in self._policies:
            # 清除之前的默认策略标记
            if self._default_policy_id:
                self._policies[self._default_policy_id].is_default = False
            
            # 设置新的默认策略
            self._policies[policy_id].is_default = True
            self._default_policy_id = policy_id
            return True
        return False
    
    def get_provider_priority(self, policy_id: str, provider_name: str) -> Optional[ProviderPriority]:
        """获取提供商在特定策略下的优先级"""
        policy = self.get_policy(policy_id)
        if not policy:
            return None
        
        for priority in policy.provider_priorities:
            if priority.provider_name == provider_name:
                return priority
        return None
    
    def evaluate_scheduling_rules(self, policy_id: str, context: Dict[str, Any]) -> List[SchedulingRule]:
        """评估调度规则"""
        policy = self.get_policy(policy_id)
        if not policy:
            return []
        
        applicable_rules = []
        current_time = datetime.now(timezone.utc)
        
        for rule in policy.scheduling_rules:
            if not rule.enabled:
                continue
            
            # 检查时间限制
            if rule.active_hours:
                if current_time.hour not in rule.active_hours:
                    continue
            
            if rule.active_days:
                weekday = current_time.isoweekday()  # 1=周一, 7=周日
                if weekday not in rule.active_days:
                    continue
            
            # 检查条件
            if self._evaluate_conditions(rule.conditions, context):
                applicable_rules.append(rule)
        
        # 按优先级排序
        applicable_rules.sort(key=lambda r: r.priority, reverse=True)
        return applicable_rules
    
    def _evaluate_conditions(self, conditions: Dict[str, Any], context: Dict[str, Any]) -> bool:
        """评估规则条件"""
        if not conditions:
            return True
        
        # 简单的条件评估实现
        # 在生产环境中可以使用更复杂的规则引擎
        for key, expected_value in conditions.items():
            context_value = context.get(key)
            
            if isinstance(expected_value, dict):
                # 支持比较操作符
                for op, value in expected_value.items():
                    if op == "eq" and context_value != value:
                        return False
                    elif op == "gt" and (context_value is None or context_value <= value):
                        return False
                    elif op == "lt" and (context_value is None or context_value >= value):
                        return False
                    elif op == "gte" and (context_value is None or context_value < value):
                        return False
                    elif op == "lte" and (context_value is None or context_value > value):
                        return False
                    elif op == "in":
                        if isinstance(context_value, (list, set, tuple)):
                            # 至少有一个元素在期望集合中
                            if not any(elem in value for elem in context_value):
                                return False
                        else:
                            if context_value not in value:
                                return False
            else:
                # 直接比较
                if context_value != expected_value:
                    return False
        
        return True
    
    def export_policy(self, policy_id: str) -> Optional[str]:
        """导出策略为JSON"""
        policy = self.get_policy(policy_id)
        if not policy:
            return None
        return policy.model_dump_json(indent=2)
    
    def import_policy(self, policy_json: str) -> bool:
        """从JSON导入策略"""
        try:
            policy_data = json.loads(policy_json)
            policy = SchedulingPolicy(**policy_data)
            return self.add_policy(policy)
        except Exception:
            return False


# 全局配置管理器实例
scheduling_config_manager = SchedulingConfigManager()


def get_scheduling_config_manager() -> SchedulingConfigManager:
    """获取调度配置管理器实例"""
    return scheduling_config_manager
