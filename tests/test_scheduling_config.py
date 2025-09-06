import pytest
import json
from datetime import datetime, timezone
from unittest.mock import patch

from app.core.scheduling_config import (
    SchedulingStrategy, ProviderPriority, GPUTypeMapping, SchedulingRule,
    CostOptimizationConfig, PerformanceConfig, AvailabilityConfig,
    SchedulingPolicy, SchedulingConfigManager, get_scheduling_config_manager
)


class TestSchedulingModels:
    """调度模型测试"""
    
    def test_provider_priority_creation(self):
        """测试提供商优先级创建"""
        priority = ProviderPriority(
            provider_name="runpod",
            priority=8,
            enabled=True,
            max_concurrent_tasks=10,
            cost_multiplier=0.9,
            performance_multiplier=1.1
        )
        
        assert priority.provider_name == "runpod"
        assert priority.priority == 8
        assert priority.enabled is True
        assert priority.max_concurrent_tasks == 10
        assert priority.cost_multiplier == 0.9
        assert priority.performance_multiplier == 1.1
    
    def test_gpu_type_mapping_creation(self):
        """测试GPU类型映射创建"""
        mapping = GPUTypeMapping(
            gpu_type="A100",
            provider_mappings={
                "runpod": "NVIDIA A100 PCIe 40GB",
                "tencent": "A100-SXM4-40GB",
                "alibaba": "ecs.gn7-c13g1.3xlarge"
            },
            performance_score=1.0,
            cost_per_hour=2.5
        )
        
        assert mapping.gpu_type == "A100"
        assert mapping.provider_mappings["runpod"] == "NVIDIA A100 PCIe 40GB"
        assert mapping.performance_score == 1.0
        assert mapping.cost_per_hour == 2.5
    
    def test_scheduling_rule_creation(self):
        """测试调度规则创建"""
        rule = SchedulingRule(
            rule_id="high_priority_fast_route",
            name="High Priority Fast Route",
            description="Route high priority tasks to fastest provider",
            enabled=True,
            priority=9,
            conditions={"priority": {"gte": 8}},
            action="route_to_provider",
            action_params={"preferred_provider": "runpod"},
            active_hours=[9, 10, 11, 12, 13, 14, 15, 16, 17],
            active_days=[1, 2, 3, 4, 5]  # 工作日
        )
        
        assert rule.rule_id == "high_priority_fast_route"
        assert rule.name == "High Priority Fast Route"
        assert rule.enabled is True
        assert rule.priority == 9
        assert rule.conditions["priority"]["gte"] == 8
        assert rule.action == "route_to_provider"
        assert rule.active_hours == [9, 10, 11, 12, 13, 14, 15, 16, 17]
    
    def test_cost_optimization_config(self):
        """测试成本优化配置"""
        config = CostOptimizationConfig(
            enable_spot_instances=True,
            max_cost_per_hour=5.0,
            cost_threshold_multiplier=1.2,
            prefer_cheaper_alternatives=True,
            daily_budget=100.0,
            monthly_budget=2500.0,
            budget_alert_threshold=0.8
        )
        
        assert config.enable_spot_instances is True
        assert config.max_cost_per_hour == 5.0
        assert config.daily_budget == 100.0
        assert config.monthly_budget == 2500.0
        assert config.budget_alert_threshold == 0.8
    
    def test_performance_config(self):
        """测试性能配置"""
        config = PerformanceConfig(
            min_performance_score=0.8,
            prefer_dedicated_instances=True,
            enable_gpu_memory_optimization=True,
            min_gpu_memory_gb=32,
            min_cpu_cores=8,
            min_ram_gb=64
        )
        
        assert config.min_performance_score == 0.8
        assert config.prefer_dedicated_instances is True
        assert config.min_gpu_memory_gb == 32
        assert config.min_cpu_cores == 8
        assert config.min_ram_gb == 64
    
    def test_availability_config(self):
        """测试可用性配置"""
        config = AvailabilityConfig(
            min_availability_score=0.9,
            max_queue_wait_minutes=15,
            enable_multi_region=True,
            preferred_regions=["us-east-1", "us-west-2", "eu-west-1"],
            enable_failover=True,
            failover_providers=["tencent", "alibaba"],
            max_retry_attempts=3
        )
        
        assert config.min_availability_score == 0.9
        assert config.max_queue_wait_minutes == 15
        assert config.enable_multi_region is True
        assert config.preferred_regions == ["us-east-1", "us-west-2", "eu-west-1"]
        assert config.enable_failover is True
        assert config.failover_providers == ["tencent", "alibaba"]
    
    def test_scheduling_policy_creation(self):
        """测试调度策略创建"""
        policy = SchedulingPolicy(
            policy_id="test_policy",
            name="Test Policy",
            description="Test scheduling policy",
            strategy=SchedulingStrategy.BALANCED,
            provider_priorities=[
                ProviderPriority(provider_name="runpod", priority=8),
                ProviderPriority(provider_name="tencent", priority=7)
            ],
            cost_weight=0.3,
            performance_weight=0.4,
            availability_weight=0.3
        )
        
        assert policy.policy_id == "test_policy"
        assert policy.name == "Test Policy"
        assert policy.strategy == SchedulingStrategy.BALANCED
        assert len(policy.provider_priorities) == 2
        assert policy.cost_weight == 0.3
        assert policy.performance_weight == 0.4
        assert policy.availability_weight == 0.3
        assert policy.is_active is True
        assert policy.is_default is False


class TestSchedulingConfigManager:
    """调度配置管理器测试"""
    
    @pytest.fixture
    def config_manager(self):
        """创建配置管理器实例"""
        return SchedulingConfigManager()
    
    def test_manager_initialization(self, config_manager):
        """测试管理器初始化"""
        assert config_manager is not None
        assert len(config_manager._policies) == 4  # 4个默认策略
        assert config_manager._default_policy_id is not None
        
        # 检查默认策略
        policies = config_manager.list_policies()
        policy_ids = [p.policy_id for p in policies]
        assert "cost_optimized" in policy_ids
        assert "performance_optimized" in policy_ids
        assert "availability_optimized" in policy_ids
        assert "balanced" in policy_ids
    
    def test_get_policy(self, config_manager):
        """测试获取策略"""
        policy = config_manager.get_policy("cost_optimized")
        assert policy is not None
        assert policy.policy_id == "cost_optimized"
        assert policy.name == "成本优化"
        assert policy.strategy == SchedulingStrategy.COST_OPTIMIZED
        
        # 测试不存在的策略
        nonexistent = config_manager.get_policy("nonexistent")
        assert nonexistent is None
    
    def test_get_default_policy(self, config_manager):
        """测试获取默认策略"""
        default_policy = config_manager.get_default_policy()
        assert default_policy is not None
        assert default_policy.is_default is True
        assert default_policy.policy_id == "cost_optimized"
    
    def test_list_policies(self, config_manager):
        """测试列出所有策略"""
        policies = config_manager.list_policies()
        assert len(policies) == 4
        assert all(isinstance(p, SchedulingPolicy) for p in policies)
        
        policy_names = [p.name for p in policies]
        assert "成本优化" in policy_names
        assert "性能优化" in policy_names
        assert "可用性优化" in policy_names
        assert "均衡策略" in policy_names
    
    def test_add_policy(self, config_manager):
        """测试添加策略"""
        new_policy = SchedulingPolicy(
            policy_id="custom_policy",
            name="Custom Policy",
            description="Custom test policy",
            strategy=SchedulingStrategy.CUSTOM,
            cost_weight=0.5,
            performance_weight=0.3,
            availability_weight=0.2
        )
        
        result = config_manager.add_policy(new_policy)
        assert result is True
        
        # 验证策略已添加
        retrieved = config_manager.get_policy("custom_policy")
        assert retrieved is not None
        assert retrieved.name == "Custom Policy"
        assert len(config_manager.list_policies()) == 5
    
    def test_update_policy(self, config_manager):
        """测试更新策略"""
        # 获取现有策略
        original = config_manager.get_policy("balanced")
        assert original is not None
        
        # 更新策略
        updated_policy = SchedulingPolicy(
            policy_id="balanced",
            name="Updated Balanced Policy",
            description="Updated description",
            strategy=SchedulingStrategy.BALANCED,
            cost_weight=0.4,
            performance_weight=0.3,
            availability_weight=0.3
        )
        
        result = config_manager.update_policy("balanced", updated_policy)
        assert result is True
        
        # 验证更新
        retrieved = config_manager.get_policy("balanced")
        assert retrieved.name == "Updated Balanced Policy"
        assert retrieved.cost_weight == 0.4
        assert retrieved.updated_at is not None
    
    def test_update_nonexistent_policy(self, config_manager):
        """测试更新不存在的策略"""
        policy = SchedulingPolicy(
            policy_id="nonexistent",
            name="Nonexistent",
            strategy=SchedulingStrategy.BALANCED
        )
        
        result = config_manager.update_policy("nonexistent", policy)
        assert result is False
    
    def test_delete_policy(self, config_manager):
        """测试删除策略"""
        # 添加一个测试策略
        test_policy = SchedulingPolicy(
            policy_id="deletable_policy",
            name="Deletable Policy",
            strategy=SchedulingStrategy.CUSTOM
        )
        config_manager.add_policy(test_policy)
        
        # 删除策略
        result = config_manager.delete_policy("deletable_policy")
        assert result is True
        
        # 验证删除
        retrieved = config_manager.get_policy("deletable_policy")
        assert retrieved is None
    
    def test_delete_default_policy_fails(self, config_manager):
        """测试不能删除默认策略"""
        default_id = config_manager._default_policy_id
        result = config_manager.delete_policy(default_id)
        assert result is False
        
        # 默认策略仍然存在
        default_policy = config_manager.get_policy(default_id)
        assert default_policy is not None
    
    def test_set_default_policy(self, config_manager):
        """测试设置默认策略"""
        # 设置新的默认策略
        result = config_manager.set_default_policy("performance_optimized")
        assert result is True
        
        # 验证默认策略已更改
        new_default = config_manager.get_default_policy()
        assert new_default.policy_id == "performance_optimized"
        assert new_default.is_default is True
        
        # 验证之前的默认策略不再是默认
        old_default = config_manager.get_policy("cost_optimized")
        assert old_default.is_default is False
    
    def test_set_nonexistent_default_policy(self, config_manager):
        """测试设置不存在的默认策略"""
        result = config_manager.set_default_policy("nonexistent")
        assert result is False
    
    def test_get_provider_priority(self, config_manager):
        """测试获取提供商优先级"""
        priority = config_manager.get_provider_priority("cost_optimized", "alibaba")
        assert priority is not None
        assert priority.provider_name == "alibaba"
        assert priority.priority == 8
        assert priority.cost_multiplier == 0.8
        
        # 测试不存在的提供商
        nonexistent = config_manager.get_provider_priority("cost_optimized", "nonexistent")
        assert nonexistent is None
        
        # 测试不存在的策略
        none_policy = config_manager.get_provider_priority("nonexistent", "alibaba")
        assert none_policy is None
    
    def test_evaluate_scheduling_rules_basic(self, config_manager):
        """测试基本规则评估"""
        # 添加测试规则到策略
        test_policy = config_manager.get_policy("balanced")
        test_rule = SchedulingRule(
            rule_id="test_rule",
            name="Test Rule",
            enabled=True,
            priority=5,
            conditions={"priority": 8},
            action="route_to_provider",
            action_params={"provider": "runpod"}
        )
        test_policy.scheduling_rules.append(test_rule)
        
        # 评估规则
        context = {"priority": 8, "gpu_type": "A100"}
        applicable_rules = config_manager.evaluate_scheduling_rules("balanced", context)
        
        assert len(applicable_rules) == 1
        assert applicable_rules[0].rule_id == "test_rule"
    
    def test_evaluate_scheduling_rules_time_constraints(self, config_manager):
        """测试带时间约束的规则评估"""
        test_policy = config_manager.get_policy("balanced")
        
        # 创建只在工作时间生效的规则
        work_hours_rule = SchedulingRule(
            rule_id="work_hours_rule",
            name="Work Hours Rule",
            enabled=True,
            priority=7,
            conditions={},
            action="prioritize",
            active_hours=[9, 10, 11, 12, 13, 14, 15, 16, 17],
            active_days=[1, 2, 3, 4, 5]  # 周一到周五
        )
        test_policy.scheduling_rules.append(work_hours_rule)
        
        context = {"priority": 5}
        
        # 模拟工作时间
        with patch('app.core.scheduling_config.datetime') as mock_datetime:
            mock_now = datetime(2024, 1, 15, 10, 0, 0, tzinfo=timezone.utc)  # 周一上午10点
            mock_datetime.now.return_value = mock_now
            mock_datetime.timezone = timezone
            
            applicable_rules = config_manager.evaluate_scheduling_rules("balanced", context)
            # 规则应该适用
            rule_ids = [r.rule_id for r in applicable_rules]
            assert "work_hours_rule" in rule_ids
        
        # 模拟非工作时间
        with patch('app.core.scheduling_config.datetime') as mock_datetime:
            mock_now = datetime(2024, 1, 13, 22, 0, 0, tzinfo=timezone.utc)  # 周六晚上10点
            mock_datetime.now.return_value = mock_now
            mock_datetime.timezone = timezone
            
            applicable_rules = config_manager.evaluate_scheduling_rules("balanced", context)
            # 规则不应该适用
            rule_ids = [r.rule_id for r in applicable_rules]
            assert "work_hours_rule" not in rule_ids
    
    def test_evaluate_conditions_simple(self, config_manager):
        """测试简单条件评估"""
        # 直接比较
        result = config_manager._evaluate_conditions(
            {"priority": 8, "gpu_type": "A100"}, 
            {"priority": 8, "gpu_type": "A100", "extra": "value"}
        )
        assert result is True
        
        # 不匹配
        result = config_manager._evaluate_conditions(
            {"priority": 8}, 
            {"priority": 5}
        )
        assert result is False
    
    def test_evaluate_conditions_operators(self, config_manager):
        """测试条件操作符"""
        context = {"priority": 8, "cost": 2.5, "gpu_types": ["A100", "V100"]}
        
        # 大于
        result = config_manager._evaluate_conditions(
            {"priority": {"gt": 5}}, context
        )
        assert result is True
        
        # 小于等于
        result = config_manager._evaluate_conditions(
            {"cost": {"lte": 2.5}}, context
        )
        assert result is True
        
        # 包含
        result = config_manager._evaluate_conditions(
            {"gpu_types": {"in": ["A100", "V100"]}}, context
        )
        assert result is True
        
        # 不满足的条件
        result = config_manager._evaluate_conditions(
            {"priority": {"lt": 5}}, context
        )
        assert result is False
    
    def test_export_policy(self, config_manager):
        """测试导出策略"""
        policy_json = config_manager.export_policy("cost_optimized")
        assert policy_json is not None
        assert isinstance(policy_json, str)
        
        # 验证可以解析JSON
        policy_data = json.loads(policy_json)
        assert policy_data["policy_id"] == "cost_optimized"
        assert policy_data["name"] == "成本优化"
        
        # 测试不存在的策略
        nonexistent = config_manager.export_policy("nonexistent")
        assert nonexistent is None
    
    def test_import_policy(self, config_manager):
        """测试导入策略"""
        # 先导出一个策略
        exported_json = config_manager.export_policy("cost_optimized")
        
        # 修改策略ID和名称
        policy_data = json.loads(exported_json)
        policy_data["policy_id"] = "imported_policy"
        policy_data["name"] = "Imported Policy"
        modified_json = json.dumps(policy_data)
        
        # 导入策略
        result = config_manager.import_policy(modified_json)
        assert result is True
        
        # 验证导入
        imported_policy = config_manager.get_policy("imported_policy")
        assert imported_policy is not None
        assert imported_policy.name == "Imported Policy"
    
    def test_import_invalid_policy_json(self, config_manager):
        """测试导入无效JSON"""
        result = config_manager.import_policy("invalid json")
        assert result is False
    
    def test_import_invalid_policy_structure(self, config_manager):
        """测试导入无效策略结构"""
        invalid_policy = json.dumps({"invalid": "structure"})
        result = config_manager.import_policy(invalid_policy)
        assert result is False


class TestDefaultPolicies:
    """默认策略测试"""
    
    def test_cost_optimized_policy(self):
        """测试成本优化策略"""
        manager = SchedulingConfigManager()
        policy = manager.get_policy("cost_optimized")
        
        assert policy is not None
        assert policy.strategy == SchedulingStrategy.COST_OPTIMIZED
        assert policy.cost_weight > policy.performance_weight
        assert policy.cost_weight > policy.availability_weight
        assert policy.cost_config.enable_spot_instances is True
        assert policy.cost_config.prefer_cheaper_alternatives is True
        
        # 检查提供商优先级（阿里云成本最低）
        alibaba_priority = None
        for priority in policy.provider_priorities:
            if priority.provider_name == "alibaba":
                alibaba_priority = priority
                break
        
        assert alibaba_priority is not None
        assert alibaba_priority.cost_multiplier == 0.8
    
    def test_performance_optimized_policy(self):
        """测试性能优化策略"""
        manager = SchedulingConfigManager()
        policy = manager.get_policy("performance_optimized")
        
        assert policy is not None
        assert policy.strategy == SchedulingStrategy.PERFORMANCE_OPTIMIZED
        assert policy.performance_weight > policy.cost_weight
        assert policy.performance_weight > policy.availability_weight
        assert policy.performance_config.min_performance_score == 0.8
        assert policy.performance_config.prefer_dedicated_instances is True
        
        # 检查提供商优先级（RunPod性能最好）
        runpod_priority = None
        for priority in policy.provider_priorities:
            if priority.provider_name == "runpod":
                runpod_priority = priority
                break
        
        assert runpod_priority is not None
        assert runpod_priority.priority == 9
        assert runpod_priority.performance_multiplier == 1.2
    
    def test_availability_optimized_policy(self):
        """测试可用性优化策略"""
        manager = SchedulingConfigManager()
        policy = manager.get_policy("availability_optimized")
        
        assert policy is not None
        assert policy.strategy == SchedulingStrategy.AVAILABILITY_OPTIMIZED
        assert policy.availability_weight > policy.cost_weight
        assert policy.availability_weight > policy.performance_weight
        assert policy.availability_config.min_availability_score == 0.9
        assert policy.availability_config.enable_failover is True
        assert policy.availability_config.max_retry_attempts == 3
    
    def test_balanced_policy(self):
        """测试均衡策略"""
        manager = SchedulingConfigManager()
        policy = manager.get_policy("balanced")
        
        assert policy is not None
        assert policy.strategy == SchedulingStrategy.BALANCED
        # 权重应该大致相等
        assert abs(policy.cost_weight - policy.performance_weight) < 0.1
        assert abs(policy.performance_weight - policy.availability_weight) < 0.1
        assert abs(policy.cost_weight - policy.availability_weight) < 0.1


class TestConfigManagerSingleton:
    """配置管理器单例测试"""
    
    def test_get_scheduling_config_manager_singleton(self):
        """测试获取配置管理器单例"""
        manager1 = get_scheduling_config_manager()
        manager2 = get_scheduling_config_manager()
        
        # 应该返回同一个实例
        assert manager1 is manager2
        assert id(manager1) == id(manager2)
    
    def test_global_config_manager_consistency(self):
        """测试全局配置管理器一致性"""
        from app.core.scheduling_config import scheduling_config_manager
        
        manager1 = get_scheduling_config_manager()
        
        # 应该与全局实例相同
        assert manager1 is scheduling_config_manager
        
        # 添加策略到一个实例
        test_policy = SchedulingPolicy(
            policy_id="global_test",
            name="Global Test",
            strategy=SchedulingStrategy.CUSTOM
        )
        manager1.add_policy(test_policy)
        
        # 应该在全局实例中可见
        assert scheduling_config_manager.get_policy("global_test") is not None


class TestComplexScenarios:
    """复杂场景测试"""
    
    def test_policy_inheritance_and_override(self):
        """测试策略继承和覆盖"""
        manager = SchedulingConfigManager()
        
        # 获取基础策略
        base_policy = manager.get_policy("balanced")
        
        # 创建继承策略
        custom_policy = SchedulingPolicy(
            policy_id="custom_balanced",
            name="Custom Balanced",
            description="Custom balanced strategy with tweaks",
            strategy=SchedulingStrategy.BALANCED,
            provider_priorities=base_policy.provider_priorities.copy(),
            cost_weight=0.5,  # 更注重成本
            performance_weight=0.3,
            availability_weight=0.2,
            cost_config=base_policy.cost_config.model_copy(),
            performance_config=base_policy.performance_config.model_copy(),
            availability_config=base_policy.availability_config.model_copy()
        )
        
        # 修改成本配置
        custom_policy.cost_config.enable_spot_instances = True
        custom_policy.cost_config.max_cost_per_hour = 3.0
        
        manager.add_policy(custom_policy)
        
        # 验证继承和修改
        retrieved = manager.get_policy("custom_balanced")
        assert retrieved.cost_weight == 0.5
        assert retrieved.cost_config.enable_spot_instances is True
        assert retrieved.cost_config.max_cost_per_hour == 3.0
        
        # 原始策略不应受影响
        original = manager.get_policy("balanced")
        assert original.cost_weight != 0.5
    
    def test_multi_condition_rule_evaluation(self):
        """测试多条件规则评估"""
        manager = SchedulingConfigManager()
        
        # 创建复杂规则
        complex_rule = SchedulingRule(
            rule_id="complex_routing",
            name="Complex Routing Rule",
            enabled=True,
            priority=10,
            conditions={
                "priority": {"gte": 7},
                "gpu_type": "A100",
                "estimated_duration": {"lt": 120},  # 少于2小时
                "user_tier": {"in": ["premium", "enterprise"]}
            },
            action="fast_track",
            action_params={"provider": "runpod", "queue": "high_priority"}
        )
        
        # 添加规则到策略
        policy = manager.get_policy("performance_optimized")
        policy.scheduling_rules.append(complex_rule)
        
        # 测试匹配的上下文
        matching_context = {
            "priority": 8,
            "gpu_type": "A100",
            "estimated_duration": 60,
            "user_tier": "premium"
        }
        
        rules = manager.evaluate_scheduling_rules("performance_optimized", matching_context)
        assert len(rules) >= 1
        assert any(r.rule_id == "complex_routing" for r in rules)
        
        # 测试不匹配的上下文
        non_matching_context = {
            "priority": 5,  # 优先级太低
            "gpu_type": "A100",
            "estimated_duration": 60,
            "user_tier": "premium"
        }
        
        rules = manager.evaluate_scheduling_rules("performance_optimized", non_matching_context)
        assert not any(r.rule_id == "complex_routing" for r in rules)
    
    def test_policy_validation_and_constraints(self):
        """测试策略验证和约束"""
        manager = SchedulingConfigManager()
        
        # 测试权重约束（权重应该总和为1.0左右）
        policy = SchedulingPolicy(
            policy_id="weight_test",
            name="Weight Test",
            strategy=SchedulingStrategy.CUSTOM,
            cost_weight=0.4,
            performance_weight=0.3,
            availability_weight=0.3
        )
        
        total_weight = policy.cost_weight + policy.performance_weight + policy.availability_weight
        assert abs(total_weight - 1.0) < 0.01  # 允许小的浮点误差
        
        manager.add_policy(policy)
        retrieved = manager.get_policy("weight_test")
        assert retrieved is not None
    
    def test_concurrent_policy_modifications(self):
        """测试并发策略修改"""
        import threading
        import time
        
        manager = SchedulingConfigManager()
        results = []
        errors = []
        
        def modify_policy(thread_id):
            try:
                for i in range(10):
                    policy_id = f"thread_{thread_id}_policy_{i}"
                    policy = SchedulingPolicy(
                        policy_id=policy_id,
                        name=f"Thread {thread_id} Policy {i}",
                        strategy=SchedulingStrategy.CUSTOM,
                        cost_weight=0.33,
                        performance_weight=0.33,
                        availability_weight=0.34
                    )
                    
                    success = manager.add_policy(policy)
                    results.append((thread_id, i, success))
                    time.sleep(0.001)
            except Exception as e:
                errors.append((thread_id, str(e)))
        
        # 启动多个线程
        threads = []
        for tid in range(3):
            thread = threading.Thread(target=modify_policy, args=(tid,))
            threads.append(thread)
            thread.start()
        
        # 等待所有线程完成
        for thread in threads:
            thread.join()
        
        # 验证结果
        assert len(errors) == 0, f"Errors occurred: {errors}"
        assert len(results) == 30  # 3个线程 × 10个策略
        assert all(success for _, _, success in results)
        
        # 验证所有策略都被正确添加
        policies = manager.list_policies()
        thread_policies = [p for p in policies if p.policy_id.startswith("thread_")]
        assert len(thread_policies) == 30
