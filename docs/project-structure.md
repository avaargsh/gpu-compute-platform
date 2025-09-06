# 项目结构说明

本文档详细说明了 GPU Compute Platform 的项目结构和各目录的作用。

## 项目根目录结构

```
gpu-compute-platform/
├── alembic/                    # 数据库迁移管理
│   ├── versions/              # 数据库迁移版本文件
│   └── env.py                # Alembic 配置
├── app/                       # 主应用代码
│   ├── api/                   # API 路由模块
│   ├── core/                  # 核心功能模块
│   ├── crud/                  # 数据库 CRUD 操作
│   ├── gpu/                   # GPU 提供商适配器
│   ├── models/                # 数据库模型
│   ├── schemas/               # Pydantic 数据模型
│   └── tasks/                 # Celery 异步任务
├── docs/                      # 项目文档
├── examples/                  # 示例代码和使用案例
├── frontend/                  # Vue3 + TypeScript 前端
│   ├── src/                   # 前端源码
│   ├── tests/                # 前端测试
│   └── public/               # 静态资源
├── scripts/                   # 开发脚本
├── tests/                     # 后端测试代码
├── pyproject.toml            # Python 项目配置
├── uv.lock                   # UV 依赖锁定文件
└── README.md                 # 项目说明文档
```

## 详细目录说明

### `/app/` - 主应用代码

- **`api/`** - API 路由层
  - `auth.py` - 用户认证相关 API
  - `protected.py` - 受保护的示例 API
  - `gpu_jobs.py` - GPU 任务管理 API
  - `dag.py` - DAG 工作流 API

- **`core/`** - 核心功能模块
  - `config.py` - 应用配置管理
  - `database.py` - 数据库连接配置
  - `auth.py` - 认证核心逻辑
  - `scheduler.py` - 任务调度器
  - `websocket_manager.py` - WebSocket 连接管理
  - `celery_app.py` - Celery 异步任务配置

- **`gpu/`** - GPU 提供商适配层
  - `interface.py` - GPU 提供商统一接口定义
  - `providers/` - 各云厂商适配器实现
    - `alibaba.py` - 阿里云 ECS 适配器
    - `tencent.py` - 腾讯云 TKE 适配器
    - `runpod.py` - RunPod 适配器

- **`models/`** - 数据库模型
  - `user.py` - 用户模型
  - `task.py` - 任务模型
  - `dag.py` - DAG 工作流模型

- **`schemas/`** - API 数据模型
  - Pydantic 模型，用于 API 请求/响应数据验证

### `/frontend/` - 前端应用

基于 Vue 3 + TypeScript + Vite 构建的现代前端应用：

- **`src/components/`** - Vue 组件
- **`src/stores/`** - Pinia 状态管理
- **`src/services/`** - API 服务层
- **`tests/`** - 前端测试（Vitest）

### `/scripts/` - 开发脚本

便捷的开发和部署脚本：

- **`start_dev.sh`** - 启动完整开发环境（前端+后端）
- **`start_backend.sh`** - 仅启动后端服务
- **`run_tests.sh`** - 运行所有测试
- **`run_dev.py`** - 后端开发服务器启动脚本

### `/tests/` - 测试代码

全面的测试覆盖：

- 单元测试：各个模块的独立功能测试
- 集成测试：API 接口和数据库交互测试
- GPU 适配器测试：模拟各云厂商 API 调用

### `/docs/` - 项目文档

- **`gpu-providers.md`** - GPU 提供商适配器详细说明
- **`websocket-realtime-status.md`** - WebSocket 实时状态推送文档
- **`testing.md`** - 测试指南
- **`project-structure.md`** - 本文档

### `/examples/` - 示例代码

- **`example_gpu_usage.py`** - GPU 适配器使用示例
- **`example_runpod_usage.py`** - RunPod 适配器示例
- **`websocket_client_example.py`** - WebSocket 客户端示例

## 开发工作流

### 1. 开发环境启动

```bash
# 完整开发环境（推荐）
./scripts/start_dev.sh

# 仅后端服务
./scripts/start_backend.sh
```

### 2. 测试执行

```bash
# 运行所有测试
./scripts/run_tests.sh

# 运行特定测试
uv run pytest tests/test_auth.py -v
```

### 3. 代码结构约定

- **分层架构**: API → Service → Repository → Model
- **依赖注入**: 使用 FastAPI 的依赖注入系统
- **异步优先**: 所有 I/O 操作使用异步模式
- **类型安全**: 全面使用 Python 类型提示
- **错误处理**: 统一的异常处理和错误响应格式

### 4. 配置管理

项目配置通过环境变量和 `app/core/config.py` 进行管理：

- **开发环境**: 使用 SQLite 数据库
- **生产环境**: 支持 PostgreSQL
- **云厂商配置**: 通过环境变量传递 API 密钥
- **功能开关**: 支持通过配置启用/禁用特定功能

## 扩展指南

### 添加新的 GPU 提供商

1. 在 `app/gpu/providers/` 创建新的适配器文件
2. 实现 `GpuProviderInterface` 接口
3. 在 `tests/` 添加对应的测试文件
4. 更新文档说明

### 添加新的 API 端点

1. 在 `app/api/` 创建或修改路由文件
2. 在 `app/schemas/` 定义请求/响应模型
3. 在 `app/crud/` 实现数据库操作（如需要）
4. 添加相应的测试用例

### 前端功能扩展

1. 在 `frontend/src/components/` 添加新组件
2. 在 `frontend/src/stores/` 添加状态管理
3. 在 `frontend/src/services/` 添加 API 调用
4. 添加对应的测试用例
