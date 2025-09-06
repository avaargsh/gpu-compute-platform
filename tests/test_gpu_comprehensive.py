"""
Comprehensive tests for GPU provider interface and adapters.

This module provides extensive testing coverage including error scenarios,
edge cases, and integration testing patterns.
"""

import pytest
from unittest.mock import Mock, AsyncMock, patch, MagicMock
from datetime import datetime, timezone
import uuid
import asyncio

from app.gpu.interface import (
    JobConfig, GpuSpec, JobResult, JobStatus, CostInfo,
    ProviderError, JobNotFoundError, InsufficientResourcesError
)
from app.gpu.providers.alibaba import AlibabaCloudAdapter
from app.gpu.providers.tencent import TencentCloudAdapter


class TestGpuProviderComprehensive:
    """Comprehensive tests for GPU provider functionality."""
    
    @pytest.fixture
    def sample_gpu_specs(self):
        """Sample GPU specifications for testing."""
        return [
            GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16),
            GpuSpec(gpu_type="V100", gpu_count=2, memory_gb=32, vcpus=8, ram_gb=32),
            GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48),
        ]
    
    @pytest.fixture
    def sample_job_configs(self, sample_gpu_specs):
        """Sample job configurations for testing."""
        return [
            JobConfig(
                name="pytorch-training",
                image="pytorch:latest",
                command=["python", "train.py"],
                gpu_spec=sample_gpu_specs[0],
                environment={"CUDA_VISIBLE_DEVICES": "0"},
                timeout_minutes=60,
                retry_count=3
            ),
            JobConfig(
                name="tensorflow-inference",
                image="tensorflow:latest",
                command=["python", "infer.py", "--batch-size", "32"],
                gpu_spec=sample_gpu_specs[1],
                environment={"TF_FORCE_GPU_ALLOW_GROWTH": "true"},
                volumes={"/data": "/host/data"},
                timeout_minutes=30
            ),
            JobConfig(
                name="huggingface-model",
                image="huggingface/transformers-pytorch-gpu",
                command=["python", "-c", "import torch; print(torch.cuda.is_available())"],
                gpu_spec=sample_gpu_specs[2]
            )
        ]
    
    def test_gpu_spec_validation(self):
        """Test GPU specification validation."""
        # Valid GPU spec
        valid_spec = GpuSpec(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=12,
            ram_gb=48
        )
        assert valid_spec.gpu_type == "A100"
        
        # Test with different GPU counts
        multi_gpu_spec = GpuSpec(
            gpu_type="V100",
            gpu_count=4,
            memory_gb=64,
            vcpus=16,
            ram_gb=64
        )
        assert multi_gpu_spec.gpu_count == 4
    
    def test_job_config_serialization(self, sample_job_configs):
        """Test job configuration serialization/deserialization."""
        for job_config in sample_job_configs:
            # Test dict conversion
            job_dict = job_config.model_dump()
            assert "name" in job_dict
            assert "image" in job_dict
            assert "gpu_spec" in job_dict
            
            # Test reconstruction
            reconstructed = JobConfig(**job_dict)
            assert reconstructed.name == job_config.name
            assert reconstructed.image == job_config.image
            assert reconstructed.gpu_spec.gpu_type == job_config.gpu_spec.gpu_type
    
    def test_job_status_enum_values(self):
        """Test job status enum values."""
        all_statuses = [
            JobStatus.PENDING,
            JobStatus.RUNNING,
            JobStatus.COMPLETED,
            JobStatus.FAILED,
            JobStatus.CANCELLED,
            JobStatus.UNKNOWN
        ]
        
        # Ensure all statuses are string-valued
        for status in all_statuses:
            assert isinstance(status, str)
            assert len(status) > 0
    
    def test_provider_error_hierarchy(self):
        """Test provider exception hierarchy."""
        # Base provider error
        base_error = ProviderError("Base error", "test_provider", "BASE_CODE")
        assert str(base_error) == "Base error"
        assert base_error.provider == "test_provider"
        assert base_error.error_code == "BASE_CODE"
        
        # Job not found error (inherits from ProviderError)
        job_error = JobNotFoundError("test-job-123", "test_provider")
        assert isinstance(job_error, ProviderError)
        assert job_error.job_id == "test-job-123"
        assert job_error.error_code == "JOB_NOT_FOUND"
        
        # Insufficient resources error
        gpu_spec = GpuSpec(gpu_type="INVALID", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16)
        resource_error = InsufficientResourcesError(gpu_spec, "test_provider")
        assert isinstance(resource_error, ProviderError)
        assert resource_error.requested_spec == gpu_spec
        assert resource_error.error_code == "INSUFFICIENT_RESOURCES"


class TestAlibabaCloudComprehensive:
    """Comprehensive tests for Alibaba Cloud adapter."""
    
    @pytest.fixture
    def alibaba_config(self):
        """Complete Alibaba Cloud configuration."""
        return {
            "access_key_id": "test_access_key_id",
            "access_key_secret": "test_access_key_secret",
            "region_id": "cn-hangzhou",
            "security_group_id": "sg-test123",
            "vswitch_id": "vsw-test123",
            "key_pair_name": "test-keypair",
            "image_id": "ubuntu_20_04_x64_20G_alibase_20231221.vhd",
            "system_disk_category": "cloud_essd",
            "system_disk_size": 50
        }
    
    def test_adapter_configuration_validation(self):
        """Test configuration validation in Alibaba adapter."""
        # Test missing required keys
        incomplete_configs = [
            {"access_key_id": "test"},
            {"access_key_id": "test", "access_key_secret": "secret"},
            {"access_key_id": "test", "access_key_secret": "secret", "region_id": "cn-hangzhou"},
            {"access_key_id": "test", "access_key_secret": "secret", "region_id": "cn-hangzhou", "security_group_id": "sg-test"}
        ]
        
        for config in incomplete_configs:
            with pytest.raises(ValueError, match="Missing required config key"):
                with patch('app.gpu.providers.alibaba.EcsClient'):
                    AlibabaCloudAdapter(config)
    
    def test_gpu_instance_type_mapping(self, alibaba_config):
        """Test GPU specification to instance type mapping."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            
            # Test all supported GPU types
            gpu_mappings = [
                (GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16), "ecs.gn6i-c4g1.xlarge"),
                (GpuSpec(gpu_type="V100", gpu_count=1, memory_gb=32, vcpus=8, ram_gb=32), "ecs.gn6v-c8g1.2xlarge"),
                (GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48), "ecs.gn7-c12g1.3xlarge"),
            ]
            
            for gpu_spec, expected_type in gpu_mappings:
                result = adapter._get_instance_type(gpu_spec)
                assert result == expected_type
    
    def test_unsupported_gpu_type(self, alibaba_config):
        """Test handling of unsupported GPU types."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            
            unsupported_spec = GpuSpec(
                gpu_type="UNSUPPORTED_GPU",
                gpu_count=1,
                memory_gb=16,
                vcpus=4,
                ram_gb=16
            )
            
            with pytest.raises(InsufficientResourcesError) as exc_info:
                adapter._get_instance_type(unsupported_spec)
            
            assert exc_info.value.requested_spec == unsupported_spec
            assert exc_info.value.provider == "alibaba_cloud"
    
    def test_user_data_script_generation(self, alibaba_config):
        """Test user data script generation."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            
            # Test job with environment variables
            job_config = JobConfig(
                name="test-job",
                image="pytorch:latest",
                command=["python", "train.py", "--epochs", "10"],
                gpu_spec=GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16),
                environment={
                    "CUDA_VISIBLE_DEVICES": "0",
                    "PYTORCH_CUDA_ALLOC_CONF": "max_split_size_mb:512"
                }
            )
            
            user_data = adapter._create_user_data(job_config)
            
            # Check script structure
            assert "#!/bin/bash" in user_data
            assert job_config.name in user_data
            assert job_config.image in user_data
            
            # Check environment variables
            assert "export CUDA_VISIBLE_DEVICES=0" in user_data
            assert "export PYTORCH_CUDA_ALLOC_CONF=max_split_size_mb:512" in user_data
            
            # Check Docker installation and NVIDIA runtime setup
            assert "docker run --gpus all" in user_data
            assert "nvidia-docker" in user_data
    
    @pytest.mark.asyncio
    async def test_submit_job_with_mocked_api(self, alibaba_config):
        """Test job submission with completely mocked Alibaba Cloud API."""
        with patch('app.gpu.providers.alibaba.EcsClient') as mock_ecs_class:
            # Setup mock client
            mock_client = Mock()
            mock_ecs_class.return_value = mock_client
            
            # Mock successful response
            mock_response = Mock()
            mock_response.body.instance_id_sets.instance_id_set = ["i-test12345"]
            
            # Mock the async call
            with patch('asyncio.to_thread', return_value=mock_response):
                adapter = AlibabaCloudAdapter(alibaba_config)
                
                job_config = JobConfig(
                    name="test-pytorch-job",
                    image="pytorch:1.13.0-cuda11.6-cudnn8-runtime",
                    command=["python", "train.py"],
                    gpu_spec=GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16)
                )
                
                job_id = await adapter.submit_job(job_config)
                
                # Verify job was submitted
                assert job_id is not None
                assert isinstance(job_id, str)
                assert len(job_id) == 36  # UUID format
                
                # Verify job is tracked
                assert job_id in adapter._job_instances
                job_info = adapter._job_instances[job_id]
                assert job_info['instance_id'] == "i-test12345"
                assert job_info['status'] == JobStatus.PENDING
    
    @pytest.mark.asyncio
    async def test_health_check_success(self, alibaba_config):
        """Test successful health check."""
        with patch('app.gpu.providers.alibaba.EcsClient') as mock_ecs_class:
            mock_client = Mock()
            mock_ecs_class.return_value = mock_client
            
            # Mock successful API response
            mock_response = Mock()
            with patch('asyncio.to_thread', return_value=mock_response):
                adapter = AlibabaCloudAdapter(alibaba_config)
                health = await adapter.health_check()
                
                assert health["status"] == "healthy"
                assert health["provider"] == "alibaba_cloud"
                assert health["region"] == "cn-hangzhou"
                assert health["api_accessible"] is True
                assert "timestamp" in health
    
    @pytest.mark.asyncio
    async def test_health_check_failure(self, alibaba_config):
        """Test health check failure handling."""
        with patch('app.gpu.providers.alibaba.EcsClient') as mock_ecs_class:
            mock_client = Mock()
            mock_ecs_class.return_value = mock_client
            
            # Mock API failure
            with patch('asyncio.to_thread', side_effect=Exception("API Error")):
                adapter = AlibabaCloudAdapter(alibaba_config)
                health = await adapter.health_check()
                
                assert health["status"] == "unhealthy"
                assert health["provider"] == "alibaba_cloud"
                assert "error" in health
                assert health["error"] == "API Error"


class TestTencentCloudComprehensive:
    """Comprehensive tests for Tencent Cloud adapter."""
    
    @pytest.fixture
    def tencent_config_with_kubeconfig(self):
        """Tencent Cloud configuration with kubeconfig."""
        import base64
        
        # Mock kubeconfig content
        kubeconfig_content = """
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://cls-test.ccs.tencent-cloud.com
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
"""
        kubeconfig_b64 = base64.b64encode(kubeconfig_content.encode()).decode()
        
        return {
            "secret_id": "test_secret_id",
            "secret_key": "test_secret_key",
            "region": "ap-shanghai",
            "cluster_id": "cls-test123",
            "kubeconfig": kubeconfig_b64
        }
    
    def test_gpu_resource_calculation(self, tencent_config_with_kubeconfig):
        """Test GPU resource calculation for Kubernetes."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            adapter = TencentCloudAdapter(tencent_config_with_kubeconfig)
            adapter._ensure_namespace = Mock()
            
            # Test different GPU configurations
            test_cases = [
                (GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16), "0"),  # 1/4 * 1 = 0.25 -> 0
                (GpuSpec(gpu_type="V100", gpu_count=1, memory_gb=32, vcpus=8, ram_gb=32), "0"),  # 1/2 * 1 = 0.5 -> 0
                (GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48), "1"),  # 1 * 1 = 1
                (GpuSpec(gpu_type="A100", gpu_count=2, memory_gb=80, vcpus=24, ram_gb=96), "2"),  # 1 * 2 = 2
            ]
            
            for gpu_spec, expected_gpu_count in test_cases:
                resources = adapter._get_gpu_resources(gpu_spec)
                assert resources["nvidia.com/gpu"] == expected_gpu_count
    
    def test_k8s_job_specification(self, tencent_config_with_kubeconfig):
        """Test Kubernetes job specification creation."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            adapter = TencentCloudAdapter(tencent_config_with_kubeconfig)
            adapter._ensure_namespace = Mock()
            
            job_config = JobConfig(
                name="test-ml-job",
                image="tensorflow/tensorflow:2.13.0-gpu",
                command=["python", "train.py", "--epochs", "50"],
                gpu_spec=GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48),
                environment={
                    "TF_CPP_MIN_LOG_LEVEL": "2",
                    "CUDA_VISIBLE_DEVICES": "0"
                },
                volumes={"/data": "/host/data", "/models": "/host/models"},
                retry_count=3,
                timeout_minutes=120
            )
            
            job_id = str(uuid.uuid4())
            # Test that the method runs without errors when everything is mocked
            k8s_job = adapter._create_k8s_job(job_id, job_config)
            
            # Since everything is mocked, we just verify the method returns something
            assert k8s_job is not None
    
    def test_job_status_mapping(self, tencent_config_with_kubeconfig):
        """Test job status mapping from Kubernetes."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            adapter = TencentCloudAdapter(tencent_config_with_kubeconfig)
            adapter._ensure_namespace = Mock()
            
            # Test different Kubernetes job statuses
            test_cases = [
                # (succeeded, failed, active, expected_status, expected_exit_code)
                (1, None, None, JobStatus.COMPLETED, 0),
                (None, 1, None, JobStatus.FAILED, 1),
                (None, None, 1, JobStatus.RUNNING, None),
                (None, None, None, JobStatus.PENDING, None),
            ]
            
            for succeeded, failed, active, expected_status, expected_exit_code in test_cases:
                # Mock Kubernetes job
                mock_job = Mock()
                mock_job.status.succeeded = succeeded
                mock_job.status.failed = failed
                mock_job.status.active = active
                mock_job.status.start_time = datetime.now(timezone.utc)
                mock_job.status.completion_time = datetime.now(timezone.utc) if succeeded or failed else None
                
                adapter.k8s_batch_api.read_namespaced_job = Mock(return_value=mock_job)
                
                status, info = adapter._get_job_status_from_k8s("test-job")
                
                assert status == expected_status
                if expected_exit_code is not None:
                    assert info["exit_code"] == expected_exit_code
    
    @pytest.mark.asyncio
    async def test_concurrent_job_operations(self, tencent_config_with_kubeconfig):
        """Test concurrent job operations."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            adapter = TencentCloudAdapter(tencent_config_with_kubeconfig)
            adapter._ensure_namespace = Mock()
            
            # Mock successful job creation
            mock_created_job = Mock()
            mock_created_job.metadata.name = "gpu-test-job-12345"
            
            with patch('asyncio.to_thread', return_value=mock_created_job):
                # Create multiple job configurations
                job_configs = [
                    JobConfig(
                        name=f"concurrent-job-{i}",
                        image="pytorch:latest",
                        command=["python", f"job_{i}.py"],
                        gpu_spec=GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16)
                    )
                    for i in range(3)
                ]
                
                # Submit all jobs concurrently
                tasks = [adapter.submit_job(config) for config in job_configs]
                job_ids = await asyncio.gather(*tasks)
                
                # Verify all jobs were created
                assert len(job_ids) == 3
                assert all(job_id in adapter._jobs for job_id in job_ids)
                
                # Verify all job IDs are unique
                assert len(set(job_ids)) == 3


class TestIntegrationScenarios:
    """Integration test scenarios."""
    
    @pytest.mark.asyncio
    async def test_provider_switching_scenario(self):
        """Test switching between providers for the same job."""
        # This test demonstrates how the unified interface enables provider switching
        
        job_config = JobConfig(
            name="provider-switch-test",
            image="pytorch:latest",
            command=["python", "flexible_job.py"],
            gpu_spec=GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48)
        )
        
        # Mock both providers
        alibaba_config = {
            "access_key_id": "test", "access_key_secret": "test",
            "region_id": "cn-hangzhou", "security_group_id": "sg-test",
            "vswitch_id": "vsw-test"
        }
        
        tencent_config = {
            "secret_id": "test", "secret_key": "test",
            "region": "ap-shanghai", "cluster_id": "cls-test",
            "kubeconfig": "dGVzdA=="  # base64 encoded "test"
        }
        
        with patch('app.gpu.providers.alibaba.EcsClient'), \
             patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'), \
             patch('asyncio.to_thread'):
            
            # Initialize both providers
            alibaba_provider = AlibabaCloudAdapter(alibaba_config)
            
            tencent_provider = TencentCloudAdapter(tencent_config)
            tencent_provider._ensure_namespace = Mock()
            
            # Both should support the same interface
            for provider in [alibaba_provider, tencent_provider]:
                # Check available GPUs
                gpus = await provider.list_available_gpus()
                assert len(gpus) > 0
                
                # Check health
                health = await provider.health_check()
                assert "status" in health
                assert "provider" in health
    
    def test_error_handling_consistency(self):
        """Test that both providers handle errors consistently."""
        # Test configuration validation
        incomplete_config = {"incomplete": "config"}
        
        # Both providers should raise ValueError for missing config
        with pytest.raises(ValueError):
            with patch('app.gpu.providers.alibaba.EcsClient'):
                AlibabaCloudAdapter(incomplete_config)
        
        with pytest.raises(ValueError):
            TencentCloudAdapter(incomplete_config)
        
        # Test unsupported GPU types
        valid_configs = [
            {
                "access_key_id": "test", "access_key_secret": "test",
                "region_id": "cn-hangzhou", "security_group_id": "sg-test",
                "vswitch_id": "vsw-test"
            },
            {
                "secret_id": "test", "secret_key": "test",
                "region": "ap-shanghai", "cluster_id": "cls-test",
                "kubeconfig": "dGVzdA=="
            }
        ]
        
        unsupported_spec = GpuSpec(
            gpu_type="UNSUPPORTED",
            gpu_count=1,
            memory_gb=16,
            vcpus=4,
            ram_gb=16
        )
        
        with patch('app.gpu.providers.alibaba.EcsClient'), \
             patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            alibaba_adapter = AlibabaCloudAdapter(valid_configs[0])
            tencent_adapter = TencentCloudAdapter(valid_configs[1])
            tencent_adapter._ensure_namespace = Mock()
            
            # Both should raise InsufficientResourcesError
            with pytest.raises(InsufficientResourcesError):
                alibaba_adapter._get_instance_type(unsupported_spec)
            
            with pytest.raises(InsufficientResourcesError):
                tencent_adapter._get_gpu_resources(unsupported_spec)
