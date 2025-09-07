#!/usr/bin/env python3
"""
升级版数据库管理脚本
集成Alembic迁移和演示数据初始化
"""
import asyncio
import argparse
import sys
import subprocess
from pathlib import Path
from sqlalchemy import text

from app.core.database import engine, get_async_session
from app.models.base import Base
from app.core.init_data import initialize_demo_data


async def run_alembic_upgrade():
    """运行Alembic数据库迁移"""
    print("🔧 正在运行Alembic数据库迁移...")
    
    try:
        # 运行Alembic升级
        result = subprocess.run(
            ["uv", "run", "python", "-m", "alembic", "upgrade", "head"],
            capture_output=True,
            text=True,
            cwd=Path(__file__).parent
        )
        
        if result.returncode == 0:
            print("✅ Alembic数据库迁移完成")
            print(result.stdout)
            return True
        else:
            print(f"❌ Alembic迁移失败: {result.stderr}")
            return False
    except Exception as e:
        print(f"❌ 运行Alembic迁移时出错: {e}")
        return False


async def run_alembic_downgrade(revision: str = "-1"):
    """运行Alembic数据库降级"""
    print(f"⬇️ 正在降级数据库到版本: {revision}")
    
    try:
        result = subprocess.run(
            ["uv", "run", "python", "-m", "alembic", "downgrade", revision],
            capture_output=True,
            text=True,
            cwd=Path(__file__).parent
        )
        
        if result.returncode == 0:
            print("✅ Alembic数据库降级完成")
            print(result.stdout)
            return True
        else:
            print(f"❌ Alembic降级失败: {result.stderr}")
            return False
    except Exception as e:
        print(f"❌ 运行Alembic降级时出错: {e}")
        return False


async def get_alembic_current():
    """获取当前Alembic版本"""
    try:
        result = subprocess.run(
            ["uv", "run", "python", "-m", "alembic", "current"],
            capture_output=True,
            text=True,
            cwd=Path(__file__).parent
        )
        
        if result.returncode == 0:
            return result.stdout.strip()
        else:
            return "无法获取版本信息"
    except Exception as e:
        return f"错误: {e}"


async def get_alembic_history():
    """获取Alembic迁移历史"""
    try:
        result = subprocess.run(
            ["uv", "run", "python", "-m", "alembic", "history"],
            capture_output=True,
            text=True,
            cwd=Path(__file__).parent
        )
        
        if result.returncode == 0:
            return result.stdout
        else:
            return f"错误: {result.stderr}"
    except Exception as e:
        return f"错误: {e}"


async def create_new_migration(message: str):
    """创建新的迁移文件"""
    print(f"📝 正在创建新的迁移文件: {message}")
    
    try:
        result = subprocess.run(
            ["uv", "run", "python", "-m", "alembic", "revision", "--autogenerate", "-m", message],
            capture_output=True,
            text=True,
            cwd=Path(__file__).parent
        )
        
        if result.returncode == 0:
            print("✅ 迁移文件创建成功")
            print(result.stdout)
            return True
        else:
            print(f"❌ 创建迁移文件失败: {result.stderr}")
            return False
    except Exception as e:
        print(f"❌ 创建迁移文件时出错: {e}")
        return False


async def check_database_status():
    """检查数据库连接状态和迁移状态"""
    print("🔍 正在检查数据库状态...")
    
    try:
        async with engine.begin() as conn:
            # 检查数据库连接
            result = await conn.execute(text("SELECT 1"))
            if result.fetchone():
                print("✅ 数据库连接正常")
                
                # 检查Alembic迁移状态
                current_version = await get_alembic_current()
                print(f"📊 当前迁移版本: {current_version}")
                
                # 检查表是否存在以及记录数
                try:
                    user_count_result = await conn.execute(text("SELECT COUNT(*) FROM users"))
                    user_count = user_count_result.fetchone()[0]
                    print(f"👥 用户数量: {user_count}")
                    
                    task_count_result = await conn.execute(text("SELECT COUNT(*) FROM gpu_tasks"))
                    task_count = task_count_result.fetchone()[0]
                    print(f"📋 任务数量: {task_count}")
                    
                except Exception as e:
                    print(f"⚠️  数据库表可能不存在: {e}")
                    print("💡 请运行 'migrate' 命令来应用数据库迁移")
                    
    except Exception as e:
        print(f"❌ 数据库连接失败: {e}")
        return False
    
    return True


async def reset_database_with_migration():
    """使用迁移重置数据库"""
    print("🔄 正在使用迁移重置数据库...")
    
    # 降级到base（删除所有表）
    success = await run_alembic_downgrade("base")
    if not success:
        return False
    
    # 重新升级到最新版本
    success = await run_alembic_upgrade()
    if not success:
        return False
    
    print("✅ 数据库重置完成")
    return True


async def show_migration_info():
    """显示迁移信息"""
    print("📜 数据库迁移信息:")
    print("=" * 50)
    
    # 当前版本
    current = await get_alembic_current()
    print(f"当前版本: {current}")
    print()
    
    # 迁移历史
    history = await get_alembic_history()
    print("迁移历史:")
    print(history)


async def show_demo_users():
    """显示演示用户信息"""
    print("📝 演示用户账户信息:")
    print("=" * 50)
    
    demo_accounts = [
        ("管理员账户", "admin@example.com", "admin123", "可访问所有功能"),
        ("普通用户", "user@example.com", "user123", "普通用户权限"),
        ("测试用户1", "alice@example.com", "alice123", "普通用户权限"),
        ("测试用户2", "bob@example.com", "bob123", "普通用户权限")
    ]
    
    for role, email, password, desc in demo_accounts:
        print(f"{role}:")
        print(f"  邮箱: {email}")
        print(f"  密码: {password}")
        print(f"  权限: {desc}")
        print()


async def main():
    parser = argparse.ArgumentParser(description="GPU计算平台数据库管理工具 v2.0")
    parser.add_argument(
        "command", 
        choices=[
            "migrate", "status", "demo-data", "demo-users", "reset", 
            "downgrade", "history", "create-migration", "current"
        ],
        help="要执行的命令"
    )
    parser.add_argument(
        "--message", "-m",
        help="迁移文件描述信息（用于create-migration命令）"
    )
    parser.add_argument(
        "--revision", "-r",
        default="-1",
        help="降级到的版本（用于downgrade命令，默认为上一个版本）"
    )
    
    args = parser.parse_args()
    
    try:
        if args.command == "migrate":
            success = await run_alembic_upgrade()
            if success:
                print("💡 提示：运行 'python manage_db_v2.py demo-data' 来创建演示数据")
                
        elif args.command == "status":
            await check_database_status()
            
        elif args.command == "demo-data":
            # 先检查数据库状态
            if await check_database_status():
                await initialize_demo_data()
            else:
                print("💡 请先运行 'python manage_db_v2.py migrate' 来应用数据库迁移")
                
        elif args.command == "demo-users":
            await show_demo_users()
            
        elif args.command == "reset":
            confirm = input("⚠️  这将删除所有数据，确定要继续吗？(y/N): ")
            if confirm.lower() in ['y', 'yes']:
                success = await reset_database_with_migration()
                if success:
                    print("💡 提示：运行 'python manage_db_v2.py demo-data' 来创建演示数据")
            else:
                print("操作已取消")
                
        elif args.command == "downgrade":
            await run_alembic_downgrade(args.revision)
            
        elif args.command == "history":
            await show_migration_info()
            
        elif args.command == "current":
            current = await get_alembic_current()
            print(f"当前迁移版本: {current}")
            
        elif args.command == "create-migration":
            if not args.message:
                message = input("请输入迁移描述信息: ")
                if not message.strip():
                    print("❌ 迁移描述不能为空")
                    sys.exit(1)
            else:
                message = args.message
                
            await create_new_migration(message)
            
    except KeyboardInterrupt:
        print("\n❌ 操作被用户中断")
        sys.exit(1)
    except Exception as e:
        print(f"❌ 执行命令时出错: {e}")
        sys.exit(1)


if __name__ == "__main__":
    print("🚀 GPU计算平台数据库管理工具 v2.0")
    print("使用Alembic进行数据库迁移管理")
    print("=" * 50)
    asyncio.run(main())
