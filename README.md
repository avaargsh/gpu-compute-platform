# GPU 计算平台 MVP

一个基于 FastAPI 的 GPU 算力平台 MVP，实现用户认证管理和统一的 GPU 提供商适配层。

## 项目概述

本项目是一个**MVP（最小可行产品）**，旨在构建一个完整的 GPU 算力平台，支持多云厂商的 GPU 资源统一管理。通过抽象的适配器模式，平台可以轻松集成不同云厂商的 GPU 服务，为用户提供统一的接口体验。

## 🚀 核心功能

### ✅ 已实现功能

#### 1. 用户认证与管理系统
- **完整认证流程**: 基于 FastAPI Users 实现用户注册、登录、JWT 令牌认证
- **用户资料管理**: 支持姓名、组织、使用统计等个人信息管理
- **权限控制**: 支持普通用户和管理员角色权限区分
- **会话管理**: JWT 令牌自动刷新和安全登出

#### 2. 统一 GPU 提供商适配层
- **抽象接口设计**: `GpuProviderInterface` 定义统一的 GPU 服务标准
- **多云厂商支持**:
  - **阿里云 ECS 适配器**: 基于 IaaS 模式，通过 GPU ECS 实例运行容器化任务
  - **腾讯云 TKE 适配器**: 基于 Kubernetes 模式，通过 GPU Job 调度算力任务
  - **RunPod 适配器**: 基于 Serverless GPU 的 GraphQL API，快速弹性运行
- **统一数据模型**: `GpuSpec`、`JobConfig`、`JobResult` 等标准化数据结构
- **任务生命周期管理**: 作业提交、状态查询、日志获取、费用估算
- **错误处理**: 统一异常处理机制和错误信息反馈

#### 3. 数据持久化与 API 服务
- **数据库支持**: SQLAlchemy + Alembic 实现数据模型和迁移管理
- **RESTful API**: 完整的 OpenAPI/Swagger 文档自动生成
- **异步处理**: 全异步架构，支持高并发请求
- **CORS 支持**: 跨域资源共享配置，方便前端集成

#### 4. 测试与质量保证
- **全面测试覆盖**: 73% 代码覆盖率，包含单元测试和集成测试
- **GPU 提供商测试**: 52 个测试用例覆盖所有 GPU 适配器功能
- **认证系统测试**: 完整的用户注册、登录、权限验证测试
- **模拟环境测试**: 无需真实云资源即可完成功能验证

#### 5. 任务调度与工作流
- **任务队列**: 已集成 Celery + Redis 异步任务队列
- **DAG 工作流**: 支持 DAG 定义/运行/状态跟踪（API: `/api/dag`）
- **Worker 路由**: GPU 任务与 DAG 任务分队列执行，支持优先级路由

### 📋 技术路线图

#### 近期计划（T3-T4周）
- **监控仪表板**: 基础的前端管理界面，展示任务状态和资源使用情况
- **成本优化**: 智能 GPU 实例选型和成本估算优化

#### 中期计划（T5-T6周）
- **更多云厂商**: 集成华为云、百度云等国内主流云厂商
- **高级监控**: 实时资源监控、性能指标收集和告警系统
- **批量任务**: 支持批量提交和管理 GPU 计算任务
- **API 网关**: 完善的 API 限流、鉴权和负载均衡

## 🛠 技术架构

### 核心技术栈
- **Web 框架**: FastAPI 0.116+ (高性能异步框架)
- **数据库**: SQLite (开发环境) / PostgreSQL (生产环境)
- **ORM**: SQLAlchemy 2.0+ (异步 ORM)
- **认证授权**: FastAPI Users + JWT (JSON Web Token)
- **数据库迁移**: Alembic (数据库版本管理)
- **测试框架**: pytest + pytest-asyncio (异步测试)
- **包管理**: uv (现代 Python 包管理器)

### GPU 提供商技术依赖
- **阿里云**: alibabacloud-ecs20140526（ECS GPU 实例管理）
- **腾讯云**: tencentcloud-sdk-python + kubernetes（TKE 集群管理）
- **RunPod**: runpod（Serverless GPU 平台，GraphQL API）
- **容器编排**: Docker + Kubernetes（统一的容器运行环境）
- **数据验证**: Pydantic 2.0+（类型安全和数据验证）

## 🚀 快速上手指南

### 环境要求
- **Python 版本**: >= 3.12
- **操作系统**: Linux / macOS / Windows
- **包管理器**: uv (推荐) 或 pip

### 1. 克隆项目并安装依赖

```bash path=null start=null
# 克隆仓库
git clone <repository-url>
cd gpu-compute-platform

# 使用 uv 安装依赖（推荐）
uv sync

# 或使用 pip 安装依赖
pip install -e .
```

### 2. 数据库初始化

```bash path=null start=null
# 运行数据库迁移，创建必要的表结构
uv run alembic upgrade head
```

### 3. 启动开发服务器

```bash
# 方式1: 使用项目提供的启动脚本（推荐）
uv run python run_dev.py

# 方式2: 直接使用 uvicorn
uv run uvicorn app.main:app --reload --host 0.0.0.0 --port 8000
```

### 3.1 启动 Redis 与 Celery Worker（异步任务与 DAG）

```bash
# 确保本地 Redis 已运行（默认 redis://localhost:6379/0）
# Ubuntu 可安装：sudo apt-get install -y redis-server

# 启动 Celery worker（默认队列 + GPU 任务队列）
uv run celery -A app.core.celery_app.celery_app worker -Q default,gpu_tasks -l info
```

### 4. 验证服务启动

服务启动后，访问以下地址验证功能：
- **API 交互文档**: http://localhost:8000/docs (Swagger UI)
- **API 文档**: http://localhost:8000/redoc (ReDoc)
- **健康检查**: http://localhost:8000/health
- **根端点**: http://localhost:8000/

## 📚 API 接口文档

### 🔐 用户认证接口
| 方法 | 路径 | 功能 | 说明 |
|------|------|------|------|
| `POST` | `/auth/register` | 用户注册 | 创建新用户账户 |
| `POST` | `/auth/jwt/login` | 用户登录 | 获取访问令牌 |
| `POST` | `/auth/jwt/logout` | 用户登出 | 撤销访问令牌 |

### 👤 用户管理接口
| 方法 | 路径 | 功能 | 说明 |
|------|------|------|------|
| `GET` | `/auth/users/me` | 获取用户信息 | 查看当前用户详情 |
| `PATCH` | `/auth/users/me` | 更新用户信息 | 修改用户资料 |

### 🛡️ 受保护的示例接口
| 方法 | 路径 | 功能 | 权限要求 |
|------|------|------|----------|
| `GET` | `/api/protected-route` | 认证示例 | 需要有效令牌 |
| `GET` | `/api/admin-only` | 管理员示例 | 需要管理员权限 |

## 🧪 测试与验证

### 运行测试用例

```bash
# 推荐：在测试模式下运行，避免连接外部 MLflow 服务
TESTING=true uv run pytest

# 运行特定模块测试
uv run pytest tests/test_auth.py -v                     # 认证功能测试
uv run pytest tests/test_gpu_providers.py -v           # GPU 提供商测试
uv run pytest tests/test_gpu_comprehensive.py -v       # GPU 综合测试

# 生成测试覆盖报告
uv run pytest --cov=app --cov-report=html
uv run pytest --cov=app --cov-report=term-missing
```

### 功能验证指令

```bash path=null start=null
# 测试 GPU 提供商适配器（使用模拟数据）
uv run python example_gpu_usage.py

# 手动测试 API 端点
curl -s http://localhost:8000/health | jq
```

## 📝 API 使用示例

### 1. 用户注册

```bash path=null start=null
curl -X POST "http://localhost:8000/auth/register" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "demo@example.com",
    "password": "securepassword123",
    "first_name": "张",
    "last_name": "三",
    "organization": "示例科技有限公司"
  }'
```

**预期返回**：
```json path=null start=null
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "email": "demo@example.com",
  "first_name": "张",
  "last_name": "三",
  "organization": "示例科技有限公司",
  "is_active": true,
  "is_superuser": false,
  "is_verified": false
}
```

### 2. 用户登录

```bash path=null start=null
curl -X POST "http://localhost:8000/auth/jwt/login" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=demo@example.com&password=securepassword123"
```

**预期返回**：
```json path=null start=null
{
  "access_token": "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9...",
  "token_type": "bearer"
}
```

### 3. 访问受保护的路由

```bash path=null start=null
# 使用获取的 access_token
TOKEN="your_access_token_here"
curl -X GET "http://localhost:8000/api/protected-route" \
  -H "Authorization: Bearer $TOKEN"
```

**预期返回**：
```json path=null start=null
{
  "message": "这是一个受保护的路由",
  "user": "demo@example.com"
}
```

## 📦 GPU 算力服务使用指南

### 支持的 GPU 提供商

#### 阿里云 ECS GPU 适配器
- **服务模式**: IaaS （基础设施即服务）
- **计算资源**: GPU ECS 实例（ecs.gn6i/gn6v/gn7i 系列）
- **容器支持**: Docker + NVIDIA Runtime 自动配置
- **支持的 GPU**: T4、V100、A100
- **优势**: 灵活的资源管理，支持自定义镜像

#### 腾讯云 TKE GPU 适配器  
- **服务模式**: PaaS （平台即服务）
- **计算资源**: Kubernetes GPU Job
- **容器支持**: 原生 Kubernetes + nvidia.com/gpu 资源管理
- **支持的 GPU**: T4、V100、A100、RTX 系列
- **优势**: Kubernetes 原生调度，自动扩缩容，资源共享

#### RunPod Serverless GPU 适配器
- **服务模式**: Serverless（按需付费、秒级弹性）
- **计算资源**: Pod 级 GPU 容器
- **接口协议**: GraphQL API（https://api.runpod.ai/graphql）
- **支持的 GPU**: A100、RTX 4090、A6000、T4 等
- **优势**: 快速供给、性价比高、API 简洁

### GPU 任务提交示例

```python path=example_gpu_usage.py start=44
# 定义 GPU 计算任务
job_config = JobConfig(
    name="pytorch-training",
    image="nvcr.io/nvidia/pytorch:24.02-py3",
    command=["python", "-c", "import torch; print(f'CUDA available: {torch.cuda.is_available()}'); print(f'GPU count: {torch.cuda.device_count()}')"],
    gpu_spec=GpuSpec(
        gpu_type="A100",
        gpu_count=1,
        memory_gb=40,
        vcpus=12,
        ram_gb=48
    ),
    environment={
        "NVIDIA_VISIBLE_DEVICES": "all",
        "CUDA_VISIBLE_DEVICES": "0"
    },
    timeout_minutes=30
)
```

### 🛜️ 配置项说明

项目配置主要通过环境变量进行管理，所有配置项都在 `app/core/config.py` 中定义。

#### 基础配置
| 配置项 | 环境变量 | 默认值 | 说明 |
|---------|-----------|-------|------|
| 数据库 URL | `DATABASE_URL` | `sqlite:///./database.db` | 数据库连接地址 |
| JWT 密钥 | `SECRET_KEY` | `auto-generated` | JWT 令牌签名密钥 |
| CORS 允许源 | `ALLOWED_ORIGINS` | `["*"]` | 跨域请求允许列表 |

#### GPU 提供商配置

**阿里云**：
```bash
export ALIBABA_ACCESS_KEY_ID="your_access_key"
export ALIBABA_ACCESS_KEY_SECRET="your_secret_key"
export ALIBABA_REGION_ID="cn-hangzhou"
export ALIBABA_SECURITY_GROUP_ID="sg-xxxxxxxxx"
export ALIBABA_VSWITCH_ID="vsw-xxxxxxxxx"
export ALIBABA_KEY_PAIR_NAME="gpu-compute-keypair"
```

**腾讯云**：
```bash
export TENCENT_SECRET_ID="your_secret_id"
export TENCENT_SECRET_KEY="your_secret_key"
export TENCENT_REGION="ap-shanghai"
export TENCENT_CLUSTER_ID="cls-xxxxxx"
# 可选：提供 base64 编码的 kubeconfig
export TENCENT_KUBECONFIG="base64_encoded_kubeconfig"
```

**RunPod**：
```bash
export RUNPOD_API_KEY="your_runpod_api_key"
# 可选：现有 Serverless endpoint ID
export RUNPOD_ENDPOINT_ID="your_endpoint_id"
```

**Celery/Redis**（可选覆盖默认值）：
```bash
export CELERY_BROKER_URL="redis://localhost:6379/0"
export CELERY_RESULT_BACKEND="redis://localhost:6379/0"
```

## 📊 项目结构

```
gpu-compute-platform/
├── app/                     # 主应用模块
│   ├── api/                 # API 路由模块
│   │   ├── auth.py          # 认证相关 API
│   │   ├── protected.py     # 受保护的 API
│   │   ├── gpu_jobs.py      # GPU 作业提交/调度 API
│   │   └── dag.py           # DAG 工作流 API
│   ├── core/                # 核心配置模块
│   │   ├── auth.py          # 认证配置
│   │   ├── config.py        # 应用配置
│   │   ├── database.py      # 数据库配置
│   │   ├── celery_app.py    # Celery 应用与队列配置
│   │   └── dag_engine.py    # DAG 执行引擎
│   ├── gpu/                 # GPU 提供商适配层
│   │   ├── interface.py     # 统一接口定义
│   │   └── providers/       # 各个云厂商适配器
│   │       ├── alibaba.py   # 阿里云 ECS 适配器
│   │       ├── tencent.py   # 腾讯云 TKE 适配器
│   │       └── runpod.py    # RunPod Serverless 适配器
│   ├── models/              # 数据模型
│   │   ├── user.py          # 用户模型
│   │   ├── dag.py           # DAG/节点/运行模型
│   │   └── task.py          # GPU 任务模型
│   ├── schemas/             # API 数据模式
│   │   └── user.py          # 用户数据模式
│   └── main.py              # FastAPI 应用入口
├── tests/                   # 测试文件
│   ├── ...                  # 各模块单测/集成测试
├── docs/                    # 文档文件
│   ├── gpu-providers.md     # GPU 提供商文档
│   └── testing.md           # 测试说明文档
├── alembic/                 # 数据库迁移文件
├── example_gpu_usage.py     # GPU 使用示例（通用）
├── example_runpod_usage.py  # RunPod 使用示例
├── run_dev.py               # 开发服务器启动脚本
├── pyproject.toml           # Python 项目配置
├── pytest.ini               # pytest 配置
└── README.md                # 项目说明文档
```

## 📚 扩展阅读

### 详细文档
- [GPU 提供商适配器设计](docs/gpu-providers.md)
- [测试策略和测试用例](docs/testing.md)

### 相关资源
- [FastAPI 官方文档](https://fastapi.tiangolo.com/)
- [FastAPI Users 文档](https://fastapi-users.github.io/fastapi-users/)
- [SQLAlchemy 2.0 文档](https://docs.sqlalchemy.org/en/20/)
- [Alembic 文档](https://alembic.sqlalchemy.org/)
- [uv 包管理器](https://github.com/astral-sh/uv)

### 云厂商 API 文档
- [阿里云 ECS API](https://help.aliyun.com/product/25365.html)
- [腾讯云 TKE API](https://cloud.tencent.com/document/product/457)
- [Kubernetes API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/)

## 🚀 贡献指南

### 开发环境搭建

1. **Fork 项目**并克隆到本地
2. **创建新分支**: `git checkout -b feature/new-provider`
3. **安装开发依赖**: `uv sync --dev`
4. **运行测试**: `uv run pytest`
5. **提交变更**: `git commit -m "feat: add new GPU provider"`
6. **提交 PR**: 创建 Pull Request

### 添加新的 GPU 提供商

1. 在 `app/gpu/providers/` 目录下创建新的适配器文件
2. 继承 `GpuProviderInterface` 并实现所有抽象方法
3. 在 `tests/` 目录下创建对应的测试文件
4. 更新文档和示例代码

### 代码质量要求

- **类型注释**: 所有公共方法必须有类型注释
- **测试覆盖**: 新功能必须有对应的测试用例
- **文档更新**: 重要功能变更需要更新文档
- **错误处理**: 适当的异常处理和日志记录

---

## 📞 联系和支持

如果您在使用过程中遇到问题或有改进建议，欢迎：

- **提交 Issue**: 在 GitHub 仓库中创建问题报告
- **参与讨论**: 加入项目讨论组或开发者社区  
- **贡献代码**: Fork 项目并提交 Pull Request

**项目状态**: MVP 阶段，欢迎反馈和贡献 🚀

---

*最后更新: 2025-01*
