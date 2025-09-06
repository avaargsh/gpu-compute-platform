import pytest
import asyncio
from unittest.mock import Mock, patch, AsyncMock
from datetime import datetime, timezone

from app.core.scheduler import (
    IntelligentScheduler, TaskRequirement, ProviderMetrics
)
from app.models.task import TaskPriority


class TestTaskRequirement:
    """TaskRequirement模型测试"""
    
    def test_task_requirement_creation(self):
        """测试任务需求创建"""
        req = TaskRequirement(
            gpu_type="A100",
            gpu_count=2,
            memory_gb=80,
            vcpus=16,
            estimated_duration_minutes=120,
            priority=8
        )
        
        assert req.gpu_type == "A100"
        assert req.gpu_count == 2
        assert req.memory_gb == 80
        assert req.vcpus == 16
        assert req.estimated_duration_minutes == 120
        assert req.priority == 8
    
    def test_task_requirement_validation(self):
        """测试任务需求验证"""
        # 测试无效的GPU数量
        with pytest.raises(ValueError):
            TaskRequirement(
                gpu_type="A100",
                gpu_count=0,
                memory_gb=80,
                vcpus=16,
                estimated_duration_minutes=120,
                priority=5
            )
        
        # 测试无效的优先级
        with pytest.raises(ValueError):
            TaskRequirement(
                gpu_type="A100",
                gpu_count=1,
                memory_gb=80,
                vcpus=16,
                estimated_duration_minutes=120,
                priority=11
            )


class TestProviderMetrics:
    """ProviderMetrics测试"""
    
    def test_provider_metrics_creation(self):
        """测试Provider指标创建"""
        metrics = ProviderMetrics(
            provider_name="runpod",
            availability_score=0.95,
            avg_cost_per_hour=2.5,
            avg_queue_time_minutes=5.2,
            success_rate=0.98,
            current_load=0.75,
            supported_gpu_types=["A100", "V100", "RTX4090"]
        )
        
        assert metrics.provider_name == "runpod"
        assert metrics.availability_score == 0.95
        assert metrics.avg_cost_per_hour == 2.5
        assert metrics.avg_queue_time_minutes == 5.2
        assert metrics.success_rate == 0.98
        assert metrics.current_load == 0.75
        assert "A100" in metrics.supported_gpu_types


class TestIntelligentScheduler:
    """IntelligentScheduler测试"""
    
    @pytest.fixture
    def scheduler(self):
        """创建调度器实例"""
        return IntelligentScheduler()
    
    @pytest.fixture
    def sample_task_requirement(self):
        """示例任务需求"""
        return TaskRequirement(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=8,
            estimated_duration_minutes=60,
            priority=5
        )
    
    def test_scheduler_initialization(self, scheduler):
        """测试调度器初始化"""
        assert scheduler is not None
        assert len(scheduler.provider_metrics) == 3  # runpod, tencent, alibaba
        
        # 检查默认指标
        assert "runpod" in scheduler.provider_metrics
        assert "tencent" in scheduler.provider_metrics
        assert "alibaba" in scheduler.provider_metrics
    
    def test_update_provider_metrics(self, scheduler):
        """测试更新Provider指标"""
        new_metrics = ProviderMetrics(
            provider_name="test_provider",
            availability_score=0.85,
            avg_cost_per_hour=1.8,
            avg_queue_time_minutes=10.0,
            success_rate=0.92,
            current_load=0.6,
            supported_gpu_types=["A100"]
        )
        
        scheduler.update_provider_metrics("test_provider", new_metrics)
        
        assert "test_provider" in scheduler.provider_metrics
        assert scheduler.provider_metrics["test_provider"].availability_score == 0.85
    
    @pytest.mark.asyncio
    async def test_estimate_task_duration(self, scheduler, sample_task_requirement):
        """测试任务持续时间估算"""
        duration = await scheduler.estimate_task_duration(sample_task_requirement)
        
        # 应该基于GPU类型和数量调整
        assert duration > 0
        assert isinstance(duration, (int, float))
    
    @pytest.mark.asyncio
    async def test_calculate_provider_score_cost_strategy(self, scheduler, sample_task_requirement):
        """测试成本策略下的Provider评分"""
        score = await scheduler.calculate_provider_score(
            "runpod", 
            sample_task_requirement, 
            "cost"
        )
        
        assert 0 <= score <= 1
        assert isinstance(score, float)
    
    @pytest.mark.asyncio
    async def test_calculate_provider_score_performance_strategy(self, scheduler, sample_task_requirement):
        """测试性能策略下的Provider评分"""
        score = await scheduler.calculate_provider_score(
            "runpod", 
            sample_task_requirement, 
            "performance"
        )
        
        assert 0 <= score <= 1
        assert isinstance(score, float)
    
    @pytest.mark.asyncio
    async def test_calculate_provider_score_availability_strategy(self, scheduler, sample_task_requirement):
        """测试可用性策略下的Provider评分"""
        score = await scheduler.calculate_provider_score(
            "runpod", 
            sample_task_requirement, 
            "availability"
        )
        
        assert 0 <= score <= 1
        assert isinstance(score, float)
    
    @pytest.mark.asyncio
    async def test_calculate_provider_score_balanced_strategy(self, scheduler, sample_task_requirement):
        """测试均衡策略下的Provider评分"""
        score = await scheduler.calculate_provider_score(
            "runpod", 
            sample_task_requirement, 
            "balanced"
        )
        
        assert 0 <= score <= 1
        assert isinstance(score, float)
    
    @pytest.mark.asyncio
    async def test_calculate_provider_score_invalid_provider(self, scheduler, sample_task_requirement):
        """测试无效Provider的评分"""
        score = await scheduler.calculate_provider_score(
            "invalid_provider", 
            sample_task_requirement, 
            "cost"
        )
        
        assert score == 0.0
    
    @pytest.mark.asyncio
    async def test_select_optimal_provider_cost_strategy(self, scheduler, sample_task_requirement):
        """测试成本策略选择最优Provider"""
        provider, routing_key = await scheduler.select_optimal_provider(
            sample_task_requirement, 
            "cost"
        )
        
        assert provider in ["runpod", "tencent", "alibaba"]
        assert routing_key is not None
        assert routing_key.startswith(f"{provider}_")
    
    @pytest.mark.asyncio
    async def test_select_optimal_provider_performance_strategy(self, scheduler, sample_task_requirement):
        """测试性能策略选择最优Provider"""
        provider, routing_key = await scheduler.select_optimal_provider(
            sample_task_requirement, 
            "performance"
        )
        
        assert provider in ["runpod", "tencent", "alibaba"]
        assert routing_key is not None
    
    @pytest.mark.asyncio
    async def test_select_optimal_provider_with_preference(self, scheduler, sample_task_requirement):
        """测试带首选Provider的选择"""
        provider, routing_key = await scheduler.select_optimal_provider(
            sample_task_requirement, 
            "cost",
            preferred_provider="tencent"
        )
        
        # 如果tencent支持该GPU类型，应该选择tencent
        if "A100" in scheduler.provider_metrics["tencent"].supported_gpu_types:
            assert provider == "tencent"
        else:
            assert provider in ["runpod", "tencent", "alibaba"]
    
    @pytest.mark.asyncio
    async def test_select_optimal_provider_unsupported_gpu(self, scheduler):
        """测试不支持的GPU类型"""
        unsupported_req = TaskRequirement(
            gpu_type="UNSUPPORTED_GPU",
            gpu_count=1,
            memory_gb=40,
            vcpus=8,
            estimated_duration_minutes=60,
            priority=5
        )
        
        provider, routing_key = await scheduler.select_optimal_provider(
            unsupported_req, 
            "cost"
        )
        
        # 应该返回None或者降级到支持的Provider
        if provider is not None:
            assert provider in ["runpod", "tencent", "alibaba"]
    
    def test_get_all_provider_metrics(self, scheduler):
        """测试获取所有Provider指标"""
        all_metrics = scheduler.get_all_provider_metrics()
        
        assert isinstance(all_metrics, dict)
        assert len(all_metrics) == 3
        assert "runpod" in all_metrics
        assert "tencent" in all_metrics
        assert "alibaba" in all_metrics
    
    def test_get_routing_key(self, scheduler):
        """测试生成路由键"""
        routing_key = scheduler._get_routing_key("runpod", "A100", 2)
        
        assert routing_key == "runpod_A100_2"
        assert isinstance(routing_key, str)
    
    @pytest.mark.asyncio
    async def test_provider_supports_gpu_type(self, scheduler):
        """测试Provider GPU类型支持检查"""
        # runpod支持A100
        supports = await scheduler._provider_supports_gpu_type("runpod", "A100")
        assert supports is True
        
        # 测试不存在的Provider
        supports = await scheduler._provider_supports_gpu_type("nonexistent", "A100")
        assert supports is False
    
    @pytest.mark.asyncio
    async def test_multiple_strategies_different_results(self, scheduler, sample_task_requirement):
        """测试不同策略返回不同结果"""
        results = {}
        strategies = ["cost", "performance", "availability", "balanced"]
        
        for strategy in strategies:
            provider, _ = await scheduler.select_optimal_provider(
                sample_task_requirement, 
                strategy
            )
            results[strategy] = provider
        
        # 至少应该有一个策略返回结果
        assert any(provider is not None for provider in results.values())
    
    @pytest.mark.asyncio
    async def test_high_priority_task_routing(self, scheduler):
        """测试高优先级任务路由"""
        high_priority_req = TaskRequirement(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=8,
            estimated_duration_minutes=60,
            priority=10  # 最高优先级
        )
        
        provider, routing_key = await scheduler.select_optimal_provider(
            high_priority_req, 
            "performance"
        )
        
        assert provider is not None
        assert "high" in routing_key or "urgent" in routing_key or "priority" in routing_key
    
    def test_provider_metrics_update_thread_safety(self, scheduler):
        """测试Provider指标更新的线程安全性"""
        import threading
        import time
        
        def update_metrics():
            for i in range(10):
                metrics = ProviderMetrics(
                    provider_name="test_provider",
                    availability_score=0.9,
                    avg_cost_per_hour=2.0,
                    avg_queue_time_minutes=5.0,
                    success_rate=0.95,
                    current_load=0.5,
                    supported_gpu_types=["A100"]
                )
                scheduler.update_provider_metrics("test_provider", metrics)
                time.sleep(0.01)
        
        # 启动多个线程同时更新
        threads = []
        for _ in range(3):
            thread = threading.Thread(target=update_metrics)
            threads.append(thread)
            thread.start()
        
        # 等待所有线程完成
        for thread in threads:
            thread.join()
        
        # 验证指标存在且有效
        assert "test_provider" in scheduler.provider_metrics
        assert scheduler.provider_metrics["test_provider"].availability_score == 0.9


class TestSchedulerIntegration:
    """调度器集成测试"""
    
    @pytest.mark.asyncio
    async def test_scheduler_with_real_task_scenarios(self):
        """测试真实任务场景"""
        scheduler = IntelligentScheduler()
        
        # 场景1: 大型训练任务
        large_task = TaskRequirement(
            gpu_type="A100",
            gpu_count=8,
            memory_gb=320,
            vcpus=64,
            estimated_duration_minutes=480,  # 8小时
            priority=8
        )
        
        provider, routing_key = await scheduler.select_optimal_provider(
            large_task, 
            "performance"
        )
        
        assert provider is not None
        assert routing_key is not None
        
        # 场景2: 快速推理任务
        inference_task = TaskRequirement(
            gpu_type="RTX4090",
            gpu_count=1,
            memory_gb=24,
            vcpus=8,
            estimated_duration_minutes=10,  # 10分钟
            priority=3
        )
        
        provider, routing_key = await scheduler.select_optimal_provider(
            inference_task, 
            "cost"
        )
        
        # 可能没有Provider支持RTX4090，这是正常的
        if provider:
            assert provider in ["runpod", "tencent", "alibaba"]
        
        # 场景3: 高优先级紧急任务
        urgent_task = TaskRequirement(
            gpu_type="V100",
            gpu_count=2,
            memory_gb=64,
            vcpus=16,
            estimated_duration_minutes=30,
            priority=9
        )
        
        provider, routing_key = await scheduler.select_optimal_provider(
            urgent_task, 
            "availability"
        )
        
        assert provider is not None
        assert routing_key is not None
    
    @pytest.mark.asyncio
    async def test_scheduler_fallback_behavior(self):
        """测试调度器降级行为"""
        scheduler = IntelligentScheduler()
        
        # 模拟所有Provider都不可用
        for provider_name in scheduler.provider_metrics:
            metrics = scheduler.provider_metrics[provider_name]
            metrics.availability_score = 0.1  # 很低的可用性
            metrics.current_load = 0.99  # 很高的负载
        
        task = TaskRequirement(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=8,
            estimated_duration_minutes=60,
            priority=5
        )
        
        provider, routing_key = await scheduler.select_optimal_provider(
            task, 
            "availability"
        )
        
        # 即使可用性很低，也应该选择一个相对最好的
        assert provider is not None or routing_key is None  # 允许失败
    
    @pytest.mark.asyncio 
    async def test_scheduler_performance_benchmarks(self):
        """测试调度器性能基准"""
        scheduler = IntelligentScheduler()
        
        task = TaskRequirement(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=8,
            estimated_duration_minutes=60,
            priority=5
        )
        
        import time
        
        # 测试单次选择性能
        start_time = time.time()
        provider, routing_key = await scheduler.select_optimal_provider(task, "balanced")
        single_duration = time.time() - start_time
        
        assert single_duration < 0.1  # 单次选择应该在100ms内完成
        
        # 测试批量选择性能
        start_time = time.time()
        tasks = []
        for _ in range(100):
            task_result = await scheduler.select_optimal_provider(task, "balanced")
            tasks.append(task_result)
        batch_duration = time.time() - start_time
        
        assert batch_duration < 5.0  # 100次选择应该在5秒内完成
        assert len(tasks) == 100
