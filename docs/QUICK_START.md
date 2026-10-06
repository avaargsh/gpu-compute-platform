# AI Compute Control Plane 快速启动

> 当前受支持实现是 **Go Control Plane + Cluster Agent**。仓库中的 Python/FastAPI 代码属于旧实现，不是当前 Golden Path，也不应作为新部署入口。

## 1. 前置环境

本地开发与完整验收需要：

- Go 1.24+
- Docker
- kind
- kubectl
- Helm

仅运行单元测试和本地 Control Plane 时不要求 kind/Kueue。

## 2. 本地质量门禁

```bash
make fmt-check
make vet
make test
make build
make acceptance-contract
make supply-chain-check
```

这些命令验证 Go 代码格式、静态检查、单元测试、构建、v0.1 生命周期/恢复合同和生产镜像策略。

## 3. 启动 Control Plane

### 内存模式

未设置 `DATABASE_URL` 时，Control Plane 使用进程内 store，适合 API 开发和快速验证：

```bash
go run ./cmd/control-plane
```

默认监听：

```text
http://127.0.0.1:8080
```

### PostgreSQL 模式

设置 PostgreSQL DSN 后，Control Plane 使用 PostgreSQL 作为管理面的 Source of Truth：

```bash
export DATABASE_URL='postgres://gpu:gpu@127.0.0.1:5432/gpu_platform?sslmode=disable'
go run ./cmd/control-plane
```

## 4. 构建容器

根目录 `Dockerfile` 是当前默认容器入口。

```bash
# 默认产出 control-plane 镜像
docker build -t gpu-control-plane:dev .

# 显式构建两个 runtime target
docker build --target control-plane -t gpu-control-plane:dev .
docker build --target cluster-agent -t gpu-cluster-agent:dev .
```

`Dockerfile.control-plane` 暂时保留为兼容入口，并由 CI 同步验证；新脚本和新部署应使用根 `Dockerfile`。

## 5. 运行真实 Golden Path

当前支持的执行路径只有 Kueue provider。完整验收会创建 kind 集群、安装 Kueue 与 Fake GPU Operator，并验证：

```text
Project
  -> ProjectBinding
  -> ComputePool
  -> ClusterBinding(provider=kueue)
  -> Workload
  -> Kueue admission
  -> Job / Pod
  -> Observation / Evidence
  -> Finalizer / Provider Cleanup
  -> Tombstone / Hard Delete
```

执行：

```bash
make e2e-golden
```

该路径同时覆盖 lease fencing、进程 takeover、lost-ACK/create-or-adopt、generation fencing 和删除恢复。

## 6. Cluster Agent

Cluster Agent 需要连接 Kubernetes，并显式指定集群和 Control Plane：

```bash
export CLUSTER_ID=kind-golden
export CONTROL_PLANE_URL=http://127.0.0.1:8080
export AGENT_INSTANCE_ID=agent-a
export AGENT_SYNC_INTERVAL=1s

go run ./cmd/cluster-agent
```

启动时 Agent 会发现本集群的 Kubernetes/Kueue/DRA/accelerator facts，并将它们作为 capability facts 注册到 Control Plane。发现结果不是调度决策，也不会自动选择 provider。

## 7. 当前支持边界

当前生产语义保持窄边界：

- PostgreSQL 是管理面 Source of Truth。
- Cluster Agent pull desired state 并执行 provider reconcile。
- Kueue 是唯一已注册 provider。
- Accelerator class 是可移植意图，由 binding 映射到 provider-specific resource/flavor。
- DRA、HAMi、KAI、Volcano、Serving、多集群 placement 暂不作为 v0.1 执行路径。
- Provider Adapter SPI 已建立，但第二 provider 必须独立通过 recovery/chaos/acceptance 后才能注册。

继续阅读：

- `docs/CONTROL_PLANE_V2_GO.md`
- `docs/WORKLOAD_LIFECYCLE.md`
- `docs/PROVIDER_RECOVERY_CONTRACT.md`
- `docs/PROVIDER_ADAPTER_SPI.md`
- `docs/RELEASE_ACCEPTANCE_V0_1.md`
- `docs/CAPABILITY_RELEASE_MATRIX.md`
