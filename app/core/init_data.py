"""
数据库初始化和演示数据创建脚本
"""
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from datetime import datetime, timezone
from fastapi_users.password import PasswordHelper
import json
import uuid

from app.models.user import User, UserRole
from app.models.task import GpuTask, TaskStatus, TaskPriority
from app.core.database import get_async_session


async def create_demo_users(session: AsyncSession):
    """创建演示用户"""
    password_helper = PasswordHelper()
    
    # 检查管理员用户是否已存在
    admin_stmt = select(User).where(User.email == "admin@example.com")
    admin_result = await session.execute(admin_stmt)
    admin_user = admin_result.scalar_one_or_none()
    
    if not admin_user:
        # 创建管理员用户
        admin_user = User(
            email="admin@example.com",
            hashed_password=password_helper.hash("admin123"),
            nickname="管理员",
            role=UserRole.ADMIN,
            is_active=True,
            is_verified=True,
            created_at=datetime.now(timezone.utc)
        )
        session.add(admin_user)
        print("✓ 创建管理员用户: admin@example.com / admin123")
    
    # 检查普通用户是否已存在
    user_stmt = select(User).where(User.email == "user@example.com")
    user_result = await session.execute(user_stmt)
    regular_user = user_result.scalar_one_or_none()
    
    if not regular_user:
        # 创建普通用户
        regular_user = User(
            email="user@example.com",
            hashed_password=password_helper.hash("user123"),
            nickname="测试用户",
            role=UserRole.USER,
            is_active=True,
            is_verified=True,
            created_at=datetime.now(timezone.utc)
        )
        session.add(regular_user)
        print("✓ 创建普通用户: user@example.com / user123")
    
    # 创建额外的测试用户
    test_users = [
        {
            "email": "alice@example.com",
            "password": "alice123",
            "nickname": "Alice",
            "role": UserRole.USER
        },
        {
            "email": "bob@example.com", 
            "password": "bob123",
            "nickname": "Bob",
            "role": UserRole.USER
        }
    ]
    
    for user_data in test_users:
        stmt = select(User).where(User.email == user_data["email"])
        result = await session.execute(stmt)
        existing_user = result.scalar_one_or_none()
        
        if not existing_user:
            new_user = User(
                email=user_data["email"],
                hashed_password=password_helper.hash(user_data["password"]),
                nickname=user_data["nickname"],
                role=user_data["role"],
                is_active=True,
                is_verified=True,
                created_at=datetime.now(timezone.utc)
            )
            session.add(new_user)
            print(f"✓ 创建用户: {user_data['email']} / {user_data['password']}")
    
    await session.commit()
    
    # 返回用户ID用于创建任务
    admin_result = await session.execute(select(User).where(User.email == "admin@example.com"))
    admin_user = admin_result.scalar_one()
    
    user_result = await session.execute(select(User).where(User.email == "user@example.com"))
    regular_user = user_result.scalar_one()
    
    return str(admin_user.id), str(regular_user.id)


async def create_demo_tasks(session: AsyncSession, admin_id: str, user_id: str):
    """创建演示任务"""
    
    # 检查是否已有任务
    existing_tasks_stmt = select(GpuTask).limit(1)
    result = await session.execute(existing_tasks_stmt)
    if result.scalar_one_or_none():
        print("! 任务数据已存在，跳过创建演示任务")
        return
    
    demo_tasks = [
        {
            "name": "BERT模型训练",
            "description": "使用PyTorch训练BERT模型进行文本分类",
            "user_id": user_id,
            "provider_name": "alibaba",
            "job_config": {
                "provider": "alibaba",
                "gpu_model": "alibaba-v100",
                "image": "pytorch/pytorch:2.0.1-cuda11.7-cudnn8-runtime",
                "script": "python train_bert.py --epochs 10 --batch_size 32",
                "dataset_path": "/data/text_classification",
                "scheduling_strategy": "performance",
                "max_duration": 6,
                "budget_limit": 50.0,
                "environment_vars": {"CUDA_VISIBLE_DEVICES": "0"},
                "requirements": ["transformers", "datasets", "torch"]
            },
            "status": TaskStatus.COMPLETED,
            "priority": TaskPriority.HIGH,
            "estimated_cost": 18.36,
            "actual_cost": 17.95,
            "started_at": datetime.now(timezone.utc).replace(hour=8),
            "completed_at": datetime.now(timezone.utc).replace(hour=14)
        },
        {
            "name": "图像分类实验",
            "description": "使用ResNet50进行ImageNet图像分类",
            "user_id": user_id,
            "provider_name": "tencent",
            "job_config": {
                "provider": "tencent",
                "gpu_model": "tencent-t4",
                "image": "tensorflow/tensorflow:2.12.0-gpu",
                "script": "python train_resnet.py --dataset imagenet --epochs 20",
                "dataset_path": "/data/imagenet",
                "scheduling_strategy": "cost",
                "max_duration": 12,
                "budget_limit": 20.0,
                "environment_vars": {"TF_GPU_MEMORY_GROWTH": "true"},
                "requirements": ["tensorflow", "pillow", "numpy"]
            },
            "status": TaskStatus.RUNNING,
            "priority": TaskPriority.NORMAL,
            "estimated_cost": 4.20,
            "actual_cost": None,
            "started_at": datetime.now(timezone.utc).replace(minute=30)
        },
        {
            "name": "强化学习训练",
            "description": "使用PPO算法训练游戏AI",
            "user_id": admin_id,
            "provider_name": "runpod",
            "job_config": {
                "provider": "runpod",
                "gpu_model": "runpod-rtx4090",
                "image": "pytorch/pytorch:2.0.1-cuda11.7-cudnn8-runtime",
                "script": "python train_ppo.py --env AtariBreakout --steps 1000000",
                "dataset_path": None,
                "scheduling_strategy": "availability",
                "max_duration": 24,
                "budget_limit": 12.0,
                "environment_vars": {"PYTHONPATH": "/app"},
                "requirements": ["gym", "stable-baselines3", "torch"]
            },
            "status": TaskStatus.PENDING,
            "priority": TaskPriority.NORMAL,
            "estimated_cost": 12.0,
            "actual_cost": None
        },
        {
            "name": "数据预处理失败任务",
            "description": "大规模数据预处理任务",
            "user_id": user_id,
            "provider_name": "tencent",
            "job_config": {
                "provider": "tencent",
                "gpu_model": "tencent-v100",
                "image": "python:3.9-slim",
                "script": "python preprocess_data.py --input /data/raw --output /data/processed",
                "dataset_path": "/data/raw",
                "scheduling_strategy": "cost",
                "max_duration": 4,
                "budget_limit": 15.0,
                "environment_vars": {},
                "requirements": ["pandas", "numpy", "scikit-learn"]
            },
            "status": TaskStatus.FAILED,
            "priority": TaskPriority.LOW,
            "estimated_cost": 12.80,
            "actual_cost": 3.20,
            "started_at": datetime.now(timezone.utc).replace(hour=10, minute=0),
            "completed_at": datetime.now(timezone.utc).replace(hour=11, minute=0),
            "error_message": "内存不足错误：无法分配足够的内存来处理大型数据集",
            "exit_code": 1
        },
        {
            "name": "深度学习推理服务",
            "description": "部署模型推理API服务",
            "user_id": admin_id,
            "provider_name": "alibaba",
            "job_config": {
                "provider": "alibaba",
                "gpu_model": "alibaba-t4",
                "image": "nvcr.io/nvidia/pytorch:23.03-py3",
                "script": "python serve_model.py --model bert-base --port 8080",
                "dataset_path": None,
                "scheduling_strategy": "availability",
                "max_duration": 48,
                "budget_limit": 25.0,
                "environment_vars": {"MODEL_PATH": "/models/bert"},
                "requirements": ["fastapi", "uvicorn", "transformers"]
            },
            "status": TaskStatus.QUEUED,
            "priority": TaskPriority.HIGH,
            "estimated_cost": 25.25,
            "actual_cost": None
        }
    ]
    
    for task_data in demo_tasks:
        job_config_str = json.dumps(task_data["job_config"])
        
        new_task = GpuTask(
            name=task_data["name"],
            description=task_data["description"],
            user_id=task_data["user_id"],
            provider_name=task_data["provider_name"],
            job_config=job_config_str,
            status=task_data["status"],
            priority=task_data["priority"],
            estimated_cost=task_data["estimated_cost"],
            actual_cost=task_data.get("actual_cost"),
            currency="USD",
            created_at=datetime.now(timezone.utc),
            started_at=task_data.get("started_at"),
            completed_at=task_data.get("completed_at"),
            updated_at=datetime.now(timezone.utc),
            error_message=task_data.get("error_message"),
            exit_code=task_data.get("exit_code"),
            logs=task_data.get("logs", f"任务日志：{task_data['name']}\n开始时间：{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
        )
        
        session.add(new_task)
        print(f"✓ 创建任务: {task_data['name']} ({task_data['status'].value})")
    
    await session.commit()


async def initialize_demo_data():
    """初始化演示数据"""
    print("🚀 开始初始化演示数据...")
    
    async for session in get_async_session():
        try:
            # 创建用户
            admin_id, user_id = await create_demo_users(session)
            
            # 创建任务
            await create_demo_tasks(session, admin_id, user_id)
            
            print("✅ 演示数据初始化完成！")
            print("\n📝 演示账户信息:")
            print("管理员账户: admin@example.com / admin123")
            print("普通用户: user@example.com / user123")
            print("测试用户1: alice@example.com / alice123")
            print("测试用户2: bob@example.com / bob123")
            print("\n🎯 登录后即可查看演示任务数据")
            
        except Exception as e:
            print(f"❌ 初始化数据时出错: {e}")
            await session.rollback()
            raise
        
        break  # 只执行一次


if __name__ == "__main__":
    import asyncio
    asyncio.run(initialize_demo_data())
