# Go Control Plane 容器构建与本地验证

> 当前默认运行时是 **Go Control Plane + Cluster Agent**。旧 Python/FastAPI Compose 栈不属于当前 Golden Path；不要把 `docker-compose.yml` / `docker-compose.dev.yml` 当作 v0.1 发布入口。

## 1. 构建镜像

根目录 `Dockerfile` 是新部署与开发脚本的默认入口。

```bash
# 默认镜像：control-plane
docker build -t gpu-control-plane:dev .

# 显式 target
docker build --target control-plane -t gpu-control-plane:dev .
docker build --target cluster-agent -t gpu-cluster-agent:dev .
```

`Dockerfile.control-plane` 暂时作为兼容构建入口保留，CI 会同时验证；后续新引用应统一使用根 `Dockerfile`。

## 2. 运行 Control Plane

Control Plane 默认监听 8080。

### 无 PostgreSQL

```bash
docker run --rm -p 8080:8080 gpu-control-plane:dev
```

未设置 `DATABASE_URL` 时使用内存 store，仅适合本地开发和 API 验证。

### 使用 PostgreSQL

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL='postgres://gpu:gpu@host.docker.internal:5432/gpu_platform?sslmode=disable' \
  gpu-control-plane:dev
```

生产/发布验收语义以 PostgreSQL Source of Truth 为准。

## 3. Cluster Agent

Cluster Agent 不是独立的云 GPU broker。它连接一个 Kubernetes 集群，发现 capability facts，并按冻结的 provider identity 执行 reconcile。

运行至少需要：

```text
CLUSTER_ID
CONTROL_PLANE_URL
```

可选：

```text
AGENT_INSTANCE_ID
AGENT_SYNC_INTERVAL
```

由于 Agent 需要真实 Kubernetes API，手工 `docker run` 还需正确挂载 kubeconfig / service-account 环境。仓库推荐优先使用完整 Golden Path 验证，而不是维护另一套 Docker-only 假环境。

## 4. 推荐验收路径

```bash
make fmt-check
make vet
make test
make build
make acceptance-contract
make supply-chain-check
make e2e-golden
```

`make e2e-golden` 会：

1. 创建/复用 `kind-golden`；
2. 安装 Kueue；
3. 安装 Fake GPU Operator；
4. 注册稳定 H100 capability；
5. 启动 Go Control Plane 与两个 Cluster Agent 进程；
6. 验证 desired -> provider side effect -> observation/evidence；
7. 验证 lease fencing、takeover、generation fencing；
8. 验证 provider cleanup、tombstone 与 hard delete。

这是当前最可信的“能否工作”证据。

## 5. 容器职责边界

### control-plane

负责：

- REST API；
- Desired State；
- Project/Cluster binding；
- PostgreSQL persistence；
- reconcile lease authority；
- observation/evidence ingestion；
- finalization/tombstone。

不负责 GPU 驱动、CUDA runtime 或 scheduler 实现。

### cluster-agent

负责：

- Kubernetes capability discovery；
- desired-state pull；
- provider adapter dispatch；
- Kueue/Kubernetes projection；
- downstream observation；
- evidence reporting。

当前仅注册 `kueue` adapter。

## 6. 旧 Compose 文件

仓库仍有早期 Python/FastAPI 时代的 `docker-compose.yml`、`docker-compose.dev.yml` 及相关脚本/前端代码。它们用于历史迁移参考，不在当前 CI/Golden Path 支持范围内。

不要基于这些文件判断当前系统的：

- API 端口与接口；
- provider 模型；
- Celery/Redis 依赖；
- 云厂商调度能力；
- 发布可用性。

当前事实以 `README.md`、`CONTROL_PLANE_V2_GO.md`、`PROVIDER_ADAPTER_SPI.md` 和 `RELEASE_ACCEPTANCE_V0_1.md` 为准。
