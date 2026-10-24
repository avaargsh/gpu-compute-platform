# AI Compute Control Plane 快速启动

> 当前受支持实现是 **Go Control Plane + Cluster Agent**。仓库中的 Python/FastAPI 代码属于旧实现，不是当前 Golden Path，也不应作为新部署入口。

## 1. 前置环境

本地开发与完整验收需要：

- Go 1.27.3+
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
```

DEV 阶段优先只跑格式、静态检查、单元测试和构建。v0.1 正式发布合同、生产镜像策略与完整环境联调属于 RELEASE/TEST 门禁；不阻塞普通开发。

## 3. 启动 Control Plane

### 内存模式

未设置 `DATABASE_URL` 时，Control Plane 使用进程内 store，适合 API 开发和快速验证：

```bash
go run ./cmd/control-plane
```

默认仅监听回环地址（不会对局域网、NodePort 或公网开放）：

```text
http://127.0.0.1:8080
```

### 启动安全模式（S0）

Go Control Plane 现在在建立监听器和数据库连接前运行 fail-closed 配置校验：

| Profile | 监听 | 数据库 | 用途 |
| --- | --- | --- | --- |
| `development`（默认） | 仅 loopback | 可内存模式；兼容本地密码/非 TLS PostgreSQL | 本地开发与已有 Golden Path |
| `poc` | 仅 loopback | 必须 PostgreSQL URI + mTLS 客户端证书 + `sslmode=verify-full`，禁止密码及 PG 环境覆盖 | CNPG 隔离实验 |
| `production` | **直接拒绝启动** | — | Go Router 尚缺 OIDC/Project RBAC 和 Cluster Agent 身份认证 |

通过 `CONTROL_PLANE_PROFILE` 设置 profile。为了避免原有设置绕过安全边界，若旧 `ENVIRONMENT` 为 `production` 或 `staging`，也会强制走生产拒绝路径；两个环境变量冲突时直接报错。任何 profile 都不允许当前 Go API 监听非回环地址；这是一项临时安全阻断措施，**不是** OIDC 或 RBAC 的替代品。

例如，使用 CNPG 签发的 `platform_runtime` 证书测试 Go Store（证书必须已挂载，并且服务端证书的 SAN 必须匹配连接域名）：

```bash
export CONTROL_PLANE_PROFILE=poc
export CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080
export DATABASE_URL='postgres://platform_runtime@platform-db-rw.security.svc:5432/platform?sslmode=verify-full&sslrootcert=/etc/pg/ca.crt&sslcert=/etc/pg/tls.crt&sslkey=/etc/pg/tls.key'
go run ./cmd/control-plane
```

此例只规定客户端连接方式，并未创建 CNPG Cluster、角色、证书或数据库表。PoC Profile 会拒绝 `PGHOST/PGHOSTADDR/PGSERVICE/PGSSLMODE/PGOPTIONS` 等环境变量，以避免连接目标和 TLS 配置发生隐式变化。证书轮换、NetworkPolicy、备份和恢复尚需独立部署及验证。

**安全约束：当前 API 未完成生产认证，不得通过反向代理、隧道或端口转发开放给不受信任的访问者。**


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

## 开发、联调、发布三档门禁

- **DEV**：PR 继续执行 Go/Python 单测、PostgreSQL 隔离回归、
  Portal/Management 单测与构建、授权/CAS 等安全检查。
  默认不要求 kind/Kueue、真实 Chromium 或多个生产镜像构建通过。
- **TEST**：手动触发 `Go Control Plane` 与
  `Management Full Stack Real OIDC Acceptance` 的
  `workflow_dispatch`，在临时 kind、Keycloak、PostgreSQL、
  Chromium 中跑端到端联调，并标注真实依赖与结果。
- **RELEASE**：创建 `v*` tag 自动跑上述完整门禁；
  在对外宣称 production-ready 前必须重新运行
  `make acceptance-contract`、`make supply-chain-check`
  和容器镜像/安全检查，并保留产物证据。

不要把本地 fake GPU、Anvil、回归 fixture 或单机浏览器通过
写成真实 GPU、多机生产部署或支付终局性。真实安全边界、
认证拒绝、租约与 CAS 在开发期也必须保持 fail-closed。
