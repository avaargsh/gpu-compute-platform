"""
Alibaba Cloud ECS GPU Provider Adapter

This adapter implements the unified GPU provider interface for Alibaba Cloud ECS.
It handles GPU instance creation, management, and cost tracking through ECS APIs.
"""

import asyncio
import json
import uuid
from datetime import datetime, timezone
from typing import Dict, Any, Optional, List
import logging

from alibabacloud_ecs20140526.client import Client as EcsClient
from alibabacloud_ecs20140526.models import (
    RunInstancesRequest,
    DescribeInstancesRequest,
    StopInstancesRequest,
    DeleteInstancesRequest,
    DescribePriceRequest
)
from alibabacloud_tea_openapi.models import Config as OpenApiConfig
from alibabacloud_tea_util.models import RuntimeOptions

from ..interface import (
    GpuProviderInterface, JobConfig, JobResult, JobStatus, 
    GpuSpec, CostInfo, ProviderError, JobNotFoundError,
    InsufficientResourcesError
)

logger = logging.getLogger(__name__)


class AlibabaCloudAdapter(GpuProviderInterface):
    """
    Alibaba Cloud ECS adapter for GPU compute.
    
    This adapter treats GPU jobs as ECS instances with GPU configurations.
    Since Alibaba Cloud doesn't have native job abstraction, we manage
    the full lifecycle of GPU instances.
    """
    
    PROVIDER_NAME = "alibaba_cloud"
    
    # Mapping of standard GPU types to Alibaba Cloud instance types
    GPU_INSTANCE_TYPES = {
        "T4": "ecs.gn6i-c4g1.xlarge",      # 1x Tesla T4, 4 vCPU, 15GB RAM
        "V100": "ecs.gn6v-c8g1.2xlarge",   # 1x Tesla V100, 8 vCPU, 32GB RAM
        "A100": "ecs.gn7-c12g1.3xlarge",   # 1x A100, 12 vCPU, 46GB RAM
    }
    
    def __init__(self, config: Dict[str, Any]):
        """
        Initialize Alibaba Cloud adapter.
        
        Args:
            config: Configuration containing:
                - access_key_id: Alibaba Cloud access key
                - access_key_secret: Alibaba Cloud secret key
                - region_id: Target region (e.g., 'cn-hangzhou')
                - security_group_id: Security group for instances
                - vswitch_id: VSwitch ID for network configuration
                - key_pair_name: SSH key pair name
        """
        super().__init__(config)
        
        # Validate required configuration
        required_keys = [
            'access_key_id', 'access_key_secret', 'region_id',
            'security_group_id', 'vswitch_id'
        ]
        for key in required_keys:
            if key not in config:
                raise ValueError(f"Missing required config key: {key}")
        
        # Initialize ECS client
        ecs_config = OpenApiConfig(
            access_key_id=config['access_key_id'],
            access_key_secret=config['access_key_secret'],
            region_id=config['region_id'],
            endpoint=f"ecs.{config['region_id']}.aliyuncs.com"
        )
        self.ecs_client = EcsClient(ecs_config)
        
        # Store instance metadata (in production, this should be in a database)
        self._job_instances: Dict[str, Dict] = {}
    
    def _get_instance_type(self, gpu_spec: GpuSpec) -> str:
        """Map GPU specification to Alibaba Cloud instance type."""
        if gpu_spec.gpu_type not in self.GPU_INSTANCE_TYPES:
            raise InsufficientResourcesError(gpu_spec, self.PROVIDER_NAME)
        
        # For simplicity, we use single GPU instances
        # In production, you'd need more complex mapping for multi-GPU
        if gpu_spec.gpu_count > 1:
            logger.warning(f"Multi-GPU support limited. Using single GPU instance for {gpu_spec.gpu_count}x{gpu_spec.gpu_type}")
        
        return self.GPU_INSTANCE_TYPES[gpu_spec.gpu_type]
    
    def _create_user_data(self, job_config: JobConfig) -> str:
        """Create user data script for instance initialization."""
        env_vars = ""
        if job_config.environment:
            env_vars = "\n".join([f"export {k}={v}" for k, v in job_config.environment.items()])
        
        user_data = f"""#!/bin/bash
{env_vars}

# Install Docker if not present
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh
    systemctl start docker
    systemctl enable docker
fi

# Install NVIDIA Docker support
if ! command -v nvidia-docker &> /dev/null; then
    distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
    curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | apt-key add -
    curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | tee /etc/apt/sources.list.d/nvidia-docker.list
    apt-get update && apt-get install -y nvidia-docker2
    systemctl restart docker
fi

# Run the job
cd /root
echo "Starting job: {job_config.name}"
docker run --gpus all --rm {job_config.image} {' '.join(job_config.command)} > job_output.log 2>&1
echo $? > job_exit_code.txt
echo "$(date)" > job_completed_at.txt
"""
        return user_data
    
    async def submit_job(self, job_config: JobConfig) -> str:
        """Submit a job by creating an ECS instance."""
        try:
            job_id = str(uuid.uuid4())
            instance_type = self._get_instance_type(job_config.gpu_spec)
            user_data = self._create_user_data(job_config)
            
            # Create the request object with correct field names
            request = RunInstancesRequest()
            request.region_id = self.config['region_id']
            request.image_id = self.config.get('image_id', 'ubuntu_20_04_x64_20G_alibase_20231221.vhd')
            request.instance_type = instance_type
            request.security_group_id = self.config['security_group_id']
            request.v_switch_id = self.config['vswitch_id']  # Correct field name
            request.instance_name = f"gpu-job-{job_config.name}-{job_id[:8]}"
            request.min_amount = 1
            request.max_amount = 1
            request.user_data = user_data
            request.key_pair_name = self.config.get('key_pair_name')
            request.instance_charge_type = 'PostPaid'
            request.system_disk_category = self.config.get('system_disk_category', 'cloud_ssd')
            request.system_disk_size = self.config.get('system_disk_size', 40)
            # Tags are handled differently in Alibaba Cloud SDK
            if 'key_pair_name' in self.config:
                request.key_pair_name = self.config['key_pair_name']
            
            runtime_options = RuntimeOptions()
            response = await asyncio.to_thread(
                self.ecs_client.run_instances_with_options,
                request,
                runtime_options
            )
            
            if not response.body.instance_id_sets or not response.body.instance_id_sets.instance_id_set:
                raise ProviderError("Failed to create instance", self.PROVIDER_NAME)
            
            instance_id = response.body.instance_id_sets.instance_id_set[0]
            
            # Store job metadata
            self._job_instances[job_id] = {
                'instance_id': instance_id,
                'job_config': job_config.model_dump(),
                'created_at': datetime.now(timezone.utc),
                'status': JobStatus.PENDING
            }
            
            logger.info(f"Created instance {instance_id} for job {job_id}")
            return job_id
            
        except Exception as e:
            raise ProviderError(f"Failed to submit job: {str(e)}", self.PROVIDER_NAME)
    
    async def get_job_status(self, job_id: str) -> JobResult:
        """Get job status by checking ECS instance status."""
        if job_id not in self._job_instances:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        try:
            job_info = self._job_instances[job_id]
            instance_id = job_info['instance_id']
            
            request = DescribeInstancesRequest(
                region_id=self.config['region_id'],
                instance_ids=f'["{instance_id}"]'
            )
            
            runtime_options = RuntimeOptions()
            response = await asyncio.to_thread(
                self.ecs_client.describe_instances_with_options,
                request,
                runtime_options
            )
            
            if not response.body.instances or not response.body.instances.instance:
                raise JobNotFoundError(job_id, self.PROVIDER_NAME)
            
            instance = response.body.instances.instance[0]
            
            # Map ECS instance status to job status
            status_mapping = {
                'Pending': JobStatus.PENDING,
                'Running': JobStatus.RUNNING,
                'Stopping': JobStatus.RUNNING,
                'Stopped': JobStatus.COMPLETED,
                'Starting': JobStatus.PENDING,
            }
            
            ecs_status = instance.status
            job_status = status_mapping.get(ecs_status, JobStatus.UNKNOWN)
            
            # Update cached status
            job_info['status'] = job_status
            
            result = JobResult(
                job_id=job_id,
                status=job_status,
                created_at=job_info['created_at'],
                started_at=datetime.fromisoformat(instance.creation_time) if instance.creation_time else None,
                completed_at=None,  # Would need to check user data execution
                logs=None,  # Would need to fetch from instance
                exit_code=None,
                error_message=None
            )
            
            return result
            
        except JobNotFoundError:
            raise
        except Exception as e:
            raise ProviderError(f"Failed to get job status: {str(e)}", self.PROVIDER_NAME)
    
    async def cancel_job(self, job_id: str) -> bool:
        """Cancel job by stopping the ECS instance."""
        if job_id not in self._job_instances:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        try:
            job_info = self._job_instances[job_id]
            instance_id = job_info['instance_id']
            
            request = StopInstancesRequest(
                region_id=self.config['region_id'],
                instance_ids=f'["{instance_id}"]',
                force_stop=True
            )
            
            runtime_options = RuntimeOptions()
            await asyncio.to_thread(
                self.ecs_client.stop_instances_with_options,
                request,
                runtime_options
            )
            
            job_info['status'] = JobStatus.CANCELLED
            return True
            
        except Exception as e:
            raise ProviderError(f"Failed to cancel job: {str(e)}", self.PROVIDER_NAME)
    
    async def get_job_logs(self, job_id: str, lines: Optional[int] = None) -> str:
        """Get job logs from ECS instance (simplified implementation)."""
        if job_id not in self._job_instances:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        # In a real implementation, you would:
        # 1. SSH into the instance
        # 2. Fetch the job_output.log file
        # 3. Return the contents
        
        return f"Log retrieval for job {job_id} requires SSH access to instance"
    
    async def get_cost_info(self, job_id: str) -> CostInfo:
        """Get cost information for the job."""
        if job_id not in self._job_instances:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        try:
            job_info = self._job_instances[job_id]
            instance_type = self._get_instance_type(
                GpuSpec(**job_info['job_config']['gpu_spec'])
            )
            
            # This is a simplified cost calculation
            # In production, you'd use DescribePrice API for accurate pricing
            
            return CostInfo(
                job_id=job_id,
                total_cost=0.0,  # Would calculate based on runtime and instance type
                currency="CNY",
                cost_breakdown={
                    "compute": 0.0,
                    "storage": 0.0,
                    "network": 0.0
                },
                billing_period="hourly"
            )
            
        except Exception as e:
            raise ProviderError(f"Failed to get cost info: {str(e)}", self.PROVIDER_NAME)
    
    async def list_available_gpus(self) -> List[GpuSpec]:
        """List available GPU specifications."""
        return [
            GpuSpec(
                gpu_type="T4",
                gpu_count=1,
                memory_gb=15,
                vcpus=4,
                ram_gb=15
            ),
            GpuSpec(
                gpu_type="V100",
                gpu_count=1,
                memory_gb=16,
                vcpus=8,
                ram_gb=32
            ),
            GpuSpec(
                gpu_type="A100",
                gpu_count=1,
                memory_gb=40,
                vcpus=12,
                ram_gb=46
            )
        ]
    
    async def health_check(self) -> Dict[str, Any]:
        """Check Alibaba Cloud connectivity."""
        try:
            request = DescribeInstancesRequest(
                region_id=self.config['region_id'],
                page_size=1
            )
            
            runtime_options = RuntimeOptions()
            response = await asyncio.to_thread(
                self.ecs_client.describe_instances_with_options,
                request,
                runtime_options
            )
            
            return {
                "status": "healthy",
                "provider": self.PROVIDER_NAME,
                "region": self.config['region_id'],
                "api_accessible": True,
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
        except Exception as e:
            return {
                "status": "unhealthy",
                "provider": self.PROVIDER_NAME,
                "error": str(e),
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
