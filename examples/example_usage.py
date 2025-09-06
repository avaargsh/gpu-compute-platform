#!/usr/bin/env python3
"""
Example script demonstrating GPU Compute Platform authentication system usage.

This script shows how to:
1. Register a new user
2. Login to get an access token
3. Use the token to access protected routes

Run this script while the server is running on localhost:8000
"""

import asyncio
import httpx
import json
from datetime import datetime


async def demo_authentication_flow():
    """Demonstrate the complete authentication flow."""
    
    base_url = "http://localhost:8000"
    
    print("🚀 GPU Compute Platform - Authentication Demo")
    print("=" * 50)
    
    # Create a unique user for this demo
    timestamp = datetime.now().strftime("%H%M%S")
    demo_user = {
        "email": f"demo_{timestamp}@example.com",
        "password": "SecurePassword123!",
        "first_name": "演示",
        "last_name": "用户",
        "organization": "GPU计算平台",
    }
    
    try:
        async with httpx.AsyncClient() as client:
            
            # Step 1: Register new user
            print(f"\n1. 👤 注册新用户: {demo_user['email']}")
            register_response = await client.post(
                f"{base_url}/auth/register",
                json=demo_user
            )
            
            if register_response.status_code == 201:
                user_data = register_response.json()
                print(f"   ✅ 注册成功!")
                print(f"   用户ID: {user_data['id']}")
                print(f"   姓名: {user_data['first_name']} {user_data['last_name']}")
                print(f"   组织: {user_data['organization']}")
            else:
                print(f"   ❌ 注册失败: {register_response.text}")
                return
            
            # Step 2: Login to get access token
            print(f"\n2. 🔐 用户登录")
            login_data = {
                "username": demo_user["email"],
                "password": demo_user["password"]
            }
            
            login_response = await client.post(
                f"{base_url}/auth/jwt/login",
                data=login_data
            )
            
            if login_response.status_code == 200:
                token_data = login_response.json()
                access_token = token_data["access_token"]
                print(f"   ✅ 登录成功!")
                print(f"   Token类型: {token_data['token_type']}")
                print(f"   访问令牌: {access_token[:20]}...{access_token[-10:]}")
            else:
                print(f"   ❌ 登录失败: {login_response.text}")
                return
            
            # Step 3: Access protected route
            print(f"\n3. 🛡️  访问受保护的路由")
            headers = {"Authorization": f"Bearer {access_token}"}
            protected_response = await client.get(
                f"{base_url}/api/protected-route",
                headers=headers
            )
            
            if protected_response.status_code == 200:
                protected_data = protected_response.json()
                print(f"   ✅ 成功访问受保护的路由!")
                print(f"   服务器消息: {protected_data['message']}")
                print(f"   用户信息:")
                user_info = protected_data['user_data']
                for key, value in user_info.items():
                    if value:  # Only show non-empty values
                        print(f"     {key}: {value}")
            else:
                print(f"   ❌ 访问失败: {protected_response.text}")
            
            # Step 4: Get current user info
            print(f"\n4. 📋 获取当前用户完整信息")
            me_response = await client.get(
                f"{base_url}/auth/users/me",
                headers=headers
            )
            
            if me_response.status_code == 200:
                me_data = me_response.json()
                print(f"   ✅ 成功获取用户信息!")
                print(f"   完整用户数据:")
                print(json.dumps(me_data, indent=4, ensure_ascii=False))
            else:
                print(f"   ❌ 获取失败: {me_response.text}")
            
            # Step 5: Demonstrate protected route without token
            print(f"\n5. 🚫 尝试无认证访问 (应该失败)")
            no_auth_response = await client.get(f"{base_url}/api/protected-route")
            
            if no_auth_response.status_code == 401:
                print(f"   ✅ 正确拒绝了无认证的请求 (状态码: 401)")
            else:
                print(f"   ⚠️  意外的响应: {no_auth_response.status_code}")
            
            print(f"\n" + "=" * 50)
            print(f"🎉 认证系统演示完成!")
            print(f"\n📍 重要端点:")
            print(f"   • API文档: {base_url}/docs")
            print(f"   • 健康检查: {base_url}/health")
            print(f"   • 用户注册: POST {base_url}/auth/register")
            print(f"   • 用户登录: POST {base_url}/auth/jwt/login")
            print(f"   • 受保护路由: GET {base_url}/api/protected-route")
            
    except httpx.ConnectError:
        print("❌ 无法连接到服务器!")
        print("请先启动服务器: uv run python run_dev.py")
        print("或者: uv run uvicorn app.main:app --reload")
    except Exception as e:
        print(f"❌ 演示过程中发生错误: {e}")


if __name__ == "__main__":
    print("请确保服务器正在运行在 http://localhost:8000")
    print("启动命令: uv run python run_dev.py")
    print("\n按Enter继续演示...")
    input()
    
    asyncio.run(demo_authentication_flow())
