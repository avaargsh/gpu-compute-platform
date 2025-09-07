"""
综合API集成测试
整合了基础API测试和功能测试
"""
import pytest
import aiohttp
import json
from typing import Dict, Any, Optional
from httpx import AsyncClient


class TestBasicAPI:
    """测试基础API端点"""
    
    async def test_health_check(self, client: AsyncClient):
        """测试健康检查端点"""
        response = await client.get("/health")
        
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "healthy"
    
    async def test_root_endpoint(self, client: AsyncClient):
        """测试根端点"""
        response = await client.get("/")
        
        assert response.status_code == 200
        data = response.json()
        assert "message" in data
        assert "version" in data
        assert "docs" in data
        assert data["docs"] == "/docs"
    
    async def test_docs_endpoint(self, client: AsyncClient):
        """测试API文档端点"""
        response = await client.get("/docs")
        
        assert response.status_code == 200
        assert "text/html" in response.headers["content-type"]
    
    async def test_openapi_endpoint(self, client: AsyncClient):
        """测试OpenAPI模式端点"""
        response = await client.get("/openapi.json")
        
        assert response.status_code == 200
        data = response.json()
        assert "openapi" in data
        assert "info" in data
        assert data["info"]["title"] == "GPU Compute Platform"


class TestAuthAPI:
    """测试认证API端点"""
    
    async def test_user_registration(self, client: AsyncClient):
        """测试用户注册"""
        test_user = {
            "email": "test_integration@example.com",
            "password": "test123456",
            "nickname": "集成测试用户"
        }
        
        response = await client.post("/auth/register", json=test_user)
        # 用户可能已存在，所以200或400都是可接受的
        assert response.status_code in [200, 400]
    
    async def test_user_login(self, client: AsyncClient):
        """测试用户登录"""
        login_data = {
            "username": "admin@example.com",
            "password": "admin123"
        }
        
        response = await client.post("/auth/login", json=login_data)
        assert response.status_code == 200
        
        data = response.json()
        assert "access_token" in data
        assert "token_type" in data
        assert data["token_type"] == "bearer"
        
        return data["access_token"]
    
    async def test_get_current_user(self, client: AsyncClient):
        """测试获取当前用户信息"""
        # 先登录获取token
        token = await self.test_user_login(client)
        
        headers = {"Authorization": f"Bearer {token}"}
        response = await client.get("/auth/me", headers=headers)
        
        assert response.status_code == 200
        data = response.json()
        assert "id" in data
        assert "email" in data
        assert "nickname" in data
        assert "role" in data
    
    async def test_logout(self, client: AsyncClient):
        """测试用户登出"""
        # 先登录获取token
        token = await self.test_user_login(client)
        
        headers = {"Authorization": f"Bearer {token}"}
        response = await client.post("/auth/logout", headers=headers)
        
        assert response.status_code == 200


class TestTaskAPI:
    """测试任务管理API端点"""
    
    @pytest.fixture
    async def auth_headers(self, client: AsyncClient):
        """获取认证头"""
        login_data = {
            "username": "admin@example.com",
            "password": "admin123"
        }
        
        response = await client.post("/auth/login", json=login_data)
        assert response.status_code == 200
        
        data = response.json()
        token = data["access_token"]
        return {"Authorization": f"Bearer {token}"}
    
    async def test_get_task_list(self, client: AsyncClient, auth_headers):
        """测试获取任务列表"""
        response = await client.get("/tasks/", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert "items" in data
        assert "total" in data
        assert "page" in data
        assert "per_page" in data
        assert isinstance(data["items"], list)
    
    async def test_create_task(self, client: AsyncClient, auth_headers):
        """测试创建任务"""
        task_data = {
            "name": "集成测试任务",
            "description": "用于集成测试的GPU计算任务",
            "provider_name": "aws",
            "job_config": {
            "provider": "alibaba",
            "gpu_model": "alibaba-t4",
                "image": "python:3.9-slim",
                "script": "python test.py",
                "dataset_path": None,
                "scheduling_strategy": "cost",
                "max_duration": 1,
                "budget_limit": 5.0,
                "environment_vars": {},
                "requirements": ["numpy", "pandas"]
            },
            "priority": "normal",
            "estimated_cost": 2.5
        }
        
        response = await client.post("/tasks/", json=task_data, headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert "id" in data
        assert data["name"] == "集成测试任务"
        assert data["status"] == "pending"
        
        return data["id"]
    
    async def test_get_task_details(self, client: AsyncClient, auth_headers):
        """测试获取任务详情"""
        # 先创建一个任务
        task_id = await self.test_create_task(client, auth_headers)
        
        response = await client.get(f"/tasks/{task_id}", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert data["id"] == task_id
        assert "name" in data
        assert "status" in data
        assert "created_at" in data
    
    async def test_get_task_stats(self, client: AsyncClient, auth_headers):
        """测试获取任务统计"""
        response = await client.get("/tasks/stats", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert "total_tasks" in data
        assert "status_counts" in data
        assert isinstance(data["status_counts"], dict)


class TestProvidersAPI:
    """测试云服务商API端点"""
    
    @pytest.fixture
    async def auth_headers(self, client: AsyncClient):
        """获取认证头"""
        login_data = {
            "username": "admin@example.com",
            "password": "admin123"
        }
        
        response = await client.post("/auth/login", json=login_data)
        assert response.status_code == 200
        
        data = response.json()
        token = data["access_token"]
        return {"Authorization": f"Bearer {token}"}
    
    async def test_get_providers(self, client: AsyncClient, auth_headers):
        """测试获取云服务商列表"""
        response = await client.get("/providers/", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert isinstance(data, list)
        assert len(data) > 0
        
        # 检查第一个提供商的结构
        provider = data[0]
        assert "name" in provider
        assert "display_name" in provider
        assert "description" in provider
        assert "available" in provider
    
    async def test_get_gpu_models(self, client: AsyncClient, auth_headers):
        """测试获取GPU型号列表"""
        # 测试阿里云的GPU型号
        response = await client.get("/providers/alibaba/gpus", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert isinstance(data, list)
        assert len(data) > 0
        
        # 检查GPU型号结构
        gpu = data[0]
        assert "model" in gpu
        assert "memory" in gpu
        assert "hourly_cost" in gpu
    
    async def test_get_docker_images(self, client: AsyncClient, auth_headers):
        """测试获取Docker镜像列表"""
        response = await client.get("/providers/images", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert isinstance(data, list)
        assert len(data) > 0
        
        # 检查镜像结构
        image = data[0]
        assert "name" in image
        assert "tag" in image
        assert "description" in image
    
    async def test_get_pricing_info(self, client: AsyncClient, auth_headers):
        """测试获取价格信息"""
        response = await client.get("/providers/pricing", headers=auth_headers)
        
        assert response.status_code == 200
        data = response.json()
        assert "alibaba" in data
        assert "tencent" in data
        assert "runpod" in data
        
        # 检查阿里云价格结构
        alibaba_pricing = data["alibaba"]
        assert isinstance(alibaba_pricing, dict)


class APITester:
    """完整的API测试工具"""
    
    def __init__(self, base_url: str = "http://localhost:8000"):
        self.base_url = base_url
        self.session = None
        self.access_token = None
        
    async def __aenter__(self):
        self.session = aiohttp.ClientSession()
        return self
        
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        if self.session:
            await self.session.close()
    
    def _get_headers(self) -> Dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self.access_token:
            headers["Authorization"] = f"Bearer {self.access_token}"
        return headers
    
    async def _make_request(self, method: str, endpoint: str, data: Dict = None) -> Dict[str, Any]:
        """发送HTTP请求"""
        url = f"{self.base_url}{endpoint}"
        headers = self._get_headers()
        
        try:
            async with self.session.request(
                method=method,
                url=url,
                headers=headers,
                json=data if data else None
            ) as response:
                response_data = await response.json()
                return {
                    "status_code": response.status,
                    "data": response_data,
                    "success": 200 <= response.status < 300
                }
        except Exception as e:
            return {
                "status_code": 0,
                "data": {"error": str(e)},
                "success": False
            }
    
    async def run_integration_tests(self):
        """运行完整的集成测试"""
        print("🚀 开始完整的API集成测试...")
        print("=" * 60)
        
        test_steps = [
            ("健康检查", self._test_health_check),
            ("用户注册", self._test_user_registration),
            ("用户登录", self._test_user_login),
            ("获取用户信息", self._test_get_current_user),
            ("获取任务列表", self._test_task_list),
            ("创建任务", self._test_create_task),
            ("获取云服务商列表", self._test_providers_list),
            ("获取任务统计", self._test_task_stats),
        ]
        
        results = []
        for step_name, test_func in test_steps:
            print(f"\n🔍 执行: {step_name}")
            try:
                result = await test_func()
                results.append((step_name, result))
                if result:
                    print(f"✅ {step_name} - 通过")
                else:
                    print(f"❌ {step_name} - 失败")
            except Exception as e:
                print(f"❌ {step_name} - 异常: {e}")
                results.append((step_name, False))
            print("-" * 40)
        
        # 统计结果
        success_count = sum(1 for _, success in results if success)
        total_count = len(results)
        
        print(f"\n📊 测试结果统计:")
        print(f"总计: {total_count} 个测试")
        print(f"通过: {success_count} 个")
        print(f"失败: {total_count - success_count} 个")
        print(f"成功率: {success_count/total_count*100:.1f}%")
        
        if success_count == total_count:
            print("\n🎉 所有集成测试通过！")
        else:
            print(f"\n⚠️  {total_count - success_count} 个测试失败，请检查系统配置")
        
        return success_count, total_count
    
    async def _test_health_check(self):
        result = await self._make_request("GET", "/")
        return result["success"]
    
    async def _test_user_registration(self):
        test_user = {
            "email": "integration_test@example.com",
            "password": "test123456",
            "nickname": "集成测试用户"
        }
        result = await self._make_request("POST", "/auth/register", test_user)
        return result["success"] or result["status_code"] == 400
    
    async def _test_user_login(self):
        login_data = {
            "username": "admin@example.com",
            "password": "admin123"
        }
        result = await self._make_request("POST", "/auth/login", login_data)
        if result["success"] and "access_token" in result["data"]:
            self.access_token = result["data"]["access_token"]
            return True
        return False
    
    async def _test_get_current_user(self):
        result = await self._make_request("GET", "/auth/me")
        return result["success"]
    
    async def _test_task_list(self):
        result = await self._make_request("GET", "/tasks/")
        return result["success"]
    
    async def _test_create_task(self):
        task_data = {
            "name": "集成测试任务",
            "description": "自动化集成测试创建的任务",
            "provider_name": "alibaba",
            "job_config": {
                "provider": "alibaba",
                "gpu_model": "alibaba-t4",
                "image": "python:3.9-slim",
                "script": "echo 'Hello World'",
                "dataset_path": None,
                "scheduling_strategy": "cost",
                "max_duration": 1,
                "budget_limit": 5.0,
                "environment_vars": {},
                "requirements": []
            },
            "priority": "normal",
            "estimated_cost": 1.0
        }
        result = await self._make_request("POST", "/tasks/", task_data)
        return result["success"]
    
    async def _test_providers_list(self):
        result = await self._make_request("GET", "/providers/")
        return result["success"]
    
    async def _test_task_stats(self):
        result = await self._make_request("GET", "/tasks/stats")
        return result["success"]


if __name__ == "__main__":
    import asyncio
    import argparse
    
    parser = argparse.ArgumentParser(description="GPU计算平台API集成测试")
    parser.add_argument(
        "--url",
        default="http://localhost:8000",
        help="API服务器地址 (默认: http://localhost:8000)"
    )
    
    args = parser.parse_args()
    
    async def main():
        print(f"🔗 连接到API服务器: {args.url}")
        
        async with APITester(args.url) as tester:
            await tester.run_integration_tests()
    
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n❌ 测试被用户中断")
    except Exception as e:
        print(f"❌ 运行测试时出错: {e}")
