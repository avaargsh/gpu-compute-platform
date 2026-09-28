"""Portable domain contracts for the AI compute control plane.

These models describe user intent. Provider-specific resource names, physical GPU
IDs, node names and scheduler decisions intentionally do not belong here.
"""

from enum import Enum
from typing import Any

from pydantic import BaseModel, Field


class WorkloadKind(str, Enum):
    TRAINING = "training"
    BATCH = "batch"
    WORKSPACE = "workspace"


class RuntimeKind(str, Enum):
    VLLM = "vllm"
    SGLANG = "sglang"


class AcceleratorClass(BaseModel):
    name: str
    vendor: str | None = None
    family: str | None = None
    memory_gb_min: int | None = Field(default=None, ge=1)
    count: int = Field(default=1, ge=1)
    dedicated: bool = True
    extensions: dict[str, Any] = Field(default_factory=dict)


class ComputePoolRef(BaseModel):
    name: str


class WorkloadSpec(BaseModel):
    name: str
    kind: WorkloadKind
    compute_pool: ComputePoolRef
    accelerator: AcceleratorClass
    image: str
    command: list[str] = Field(default_factory=list)
    env: dict[str, str] = Field(default_factory=dict)
    priority_class: str | None = None
    extensions: dict[str, Any] = Field(default_factory=dict)


class ModelRevisionRef(BaseModel):
    model: str
    revision: str


class ServingConfig(BaseModel):
    runtime: RuntimeKind
    accelerator: AcceleratorClass
    tensor_parallelism: int = Field(default=1, ge=1)
    pipeline_parallelism: int = Field(default=1, ge=1)
    expert_parallelism: int = Field(default=1, ge=1)
    quantization: str | None = None
    runtime_args: dict[str, Any] = Field(default_factory=dict)
    extensions: dict[str, Any] = Field(default_factory=dict)


class DeploymentSpec(BaseModel):
    name: str
    model_revision: ModelRevisionRef
    serving: ServingConfig
    compute_pool: ComputePoolRef
    replicas: int = Field(default=1, ge=0)
    extensions: dict[str, Any] = Field(default_factory=dict)


class EndpointSpec(BaseModel):
    name: str
    deployments: dict[str, int]
    rate_limit_rpm: int | None = Field(default=None, ge=1)
    extensions: dict[str, Any] = Field(default_factory=dict)
