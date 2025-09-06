"""
GPU Provider Interface - Unified API Layer

This module defines the abstract interface that all GPU providers must implement.
It provides a standardized way to interact with different cloud platforms for GPU compute.
"""

from abc import ABC, abstractmethod
from typing import Dict, Any, Optional, List
from enum import Enum
from pydantic import BaseModel
from datetime import datetime


class JobStatus(str, Enum):
    """Standard job statuses across all providers."""
    PENDING = "pending"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"
    UNKNOWN = "unknown"


class GpuSpec(BaseModel):
    """Standard GPU specification."""
    gpu_type: str  # e.g., "V100", "A100", "T4"
    gpu_count: int
    memory_gb: int
    vcpus: int
    ram_gb: int


class JobConfig(BaseModel):
    """Standard job configuration."""
    name: str
    image: str  # Docker image
    command: List[str]
    gpu_spec: GpuSpec
    environment: Optional[Dict[str, str]] = None
    volumes: Optional[Dict[str, str]] = None
    timeout_minutes: Optional[int] = None
    retry_count: int = 0


class JobResult(BaseModel):
    """Standard job result."""
    job_id: str
    status: JobStatus
    created_at: datetime
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    logs: Optional[str] = None
    exit_code: Optional[int] = None
    error_message: Optional[str] = None


class CostInfo(BaseModel):
    """Standard cost information."""
    job_id: str
    total_cost: float
    currency: str
    cost_breakdown: Dict[str, float]
    billing_period: str


class GpuProviderInterface(ABC):
    """
    Abstract base class defining the unified interface for GPU providers.
    
    All GPU provider adapters must implement this interface to ensure
    consistent behavior across different cloud platforms.
    """
    
    def __init__(self, config: Dict[str, Any]):
        """
        Initialize the provider with configuration.
        
        Args:
            config: Provider-specific configuration dictionary
        """
        self.config = config
    
    @abstractmethod
    async def submit_job(self, job_config: JobConfig) -> str:
        """
        Submit a GPU job for execution.
        
        Args:
            job_config: Job configuration including GPU requirements and commands
            
        Returns:
            job_id: Unique identifier for the submitted job
            
        Raises:
            ProviderError: If job submission fails
        """
        pass
    
    @abstractmethod
    async def get_job_status(self, job_id: str) -> JobResult:
        """
        Get the current status of a job.
        
        Args:
            job_id: Unique identifier of the job
            
        Returns:
            JobResult: Current job status and metadata
            
        Raises:
            ProviderError: If status retrieval fails
            JobNotFoundError: If job doesn't exist
        """
        pass
    
    @abstractmethod
    async def cancel_job(self, job_id: str) -> bool:
        """
        Cancel a running job.
        
        Args:
            job_id: Unique identifier of the job to cancel
            
        Returns:
            bool: True if cancellation was successful
            
        Raises:
            ProviderError: If cancellation fails
            JobNotFoundError: If job doesn't exist
        """
        pass
    
    @abstractmethod
    async def get_job_logs(self, job_id: str, lines: Optional[int] = None) -> str:
        """
        Get logs for a job.
        
        Args:
            job_id: Unique identifier of the job
            lines: Number of recent log lines to retrieve (None for all)
            
        Returns:
            str: Job logs
            
        Raises:
            ProviderError: If log retrieval fails
            JobNotFoundError: If job doesn't exist
        """
        pass
    
    @abstractmethod
    async def get_cost_info(self, job_id: str) -> CostInfo:
        """
        Get cost information for a job.
        
        Args:
            job_id: Unique identifier of the job
            
        Returns:
            CostInfo: Cost breakdown and total cost
            
        Raises:
            ProviderError: If cost retrieval fails
            JobNotFoundError: If job doesn't exist
        """
        pass
    
    @abstractmethod
    async def list_available_gpus(self) -> List[GpuSpec]:
        """
        List available GPU specifications.
        
        Returns:
            List[GpuSpec]: Available GPU configurations
            
        Raises:
            ProviderError: If GPU listing fails
        """
        pass
    
    @abstractmethod
    async def health_check(self) -> Dict[str, Any]:
        """
        Check provider health and connectivity.
        
        Returns:
            Dict: Health status information
            
        Raises:
            ProviderError: If health check fails
        """
        pass


class ProviderError(Exception):
    """Base exception for GPU provider errors."""
    
    def __init__(self, message: str, provider: str, error_code: Optional[str] = None):
        super().__init__(message)
        self.provider = provider
        self.error_code = error_code


class JobNotFoundError(ProviderError):
    """Exception raised when a job is not found."""
    
    def __init__(self, job_id: str, provider: str):
        super().__init__(f"Job {job_id} not found", provider, "JOB_NOT_FOUND")
        self.job_id = job_id


class InsufficientResourcesError(ProviderError):
    """Exception raised when insufficient GPU resources are available."""
    
    def __init__(self, requested_spec: GpuSpec, provider: str):
        super().__init__(
            f"Insufficient resources for {requested_spec.gpu_type} x{requested_spec.gpu_count}",
            provider,
            "INSUFFICIENT_RESOURCES"
        )
        self.requested_spec = requested_spec
