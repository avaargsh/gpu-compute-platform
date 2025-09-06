#!/usr/bin/env python3
"""
RunPod GPU provider adapter.

This module implements the GpuProviderInterface for RunPod,
providing a unified interface for GPU compute resources via RunPod's serverless platform.

RunPod API Documentation: https://docs.runpod.io/reference/overview
"""

import asyncio
import json
import logging
import time
from datetime import datetime, timezone
from typing import Dict, List, Optional, Any

import httpx
from pydantic import BaseModel, Field

from ..interface import (
    GpuProviderInterface,
    GpuSpec,
    JobConfig,
    JobResult,
    JobStatus,
    CostInfo,
    ProviderError,
    JobNotFoundError,
    InsufficientResourcesError,
)

logger = logging.getLogger(__name__)


class RunPodPodSpec(BaseModel):
    """RunPod-specific pod specification."""
    
    gpu_type_id: str = Field(description="RunPod GPU type ID")
    gpu_count: int = Field(ge=1, description="Number of GPUs")
    memory_gb: int = Field(ge=1, description="Memory in GB")
    vcpu_count: int = Field(ge=1, description="Number of vCPUs")
    storage_gb: int = Field(ge=10, description="Storage in GB")
    
    @classmethod
    def from_gpu_spec(cls, gpu_spec: GpuSpec) -> "RunPodPodSpec":
        """Convert GpuSpec to RunPod-specific specification."""
        # GPU type mapping
        gpu_type_mapping = {
            "A100": "NVIDIA A100",
            "A6000": "NVIDIA RTX A6000",
            "A5000": "NVIDIA RTX A5000", 
            "A4000": "NVIDIA RTX A4000",
            "RTX4090": "NVIDIA GeForce RTX 4090",
            "RTX3090": "NVIDIA GeForce RTX 3090",
            "H100": "NVIDIA H100",
            "V100": "NVIDIA Tesla V100",
            "T4": "NVIDIA Tesla T4",
        }
        
        gpu_type_id = gpu_type_mapping.get(gpu_spec.gpu_type, gpu_spec.gpu_type)
        
        return cls(
            gpu_type_id=gpu_type_id,
            gpu_count=gpu_spec.gpu_count,
            memory_gb=gpu_spec.memory_gb,
            vcpu_count=gpu_spec.vcpus,
            storage_gb=max(gpu_spec.memory_gb, 20)  # At least 20GB storage
        )


class RunPodAdapter(GpuProviderInterface):
    """RunPod GPU provider adapter."""
    
    def __init__(self, config: Dict[str, Any]):
        """
        Initialize RunPod adapter.
        
        Args:
            config: Configuration dictionary containing:
                - api_key: RunPod API key
                - endpoint_id: (Optional) Serverless endpoint ID for existing endpoints
                - base_url: (Optional) RunPod API base URL
                - timeout: (Optional) Request timeout in seconds
        """
        self.api_key = config.get("api_key")
        if not self.api_key:
            raise ValueError("RunPod API key is required")
            
        self.endpoint_id = config.get("endpoint_id")
        self.base_url = config.get("base_url", "https://api.runpod.ai/graphql")
        self.timeout = config.get("timeout", 300)
        
        # Initialize HTTP client
        self.client = httpx.AsyncClient(
            headers={
                "Authorization": f"Bearer {self.api_key}",
                "Content-Type": "application/json"
            },
            timeout=self.timeout
        )
        
        # Job tracking
        self._jobs: Dict[str, Dict] = {}
        
    async def __aenter__(self):
        """Async context manager entry."""
        return self
        
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        """Async context manager exit."""
        await self.client.aclose()

    def _create_graphql_query(self, query: str, variables: Optional[Dict] = None) -> Dict:
        """Create GraphQL query payload."""
        return {
            "query": query,
            "variables": variables or {}
        }

    async def _execute_query(self, query: str, variables: Optional[Dict] = None) -> Dict:
        """Execute GraphQL query against RunPod API."""
        payload = self._create_graphql_query(query, variables)
        
        try:
            response = await self.client.post(self.base_url, json=payload)
            response.raise_for_status()
            
            data = response.json()
            if "errors" in data:
                error_msg = "; ".join([e.get("message", "Unknown error") for e in data["errors"]])
                raise ProviderError(f"RunPod API error: {error_msg}", "runpod")
                
            return data.get("data", {})
            
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 401:
                raise ProviderError("Invalid RunPod API key", "runpod", "AUTH_FAILED")
            elif e.response.status_code == 404:
                raise ProviderError(f"RunPod resource not found: {e}", "runpod", "NOT_FOUND")
            elif e.response.status_code == 429:
                raise ProviderError("RunPod rate limit exceeded", "runpod", "RATE_LIMITED")
            else:
                raise ProviderError(f"RunPod API error: {e}", "runpod")
        except Exception as e:
            raise ProviderError(f"Failed to communicate with RunPod: {e}", "runpod")

    async def submit_job(self, job_config: JobConfig) -> str:
        """
        Submit a GPU job to RunPod.
        
        Creates a serverless pod or uses an existing endpoint to run the job.
        """
        try:
            pod_spec = RunPodPodSpec.from_gpu_spec(job_config.gpu_spec)
            
            # Create serverless pod
            create_pod_query = """
            mutation createPod($input: PodFindAndDeployOnDemandInput!) {
                podFindAndDeployOnDemand(input: $input) {
                    id
                    desiredStatus
                    imageName
                    env
                    machineId
                    machine {
                        podHostId
                    }
                }
            }
            """
            
            # Prepare environment variables
            env_vars = []
            for key, value in job_config.environment.items():
                env_vars.append({"name": key, "value": str(value)})
            
            variables = {
                "input": {
                    "cloudType": "ALL",
                    "gpuCount": pod_spec.gpu_count,
                    "minMemoryInGb": pod_spec.memory_gb,
                    "minVcpuCount": pod_spec.vcpu_count,
                    "name": job_config.name,
                    "imageName": job_config.image,
                    "dockerArgs": " ".join(job_config.command) if job_config.command else "",
                    "env": env_vars,
                    "volumeInGb": pod_spec.storage_gb,
                    "containerDiskInGb": pod_spec.storage_gb // 2,
                }
            }
            
            logger.info(f"Creating RunPod pod for job: {job_config.name}")
            result = await self._execute_query(create_pod_query, variables)
            
            pod_data = result.get("podFindAndDeployOnDemand")
            if not pod_data or not pod_data.get("id"):
                raise ProviderError("Failed to create RunPod pod")
            
            pod_id = pod_data["id"]
            
            # Store job information
            self._jobs[pod_id] = {
                "id": pod_id,
                "config": job_config.model_dump(),
                "pod_spec": pod_spec.model_dump(),
                "pod_data": pod_data,
                "created_at": datetime.now(timezone.utc),
                "status": JobStatus.PENDING,
            }
            
            logger.info(f"Successfully created RunPod pod: {pod_id}")
            return pod_id
            
        except Exception as e:
            logger.error(f"Failed to submit job to RunPod: {e}")
            if isinstance(e, (ProviderError, JobNotFoundError, InsufficientResourcesError)):
                raise
            raise ProviderError(f"Failed to submit job: {e}", "runpod")

    async def get_job_status(self, job_id: str) -> JobResult:
        """Get the status of a RunPod job."""
        try:
            # Query pod status
            pod_status_query = """
            query Pod($podId: String!) {
                pod(input: {podId: $podId}) {
                    id
                    name
                    imageName
                    desiredStatus
                    lastStatusChange
                    machine {
                        podHostId
                    }
                    runtime {
                        uptimeInSeconds
                        ports {
                            ip
                            isIpPublic
                            privatePort
                            publicPort
                            type
                        }
                    }
                }
            }
            """
            
            variables = {"podId": job_id}
            result = await self._execute_query(pod_status_query, variables)
            
            pod_data = result.get("pod")
            if not pod_data:
                if job_id not in self._jobs:
                    raise JobNotFoundError(job_id, "runpod")
                # Use cached data
                job_info = self._jobs[job_id]
                pod_data = job_info.get("pod_data", {})
            
            # Map RunPod status to JobStatus
            runpod_status = pod_data.get("desiredStatus", "UNKNOWN").upper()
            status_mapping = {
                "RUNNING": JobStatus.RUNNING,
                "EXITED": JobStatus.COMPLETED,
                "FAILED": JobStatus.FAILED,
                "PENDING": JobStatus.PENDING,
                "TERMINATED": JobStatus.CANCELLED,
                "UNKNOWN": JobStatus.PENDING,
            }
            
            status = status_mapping.get(runpod_status, JobStatus.PENDING)
            
            # Calculate runtime
            runtime_seconds = 0
            if pod_data.get("runtime") and pod_data["runtime"].get("uptimeInSeconds"):
                runtime_seconds = pod_data["runtime"]["uptimeInSeconds"]
            
            # Get creation time
            created_at = datetime.now(timezone.utc)
            if job_id in self._jobs:
                created_at = self._jobs[job_id]["created_at"]
            elif pod_data.get("lastStatusChange"):
                created_at = datetime.fromisoformat(pod_data["lastStatusChange"].replace("Z", "+00:00"))
            
            # Update job cache
            if job_id in self._jobs:
                self._jobs[job_id]["status"] = status
                self._jobs[job_id]["pod_data"].update(pod_data)
            
            return JobResult(
                job_id=job_id,
                status=status,
                created_at=created_at,
                started_at=created_at if status in [JobStatus.RUNNING, JobStatus.COMPLETED] else None,
                completed_at=created_at if status == JobStatus.COMPLETED else None,
                exit_code=0 if status == JobStatus.COMPLETED else None,
            )
            
        except JobNotFoundError:
            raise
        except Exception as e:
            logger.error(f"Failed to get job status from RunPod: {e}")
            raise ProviderError(f"Failed to get job status: {e}", "runpod")

    async def cancel_job(self, job_id: str) -> bool:
        """Cancel a RunPod job."""
        try:
            terminate_pod_query = """
            mutation terminatePod($input: PodTerminateInput!) {
                podTerminate(input: $input)
            }
            """
            
            variables = {"input": {"podId": job_id}}
            result = await self._execute_query(terminate_pod_query, variables)
            
            success = result.get("podTerminate", False)
            
            if success and job_id in self._jobs:
                self._jobs[job_id]["status"] = JobStatus.CANCELLED
            
            logger.info(f"RunPod pod termination {'succeeded' if success else 'failed'}: {job_id}")
            return success
            
        except JobNotFoundError:
            return False
        except Exception as e:
            logger.error(f"Failed to cancel RunPod job: {e}")
            return False

    async def get_job_logs(self, job_id: str, lines: int = 100) -> str:
        """Get logs from a RunPod job."""
        try:
            pod_logs_query = """
            query PodLogs($podId: String!) {
                pod(input: {podId: $podId}) {
                    id
                    logs
                }
            }
            """
            
            variables = {"podId": job_id}
            result = await self._execute_query(pod_logs_query, variables)
            
            pod_data = result.get("pod")
            if not pod_data:
                raise JobNotFoundError(job_id, "runpod")
            
            logs = pod_data.get("logs", "")
            
            # Truncate to requested number of lines
            if lines > 0:
                log_lines = logs.split("\n")
                if len(log_lines) > lines:
                    logs = "\n".join(log_lines[-lines:])
            
            return logs
            
        except JobNotFoundError:
            raise
        except Exception as e:
            logger.error(f"Failed to get logs from RunPod: {e}")
            return f"Error retrieving logs: {e}"

    async def get_cost_info(self, job_id: str) -> CostInfo:
        """Get cost information for a RunPod job."""
        try:
            # Get job status to calculate runtime
            job_result = await self.get_job_status(job_id)
            
            # RunPod pricing estimates (these would typically come from an API)
            # These are approximate rates per hour for different GPU types
            gpu_hourly_rates = {
                "NVIDIA A100": 2.89,
                "NVIDIA RTX A6000": 0.79,
                "NVIDIA RTX A5000": 0.64,
                "NVIDIA RTX A4000": 0.46,
                "NVIDIA GeForce RTX 4090": 0.83,
                "NVIDIA GeForce RTX 3090": 0.58,
                "NVIDIA H100": 4.89,
                "NVIDIA Tesla V100": 1.89,
                "NVIDIA Tesla T4": 0.22,
            }
            
            # Get GPU type from job cache
            gpu_type = "NVIDIA A100"  # Default
            if job_id in self._jobs:
                job_info = self._jobs[job_id]
                pod_spec = job_info.get("pod_spec", {})
                gpu_type = pod_spec.get("gpu_type_id", gpu_type)
                gpu_count = pod_spec.get("gpu_count", 1)
            else:
                gpu_count = 1
            
            hourly_rate = gpu_hourly_rates.get(gpu_type, 1.0) * gpu_count
            runtime_hours = job_result.runtime_seconds / 3600.0
            
            total_cost = hourly_rate * runtime_hours
            
            return CostInfo(
                job_id=job_id,
                total_cost=round(total_cost, 4),
                currency="USD",
                cost_breakdown={
                    "hourly_rate": hourly_rate,
                    "runtime_hours": round(runtime_hours, 4),
                    "compute_cost": round(total_cost, 4),
                    "gpu_count": float(gpu_count),
                },
                billing_period=f"{job_result.created_at.isoformat()} to {(job_result.completed_at or datetime.now(timezone.utc)).isoformat()}",
            )
            
        except Exception as e:
            logger.error(f"Failed to get cost info from RunPod: {e}")
            # Return zero cost on error
            return CostInfo(
                job_id=job_id,
                total_cost=0.0,
                currency="USD",
                cost_breakdown={"error_cost": 0.0},
                billing_period=datetime.now(timezone.utc).isoformat(),
            )

    async def list_available_gpus(self) -> List[GpuSpec]:
        """List available GPU types and specifications from RunPod."""
        try:
            # Query available GPU types
            gpu_types_query = """
            query {
                gpuTypes {
                    id
                    displayName
                    memoryInGb
                    secureCloud
                    communityCloud
                    lowestPrice {
                        gpuTypeId
                        uninterruptablePrice
                        interruptablePrice
                    }
                }
            }
            """
            
            result = await self._execute_query(gpu_types_query)
            gpu_types = result.get("gpuTypes", [])
            
            available_gpus = []
            
            for gpu_type in gpu_types:
                if not gpu_type.get("communityCloud", False):
                    continue  # Skip if not available in community cloud
                
                display_name = gpu_type.get("displayName", "")
                memory_gb = gpu_type.get("memoryInGb", 0)
                
                # Map to standard GPU type names
                gpu_name_mapping = {
                    "NVIDIA A100": "A100",
                    "NVIDIA RTX A6000": "A6000", 
                    "NVIDIA RTX A5000": "A5000",
                    "NVIDIA RTX A4000": "A4000",
                    "NVIDIA GeForce RTX 4090": "RTX4090",
                    "NVIDIA GeForce RTX 3090": "RTX3090",
                    "NVIDIA H100": "H100",
                    "NVIDIA Tesla V100": "V100",
                    "NVIDIA Tesla T4": "T4",
                }
                
                gpu_type_name = gpu_name_mapping.get(display_name, display_name.replace("NVIDIA ", ""))
                
                # Estimate other specs based on GPU type
                spec_estimates = {
                    "A100": {"vcpus": 12, "ram_gb": 64},
                    "A6000": {"vcpus": 8, "ram_gb": 32},
                    "A5000": {"vcpus": 8, "ram_gb": 32},
                    "A4000": {"vcpus": 6, "ram_gb": 24},
                    "RTX4090": {"vcpus": 8, "ram_gb": 32},
                    "RTX3090": {"vcpus": 8, "ram_gb": 32},
                    "H100": {"vcpus": 16, "ram_gb": 128},
                    "V100": {"vcpus": 8, "ram_gb": 32},
                    "T4": {"vcpus": 4, "ram_gb": 16},
                }
                
                estimates = spec_estimates.get(gpu_type_name, {"vcpus": 8, "ram_gb": 32})
                
                available_gpus.append(GpuSpec(
                    gpu_type=gpu_type_name,
                    gpu_count=1,  # RunPod typically provides single GPU instances
                    memory_gb=memory_gb or 16,
                    vcpus=estimates["vcpus"],
                    ram_gb=estimates["ram_gb"]
                ))
            
            return available_gpus
            
        except Exception as e:
            logger.error(f"Failed to list available GPUs from RunPod: {e}")
            # Return default GPU types if query fails
            return [
                GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=64),
                GpuSpec(gpu_type="RTX4090", gpu_count=1, memory_gb=24, vcpus=8, ram_gb=32),
                GpuSpec(gpu_type="A6000", gpu_count=1, memory_gb=48, vcpus=8, ram_gb=32),
                GpuSpec(gpu_type="T4", gpu_count=1, memory_gb=16, vcpus=4, ram_gb=16),
            ]

    async def health_check(self) -> Dict[str, Any]:
        """Check RunPod service health and connectivity."""
        try:
            # Simple query to test API connectivity
            health_query = """
            query {
                myself {
                    id
                    email
                }
            }
            """
            
            start_time = time.time()
            result = await self._execute_query(health_query)
            response_time = time.time() - start_time
            
            user_data = result.get("myself")
            if not user_data:
                return {
                    "status": "unhealthy",
                    "message": "Failed to authenticate with RunPod API",
                    "response_time_ms": round(response_time * 1000, 2),
                    "timestamp": datetime.now(timezone.utc).isoformat(),
                }
            
            return {
                "status": "healthy",
                "message": "RunPod API is accessible",
                "user_id": user_data.get("id"),
                "user_email": user_data.get("email"),
                "response_time_ms": round(response_time * 1000, 2),
                "timestamp": datetime.now(timezone.utc).isoformat(),
            }
            
        except Exception as e:
            return {
                "status": "unhealthy",
                "message": f"RunPod health check failed: {e}",
                "timestamp": datetime.now(timezone.utc).isoformat(),
            }
