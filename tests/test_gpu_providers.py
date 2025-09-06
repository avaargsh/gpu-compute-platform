"""
Tests for GPU provider interface and adapters.

This module tests the unified GPU provider interface and specific adapters
for Alibaba Cloud and Tencent Cloud.
"""

import pytest
from unittest.mock import Mock, AsyncMock, patch, MagicMock
from datetime import datetime, timezone
import uuid

from app.gpu.interface import (
    JobConfig, GpuSpec, JobResult, JobStatus, CostInfo,
    ProviderError, JobNotFoundError, InsufficientResourcesError
)
from app.gpu.providers.alibaba import AlibabaCloudAdapter
from app.gpu.providers.tencent import TencentCloudAdapter


class TestGpuInterface:
    """Test the basic GPU provider interface structures."""
    
    def test_gpu_spec_creation(self):
        """Test GpuSpec model creation."""
        gpu_spec = GpuSpec(
            gpu_type="A100",
            gpu_count=2,
            memory_gb=80,
            vcpus=24,
            ram_gb=96
        )
        
        assert gpu_spec.gpu_type == "A100"
        assert gpu_spec.gpu_count == 2
        assert gpu_spec.memory_gb == 80
        assert gpu_spec.vcpus == 24
        assert gpu_spec.ram_gb == 96
    
    def test_job_config_creation(self):
        """Test JobConfig model creation."""
        gpu_spec = GpuSpec(
            gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16
        )
        
        job_config = JobConfig(
            name="test-job",
            image="pytorch:latest",
            command=["python", "train.py"],
            gpu_spec=gpu_spec,
            environment={"CUDA_VISIBLE_DEVICES": "0"},
            timeout_minutes=60,
            retry_count=2
        )
        
        assert job_config.name == "test-job"
        assert job_config.image == "pytorch:latest"
        assert job_config.command == ["python", "train.py"]
        assert job_config.gpu_spec.gpu_type == "T4"
        assert job_config.environment["CUDA_VISIBLE_DEVICES"] == "0"
        assert job_config.timeout_minutes == 60
        assert job_config.retry_count == 2
    
    def test_job_result_creation(self):
        """Test JobResult model creation."""
        created_at = datetime.now(timezone.utc)
        
        result = JobResult(
            job_id="test-123",
            status=JobStatus.COMPLETED,
            created_at=created_at,
            started_at=created_at,
            completed_at=created_at,
            exit_code=0
        )
        
        assert result.job_id == "test-123"
        assert result.status == JobStatus.COMPLETED
        assert result.exit_code == 0


class TestAlibabaCloudAdapter:
    """Test Alibaba Cloud ECS adapter."""
    
    @pytest.fixture
    def alibaba_config(self):
        """Configuration for Alibaba Cloud adapter."""
        return {
            "access_key_id": "test_access_key",
            "access_key_secret": "test_secret",
            "region_id": "cn-hangzhou",
            "security_group_id": "sg-test",
            "vswitch_id": "vsw-test"
        }
    
    @pytest.fixture
    def gpu_spec(self):
        """Sample GPU specification."""
        return GpuSpec(
            gpu_type="T4",
            gpu_count=1,
            memory_gb=16,
            vcpus=4,
            ram_gb=16
        )
    
    @pytest.fixture
    def job_config(self, gpu_spec):
        """Sample job configuration."""
        return JobConfig(
            name="test-job",
            image="pytorch:latest",
            command=["python", "train.py"],
            gpu_spec=gpu_spec
        )
    
    def test_adapter_initialization(self, alibaba_config):
        """Test adapter initialization with valid config."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            assert adapter.config == alibaba_config
            assert adapter.PROVIDER_NAME == "alibaba_cloud"
    
    def test_adapter_initialization_missing_config(self):
        """Test adapter initialization with missing config."""
        incomplete_config = {
            "access_key_id": "test_key"
            # Missing required keys
        }
        
        with pytest.raises(ValueError, match="Missing required config key"):
            AlibabaCloudAdapter(incomplete_config)
    
    def test_get_instance_type_valid(self, alibaba_config, gpu_spec):
        """Test mapping GPU spec to instance type."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            instance_type = adapter._get_instance_type(gpu_spec)
            assert instance_type == "ecs.gn6i-c4g1.xlarge"
    
    def test_get_instance_type_invalid(self, alibaba_config):
        """Test mapping invalid GPU spec."""
        invalid_gpu_spec = GpuSpec(
            gpu_type="INVALID_GPU",
            gpu_count=1,
            memory_gb=16,
            vcpus=4,
            ram_gb=16
        )
        
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            with pytest.raises(InsufficientResourcesError):
                adapter._get_instance_type(invalid_gpu_spec)
    
    def test_create_user_data(self, alibaba_config, job_config):
        """Test user data script creation."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            user_data = adapter._create_user_data(job_config)
            
            assert "#!/bin/bash" in user_data
            assert job_config.name in user_data
            assert job_config.image in user_data
            assert "docker run --gpus all" in user_data
    
    @pytest.mark.asyncio
    async def test_submit_job_success(self, alibaba_config, job_config):
        """Test successful job submission."""
        with patch('app.gpu.providers.alibaba.EcsClient') as mock_client_class:
            # Mock the ECS client and response
            mock_client = Mock()
            mock_client_class.return_value = mock_client
            
            # Mock successful response
            mock_response = Mock()
            mock_response.body.instance_id_sets.instance_id_set = ["i-test123"]
            
            # Mock the async call
            with patch('asyncio.to_thread') as mock_to_thread:
                mock_to_thread.return_value = mock_response
                
                adapter = AlibabaCloudAdapter(alibaba_config)
                job_id = await adapter.submit_job(job_config)
                
                assert job_id is not None
                assert len(job_id) > 0
                assert job_id in adapter._job_instances
                assert adapter._job_instances[job_id]['instance_id'] == "i-test123"
    
    @pytest.mark.asyncio
    async def test_get_job_status_not_found(self, alibaba_config):
        """Test getting status for non-existent job."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            
            with pytest.raises(JobNotFoundError):
                await adapter.get_job_status("non-existent-job")
    
    @pytest.mark.asyncio
    async def test_list_available_gpus(self, alibaba_config):
        """Test listing available GPU specifications."""
        with patch('app.gpu.providers.alibaba.EcsClient'):
            adapter = AlibabaCloudAdapter(alibaba_config)
            gpus = await adapter.list_available_gpus()
            
            assert len(gpus) > 0
            assert all(gpu.gpu_type in ["T4", "V100", "A100"] for gpu in gpus)


class TestTencentCloudAdapter:
    """Test Tencent Cloud TKE adapter."""
    
    @pytest.fixture
    def tencent_config(self):
        """Configuration for Tencent Cloud adapter."""
        return {
            "secret_id": "test_secret_id",
            "secret_key": "test_secret_key",
            "region": "ap-shanghai",
            "cluster_id": "cls-test123"
        }
    
    @pytest.fixture
    def gpu_spec(self):
        """Sample GPU specification."""
        return GpuSpec(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=12,
            ram_gb=48
        )
    
    @pytest.fixture
    def job_config(self, gpu_spec):
        """Sample job configuration."""
        return JobConfig(
            name="test-pytorch-job",
            image="pytorch:latest",
            command=["python", "train.py", "--epochs", "10"],
            gpu_spec=gpu_spec,
            environment={"CUDA_VISIBLE_DEVICES": "0"}
        )
    
    def test_get_gpu_resources(self, tencent_config, gpu_spec):
        """Test GPU resource mapping for Kubernetes."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            # Mock kubeconfig content
            tencent_config['kubeconfig'] = 'YXBpVmVyc2lvbjogdjE='  # base64 encoded dummy kubeconfig
            
            adapter = TencentCloudAdapter(tencent_config)
            adapter._ensure_namespace = Mock()  # Skip namespace creation
            
            resources = adapter._get_gpu_resources(gpu_spec)
            assert "nvidia.com/gpu" in resources
            # A100 maps to "1" and we have gpu_count=1, so should be "1"
            assert resources["nvidia.com/gpu"] == "1"
    
    def test_create_k8s_job(self, tencent_config, job_config):
        """Test Kubernetes job specification creation."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            # Mock kubeconfig content
            tencent_config['kubeconfig'] = 'YXBpVmVyc2lvbjogdjE='  # base64 encoded dummy kubeconfig
            
            adapter = TencentCloudAdapter(tencent_config)
            adapter._ensure_namespace = Mock()  # Skip namespace creation
            
            job_id = str(uuid.uuid4())
            # Test that the method runs without errors
            k8s_job = adapter._create_k8s_job(job_id, job_config)
            
            # Since everything is mocked, we just verify the method returns something
            assert k8s_job is not None
    
    @pytest.mark.asyncio
    async def test_submit_job_success(self, tencent_config, job_config):
        """Test successful job submission to Kubernetes."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            # Mock kubeconfig content
            tencent_config['kubeconfig'] = 'YXBpVmVyc2lvbjogdjE='  # base64 encoded dummy kubeconfig
            
            adapter = TencentCloudAdapter(tencent_config)
            adapter._ensure_namespace = Mock()  # Skip namespace creation
            
            # Mock successful Kubernetes job creation
            mock_created_job = Mock()
            mock_created_job.metadata.name = "gpu-test-pytorch-job-12345"
            
            with patch('asyncio.to_thread') as mock_to_thread:
                mock_to_thread.return_value = mock_created_job
                
                job_id = await adapter.submit_job(job_config)
                
                assert job_id is not None
                assert len(job_id) > 0
                assert job_id in adapter._jobs
                assert adapter._jobs[job_id]['k8s_job_name'] == mock_created_job.metadata.name
    
    @pytest.mark.asyncio
    async def test_list_available_gpus(self, tencent_config):
        """Test listing available GPU specifications."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            # Mock kubeconfig content
            tencent_config['kubeconfig'] = 'YXBpVmVyc2lvbjogdjE='  # base64 encoded dummy kubeconfig
            
            adapter = TencentCloudAdapter(tencent_config)
            adapter._ensure_namespace = Mock()  # Skip namespace creation
            
            gpus = await adapter.list_available_gpus()
            
            assert len(gpus) > 0
            assert all(gpu.gpu_type in ["T4", "V100", "A100"] for gpu in gpus)
    
    def test_get_job_status_from_k8s_completed(self, tencent_config):
        """Test job status extraction from Kubernetes job."""
        with patch('app.gpu.providers.tencent.tke_client'), \
             patch('app.gpu.providers.tencent.k8s_config'), \
             patch('app.gpu.providers.tencent.client'):
            
            # Mock kubeconfig content
            tencent_config['kubeconfig'] = 'YXBpVmVyc2lvbjogdjE='  # base64 encoded dummy kubeconfig
            
            adapter = TencentCloudAdapter(tencent_config)
            adapter._ensure_namespace = Mock()  # Skip namespace creation
            
            # Mock successful job
            mock_job = Mock()
            mock_job.status.succeeded = 1
            mock_job.status.failed = None
            mock_job.status.active = None
            mock_job.status.start_time = datetime.now(timezone.utc)
            mock_job.status.completion_time = datetime.now(timezone.utc)
            
            adapter.k8s_batch_api.read_namespaced_job = Mock(return_value=mock_job)
            
            status, info = adapter._get_job_status_from_k8s("test-job")
            
            assert status == JobStatus.COMPLETED
            assert info["exit_code"] == 0


class TestProviderExceptions:
    """Test custom provider exceptions."""
    
    def test_provider_error(self):
        """Test ProviderError exception."""
        error = ProviderError("Test error", "test_provider", "TEST_CODE")
        
        assert str(error) == "Test error"
        assert error.provider == "test_provider"
        assert error.error_code == "TEST_CODE"
    
    def test_job_not_found_error(self):
        """Test JobNotFoundError exception."""
        error = JobNotFoundError("job-123", "test_provider")
        
        assert "Job job-123 not found" in str(error)
        assert error.job_id == "job-123"
        assert error.provider == "test_provider"
        assert error.error_code == "JOB_NOT_FOUND"
    
    def test_insufficient_resources_error(self):
        """Test InsufficientResourcesError exception."""
        gpu_spec = GpuSpec(
            gpu_type="INVALID", gpu_count=999, 
            memory_gb=9999, vcpus=999, ram_gb=9999
        )
        error = InsufficientResourcesError(gpu_spec, "test_provider")
        
        assert "Insufficient resources" in str(error)
        assert "INVALID x999" in str(error)
        assert error.requested_spec == gpu_spec
        assert error.provider == "test_provider"
