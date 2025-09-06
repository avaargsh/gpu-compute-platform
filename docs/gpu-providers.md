# GPU Provider Architecture

This document describes the GPU provider architecture implementation for the GPU Compute Platform.

## Overview

The GPU provider system implements a pluggable architecture that allows the platform to work with different cloud GPU providers through a unified interface. This design enables:

- **Cloud Agnostic**: Application layer remains independent of specific cloud provider APIs
- **Pluggable**: Easy to add new cloud providers by implementing the standard interface
- **Unified Experience**: Consistent API regardless of underlying cloud platform
- **Production Ready**: Comprehensive error handling, async support, and extensive test coverage

## Architecture Layers

### 1. Application Layer
The application layer contains core business logic and makes calls to GPU providers without knowing about cloud-specific implementation details.

### 2. Unified Interface Layer (`app/gpu/interface.py`)
Defines the standard interface that all GPU providers must implement:

- **Data Models**: `JobConfig`, `GpuSpec`, `JobResult`, `CostInfo`, `JobStatus`
- **Core Interface**: `GpuProviderInterface` abstract base class
- **Exception Types**: `ProviderError`, `JobNotFoundError`, `InsufficientResourcesError`

#### Key Methods
- `submit_job()` - Submit GPU jobs for execution
- `get_job_status()` - Monitor job progress and status
- `cancel_job()` - Cancel running jobs
- `get_job_logs()` - Retrieve job execution logs
- `get_cost_info()` - Get cost breakdown and billing information
- `list_available_gpus()` - Query available GPU configurations
- `health_check()` - Check provider connectivity and health

### 3. Adapter Layer (`app/gpu/providers/`)
Cloud-specific implementations that adapt the unified interface to each provider's APIs:

#### Alibaba Cloud Adapter (`alibaba.py`)
- **Strategy**: IaaS-centric approach using ECS GPU instances
- **Implementation**: Creates ECS instances for each job with Docker + NVIDIA runtime
- **Status Tracking**: Maps ECS instance states to job statuses
- **Supported GPUs**: T4, V100, A100
- **Features**: Auto-installation of Docker and NVIDIA drivers via user-data scripts

#### Tencent Cloud Adapter (`tencent.py`)
- **Strategy**: Kubernetes-centric using Tencent Kubernetes Engine (TKE)
- **Implementation**: Creates Kubernetes Jobs with GPU resource requests
- **Status Tracking**: Native Kubernetes job status mapping
- **Supported GPUs**: T4, V100, A100
- **Features**: Full Kubernetes integration with pod logs and resource management

## Implementation Details

### Data Models

#### GpuSpec
Standardized GPU specification across providers:
```python
GpuSpec(
    gpu_type="A100",     # GPU type identifier
    gpu_count=1,         # Number of GPUs
    memory_gb=40,        # GPU memory in GB
    vcpus=12,           # CPU cores
    ram_gb=48           # System RAM in GB
)
```

#### JobConfig
Unified job configuration:
```python
JobConfig(
    name="pytorch-training",
    image="pytorch:latest",
    command=["python", "train.py"],
    gpu_spec=gpu_spec,
    environment={"CUDA_VISIBLE_DEVICES": "0"},
    volumes={"/data": "/host/data"},
    timeout_minutes=60,
    retry_count=3
)
```

### Provider-Specific Implementation

#### Alibaba Cloud (ECS-based)
```python
# Job submission creates ECS instance
instance = create_gpu_instance(
    instance_type="ecs.gn7-c12g1.3xlarge",  # A100 mapping
    user_data=docker_startup_script,
    gpu_spec=job_config.gpu_spec
)

# Status from ECS instance state
status = map_ecs_status_to_job_status(instance.status)
```

#### Tencent Cloud (Kubernetes-based)
```python
# Job submission creates K8s Job
k8s_job = create_kubernetes_job(
    resources={"nvidia.com/gpu": "1"},
    image=job_config.image,
    command=job_config.command
)

# Status from Kubernetes Job
status = map_k8s_status_to_job_status(k8s_job.status)
```

## Usage Examples

### Basic Job Submission
```python
from app.gpu.interface import JobConfig, GpuSpec
from app.gpu.providers.tencent import TencentCloudAdapter

# Initialize provider
provider = TencentCloudAdapter({
    "secret_id": "{{TENCENT_SECRET_ID}}",
    "secret_key": "{{TENCENT_SECRET_KEY}}",
    "region": "ap-shanghai",
    "cluster_id": "cls-xxxxxx"
})

# Define job
job_config = JobConfig(
    name="resnet50-training",
    image="nvcr.io/nvidia/pytorch:24.02-py3",
    command=["python", "train.py", "--epochs", "10"],
    gpu_spec=GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48)
)

# Submit and monitor
job_id = await provider.submit_job(job_config)
status = await provider.get_job_status(job_id)
logs = await provider.get_job_logs(job_id)
```

### Provider Health Checking
```python
# Check provider availability
health = await provider.health_check()
if health["status"] == "healthy":
    print("Provider is ready")
    
# List available GPUs
gpus = await provider.list_available_gpus()
for gpu in gpus:
    print(f"{gpu.gpu_type}: {gpu.gpu_count}x GPU, {gpu.vcpus} CPUs")
```

### Error Handling
```python
try:
    job_id = await provider.submit_job(job_config)
except InsufficientResourcesError as e:
    print(f"No {e.requested_spec.gpu_type} GPUs available")
except ProviderError as e:
    print(f"Provider error: {e} (code: {e.error_code})")
```

## Adding New Providers

To add support for a new cloud provider:

1. **Create adapter file**: `app/gpu/providers/new_provider.py`
2. **Implement interface**: Extend `GpuProviderInterface`
3. **Add provider-specific logic**: 
   - GPU type mappings
   - Job submission mechanism
   - Status tracking
   - Cost calculation
4. **Add tests**: Create comprehensive test coverage
5. **Update documentation**: Add provider-specific notes

### Template
```python
class NewProviderAdapter(GpuProviderInterface):
    PROVIDER_NAME = "new_provider"
    
    def __init__(self, config: Dict[str, Any]):
        super().__init__(config)
        # Provider-specific initialization
    
    async def submit_job(self, job_config: JobConfig) -> str:
        # Implementation specific to new provider
        pass
    
    # Implement all other interface methods...
```

## Configuration

### Alibaba Cloud
Required configuration:
```python
{
    "access_key_id": "your_access_key",
    "access_key_secret": "your_secret_key", 
    "region_id": "cn-hangzhou",
    "security_group_id": "sg-xxxxx",
    "vswitch_id": "vsw-xxxxx"
}
```

### Tencent Cloud
Required configuration:
```python
{
    "secret_id": "your_secret_id",
    "secret_key": "your_secret_key",
    "region": "ap-shanghai", 
    "cluster_id": "cls-xxxxx",
    "kubeconfig": "base64_encoded_config"  # optional
}
```

## Testing

The GPU provider system includes comprehensive test coverage:

- **Unit Tests**: Interface validation, adapter initialization, core operations
- **Integration Tests**: Provider switching, error handling consistency
- **Mock Testing**: All external API calls are mocked for reliable testing
- **Coverage**: >90% code coverage on core GPU provider modules

Run tests:
```bash
# All GPU provider tests
uv run pytest tests/test_gpu_providers.py -v

# Comprehensive tests
uv run pytest tests/test_gpu_comprehensive.py -v

# With coverage
uv run pytest --cov=app.gpu --cov-report=html
```

## Production Considerations

### Scalability
- Each provider adapter can handle concurrent job submissions
- Job metadata should be stored in a database (not in-memory as in current implementation)
- Consider implementing job queues for high-volume scenarios

### Monitoring
- Implement metrics collection for job success rates
- Add alerting for provider health issues
- Track cost across providers for optimization

### Security
- Store cloud credentials securely (environment variables, secrets management)
- Implement proper authentication and authorization
- Validate all job configurations to prevent malicious use

### Cost Management
- Implement cost tracking and budgeting
- Add automatic job timeout and cleanup
- Monitor resource utilization across providers

## Future Enhancements

1. **Additional Providers**: AWS, Azure, Google Cloud
2. **Advanced Scheduling**: Multi-provider job scheduling based on cost/availability
3. **Auto-scaling**: Dynamic scaling based on job queue depth
4. **Spot Instances**: Support for spot/preemptible instances to reduce costs
5. **Job Templates**: Predefined job templates for common ML workloads
6. **Workflow Support**: Multi-step job workflows with dependencies
