#!/usr/bin/env python3
"""
Comprehensive mocked tests for RunPod GPU provider adapter.

This module provides extensive unit tests using pytest-mock to simulate
RunPod API responses without making real API calls.
"""

import asyncio
import json
import pytest
from datetime import datetime, timezone
from unittest.mock import AsyncMock, Mock, patch
from typing import Dict, Any

import httpx

from app.gpu.interface import (
    GpuSpec,
    JobConfig,
    JobStatus,
    JobNotFoundError,
    ProviderError,
    InsufficientResourcesError,
)
from app.gpu.providers.runpod import RunPodAdapter, RunPodPodSpec


@pytest.fixture
def runpod_config():
    """Fixture providing RunPod configuration."""
    return {
        "api_key": "test-api-key-12345",
        "base_url": "https://api.runpod.ai/graphql",
        "timeout": 30,
    }


@pytest.fixture
def sample_job_config():
    """Fixture providing sample job configuration."""
    return JobConfig(
        name="test-pytorch-training",
        image="nvcr.io/nvidia/pytorch:24.02-py3",
        command=["python", "-c", "import torch; print(f'CUDA: {torch.cuda.is_available()}')"],
        gpu_spec=GpuSpec(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=12,
            ram_gb=64
        ),
        environment={
            "NVIDIA_VISIBLE_DEVICES": "all",
            "PYTHONPATH": "/workspace"
        },
        timeout_minutes=30
    )


@pytest.fixture
def mock_pod_data():
    """Fixture providing mock RunPod pod data."""
    return {
        "id": "test-pod-12345",
        "desiredStatus": "RUNNING",
        "imageName": "nvcr.io/nvidia/pytorch:24.02-py3",
        "env": [
            {"name": "NVIDIA_VISIBLE_DEVICES", "value": "all"},
            {"name": "PYTHONPATH", "value": "/workspace"}
        ],
        "machineId": "machine-67890",
        "machine": {
            "podHostId": "host-12345"
        },
        "lastStatusChange": "2024-01-15T10:30:00Z",
        "runtime": {
            "uptimeInSeconds": 1800,  # 30 minutes
            "ports": [
                {
                    "ip": "10.0.0.1",
                    "isIpPublic": False,
                    "privatePort": 8888,
                    "publicPort": 8888,
                    "type": "http"
                }
            ]
        },
        "logs": "Starting PyTorch container...\nCUDA: True\nTraining completed successfully."
    }


@pytest.fixture
def mock_gpu_types():
    """Fixture providing mock GPU types from RunPod."""
    return [
        {
            "id": "nvidia-a100",
            "displayName": "NVIDIA A100",
            "memoryInGb": 40,
            "secureCloud": True,
            "communityCloud": True,
            "lowestPrice": {
                "gpuTypeId": "nvidia-a100",
                "uninterruptablePrice": 2.89,
                "interruptablePrice": 1.45
            }
        },
        {
            "id": "nvidia-rtx4090",
            "displayName": "NVIDIA GeForce RTX 4090",
            "memoryInGb": 24,
            "secureCloud": False,
            "communityCloud": True,
            "lowestPrice": {
                "gpuTypeId": "nvidia-rtx4090",
                "uninterruptablePrice": 0.83,
                "interruptablePrice": 0.42
            }
        },
        {
            "id": "nvidia-t4",
            "displayName": "NVIDIA Tesla T4",
            "memoryInGb": 16,
            "secureCloud": True,
            "communityCloud": True,
            "lowestPrice": {
                "gpuTypeId": "nvidia-t4",
                "uninterruptablePrice": 0.22,
                "interruptablePrice": 0.11
            }
        }
    ]


@pytest.fixture
def mock_user_data():
    """Fixture providing mock user data for health check."""
    return {
        "id": "user-12345",
        "email": "test@example.com"
    }


class TestRunPodPodSpec:
    """Test RunPod pod specification conversion."""
    
    def test_from_gpu_spec_conversion(self):
        """Test conversion from GpuSpec to RunPodPodSpec."""
        gpu_spec = GpuSpec(
            gpu_type="A100",
            gpu_count=2,
            memory_gb=40,
            vcpus=12,
            ram_gb=64
        )
        
        pod_spec = RunPodPodSpec.from_gpu_spec(gpu_spec)
        
        assert pod_spec.gpu_type_id == "NVIDIA A100"
        assert pod_spec.gpu_count == 2
        assert pod_spec.memory_gb == 40
        assert pod_spec.vcpu_count == 12
        assert pod_spec.storage_gb >= 20  # Minimum storage
        
    def test_gpu_type_mapping(self):
        """Test GPU type name mapping."""
        test_cases = [
            ("A100", "NVIDIA A100"),
            ("RTX4090", "NVIDIA GeForce RTX 4090"),
            ("T4", "NVIDIA Tesla T4"),
            ("Unknown", "Unknown"),  # Unmapped type passes through
        ]
        
        for input_type, expected_id in test_cases:
            gpu_spec = GpuSpec(
                gpu_type=input_type,
                gpu_count=1,
                memory_gb=16,
                vcpus=4,
                ram_gb=16
            )
            pod_spec = RunPodPodSpec.from_gpu_spec(gpu_spec)
            assert pod_spec.gpu_type_id == expected_id


class TestRunPodAdapterInit:
    """Test RunPod adapter initialization."""
    
    def test_valid_initialization(self, runpod_config):
        """Test successful adapter initialization."""
        adapter = RunPodAdapter(runpod_config)
        
        assert adapter.api_key == runpod_config["api_key"]
        assert adapter.base_url == runpod_config["base_url"]
        assert adapter.timeout == runpod_config["timeout"]
        assert adapter._jobs == {}
        
    def test_missing_api_key(self):
        """Test initialization fails without API key."""
        config = {"base_url": "https://api.runpod.ai/graphql"}
        
        with pytest.raises(ValueError, match="RunPod API key is required"):
            RunPodAdapter(config)
            
    def test_default_configuration(self):
        """Test default configuration values."""
        config = {"api_key": "test-key"}
        adapter = RunPodAdapter(config)
        
        assert adapter.base_url == "https://api.runpod.ai/graphql"
        assert adapter.timeout == 300


class TestRunPodAdapterMocked:
    """Test RunPod adapter with mocked API responses."""
    
    @pytest.fixture
    def adapter(self, runpod_config):
        """Fixture providing initialized adapter."""
        return RunPodAdapter(runpod_config)
    
    @pytest.mark.asyncio
    async def test_submit_job_success(self, adapter, sample_job_config, mock_pod_data, mocker):
        """Test successful job submission."""
        # Mock successful pod creation response
        mock_response_data = {
            "data": {
                "podFindAndDeployOnDemand": mock_pod_data
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        # Mock the HTTP client
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        job_id = await adapter.submit_job(sample_job_config)
        
        assert job_id == "test-pod-12345"
        assert job_id in adapter._jobs
        assert adapter._jobs[job_id]["status"] == JobStatus.PENDING
        
        # Verify the API call was made correctly
        adapter.client.post.assert_called_once()
        call_args = adapter.client.post.call_args
        assert call_args[0][0] == adapter.base_url
        
        # Verify the payload structure
        payload = call_args[1]["json"]
        assert "query" in payload
        assert "variables" in payload
        assert "podFindAndDeployOnDemand" in payload["query"]
    
    @pytest.mark.asyncio
    async def test_submit_job_api_error(self, adapter, sample_job_config, mocker):
        """Test job submission with API error."""
        # Mock API error response
        mock_response_data = {
            "errors": [
                {"message": "Insufficient GPU capacity"},
                {"message": "Invalid configuration"}
            ]
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        with pytest.raises(ProviderError, match="RunPod API error"):
            await adapter.submit_job(sample_job_config)
    
    @pytest.mark.asyncio
    async def test_submit_job_http_error(self, adapter, sample_job_config, mocker):
        """Test job submission with HTTP error."""
        # Mock HTTP 401 error
        mock_response = Mock()
        mock_response.status_code = 401
        
        http_error = httpx.HTTPStatusError("Unauthorized", request=Mock(), response=mock_response)
        mocker.patch.object(adapter.client, 'post', side_effect=http_error)
        
        with pytest.raises(ProviderError, match="Invalid RunPod API key"):
            await adapter.submit_job(sample_job_config)
    
    @pytest.mark.asyncio
    async def test_get_job_status_success(self, adapter, mock_pod_data, mocker):
        """Test successful job status retrieval."""
        job_id = "test-pod-12345"
        
        # Mock successful status query response
        mock_response_data = {
            "data": {
                "pod": mock_pod_data
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        result = await adapter.get_job_status(job_id)
        
        assert result.job_id == job_id
        assert result.status == JobStatus.RUNNING
        # Note: JobResult interface doesn't include runtime_seconds or metadata anymore
    
    @pytest.mark.asyncio
    async def test_get_job_status_not_found(self, adapter, mocker):
        """Test job status retrieval for non-existent job."""
        job_id = "non-existent-pod"
        
        # Mock response with no pod data
        mock_response_data = {"data": {"pod": None}}
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        with pytest.raises(JobNotFoundError, match="Job .+ not found"):
            await adapter.get_job_status(job_id)
    
    @pytest.mark.asyncio
    async def test_get_job_status_cached_data(self, adapter, sample_job_config, mock_pod_data):
        """Test job status retrieval using cached data."""
        job_id = "test-pod-12345"
        
        # Pre-populate job cache
        adapter._jobs[job_id] = {
            "id": job_id,
            "config": sample_job_config.model_dump(),
            "pod_data": mock_pod_data,
            "created_at": datetime.now(timezone.utc),
            "status": JobStatus.RUNNING,
        }
        
        # Mock empty API response to force cache usage
        mock_response_data = {"data": {"pod": None}}
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        with patch.object(adapter.client, 'post', return_value=mock_response):
            result = await adapter.get_job_status(job_id)
        
        assert result.job_id == job_id
        assert result.status == JobStatus.RUNNING
        # Note: JobResult interface doesn't include metadata anymore
    
    @pytest.mark.asyncio
    async def test_cancel_job_success(self, adapter, mocker):
        """Test successful job cancellation."""
        job_id = "test-pod-12345"
        
        # Mock successful termination response
        mock_response_data = {
            "data": {
                "podTerminate": True
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        result = await adapter.cancel_job(job_id)
        
        assert result is True
    
    @pytest.mark.asyncio
    async def test_cancel_job_failure(self, adapter, mocker):
        """Test failed job cancellation."""
        job_id = "test-pod-12345"
        
        # Mock failed termination response
        mock_response_data = {
            "data": {
                "podTerminate": False
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        result = await adapter.cancel_job(job_id)
        
        assert result is False
    
    @pytest.mark.asyncio
    async def test_get_job_logs_success(self, adapter, mock_pod_data, mocker):
        """Test successful log retrieval."""
        job_id = "test-pod-12345"
        
        # Mock successful logs query response
        mock_response_data = {
            "data": {
                "pod": mock_pod_data
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        logs = await adapter.get_job_logs(job_id)
        
        assert "Starting PyTorch container" in logs
        assert "CUDA: True" in logs
        assert "Training completed successfully" in logs
    
    @pytest.mark.asyncio
    async def test_get_job_logs_truncated(self, adapter, mock_pod_data, mocker):
        """Test log retrieval with line limit."""
        job_id = "test-pod-12345"
        
        # Create multi-line log data
        long_logs = "\n".join([f"Log line {i}" for i in range(20)])
        mock_pod_data_with_logs = {**mock_pod_data, "logs": long_logs}
        
        mock_response_data = {
            "data": {
                "pod": mock_pod_data_with_logs
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        logs = await adapter.get_job_logs(job_id, lines=5)
        
        log_lines = logs.split("\n")
        assert len(log_lines) == 5
        assert "Log line 19" in logs  # Should contain last 5 lines
    
    @pytest.mark.asyncio
    async def test_get_cost_info_success(self, adapter, sample_job_config, mocker):
        """Test successful cost information retrieval."""
        job_id = "test-pod-12345"
        
        # Pre-populate job cache with A100 GPU
        adapter._jobs[job_id] = {
            "id": job_id,
            "config": sample_job_config.model_dump(),
            "pod_spec": {
                "gpu_type_id": "NVIDIA A100",
                "gpu_count": 1
            },
            "created_at": datetime.now(timezone.utc),
            "status": JobStatus.COMPLETED,
        }
        
        # Mock job status response
        mock_job_result = Mock()
        mock_job_result.runtime_seconds = 3600  # 1 hour
        mock_job_result.created_at = datetime.now(timezone.utc)
        mock_job_result.completed_at = datetime.now(timezone.utc)
        
        mocker.patch.object(adapter, 'get_job_status', return_value=mock_job_result)
        
        cost_info = await adapter.get_cost_info(job_id)
        
        assert cost_info.currency == "USD"
        assert cost_info.total_cost > 0
        assert cost_info.cost_breakdown["gpu_count"] == 1.0
        assert cost_info.cost_breakdown["runtime_hours"] == 1.0
        assert cost_info.cost_breakdown["hourly_rate"] == 2.89  # A100 rate
    
    @pytest.mark.asyncio
    async def test_list_available_gpus_success(self, adapter, mock_gpu_types, mocker):
        """Test successful GPU listing."""
        mock_response_data = {
            "data": {
                "gpuTypes": mock_gpu_types
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        gpu_specs = await adapter.list_available_gpus()
        
        assert len(gpu_specs) == 3
        
        # Check A100 mapping
        a100_spec = next(spec for spec in gpu_specs if spec.gpu_type == "A100")
        assert a100_spec.memory_gb == 40
        assert a100_spec.gpu_count == 1
        assert a100_spec.vcpus == 12
        assert a100_spec.ram_gb == 64
        
        # Check RTX4090 mapping
        rtx4090_spec = next(spec for spec in gpu_specs if spec.gpu_type == "RTX4090")
        assert rtx4090_spec.memory_gb == 24
        assert rtx4090_spec.gpu_count == 1
    
    @pytest.mark.asyncio
    async def test_list_available_gpus_fallback(self, adapter, mocker):
        """Test GPU listing with API error fallback."""
        # Mock API error
        mocker.patch.object(adapter.client, 'post', side_effect=Exception("API error"))
        
        gpu_specs = await adapter.list_available_gpus()
        
        # Should return default GPU types
        assert len(gpu_specs) == 4
        gpu_types = [spec.gpu_type for spec in gpu_specs]
        assert "A100" in gpu_types
        assert "RTX4090" in gpu_types
        assert "A6000" in gpu_types
        assert "T4" in gpu_types
    
    @pytest.mark.asyncio
    async def test_health_check_success(self, adapter, mock_user_data, mocker):
        """Test successful health check."""
        mock_response_data = {
            "data": {
                "myself": mock_user_data
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        health = await adapter.health_check()
        
        assert health["status"] == "healthy"
        assert health["message"] == "RunPod API is accessible"
        assert health["user_id"] == "user-12345"
        assert health["user_email"] == "test@example.com"
        assert "response_time_ms" in health
        assert "timestamp" in health
    
    @pytest.mark.asyncio
    async def test_health_check_auth_failure(self, adapter, mocker):
        """Test health check with authentication failure."""
        mock_response_data = {
            "data": {
                "myself": None
            }
        }
        
        mock_response = Mock()
        mock_response.json.return_value = mock_response_data
        mock_response.raise_for_status.return_value = None
        
        mocker.patch.object(adapter.client, 'post', return_value=mock_response)
        
        health = await adapter.health_check()
        
        assert health["status"] == "unhealthy"
        assert "Failed to authenticate" in health["message"]
    
    @pytest.mark.asyncio
    async def test_health_check_api_error(self, adapter, mocker):
        """Test health check with API error."""
        mocker.patch.object(adapter.client, 'post', side_effect=Exception("Connection error"))
        
        health = await adapter.health_check()
        
        assert health["status"] == "unhealthy"
        assert "health check failed" in health["message"]
    
    @pytest.mark.asyncio
    async def test_context_manager(self, runpod_config, mocker):
        """Test async context manager functionality."""
        adapter = RunPodAdapter(runpod_config)
        mock_close = mocker.patch.object(adapter.client, 'aclose')
        
        async with adapter as ctx_adapter:
            assert ctx_adapter is adapter
        
        mock_close.assert_called_once()


class TestRunPodIntegrationScenarios:
    """Test end-to-end scenarios with mocked RunPod API."""
    
    @pytest.mark.asyncio
    async def test_complete_job_lifecycle(self, runpod_config, sample_job_config, mock_pod_data, mocker):
        """Test complete job lifecycle from submission to completion."""
        adapter = RunPodAdapter(runpod_config)
        
        # Mock job submission
        submit_response = {
            "data": {
                "podFindAndDeployOnDemand": {
                    "id": "lifecycle-pod-123",
                    "desiredStatus": "PENDING",
                    "imageName": sample_job_config.image,
                    "machineId": "machine-456"
                }
            }
        }
        
        # Mock status progression: PENDING -> RUNNING -> COMPLETED
        status_responses = [
            {"data": {"pod": {**mock_pod_data, "id": "lifecycle-pod-123", "desiredStatus": "PENDING", "runtime": None}}},
            {"data": {"pod": {**mock_pod_data, "id": "lifecycle-pod-123", "desiredStatus": "RUNNING", "runtime": mock_pod_data["runtime"]}}},
            {"data": {"pod": {**mock_pod_data, "id": "lifecycle-pod-123", "desiredStatus": "EXITED", "runtime": mock_pod_data["runtime"]}}},
        ]
        
        # Mock termination
        terminate_response = {"data": {"podTerminate": True}}
        
        # Setup mock responses in sequence
        mock_responses = [submit_response] + status_responses + [terminate_response]
        response_mocks = []
        
        for response_data in mock_responses:
            mock_response = Mock()
            mock_response.json.return_value = response_data
            mock_response.raise_for_status.return_value = None
            response_mocks.append(mock_response)
        
        mocker.patch.object(adapter.client, 'post', side_effect=response_mocks)
        
        # 1. Submit job
        job_id = await adapter.submit_job(sample_job_config)
        assert job_id == "lifecycle-pod-123"
        
        # 2. Check initial status (PENDING)
        result = await adapter.get_job_status(job_id)
        assert result.status == JobStatus.PENDING
        
        # 3. Check running status
        result = await adapter.get_job_status(job_id)
        assert result.status == JobStatus.RUNNING
        
        # 4. Check completed status
        result = await adapter.get_job_status(job_id)
        assert result.status == JobStatus.COMPLETED
        
        # 5. Cancel job (should succeed even if completed)
        cancelled = await adapter.cancel_job(job_id)
        assert cancelled is True
        
        # Verify all API calls were made
        assert adapter.client.post.call_count == 5
    
    @pytest.mark.asyncio
    async def test_error_handling_consistency(self, runpod_config, sample_job_config, mocker):
        """Test consistent error handling across all methods."""
        adapter = RunPodAdapter(runpod_config)
        
        # Mock HTTP 429 (rate limit) error
        mock_response = Mock()
        mock_response.status_code = 429
        http_error = httpx.HTTPStatusError("Rate limited", request=Mock(), response=mock_response)
        
        mocker.patch.object(adapter.client, 'post', side_effect=http_error)
        
        # All methods should handle rate limiting consistently
        with pytest.raises(ProviderError, match="rate limit"):
            await adapter.submit_job(sample_job_config)
        
        with pytest.raises(ProviderError, match="rate limit"):
            await adapter.get_job_status("test-job")
        
        # These methods should handle errors gracefully without raising
        result = await adapter.cancel_job("test-job")
        assert result is False
        
        logs = await adapter.get_job_logs("test-job")
        assert "Error retrieving logs" in logs
        
        health = await adapter.health_check()
        assert health["status"] == "unhealthy"
