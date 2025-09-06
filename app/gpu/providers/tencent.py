"""
Tencent Cloud GPU Provider Adapter

This adapter implements the unified GPU provider interface for Tencent Cloud TKE.
It leverages Kubernetes API to manage GPU workloads in Tencent's container service.
"""

import asyncio
import base64
import json
import uuid
import logging
from datetime import datetime, timezone
from typing import Dict, Any, Optional, List, Tuple

from kubernetes import client, config as k8s_config
from kubernetes.client.rest import ApiException
from kubernetes.stream import stream
from tencentcloud.common import credential
from tencentcloud.common.profile.client_profile import ClientProfile
from tencentcloud.common.profile.http_profile import HttpProfile
from tencentcloud.tke.v20180525 import tke_client, models

from ..interface import (
    GpuProviderInterface, JobConfig, JobResult, JobStatus, 
    GpuSpec, CostInfo, ProviderError, JobNotFoundError,
    InsufficientResourcesError
)

logger = logging.getLogger(__name__)


class TencentCloudAdapter(GpuProviderInterface):
    """
    Tencent Cloud TKE adapter for GPU compute.
    
    This adapter uses Tencent Kubernetes Engine (TKE) to run GPU workloads.
    It creates Kubernetes Jobs with GPU resource requests.
    """
    
    PROVIDER_NAME = "tencent_cloud"
    
    # GPU types available in Tencent Cloud
    # Maps our standardized types to Tencent-specific values
    GPU_TYPES = {
        "T4": "1/4",      # 1/4 Tesla T4
        "V100": "1/2",    # 1/2 Tesla V100 
        "A100": "1",      # Full A100
    }
    
    # Namespace for our GPU jobs
    JOB_NAMESPACE = "gpu-jobs"
    
    def __init__(self, config: Dict[str, Any]):
        """
        Initialize Tencent Cloud adapter.
        
        Args:
            config: Configuration containing:
                - secret_id: Tencent Cloud Secret ID
                - secret_key: Tencent Cloud Secret Key
                - region: Tencent Cloud region
                - cluster_id: TKE cluster ID
                - kubeconfig: Optional base64-encoded kubeconfig
        """
        super().__init__(config)
        
        # Validate required configuration
        required_keys = ['secret_id', 'secret_key', 'region', 'cluster_id']
        for key in required_keys:
            if key not in config:
                raise ValueError(f"Missing required config key: {key}")
        
        # Initialize Tencent Cloud TKE client
        self.cred = credential.Credential(
            config['secret_id'],
            config['secret_key']
        )
        http_profile = HttpProfile()
        http_profile.endpoint = "tke.tencentcloudapi.com"
        client_profile = ClientProfile()
        client_profile.httpProfile = http_profile
        self.tke_client = tke_client.TkeClient(self.cred, config['region'], client_profile)
        
        # Initialize Kubernetes client
        if 'kubeconfig' in config:
            # Load from provided kubeconfig
            kubeconfig_content = base64.b64decode(config['kubeconfig']).decode('utf-8')
            kubeconfig_file = '/tmp/kubeconfig'
            with open(kubeconfig_file, 'w') as f:
                f.write(kubeconfig_content)
            k8s_config.load_kube_config(kubeconfig_file)
        else:
            # Fetch kubeconfig for the cluster
            self._load_cluster_credentials()
        
        self.k8s_api = client.CoreV1Api()
        self.k8s_batch_api = client.BatchV1Api()
        
        # Create namespace if it doesn't exist
        self._ensure_namespace()
        
        # Store job metadata (in production, this should be in a database)
        self._jobs: Dict[str, Dict] = {}
    
    def _load_cluster_credentials(self):
        """Fetch kubeconfig for the specified cluster."""
        try:
            # Create request to get cluster credentials
            request = models.DescribeClusterKubeconfigRequest()
            request.ClusterId = self.config['cluster_id']
            request.IsExtranet = False
            
            # Get cluster kubeconfig
            response = self.tke_client.DescribeClusterKubeconfig(request)
            kubeconfig_content = response.Kubeconfig
            
            # Write kubeconfig to temp file and load it
            kubeconfig_file = '/tmp/kubeconfig'
            with open(kubeconfig_file, 'w') as f:
                f.write(kubeconfig_content)
            
            k8s_config.load_kube_config(kubeconfig_file)
            
        except Exception as e:
            raise ProviderError(f"Failed to load cluster credentials: {str(e)}", self.PROVIDER_NAME)
    
    def _ensure_namespace(self):
        """Create the job namespace if it doesn't exist."""
        try:
            namespaces = self.k8s_api.list_namespace()
            namespace_exists = any(ns.metadata.name == self.JOB_NAMESPACE for ns in namespaces.items)
            
            if not namespace_exists:
                namespace = client.V1Namespace(
                    metadata=client.V1ObjectMeta(name=self.JOB_NAMESPACE)
                )
                self.k8s_api.create_namespace(namespace)
                logger.info(f"Created namespace {self.JOB_NAMESPACE}")
                
        except ApiException as e:
            # If namespace already exists, that's fine
            if e.status != 409:  # 409 Conflict
                raise ProviderError(f"Failed to ensure namespace: {str(e)}", self.PROVIDER_NAME)
    
    def _get_gpu_resources(self, gpu_spec: GpuSpec) -> Dict[str, str]:
        """
        Convert GPU specification to Kubernetes resource requests.
        
        For Tencent Cloud, we use the nvidia.com/gpu resource type.
        """
        if gpu_spec.gpu_type not in self.GPU_TYPES:
            raise InsufficientResourcesError(gpu_spec, self.PROVIDER_NAME)
        
        # Convert our type to Tencent-compatible value
        gpu_fraction_str = self.GPU_TYPES[gpu_spec.gpu_type]
        if '/' in gpu_fraction_str:
            # Handle fractions like "1/4", "1/2"
            numerator, denominator = map(int, gpu_fraction_str.split('/'))
            gpu_fraction = numerator / denominator
        else:
            # Handle integers like "1"
            gpu_fraction = float(gpu_fraction_str)
        
        gpu_count = int(gpu_fraction * gpu_spec.gpu_count)
        
        # For a real implementation, we'd need to handle fractional GPUs correctly
        # This is a simplified version
        return {
            "nvidia.com/gpu": str(gpu_count)
        }
    
    def _create_k8s_job(self, job_id: str, job_config: JobConfig) -> client.V1Job:
        """Create a Kubernetes Job specification."""
        # Generate a unique name for the job
        job_name = f"gpu-{job_config.name}-{job_id[:8]}".lower().replace('_', '-')
        
        # Get GPU resource requirements
        gpu_resources = self._get_gpu_resources(job_config.gpu_spec)
        
        # Create container environment variables
        env = []
        if job_config.environment:
            for key, value in job_config.environment.items():
                env.append(client.V1EnvVar(name=key, value=value))
        
        # Create volume mounts if needed
        volume_mounts = []
        volumes = []
        if job_config.volumes:
            for mount_path, host_path in job_config.volumes.items():
                volume_name = f"vol-{len(volumes)}"
                volume_mounts.append(
                    client.V1VolumeMount(name=volume_name, mount_path=mount_path)
                )
                volumes.append(
                    client.V1Volume(
                        name=volume_name,
                        host_path=client.V1HostPathVolumeSource(path=host_path)
                    )
                )
        
        # Create the container spec
        container = client.V1Container(
            name="gpu-container",
            image=job_config.image,
            command=job_config.command,
            env=env,
            volume_mounts=volume_mounts,
            resources=client.V1ResourceRequirements(
                requests={
                    "cpu": str(job_config.gpu_spec.vcpus),
                    "memory": f"{job_config.gpu_spec.ram_gb}Gi",
                    **gpu_resources
                },
                limits={
                    "cpu": str(job_config.gpu_spec.vcpus),
                    "memory": f"{job_config.gpu_spec.ram_gb}Gi",
                    **gpu_resources
                }
            )
        )
        
        # Create the job spec
        job_spec = client.V1Job(
            api_version="batch/v1",
            kind="Job",
            metadata=client.V1ObjectMeta(
                name=job_name,
                namespace=self.JOB_NAMESPACE,
                labels={"job-id": job_id, "app": "gpu-compute"}
            ),
            spec=client.V1JobSpec(
                template=client.V1PodTemplateSpec(
                    metadata=client.V1ObjectMeta(
                        labels={"job-id": job_id, "app": "gpu-compute"}
                    ),
                    spec=client.V1PodSpec(
                        containers=[container],
                        volumes=volumes,
                        restart_policy="Never"
                    )
                ),
                backoff_limit=job_config.retry_count,
                ttl_seconds_after_finished=3600  # Clean up job after 1 hour
            )
        )
        
        return job_spec
    
    def _get_job_status_from_k8s(self, job_name: str) -> Tuple[JobStatus, Dict[str, Any]]:
        """Get current job status from Kubernetes."""
        try:
            job = self.k8s_batch_api.read_namespaced_job(
                name=job_name,
                namespace=self.JOB_NAMESPACE
            )
            
            status = job.status
            status_info = {}
            
            # Extract job status based on Kubernetes job conditions
            if status.succeeded and status.succeeded > 0:
                job_status = JobStatus.COMPLETED
                status_info["exit_code"] = 0
            elif status.failed and status.failed > 0:
                job_status = JobStatus.FAILED
                status_info["exit_code"] = 1
            elif status.active and status.active > 0:
                job_status = JobStatus.RUNNING
            else:
                job_status = JobStatus.PENDING
            
            # Get start/completion time
            if status.start_time:
                status_info["started_at"] = status.start_time
            if status.completion_time:
                status_info["completed_at"] = status.completion_time
            
            return job_status, status_info
            
        except ApiException as e:
            if e.status == 404:
                return JobStatus.UNKNOWN, {}
            raise ProviderError(f"Failed to get job status: {str(e)}", self.PROVIDER_NAME)
    
    async def submit_job(self, job_config: JobConfig) -> str:
        """Submit a job by creating a Kubernetes Job."""
        try:
            job_id = str(uuid.uuid4())
            job_spec = self._create_k8s_job(job_id, job_config)
            
            # Create the job in Kubernetes
            created_job = await asyncio.to_thread(
                self.k8s_batch_api.create_namespaced_job,
                namespace=self.JOB_NAMESPACE,
                body=job_spec
            )
            
            # Store job metadata
            self._jobs[job_id] = {
                "k8s_job_name": created_job.metadata.name,
                "k8s_namespace": self.JOB_NAMESPACE,
                "job_config": job_config.model_dump(),
                "created_at": datetime.now(timezone.utc),
                "status": JobStatus.PENDING
            }
            
            logger.info(f"Created Kubernetes job {created_job.metadata.name} for job ID {job_id}")
            return job_id
            
        except ApiException as e:
            raise ProviderError(f"Failed to submit job: {str(e)}", self.PROVIDER_NAME)
        except Exception as e:
            raise ProviderError(f"Failed to submit job: {str(e)}", self.PROVIDER_NAME)
    
    async def get_job_status(self, job_id: str) -> JobResult:
        """Get job status from Kubernetes job."""
        if job_id not in self._jobs:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        job_info = self._jobs[job_id]
        
        try:
            job_name = job_info["k8s_job_name"]
            
            k8s_status, status_info = await asyncio.to_thread(
                self._get_job_status_from_k8s,
                job_name
            )
            
            # Update cached job status
            job_info["status"] = k8s_status
            
            # Create JobResult
            result = JobResult(
                job_id=job_id,
                status=k8s_status,
                created_at=job_info["created_at"],
                started_at=status_info.get("started_at"),
                completed_at=status_info.get("completed_at"),
                exit_code=status_info.get("exit_code"),
                logs=None,  # Logs need to be fetched separately
                error_message=status_info.get("error_message")
            )
            
            return result
            
        except JobNotFoundError:
            raise
        except Exception as e:
            # Fallback to cached status when K8s is unavailable
            logger.warning(f"Failed to get live job status, using cached status: {str(e)}")
            cached_status = job_info.get("status", JobStatus.UNKNOWN)
            
            return JobResult(
                job_id=job_id,
                status=cached_status,
                created_at=job_info["created_at"],
                started_at=None,
                completed_at=None,
                exit_code=None,
                logs=None,
                error_message=f"Using cached status due to API unavailability: {str(e)}"
            )
    
    async def cancel_job(self, job_id: str) -> bool:
        """Cancel job by deleting the Kubernetes job."""
        if job_id not in self._jobs:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        try:
            job_info = self._jobs[job_id]
            job_name = job_info["k8s_job_name"]
            namespace = job_info["k8s_namespace"]
            
            # Delete the job in Kubernetes
            await asyncio.to_thread(
                self.k8s_batch_api.delete_namespaced_job,
                name=job_name,
                namespace=namespace,
                body=client.V1DeleteOptions(
                    propagation_policy="Background"
                )
            )
            
            # Update cached job status
            job_info["status"] = JobStatus.CANCELLED
            
            return True
            
        except ApiException as e:
            if e.status == 404:
                # Job already gone, consider it cancelled
                self._jobs[job_id]["status"] = JobStatus.CANCELLED
                return True
            raise ProviderError(f"Failed to cancel job: {str(e)}", self.PROVIDER_NAME)
        except Exception as e:
            raise ProviderError(f"Failed to cancel job: {str(e)}", self.PROVIDER_NAME)
    
    def _get_pod_for_job(self, job_name: str) -> Optional[str]:
        """Get pod name for a specific job."""
        try:
            pods = self.k8s_api.list_namespaced_pod(
                namespace=self.JOB_NAMESPACE,
                label_selector=f"job-name={job_name}"
            )
            
            if pods.items:
                return pods.items[0].metadata.name
            return None
            
        except ApiException as e:
            raise ProviderError(f"Failed to get pod for job: {str(e)}", self.PROVIDER_NAME)
    
    async def get_job_logs(self, job_id: str, lines: Optional[int] = None) -> str:
        """Get logs from the job's pod."""
        if job_id not in self._jobs:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        try:
            job_info = self._jobs[job_id]
            job_name = job_info["k8s_job_name"]
            namespace = job_info["k8s_namespace"]
            
            # Get the pod for this job
            pod_name = await asyncio.to_thread(self._get_pod_for_job, job_name)
            
            if not pod_name:
                return "No pods found for this job yet."
            
            # Get logs from the pod
            logs = await asyncio.to_thread(
                self.k8s_api.read_namespaced_pod_log,
                name=pod_name,
                namespace=namespace,
                tail_lines=lines
            )
            
            return logs
            
        except Exception as e:
            raise ProviderError(f"Failed to get job logs: {str(e)}", self.PROVIDER_NAME)
    
    async def get_cost_info(self, job_id: str) -> CostInfo:
        """Get cost information for the job."""
        if job_id not in self._jobs:
            raise JobNotFoundError(job_id, self.PROVIDER_NAME)
        
        try:
            job_info = self._jobs[job_id]
            
            # In a real implementation, you would:
            # 1. Get actual usage metrics from Kubernetes
            # 2. Query Tencent Cloud's billing APIs
            # 3. Calculate actual costs based on usage and pricing
            
            # This is a simplified mock implementation
            return CostInfo(
                job_id=job_id,
                total_cost=0.0,  # Placeholder
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
        """List available GPU specifications in Tencent Cloud."""
        # This would typically come from querying available node types in TKE
        return [
            GpuSpec(
                gpu_type="T4",
                gpu_count=1,
                memory_gb=16,
                vcpus=4,
                ram_gb=16
            ),
            GpuSpec(
                gpu_type="V100",
                gpu_count=1,
                memory_gb=32,
                vcpus=6,
                ram_gb=32
            ),
            GpuSpec(
                gpu_type="A100",
                gpu_count=1,
                memory_gb=40,
                vcpus=12,
                ram_gb=64
            )
        ]
    
    async def health_check(self) -> Dict[str, Any]:
        """Check Tencent Cloud and Kubernetes connectivity."""
        try:
            # Check TKE API
            tke_healthy = False
            try:
                # Simple check: list clusters
                request = models.DescribeClustersRequest()
                await asyncio.to_thread(self.tke_client.DescribeClusters, request)
                tke_healthy = True
            except Exception as e:
                logger.error(f"TKE API check failed: {str(e)}")
            
            # Check Kubernetes API
            k8s_healthy = False
            try:
                await asyncio.to_thread(self.k8s_api.list_namespace, limit=1)
                k8s_healthy = True
            except Exception as e:
                logger.error(f"Kubernetes API check failed: {str(e)}")
            
            return {
                "status": "healthy" if (tke_healthy and k8s_healthy) else "unhealthy",
                "provider": self.PROVIDER_NAME,
                "region": self.config['region'],
                "cluster_id": self.config['cluster_id'],
                "tke_api_accessible": tke_healthy,
                "kubernetes_api_accessible": k8s_healthy,
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
            
        except Exception as e:
            return {
                "status": "unhealthy",
                "provider": self.PROVIDER_NAME,
                "error": str(e),
                "timestamp": datetime.now(timezone.utc).isoformat()
            }
