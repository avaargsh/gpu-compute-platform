# GPU 计算平台 - Docker 开发环境设置指南

## 📖 概述

本文档描述如何使用 Docker 和 Docker Compose 快速启动 GPU 计算平台的开发环境。

## 🚀 快速开始

### 前置要求

1. **Docker** 和 **Docker Compose** 已安装
2. **NVIDIA Container Toolkit** (如需要GPU支持)
3. **至少 4GB 可用内存**

### 1. 环境配置

```bash
# 复制环境变量模板
cp .env.example .env

# 编辑环境变量 (可选，默认配置已可工作)
vi .env
```

### 2. 启动开发环境

```bash
# 启动开发环境 (包含热重载)
./scripts/start-docker.sh dev

# 或启动生产环境
./scripts/start-docker.sh prod

# 带监控服务启动
./scripts/start-docker.sh dev --with-monitoring
```

### 3. 验证服务

启动成功后，访问以下服务：

- **🔗 API 服务**: http://localhost:8000
- **📚 API 文档**: http://localhost:8000/docs
- **🎨 前端界面**: http://localhost:3000 (仅开发模式)
- **📊 MLflow**: http://localhost:5000
- **🗄️ 数据库**: postgresql://postgres:postgres@localhost:5432/gpu_platform

## 🏗️ 架构说明

### 生产环境 (`docker-compose.yml`)

```mermaid
graph TD
    A[Load Balancer] --> B[FastAPI App]
    B --> C[PostgreSQL]
    B --> D[Redis]
    B --> E[MLflow]
    F[Celery Worker] --> D
    F --> C
    G[Prometheus] --> B
    H[Grafana] --> G
```

**服务组件**:
- **app**: 主应用服务 (FastAPI + Vue.js 构建版本)
- **celery-worker**: 后台任务处理
- **postgres**: PostgreSQL 数据库
- **redis**: 缓存和消息队列
- **mlflow**: 机器学习实验跟踪
- **prometheus/grafana**: 监控服务 (可选)

### 开发环境 (`docker-compose.dev.yml`)

**特点**:
- 热重载支持
- 源码挂载
- 独立前端开发服务器
- 调试工具集成

## 🐳 Docker 镜像说明

### 多阶段构建 (`Dockerfile`)

1. **frontend-builder**: 构建 Vue.js 前端
2. **python-base**: 基础 Python + CUDA 环境
3. **deps**: 安装 Python 依赖
4. **app**: 最终应用镜像

### 优化特性

- **多阶段构建**: 减少最终镜像大小
- **Layer 缓存**: 依赖层缓存优化构建速度
- **非 root 用户**: 安全性提升
- **健康检查**: 自动服务健康监测

## 📋 常用命令

### 服务管理

```bash
# 查看服务状态
docker-compose -f docker-compose.dev.yml ps

# 查看服务日志
docker-compose -f docker-compose.dev.yml logs -f app-dev

# 重启单个服务
docker-compose -f docker-compose.dev.yml restart app-dev

# 停止所有服务
docker-compose -f docker-compose.dev.yml down

# 清理数据和网络
docker-compose -f docker-compose.dev.yml down -v
```

### 数据库操作

```bash
# 连接数据库
docker-compose -f docker-compose.dev.yml exec postgres psql -U postgres -d gpu_platform

# 运行数据库迁移
docker-compose -f docker-compose.dev.yml exec app-dev alembic upgrade head

# 创建新迁移
docker-compose -f docker-compose.dev.yml exec app-dev alembic revision --autogenerate -m "Add new table"
```

### 调试和开发

```bash
# 进入应用容器
docker-compose -f docker-compose.dev.yml exec app-dev bash

# 运行测试
docker-compose -f docker-compose.dev.yml exec app-dev pytest

# 安装新的 Python 包
docker-compose -f docker-compose.dev.yml exec app-dev uv pip install package-name

# 启动 Jupyter Notebook (可选)
docker-compose -f docker-compose.dev.yml --profile jupyter up jupyter
```

### 前端开发

```bash
# 查看前端日志
docker-compose -f docker-compose.dev.yml logs -f frontend-dev

# 安装前端依赖
docker-compose -f docker-compose.dev.yml exec frontend-dev npm install package-name

# 构建前端
docker-compose -f docker-compose.dev.yml exec frontend-dev npm run build
```

## 🔧 环境变量配置

### 核心配置

| 变量名 | 描述 | 默认值 | 示例 |
|--------|------|--------|------|
| `ENVIRONMENT` | 运行环境 | `development` | `production` |
| `DATABASE_URL` | 数据库连接 | `postgresql://...` | `sqlite:///./db.sqlite` |
| `SECRET_KEY` | 应用密钥 | `dev-secret-key...` | `your-secret-key` |
| `DEBUG` | 调试模式 | `true` | `false` |

### GPU 提供商配置

配置相应的云厂商 API 密钥以启用 GPU 功能：

```bash
# 阿里云
ALIBABA_ACCESS_KEY_ID=your_key
ALIBABA_ACCESS_KEY_SECRET=your_secret

# 腾讯云
TENCENT_SECRET_ID=your_id
TENCENT_SECRET_KEY=your_key

# RunPod
RUNPOD_API_KEY=your_api_key
```

## 🚨 故障排除

### 常见问题

1. **端口冲突**
   ```bash
   # 检查端口使用情况
   netstat -tlnp | grep :8000
   # 修改 docker-compose.yml 中的端口映射
   ```

2. **内存不足**
   ```bash
   # 检查 Docker 内存限制
   docker info | grep Memory
   # 增加 Docker Desktop 内存配置
   ```

3. **GPU 支持问题**
   ```bash
   # 检查 NVIDIA Docker 支持
   docker info | grep nvidia
   # 安装 nvidia-container-toolkit
   ```

4. **数据库连接失败**
   ```bash
   # 检查数据库容器状态
   docker-compose -f docker-compose.dev.yml logs postgres
   # 重启数据库服务
   docker-compose -f docker-compose.dev.yml restart postgres
   ```

### 清理和重置

```bash
# 完全清理环境
docker-compose -f docker-compose.dev.yml down -v --remove-orphans
docker system prune -a

# 重新构建镜像
docker-compose -f docker-compose.dev.yml build --no-cache

# 重置数据库
docker-compose -f docker-compose.dev.yml exec postgres dropdb gpu_platform -U postgres
docker-compose -f docker-compose.dev.yml exec postgres createdb gpu_platform -U postgres
```

## 📊 监控和日志

### 服务监控

启用监控服务后，可访问：
- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)

### 日志管理

```bash
# 查看所有服务日志
docker-compose logs -f

# 查看特定服务日志
docker-compose logs -f app-dev

# 日志过滤
docker-compose logs --since 1h app-dev
```

## 🔐 安全配置

### 生产环境安全清单

- [ ] 更改默认数据库密码
- [ ] 设置强密码 SECRET_KEY
- [ ] 配置防火墙规则
- [ ] 启用 HTTPS
- [ ] 限制 CORS 源
- [ ] 设置 Redis 密码
- [ ] 配置日志轮转

### 开发环境注意事项

- 使用默认密码仅适用于开发环境
- 不要在生产环境中启用 DEBUG 模式
- 定期更新依赖版本
- 使用 `.env` 文件管理敏感配置

## 🤝 贡献指南

1. Fork 项目
2. 创建特性分支: `git checkout -b feature/new-feature`
3. 在开发环境中测试更改
4. 提交更改: `git commit -am 'Add new feature'`
5. 推送分支: `git push origin feature/new-feature`
6. 创建 Pull Request

---

如有问题，请查看项目主 README 或提交 issue。
