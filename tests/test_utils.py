"""
测试工具模块

提供常用的测试fixtures、工具函数和模拟对象
"""
import uuid
from typing import Dict, Any, List
from unittest.mock import Mock, AsyncMock
from datetime import datetime, timezone

from app.gpu.interface import GpuSpec, JobConfig, JobResult, JobStatus


class TestDataFactory:
    """测试数据工厂类"""
    
    @staticmethod
    def create_gpu_spec(
        gpu_type: str = "A100",
        gpu_count: int = 1,
        memory_gb: int = 40,
        vcpus: int = 8,
        ram_gb: int = 32
    ) -> GpuSpec:
        """创建GPU规格测试数据"""
        return GpuSpec(
            gpu_type=gpu_type,
            gpu_count=gpu_count,
            memory_gb=memory_gb,
            vcpus=vcpus,
            ram_gb=ram_gb
        )
    
    @staticmethod
    def create_job_config(
        name: str = None,
        image: str = "pytorch:latest",
        command: List[str] = None,
        gpu_spec: GpuSpec = None,
        environment: Dict[str, str] = None,
        timeout_minutes: int = 60
    ) -> JobConfig:
        """创建作业配置测试数据"""
        if name is None:
            name = f"test-job-{uuid.uuid4().hex[:8]}"
        if command is None:
            command = ["python", "-c", "print('Hello World')"]
        if gpu_spec is None:
            gpu_spec = TestDataFactory.create_gpu_spec()
        if environment is None:
            environment = {"TEST_ENV": "test_value"}
            
        return JobConfig(
            name=name,
            image=image,
            command=command,
            gpu_spec=gpu_spec,
            environment=environment,
            timeout_minutes=timeout_minutes
        )
    
    @staticmethod
    def create_job_result(
        job_id: str = None,
        status: JobStatus = JobStatus.COMPLETED,
        exit_code: int = 0,
        logs: str = "Test job completed successfully"
    ) -> JobResult:
        """创建作业结果测试数据"""
        if job_id is None:
            job_id = f"job-{uuid.uuid4().hex[:8]}"
            
        now = datetime.now(timezone.utc)
        return JobResult(
            job_id=job_id,
            status=status,
            created_at=now,
            started_at=now,
            completed_at=now,
            exit_code=exit_code,
            logs=logs
        )
    
    @staticmethod
    def create_user_data(unique: bool = True) -> Dict[str, Any]:
        """创建用户测试数据"""
        suffix = uuid.uuid4().hex[:8] if unique else "test"
        return {
            "email": f"test+{suffix}@example.com",
            "password": "testpassword123",
            "first_name": "Test",
            "last_name": "User",
            "organization": "Test Organization"
        }
    
    @staticmethod
    def create_admin_data(unique: bool = True) -> Dict[str, Any]:
        """创建管理员测试数据"""
        suffix = uuid.uuid4().hex[:8] if unique else "admin"
        return {
            "email": f"admin+{suffix}@example.com",
            "password": "adminpassword123",
            "first_name": "Admin",
            "last_name": "User",
            "organization": "Admin Organization"
        }


class MockObjects:
    """模拟对象工厂"""
    
    @staticmethod
    def create_mock_websocket():
        """创建模拟WebSocket对象"""
        mock_ws = Mock()
        mock_ws.accepted = False
        mock_ws.closed = False
        mock_ws.sent_messages = []
        mock_ws.received_messages = []
        mock_ws.close_code = None
        mock_ws.close_reason = None
        
        async def mock_accept():
            mock_ws.accepted = True
            
        async def mock_send_text(text: str):
            mock_ws.sent_messages.append(text)
            
        async def mock_receive_text():
            if mock_ws.received_messages:
                return mock_ws.received_messages.pop(0)
            return '{"type": "ping"}'
            
        async def mock_close(code: int = 1000, reason: str = ""):
            mock_ws.closed = True
            mock_ws.close_code = code
            mock_ws.close_reason = reason
            
        mock_ws.accept = mock_accept
        mock_ws.send_text = mock_send_text
        mock_ws.receive_text = mock_receive_text
        mock_ws.close = mock_close
        
        def add_message(message: str):
            mock_ws.received_messages.append(message)
            
        mock_ws.add_message = add_message
        
        return mock_ws
    
    @staticmethod
    def create_mock_gpu_provider():
        """创建模拟GPU提供商"""
        mock_provider = AsyncMock()
        mock_provider.PROVIDER_NAME = "mock_provider"
        mock_provider.is_available.return_value = True
        mock_provider.get_supported_gpu_types.return_value = ["A100", "V100", "T4"]
        
        return mock_provider


def assert_job_config_equal(config1: JobConfig, config2: JobConfig):
    """断言两个JobConfig对象相等"""
    assert config1.name == config2.name
    assert config1.image == config2.image
    assert config1.command == config2.command
    assert config1.gpu_spec.gpu_type == config2.gpu_spec.gpu_type
    assert config1.gpu_spec.gpu_count == config2.gpu_spec.gpu_count
    assert config1.environment == config2.environment


def assert_gpu_spec_equal(spec1: GpuSpec, spec2: GpuSpec):
    """断言两个GpuSpec对象相等"""
    assert spec1.gpu_type == spec2.gpu_type
    assert spec1.gpu_count == spec2.gpu_count
    assert spec1.memory_gb == spec2.memory_gb
    assert spec1.vcpus == spec2.vcpus
    assert spec1.ram_gb == spec2.ram_gb


class ResponseMocker:
    """HTTP响应模拟器"""
    
    @staticmethod
    def success_response(data: Dict[str, Any], status_code: int = 200):
        """创建成功响应"""
        mock_response = Mock()
        mock_response.status_code = status_code
        mock_response.json.return_value = data
        mock_response.text = str(data)
        return mock_response
    
    @staticmethod
    def error_response(message: str, status_code: int = 400):
        """创建错误响应"""
        mock_response = Mock()
        mock_response.status_code = status_code
        mock_response.text = message
        mock_response.json.return_value = {"error": message}
        return mock_response


# 常用的测试常量
TEST_GPU_TYPES = ["A100", "V100", "T4", "RTX4090", "A6000"]
TEST_PROVIDERS = ["runpod", "tencent", "alibaba"]
TEST_STRATEGIES = ["cost", "performance", "availability", "balanced"]
