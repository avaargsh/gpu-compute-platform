#!/usr/bin/env python3
"""
核心功能测试脚本
测试GPU计算平台的基础功能是否正常
"""
import asyncio
import aiohttp
import json
import sys
from typing import Dict, Any, Optional


class CoreFunctionalityTester:
    """核心功能测试器"""
    
    def __init__(self, base_url: str = "http://127.0.0.1:8000"):
        self.base_url = base_url
        self.session = None
        self.test_results = []
    
    async def __aenter__(self):
        self.session = aiohttp.ClientSession()
        return self
    
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        if self.session:
            await self.session.close()
    
    async def make_request(self, method: str, endpoint: str, data: Optional[Dict] = None) -> Dict[str, Any]:
        """发送HTTP请求"""
        url = f"{self.base_url}{endpoint}"
        
        try:
            async with self.session.request(
                method=method,
                url=url,
                json=data,
                headers={"Content-Type": "application/json"}
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
    
    async def test_health_check(self) -> bool:
        """测试健康检查"""
        print("🔍 测试健康检查...")
        result = await self.make_request("GET", "/health")
        
        if result["success"] and result["data"].get("status") == "healthy":
            print("✅ 健康检查通过")
            return True
        else:
            print(f"❌ 健康检查失败: {result}")
            return False
    
    async def test_root_endpoint(self) -> bool:
        """测试根端点"""
        print("🔍 测试根端点...")
        result = await self.make_request("GET", "/")
        
        if result["success"] and "message" in result["data"]:
            print("✅ 根端点测试通过")
            return True
        else:
            print(f"❌ 根端点测试失败: {result}")
            return False
    
    async def test_providers_endpoint(self) -> bool:
        """测试云服务商端点"""
        print("🔍 测试云服务商端点...")
        result = await self.make_request("GET", "/test/providers")
        
        if result["success"] and "providers" in result["data"]:
            providers = result["data"]["providers"]
            expected_providers = ["alibaba", "tencent", "runpod"]
            
            provider_names = [p["name"] for p in providers]
            if all(name in provider_names for name in expected_providers):
                print("✅ 云服务商端点测试通过")
                print(f"   支持的服务商: {', '.join(provider_names)}")
                return True
            else:
                print(f"❌ 服务商列表不完整，期望: {expected_providers}, 实际: {provider_names}")
                return False
        else:
            print(f"❌ 云服务商端点测试失败: {result}")
            return False
    
    async def test_tasks_endpoint(self) -> bool:
        """测试任务端点"""
        print("🔍 测试任务端点...")
        result = await self.make_request("GET", "/test/tasks")
        
        if result["success"] and "tasks" in result["data"]:
            tasks = result["data"]["tasks"]
            if len(tasks) > 0:
                task = tasks[0]
                required_fields = ["id", "name", "status", "provider"]
                
                if all(field in task for field in required_fields):
                    print("✅ 任务端点测试通过")
                    print(f"   示例任务: {task['name']} ({task['status']})")
                    return True
                else:
                    missing_fields = [f for f in required_fields if f not in task]
                    print(f"❌ 任务对象缺少字段: {missing_fields}")
                    return False
            else:
                print("❌ 任务列表为空")
                return False
        else:
            print(f"❌ 任务端点测试失败: {result}")
            return False
    
    async def test_auth_endpoint(self) -> bool:
        """测试认证端点"""
        print("🔍 测试认证端点...")
        result = await self.make_request("POST", "/test/auth")
        
        if result["success"] and result["data"].get("available"):
            print("✅ 认证端点测试通过")
            return True
        else:
            print(f"❌ 认证端点测试失败: {result}")
            return False
    
    async def test_api_documentation(self) -> bool:
        """测试API文档可访问性"""
        print("🔍 测试API文档...")
        
        try:
            async with self.session.get(f"{self.base_url}/docs") as response:
                if response.status == 200:
                    print("✅ API文档可访问")
                    return True
                else:
                    print(f"❌ API文档不可访问，状态码: {response.status}")
                    return False
        except Exception as e:
            print(f"❌ API文档访问失败: {e}")
            return False
    
    async def test_cors_headers(self) -> bool:
        """测试CORS头部"""
        print("🔍 测试CORS配置...")
        
        try:
            async with self.session.options(
                f"{self.base_url}/",
                headers={"Origin": "http://localhost:3000"}
            ) as response:
                cors_headers = response.headers
                
                if "access-control-allow-origin" in cors_headers:
                    print("✅ CORS配置正常")
                    return True
                else:
                    print("❌ CORS配置缺失")
                    return False
        except Exception as e:
            print(f"❌ CORS测试失败: {e}")
            return False
    
    async def run_all_tests(self) -> tuple[int, int]:
        """运行所有核心功能测试"""
        print("🚀 开始核心功能测试")
        print("=" * 60)
        
        tests = [
            ("健康检查", self.test_health_check),
            ("根端点", self.test_root_endpoint),
            ("云服务商端点", self.test_providers_endpoint),
            ("任务端点", self.test_tasks_endpoint),
            ("认证端点", self.test_auth_endpoint),
            ("API文档", self.test_api_documentation),
            ("CORS配置", self.test_cors_headers),
        ]
        
        passed = 0
        total = len(tests)
        
        for test_name, test_func in tests:
            print(f"\n📋 执行测试: {test_name}")
            try:
                result = await test_func()
                if result:
                    passed += 1
                    print(f"   结果: ✅ 通过")
                else:
                    print(f"   结果: ❌ 失败")
            except Exception as e:
                print(f"   结果: ❌ 异常 - {e}")
            
            print("-" * 40)
        
        # 输出测试总结
        print(f"\n📊 测试总结:")
        print(f"总测试数: {total}")
        print(f"通过: {passed}")
        print(f"失败: {total - passed}")
        print(f"成功率: {passed/total*100:.1f}%")
        
        if passed == total:
            print("\n🎉 所有核心功能测试通过！系统基础功能正常。")
        else:
            print(f"\n⚠️  有 {total - passed} 个测试失败，请检查相关功能。")
        
        return passed, total


async def main():
    """主函数"""
    import argparse
    
    parser = argparse.ArgumentParser(description="GPU计算平台核心功能测试")
    parser.add_argument(
        "--url",
        default="http://127.0.0.1:8000",
        help="API服务器地址 (默认: http://127.0.0.1:8000)"
    )
    
    args = parser.parse_args()
    
    print(f"🔗 测试服务器: {args.url}")
    print("🎯 执行核心功能测试以验证基础流程")
    print()
    
    async with CoreFunctionalityTester(args.url) as tester:
        passed, total = await tester.run_all_tests()
        
        # 返回适当的退出码
        if passed == total:
            sys.exit(0)
        else:
            sys.exit(1)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n❌ 测试被用户中断")
        sys.exit(1)
    except Exception as e:
        print(f"❌ 运行测试时发生错误: {e}")
        sys.exit(1)
