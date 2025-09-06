#!/usr/bin/env python3
"""
完整功能测试脚本：
1. 用户注册
2. 用户登录
3. 提交任务
4. 异步执行状态查询
5. 查看成本信息
"""

import asyncio
import httpx
import json
import time
from datetime import datetime
from typing import Optional, Dict, Any

class GPUPlatformTester:
    def __init__(self, base_url: str = "http://localhost:8000"):
        self.base_url = base_url
        self.client = httpx.AsyncClient(base_url=base_url, timeout=30.0)
        self.access_token: Optional[str] = None
        self.user_info: Optional[Dict[str, Any]] = None
        
    def _get_auth_headers(self) -> Dict[str, str]:
        if not self.access_token:
            raise ValueError("Not authenticated")
        return {"Authorization": f"Bearer {self.access_token}"}
    
    async def test_health_check(self) -> bool:
        """测试服务健康检查"""
        print("🏥 Testing health check...")
        try:
            response = await self.client.get("/health")
            if response.status_code == 200:
                print("✅ Health check passed")
                return True
            else:
                print(f"❌ Health check failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ Health check error: {e}")
            return False
    
    async def test_user_registration(self, email: str = "test@example.com", password: str = "testpass123") -> bool:
        """测试用户注册"""
        print(f"📝 Testing user registration for {email}...")
        try:
            user_data = {
                "email": email,
                "password": password,
                "is_active": True,
                "is_superuser": False,
                "is_verified": True
            }
            
            response = await self.client.post("/auth/register", json=user_data)
            
            if response.status_code == 201:
                data = response.json()
                self.user_info = data
                print(f"✅ User registered successfully: {data.get('email')}")
                return True
            elif response.status_code == 400 and "already exists" in response.text.lower():
                print(f"ℹ️  User {email} already exists")
                return True
            else:
                print(f"❌ Registration failed: {response.status_code} - {response.text}")
                return False
        except Exception as e:
            print(f"❌ Registration error: {e}")
            return False
    
    async def test_user_login(self, email: str = "test@example.com", password: str = "testpass123") -> bool:
        """测试用户登录"""
        print(f"🔐 Testing user login for {email}...")
        try:
            login_data = {
                "username": email,  # FastAPI Users uses 'username' for email
                "password": password
            }
            
            response = await self.client.post(
                "/auth/jwt/login",
                data=login_data,  # Use form data for OAuth2
                headers={"Content-Type": "application/x-www-form-urlencoded"}
            )
            
            if response.status_code == 200:
                data = response.json()
                self.access_token = data.get("access_token")
                print("✅ Login successful")
                return True
            else:
                print(f"❌ Login failed: {response.status_code} - {response.text}")
                return False
        except Exception as e:
            print(f"❌ Login error: {e}")
            return False
    
    async def test_get_current_user(self) -> bool:
        """测试获取当前用户信息"""
        print("👤 Testing get current user...")
        try:
            response = await self.client.get("/auth/users/me", headers=self._get_auth_headers())
            
            if response.status_code == 200:
                user_data = response.json()
                print(f"✅ Current user: {user_data.get('email')}")
                return True
            else:
                print(f"❌ Get current user failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ Get current user error: {e}")
            return False
    
    async def test_list_providers(self) -> bool:
        """测试列出GPU提供商"""
        print("🏢 Testing list GPU providers...")
        try:
            response = await self.client.get("/api/gpu/providers", headers=self._get_auth_headers())
            
            if response.status_code == 200:
                data = response.json()
                providers = data.get("providers", [])
                print(f"✅ Found {len(providers)} providers:")
                for provider in providers:
                    print(f"  - {provider['display_name']} ({provider['name']})")
                return True
            else:
                print(f"❌ List providers failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ List providers error: {e}")
            return False
    
    async def test_get_scheduling_recommendations(self) -> bool:
        """测试获取调度推荐"""
        print("🎯 Testing scheduling recommendations...")
        try:
            params = {
                "gpu_type": "RTX4090",
                "gpu_count": 1,
                "memory_gb": 8,
                "vcpus": 4,
                "estimated_duration_minutes": 60,
                "priority": 5
            }
            
            response = await self.client.get(
                "/api/gpu/scheduling/recommendations", 
                params=params,
                headers=self._get_auth_headers()
            )
            
            if response.status_code == 200:
                data = response.json()
                recommendations = data.get("recommendations", {})
                print(f"✅ Got recommendations for {len(recommendations)} strategies:")
                for strategy, rec in recommendations.items():
                    print(f"  - {strategy}: {rec.get('provider')} (score: {rec.get('score', 0):.2f})")
                return True
            else:
                print(f"❌ Get recommendations failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ Get recommendations error: {e}")
            return False
    
    async def test_submit_job(self) -> Optional[str]:
        """测试提交GPU作业"""
        print("🚀 Testing job submission...")
        try:
            job_config = {
                "name": f"test-job-{int(time.time())}",
                "image": "pytorch/pytorch:latest",
                "command": ["python", "-c", "print('Hello from GPU!'); import time; time.sleep(30)"],
                "gpu_spec": {
                    "gpu_type": "RTX4090",
                    "gpu_count": 1,
                    "memory_gb": 8,
                    "vcpus": 4,
                    "ram_gb": 16
                },
                "environment_vars": {"TEST_VAR": "test_value"}
            }
            
            response = await self.client.post(
                "/api/gpu/jobs/submit",
                json=job_config,
                headers=self._get_auth_headers()
            )
            
            if response.status_code == 200:
                data = response.json()
                task_id = data.get("task_id")
                print(f"✅ Job submitted successfully:")
                print(f"  - Task ID: {task_id}")
                print(f"  - Provider: {data.get('provider')}")
                print(f"  - Status: {data.get('status')}")
                print(f"  - Celery Task ID: {data.get('celery_task_id')}")
                return task_id
            else:
                print(f"❌ Job submission failed: {response.status_code} - {response.text}")
                return None
        except Exception as e:
            print(f"❌ Job submission error: {e}")
            return None
    
    async def test_list_user_tasks(self) -> bool:
        """测试列出用户任务"""
        print("📋 Testing list user tasks...")
        try:
            response = await self.client.get("/api/gpu/tasks", headers=self._get_auth_headers())
            
            if response.status_code == 200:
                data = response.json()
                tasks = data.get("tasks", [])
                print(f"✅ Found {len(tasks)} user tasks:")
                for task in tasks:
                    print(f"  - {task['name']} ({task['task_id'][:8]}...): {task['status']} on {task['provider']}")
                return True
            else:
                print(f"❌ List user tasks failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ List user tasks error: {e}")
            return False
    
    async def test_get_task_details(self, task_id: str) -> bool:
        """测试获取任务详情"""
        print(f"🔍 Testing get task details for {task_id[:8]}...")
        try:
            response = await self.client.get(f"/api/gpu/tasks/{task_id}", headers=self._get_auth_headers())
            
            if response.status_code == 200:
                data = response.json()
                print(f"✅ Task details:")
                print(f"  - Name: {data.get('name')}")
                print(f"  - Status: {data.get('status')}")
                print(f"  - Provider: {data.get('provider')}")
                print(f"  - Created: {data.get('created_at')}")
                if data.get('started_at'):
                    print(f"  - Started: {data.get('started_at')}")
                if data.get('estimated_cost'):
                    print(f"  - Estimated Cost: {data.get('estimated_cost')} {data.get('currency', 'USD')}")
                if data.get('actual_cost'):
                    print(f"  - Actual Cost: {data.get('actual_cost')} {data.get('currency', 'USD')}")
                return True
            else:
                print(f"❌ Get task details failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ Get task details error: {e}")
            return False
    
    async def test_cancel_task(self, task_id: str) -> bool:
        """测试取消任务"""
        print(f"❌ Testing cancel task {task_id[:8]}...")
        try:
            response = await self.client.post(f"/api/gpu/tasks/{task_id}/cancel", headers=self._get_auth_headers())
            
            if response.status_code == 200:
                data = response.json()
                print(f"✅ Task cancellation requested:")
                print(f"  - Task ID: {data.get('task_id')}")
                print(f"  - Status: {data.get('status')}")
                print(f"  - Message: {data.get('message')}")
                return True
            else:
                print(f"❌ Cancel task failed: {response.status_code} - {response.text}")
                return False
        except Exception as e:
            print(f"❌ Cancel task error: {e}")
            return False
    
    async def test_provider_health(self, provider_name: str = "runpod") -> bool:
        """测试提供商健康检查"""
        print(f"🏥 Testing {provider_name} provider health...")
        try:
            response = await self.client.get(
                f"/api/gpu/providers/{provider_name}/health", 
                headers=self._get_auth_headers()
            )
            
            if response.status_code == 200:
                data = response.json()
                health = data.get("health", {})
                print(f"✅ Provider {provider_name} health:")
                print(f"  - Status: {health.get('status', 'unknown')}")
                if health.get('response_time'):
                    print(f"  - Response time: {health.get('response_time'):.3f}s")
                return True
            else:
                print(f"❌ Provider health check failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ Provider health check error: {e}")
            return False
    
    async def monitor_task_status(self, task_id: str, max_checks: int = 5, interval: int = 10) -> bool:
        """监控任务状态变化"""
        print(f"👀 Monitoring task {task_id[:8]} status (max {max_checks} checks)...")
        
        for i in range(max_checks):
            try:
                response = await self.client.get(f"/api/gpu/tasks/{task_id}", headers=self._get_auth_headers())
                
                if response.status_code == 200:
                    data = response.json()
                    status = data.get('status')
                    print(f"  Check {i+1}/{max_checks}: Status = {status}")
                    
                    # Check if task is in terminal state
                    if status in ['completed', 'failed', 'cancelled']:
                        print(f"✅ Task reached terminal state: {status}")
                        return True
                    
                    if i < max_checks - 1:  # Don't sleep on last iteration
                        print(f"    Waiting {interval}s before next check...")
                        await asyncio.sleep(interval)
                else:
                    print(f"❌ Status check failed: {response.status_code}")
                    return False
                    
            except Exception as e:
                print(f"❌ Status monitoring error: {e}")
                return False
        
        print("ℹ️  Monitoring completed (max checks reached)")
        return False
    
    async def run_complete_test(self):
        """运行完整的功能测试"""
        print("🧪 Starting Complete GPU Platform Functionality Test")
        print("=" * 60)
        
        # Generate unique test user email
        unique_email = f"test{int(time.time())}@example.com"
        test_password = "testpass123"
        print(f"🔑 Using test user: {unique_email}")
        
        test_results = {}
        
        # 1. Health Check
        test_results['health_check'] = await self.test_health_check()
        if not test_results['health_check']:
            print("❌ Server is not healthy, stopping tests")
            return test_results
        
        # 2. User Registration
        test_results['registration'] = await self.test_user_registration(unique_email, test_password)
        
        # 3. User Login
        test_results['login'] = await self.test_user_login(unique_email, test_password)
        if not test_results['login']:
            print("❌ Login failed, stopping tests")
            return test_results
        
        # 4. Get Current User
        test_results['current_user'] = await self.test_get_current_user()
        
        # 5. List Providers
        test_results['list_providers'] = await self.test_list_providers()
        
        # 6. Provider Health Check
        test_results['provider_health'] = await self.test_provider_health()
        
        # 7. Get Scheduling Recommendations
        test_results['scheduling_recommendations'] = await self.test_get_scheduling_recommendations()
        
        # 8. Submit Job
        task_id = await self.test_submit_job()
        test_results['job_submission'] = task_id is not None
        
        if task_id:
            # 9. List User Tasks
            test_results['list_tasks'] = await self.test_list_user_tasks()
            
            # 10. Get Task Details
            test_results['task_details'] = await self.test_get_task_details(task_id)
            
            # 11. Monitor Task Status
            test_results['task_monitoring'] = await self.monitor_task_status(task_id, max_checks=3, interval=5)
            
            # 12. Cancel Task (if still running)
            test_results['task_cancellation'] = await self.test_cancel_task(task_id)
        
        print("\n" + "=" * 60)
        print("📊 TEST RESULTS SUMMARY:")
        print("=" * 60)
        
        total_tests = len(test_results)
        passed_tests = sum(1 for result in test_results.values() if result)
        
        for test_name, result in test_results.items():
            status = "✅ PASS" if result else "❌ FAIL"
            print(f"{test_name.replace('_', ' ').title()}: {status}")
        
        print(f"\nOverall: {passed_tests}/{total_tests} tests passed ({passed_tests/total_tests*100:.1f}%)")
        
        return test_results
    
    async def close(self):
        """关闭HTTP客户端"""
        await self.client.aclose()


async def main():
    """主测试函数"""
    tester = GPUPlatformTester()
    
    try:
        results = await tester.run_complete_test()
        
        # Return appropriate exit code
        if all(results.values()):
            print("\n🎉 All tests passed!")
            return 0
        else:
            print("\n⚠️  Some tests failed!")
            return 1
            
    except KeyboardInterrupt:
        print("\n⚡ Test interrupted by user")
        return 130
    except Exception as e:
        print(f"\n💥 Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1
    finally:
        await tester.close()


if __name__ == "__main__":
    import sys
    exit_code = asyncio.run(main())
    sys.exit(exit_code)
