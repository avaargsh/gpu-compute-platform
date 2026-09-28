"""Portable domain contracts for the AI compute control plane.

These models describe user intent. Provider-specific resource names, physical GPU
IDs, node names and scheduler decisions intentionally do not belong here.
"""

from enum import Enum
from typing import Any

from pydantic import BaseModel, Field, model_validator


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


class ServingRole(str, Enum):
    UNIFIED = "unified"
    PREFILL = "prefill"
    DECODE = "decode"


class WorkerPoolSpec(BaseModel):
    role: ServingRole
    accelerator: AcceleratorClass
    replicas: int = Field(default=1, ge=0)
    tensor_parallelism: int = Field(default=1, ge=1)


class ServingTopology(BaseModel):
    worker_pools: list[WorkerPoolSpec]

    @model_validator(mode="after")
    def validate_roles(self):
        roles = [pool.role for pool in self.worker_pools]
        if ServingRole.UNIFIED in roles and len(roles) != 1:
            raise ValueError("unified topology cannot be mixed with prefill/decode pools")
        if ServingRole.UNIFIED not in roles:
            if roles.count(ServingRole.PREFILL) != 1 or roles.count(ServingRole.DECODE) != 1:
                raise ValueError("disaggregated topology requires one prefill and one decode pool")
        return self


class ServingConfig(BaseModel):
    runtime: RuntimeKind
    accelerator: AcceleratorClass
    tensor_parallelism: int = Field(default=1, ge=1)
    pipeline_parallelism: int = Field(default=1, ge=1)
    expert_parallelism: int = Field(default=1, ge=1)
    quantization: str | None = None
    topology: ServingTopology | None = None
    runtime_args: dict[str, Any] = Field(default_factory=dict)
    extensions: dict[str, Any] = Field(default_factory=dict)


class DeploymentSpec(BaseModel):
    name: str
    model_revision: ModelRevisionRef
    serving: ServingConfig
    compute_pool: ComputePoolRef
    replicas: int = Field(default=1, ge=0)
    extensions: dict[str, Any] = Field(default_factory=dict)


class TrafficTarget(BaseModel):
    deployment: str
    weight: int = Field(ge=0, le=100)
    shadow: bool = False


class EndpointSpec(BaseModel):
    name: str
    targets: list[TrafficTarget]
    hostname: str | None = None
    rate_limit_rpm: int | None = Field(default=None, ge=1)
    extensions: dict[str, Any] = Field(default_factory=dict)

    @model_validator(mode="after")
    def validate_traffic(self):
        primary = [target for target in self.targets if not target.shadow]
        if not primary:
            raise ValueError("endpoint requires at least one non-shadow traffic target")
        if sum(target.weight for target in primary) != 100:
            raise ValueError("non-shadow traffic weights must sum to 100")
        return self
