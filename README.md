# GPU Compute Platform MVP

一个基于FastAPI的GPU算力平台MVP，实现用户认证和管理功能。

## 功能特性

### ✅ 已实现功能
- **用户认证系统**: 基于FastAPI Users实现完整的用户注册、登录、JWT认证
- **用户管理**: 用户资料管理，包含姓名、组织、使用统计等信息
- **数据库支持**: 使用SQLAlchemy + Alembic进行数据库管理
- **API文档**: 自动生成的OpenAPI/Swagger文档
- **测试覆盖**: 完整的单元测试和集成测试

### 📋 计划功能 (按T0-T6周计划)
- **T3-T4周**: 任务调度系统 (Celery+Redis)
- **T3-T4周**: 前端Dashboard基础页面
- **T5-T6周**: 监控 & 成本统计
- **T5-T6周**: 外部GPU API集成

## 技术栈

- **后端框架**: FastAPI
- **数据库**: SQLite (开发) / PostgreSQL (生产)
- **认证**: FastAPI Users + JWT
- **数据库迁移**: Alembic
- **测试**: pytest + pytest-asyncio
- **包管理**: uv

## 快速开始

### 安装依赖

```bash
# 使用uv安装依赖
uv sync
```

### 数据库设置

```bash
# 运行数据库迁移
uv run alembic upgrade head
```

### 启动开发服务器

```bash
# 方式1: 使用启动脚本
uv run python run_dev.py

# 方式2: 直接使用uvicorn
uv run uvicorn app.main:app --reload --host 0.0.0.0 --port 8000
```

### 访问API

- **API文档**: http://localhost:8000/docs
- **ReDoc文档**: http://localhost:8000/redoc
- **健康检查**: http://localhost:8000/health

## API端点

### 认证相关
- `POST /auth/register` - 用户注册
- `POST /auth/jwt/login` - 用户登录
- `POST /auth/jwt/logout` - 用户登出

### 用户管理
- `GET /auth/users/me` - 获取当前用户信息
- `PATCH /auth/users/me` - 更新用户信息

### 受保护的路由
- `GET /api/protected-route` - 需要认证的示例路由
- `GET /api/admin-only` - 仅管理员可访问的路由

## 测试

```bash
# 运行所有测试
uv run pytest

# 运行特定测试
uv run pytest tests/test_auth.py -v
```

## 示例用法

### 用户注册

```bash
curl -X POST "http://localhost:8000/auth/register" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "securepassword123",
    "first_name": "张",
    "last_name": "三",
    "organization": "测试公司"
  }'
```

### 用户登录

```bash
curl -X POST "http://localhost:8000/auth/jwt/login" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=user@example.com&password=securepassword123"
```

### 访问受保护的路由

```bash
curl -X GET "http://localhost:8000/api/protected-route" \
  -H "Authorization: Bearer YOUR_ACCESS_TOKEN"
```
