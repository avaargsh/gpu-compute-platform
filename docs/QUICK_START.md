# GPU 计算平台快速启动指南

## 🚀 快速开始

### 1. 环境准备

确保你的系统已安装：
- Python 3.8+
- Node.js 16+
- PostgreSQL (或其他支持的数据库)

### 2. 后端启动

```bash
# 1. 安装依赖
cd gpu-compute-platform
pip install -r requirements.txt

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env 文件，配置数据库连接等信息

# 3. 初始化数据库（使用Alembic迁移）
python manage_db_v2.py migrate

# 4. 创建演示数据
python manage_db_v2.py demo-data

# 5. 启动后端服务
uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload
```

### 3. 前端启动

```bash
# 1. 进入前端目录
cd frontend

# 2. 安装依赖
npm install

# 3. 启动开发服务器
npm run dev
```

### 4. 访问系统

- **前端界面**: http://localhost:3000
- **API文档**: http://localhost:8000/docs
- **API Redoc**: http://localhost:8000/redoc

## 🔐 演示账户

系统初始化后，你可以使用以下账户登录：

| 角色 | 邮箱 | 密码 | 权限 |
|------|------|------|------|
| 管理员 | admin@example.com | admin123 | 全部功能 |
| 普通用户 | user@example.com | user123 | 基础功能 |
| 测试用户1 | alice@example.com | alice123 | 基础功能 |
| 测试用户2 | bob@example.com | bob123 | 基础功能 |

## 📋 主要功能

### 用户功能
- ✅ 用户注册和登录
- ✅ 个人信息管理
- ✅ 密码修改
- ✅ 角色权限控制

### 任务管理
- ✅ 创建GPU计算任务
- ✅ 查看任务列表和详情
- ✅ 任务状态实时更新
- ✅ 任务日志查看
- ✅ 任务操作（取消、重启、删除）

### 系统管理（管理员）
- ✅ 用户管理
- ✅ 系统统计
- ✅ 云服务商配置
- ✅ GPU资源管理

## 🛠 数据库管理工具

使用 `manage_db.py` 脚本可以方便地管理数据库：

```bash
# 查看数据库状态
python manage_db.py status

# 初始化数据库表结构
python manage_db.py init

# 创建演示数据
python manage_db.py demo-data

# 显示演示用户信息
python manage_db.py demo-users

# 重置数据库（谨慎使用）
python manage_db.py reset

# 删除所有表（谨慎使用）
python manage_db.py drop
```

## 🔧 配置说明

### 后端配置 (`.env`)

```env
# 数据库配置
DATABASE_URL=postgresql+asyncpg://user:password@localhost/gpu_platform

# JWT配置
SECRET_KEY=your-secret-key-here
ALGORITHM=HS256
ACCESS_TOKEN_EXPIRE_MINUTES=30

# CORS配置
CORS_ORIGINS=["http://localhost:3000"]

# 其他配置
DEBUG=true
```

### 前端配置

前端的API基础URL配置在 `frontend/src/config/index.ts` 中：

```typescript
export const API_BASE_URL = 'http://localhost:8000'
```

## 📝 API 接口

### 认证接口
- `POST /auth/login` - 用户登录
- `POST /auth/register` - 用户注册
- `POST /auth/logout` - 用户登出
- `GET /auth/me` - 获取当前用户信息
- `PUT /auth/profile` - 更新用户资料
- `PUT /auth/password` - 修改密码

### 任务管理接口
- `GET /tasks/` - 获取任务列表
- `POST /tasks/` - 创建新任务
- `GET /tasks/{task_id}` - 获取任务详情
- `PUT /tasks/{task_id}` - 更新任务
- `DELETE /tasks/{task_id}` - 删除任务
- `POST /tasks/{task_id}/cancel` - 取消任务
- `POST /tasks/{task_id}/restart` - 重启任务

### 云服务商接口
- `GET /providers/` - 获取云服务商列表
- `GET /providers/{provider}/gpus` - 获取GPU型号列表
- `GET /providers/images` - 获取Docker镜像列表
- `GET /providers/pricing` - 获取价格信息

## 🐛 故障排除

### 数据库连接问题
1. 检查PostgreSQL是否正在运行
2. 确认`.env`文件中的数据库配置正确
3. 运行 `python manage_db.py status` 检查连接

### 前端无法连接后端
1. 确认后端服务已启动（默认端口8000）
2. 检查前端配置中的API地址
3. 检查CORS配置是否正确

### 用户无法登录
1. 确认账户信息正确
2. 检查是否已运行 `python manage_db.py demo-data`
3. 查看后端日志获取详细错误信息

## 📚 更多信息

- 查看 `README.md` 了解详细的项目架构
- 访问 http://localhost:8000/docs 查看完整API文档
- 检查各模块的代码注释获取技术细节

## 🔄 开发流程

1. **后端开发**：修改 `app/` 目录下的代码
2. **前端开发**：修改 `frontend/` 目录下的代码
3. **数据库变更**：使用Alembic进行迁移
4. **测试**：运行相应的测试套件

---

🎉 **恭喜！你现在可以开始使用GPU计算平台了！**
