"""Provider-neutral serving topology expansion."""

from __future__ import annotations

from dataclasses import dataclass

from app.control_plane.domain import DeploymentSpec, ServingRole, WorkerPoolSpec


@dataclass(frozen=True)
class ResolvedWorkerPool:
    name: str
    role: ServingRole
    replicas: int
    tensor_parallelism: int
    accelerator_count: int


class ServingTopologyResolver:
    def resolve(self, deployment: DeploymentSpec) -> list[ResolvedWorkerPool]:
        topology = deployment.serving.topology
        if topology is None:
            return [
                ResolvedWorkerPool(
                    name=deployment.name,
                    role=ServingRole.UNIFIED,
                    replicas=deployment.replicas,
                    tensor_parallelism=deployment.serving.tensor_parallelism,
                    accelerator_count=deployment.serving.accelerator.count,
                )
            ]

        return [
            self._resolve_pool(deployment.name, pool)
            for pool in topology.worker_pools
        ]

    @staticmethod
    def _resolve_pool(deployment_name: str, pool: WorkerPoolSpec) -> ResolvedWorkerPool:
        return ResolvedWorkerPool(
            name=f"{deployment_name}-{pool.role.value}",
            role=pool.role,
            replicas=pool.replicas,
            tensor_parallelism=pool.tensor_parallelism,
            accelerator_count=pool.accelerator.count,
        )
