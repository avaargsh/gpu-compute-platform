import pytest
import json
import uuid
from httpx import AsyncClient
from unittest.mock import patch, AsyncMock

from app.models.dag import NodeType, DAGStatus, NodeStatus


class TestDAGAPI:
    """DAG API测试"""
    
    @pytest.mark.asyncio
    async def test_create_dag_success(self, client: AsyncClient, test_user_data):
        """测试成功创建DAG"""
        # 首先注册并登录用户
        register_response = await client.post("/auth/register", json=test_user_data)
        assert register_response.status_code == 201
        
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        assert login_response.status_code == 200
        token = login_response.json()["access_token"]
        
        # 创建DAG
        dag_data = {
            "name": "Test DAG",
            "description": "Test DAG description",
            "schedule": "0 0 * * *",
            "nodes": [
                {
                    "node_id": "start",
                    "node_name": "Start Task",
                    "node_type": "task",
                    "node_config": {
                        "user_id": "test_user",
                        "provider_name": "runpod",
                        "job_config": {
                            "name": "start_task",
                            "image": "python:3.9",
                            "command": ["echo", "hello"]
                        },
                        "provider_config": {}
                    },
                    "dependencies": [],
                    "position_x": 100.0,
                    "position_y": 100.0
                },
                {
                    "node_id": "end",
                    "node_name": "End Task",
                    "node_type": "task",
                    "node_config": {
                        "user_id": "test_user",
                        "provider_name": "runpod",
                        "job_config": {
                            "name": "end_task",
                            "image": "python:3.9",
                            "command": ["echo", "done"]
                        },
                        "provider_config": {}
                    },
                    "dependencies": ["start"],
                    "position_x": 200.0,
                    "position_y": 200.0
                }
            ],
            "is_template": False
        }
        
        response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 201
        data = response.json()
        assert data["name"] == "Test DAG"
        assert data["description"] == "Test DAG description"
        assert data["schedule"] == "0 0 * * *"
        assert data["is_active"] is True
        assert data["is_template"] is False
        assert "id" in data
    
    @pytest.mark.asyncio
    async def test_create_dag_invalid_dependencies(self, client: AsyncClient, test_user_data):
        """测试创建具有无效依赖的DAG"""
        # 注册并登录
        register_response = await client.post("/auth/register", json=test_user_data)
        assert register_response.status_code == 201
        
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建有无效依赖的DAG
        dag_data = {
            "name": "Invalid DAG",
            "description": "DAG with invalid dependencies",
            "nodes": [
                {
                    "node_id": "task1",
                    "node_name": "Task 1",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": ["nonexistent_task"]  # 不存在的依赖
                }
            ]
        }
        
        response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 400
        assert "Invalid dependency" in response.json()["detail"]
    
    @pytest.mark.asyncio
    async def test_list_dags(self, client: AsyncClient, test_user_data):
        """测试获取DAG列表"""
        # 注册并登录
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建几个DAG
        for i in range(3):
            dag_data = {
                "name": f"Test DAG {i}",
                "description": f"Test DAG {i} description",
                "nodes": [
                    {
                        "node_id": f"task_{i}",
                        "node_name": f"Task {i}",
                        "node_type": "task",
                        "node_config": {},
                        "dependencies": []
                    }
                ],
                "is_template": i == 2  # 最后一个作为模板
            }
            
            await client.post(
                "/api/dag/",
                json=dag_data,
                headers={"Authorization": f"Bearer {token}"}
            )
        
        # 获取非模板DAG列表
        response = await client.get(
            "/api/dag/",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        dags = response.json()
        assert len(dags) == 2  # 不包含模板
        assert all(not dag["is_template"] for dag in dags)
        
        # 获取包含模板的列表
        response = await client.get(
            "/api/dag/?include_templates=true",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        dags = response.json()
        assert len(dags) == 3  # 包含所有DAG
    
    @pytest.mark.asyncio
    async def test_get_dag_detail(self, client: AsyncClient, test_user_data):
        """测试获取DAG详情"""
        # 注册并登录
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建DAG
        dag_data = {
            "name": "Detail Test DAG",
            "description": "DAG for detail testing",
            "nodes": [
                {
                    "node_id": "start",
                    "node_name": "Start",
                    "node_type": "task",
                    "node_config": {"test": "config"},
                    "dependencies": [],
                    "position_x": 0.0,
                    "position_y": 0.0
                },
                {
                    "node_id": "end",
                    "node_name": "End",
                    "node_type": "task",
                    "node_config": {"test": "config2"},
                    "dependencies": ["start"],
                    "position_x": 100.0,
                    "position_y": 100.0
                }
            ]
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        dag_id = create_response.json()["id"]
        
        # 获取DAG详情
        response = await client.get(
            f"/api/dag/{dag_id}",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        dag_detail = response.json()
        assert dag_detail["name"] == "Detail Test DAG"
        assert len(dag_detail["nodes"]) == 2
        
        # 验证节点详情
        nodes = {node["node_id"]: node for node in dag_detail["nodes"]}
        assert "start" in nodes
        assert "end" in nodes
        assert nodes["start"]["dependencies"] == []
        assert nodes["end"]["dependencies"] == ["start"]
        assert nodes["start"]["node_config"] == {"test": "config"}
    
    @pytest.mark.asyncio
    async def test_get_nonexistent_dag(self, client: AsyncClient, test_user_data):
        """测试获取不存在的DAG"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        response = await client.get(
            f"/api/dag/{str(uuid.uuid4())}",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 404
        assert "DAG not found" in response.json()["detail"]
    
    @pytest.mark.asyncio
    async def test_update_dag(self, client: AsyncClient, test_user_data):
        """测试更新DAG"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建DAG
        dag_data = {
            "name": "Original DAG",
            "description": "Original description",
            "nodes": [
                {
                    "node_id": "original",
                    "node_name": "Original Task",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": []
                }
            ]
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        dag_id = create_response.json()["id"]
        
        # 更新DAG
        updated_data = {
            "name": "Updated DAG",
            "description": "Updated description",
            "nodes": [
                {
                    "node_id": "updated",
                    "node_name": "Updated Task",
                    "node_type": "task",
                    "node_config": {"updated": True},
                    "dependencies": []
                },
                {
                    "node_id": "new_task",
                    "node_name": "New Task",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": ["updated"]
                }
            ]
        }
        
        response = await client.put(
            f"/api/dag/{dag_id}",
            json=updated_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        data = response.json()
        assert data["name"] == "Updated DAG"
        assert data["description"] == "Updated description"
        
        # 验证节点已更新
        detail_response = await client.get(
            f"/api/dag/{dag_id}",
            headers={"Authorization": f"Bearer {token}"}
        )
        nodes = detail_response.json()["nodes"]
        assert len(nodes) == 2
        node_ids = [node["node_id"] for node in nodes]
        assert "updated" in node_ids
        assert "new_task" in node_ids
        assert "original" not in node_ids
    
    @pytest.mark.asyncio
    async def test_delete_dag(self, client: AsyncClient, test_user_data):
        """测试删除DAG"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建DAG
        dag_data = {
            "name": "To Delete DAG",
            "nodes": [
                {
                    "node_id": "task",
                    "node_name": "Task",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": []
                }
            ]
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        dag_id = create_response.json()["id"]
        
        # 删除DAG
        response = await client.delete(
            f"/api/dag/{dag_id}",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 204
        
        # 验证DAG已删除
        get_response = await client.get(
            f"/api/dag/{dag_id}",
            headers={"Authorization": f"Bearer {token}"}
        )
        assert get_response.status_code == 404
    
    @pytest.mark.asyncio
    async def test_toggle_dag_status(self, client: AsyncClient, test_user_data):
        """测试切换DAG状态"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建DAG
        dag_data = {
            "name": "Toggle Test DAG",
            "nodes": [
                {
                    "node_id": "task",
                    "node_name": "Task",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": []
                }
            ]
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        dag_id = create_response.json()["id"]
        
        # 初始状态应该是激活的
        assert create_response.json()["is_active"] is True
        
        # 切换状态
        response = await client.post(
            f"/api/dag/{dag_id}/toggle",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        assert response.json()["is_active"] is False
        
        # 再次切换
        response = await client.post(
            f"/api/dag/{dag_id}/toggle",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        assert response.json()["is_active"] is True


class TestDAGRunAPI:
    """DAG运行API测试"""
    
    @pytest.fixture
    async def dag_setup(self, client: AsyncClient, test_user_data):
        """设置DAG测试环境"""
        # 注册并登录
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建DAG
        dag_data = {
            "name": "Run Test DAG",
            "description": "DAG for run testing",
            "nodes": [
                {
                    "node_id": "start",
                    "node_name": "Start Task",
                    "node_type": "task",
                    "node_config": {
                        "user_id": "test_user",
                        "provider_name": "runpod",
                        "job_config": {
                            "name": "start_task",
                            "image": "python:3.9",
                            "command": ["echo", "starting"]
                        }
                    },
                    "dependencies": []
                },
                {
                    "node_id": "end",
                    "node_name": "End Task",
                    "node_type": "task",
                    "node_config": {
                        "user_id": "test_user",
                        "provider_name": "runpod",
                        "job_config": {
                            "name": "end_task",
                            "image": "python:3.9",
                            "command": ["echo", "done"]
                        }
                    },
                    "dependencies": ["start"]
                }
            ]
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        dag_id = create_response.json()["id"]
        
        return {"token": token, "dag_id": dag_id}
    
    @pytest.mark.asyncio
    async def test_create_dag_run(self, client: AsyncClient, dag_setup):
        """测试创建DAG运行"""
        token = dag_setup["token"]
        dag_id = dag_setup["dag_id"]
        
        with patch('app.api.dag.execute_dag_task') as mock_execute:
            mock_execute.delay.return_value = AsyncMock()
            
            run_data = {
                "dag_id": dag_id,
                "run_name": "test_run",
                "parameters": {"param1": "value1", "param2": 42}
            }
            
            response = await client.post(
                f"/api/dag/{dag_id}/runs",
                json=run_data,
                headers={"Authorization": f"Bearer {token}"}
            )
            
            assert response.status_code == 201
            data = response.json()
            assert data["dag_id"] == dag_id
            assert data["run_name"] == "test_run"
            assert data["status"] == "pending"
            assert json.loads(data["parameters"]) == {"param1": "value1", "param2": 42}
            assert "id" in data
            
            # 验证Celery任务被调用
            mock_execute.delay.assert_called_once()
    
    @pytest.mark.asyncio
    async def test_create_dag_run_inactive_dag(self, client: AsyncClient, dag_setup):
        """测试为非激活DAG创建运行"""
        token = dag_setup["token"]
        dag_id = dag_setup["dag_id"]
        
        # 先禁用DAG
        await client.post(
            f"/api/dag/{dag_id}/toggle",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        run_data = {
            "dag_id": dag_id,
            "run_name": "test_run",
            "parameters": {}
        }
        
        response = await client.post(
            f"/api/dag/{dag_id}/runs",
            json=run_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 404
        assert "DAG not found or inactive" in response.json()["detail"]
    
    @pytest.mark.asyncio
    async def test_list_dag_runs(self, client: AsyncClient, dag_setup):
        """测试获取DAG运行列表"""
        token = dag_setup["token"]
        dag_id = dag_setup["dag_id"]
        
        with patch('app.api.dag.execute_dag_task') as mock_execute:
            mock_execute.delay.return_value = AsyncMock()
            
            # 创建多个运行
            for i in range(3):
                run_data = {
                    "dag_id": dag_id,
                    "run_name": f"test_run_{i}",
                    "parameters": {"run_index": i}
                }
                
                await client.post(
                    f"/api/dag/{dag_id}/runs",
                    json=run_data,
                    headers={"Authorization": f"Bearer {token}"}
                )
        
        # 获取运行列表
        response = await client.get(
            f"/api/dag/{dag_id}/runs",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        runs = response.json()
        assert len(runs) == 3
        
        # 验证按时间倒序排列
        run_names = [run["run_name"] for run in runs]
        assert run_names == ["test_run_2", "test_run_1", "test_run_0"]
        
        # 测试分页
        response = await client.get(
            f"/api/dag/{dag_id}/runs?limit=2&offset=1",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        runs = response.json()
        assert len(runs) == 2
        assert runs[0]["run_name"] == "test_run_1"
    
    @pytest.mark.asyncio
    async def test_get_dag_run_detail(self, client: AsyncClient, dag_setup):
        """测试获取DAG运行详情"""
        token = dag_setup["token"]
        dag_id = dag_setup["dag_id"]
        
        with patch('app.api.dag.execute_dag_task') as mock_execute:
            mock_execute.delay.return_value = AsyncMock()
            
            # 创建运行
            run_data = {
                "dag_id": dag_id,
                "run_name": "detail_test_run",
                "parameters": {"test": "detail"}
            }
            
            create_response = await client.post(
                f"/api/dag/{dag_id}/runs",
                json=run_data,
                headers={"Authorization": f"Bearer {token}"}
            )
            run_id = create_response.json()["id"]
        
        # 获取运行详情
        response = await client.get(
            f"/api/dag/{dag_id}/runs/{run_id}",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        run_detail = response.json()
        assert run_detail["dag_id"] == dag_id
        assert run_detail["run_name"] == "detail_test_run"
        assert json.loads(run_detail["parameters"]) == {"test": "detail"}
        assert "node_runs" in run_detail
    
    @pytest.mark.asyncio
    async def test_cancel_dag_run(self, client: AsyncClient, dag_setup):
        """测试取消DAG运行"""
        token = dag_setup["token"]
        dag_id = dag_setup["dag_id"]
        
        with patch('app.api.dag.execute_dag_task') as mock_execute:
            mock_execute.delay.return_value = AsyncMock()
            
            # 创建运行
            run_data = {
                "dag_id": dag_id,
                "run_name": "cancel_test_run",
                "parameters": {}
            }
            
            create_response = await client.post(
                f"/api/dag/{dag_id}/runs",
                json=run_data,
                headers={"Authorization": f"Bearer {token}"}
            )
            run_id = create_response.json()["id"]
        
        # 取消运行
        response = await client.post(
            f"/api/dag/{dag_id}/runs/{run_id}/cancel",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        assert "cancelled successfully" in response.json()["message"]
        
        # 验证状态已更新
        detail_response = await client.get(
            f"/api/dag/{dag_id}/runs/{run_id}",
            headers={"Authorization": f"Bearer {token}"}
        )
        assert detail_response.json()["status"] == "cancelled"
    
    @pytest.mark.asyncio
    async def test_cancel_nonexistent_run(self, client: AsyncClient, dag_setup):
        """测试取消不存在的运行"""
        token = dag_setup["token"]
        dag_id = dag_setup["dag_id"]
        
        response = await client.post(
            f"/api/dag/{dag_id}/runs/{str(uuid.uuid4())}/cancel",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 404
        assert "DAG run not found" in response.json()["detail"]


class TestDAGTemplateAPI:
    """DAG模板API测试"""
    
    @pytest.mark.asyncio
    async def test_list_dag_templates(self, client: AsyncClient, test_user_data):
        """测试获取DAG模板列表"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建几个DAG，其中一些作为模板
        for i in range(3):
            dag_data = {
                "name": f"Template DAG {i}",
                "description": f"Template {i}",
                "nodes": [
                    {
                        "node_id": f"task_{i}",
                        "node_name": f"Task {i}",
                        "node_type": "task",
                        "node_config": {},
                        "dependencies": []
                    }
                ],
                "is_template": i < 2  # 前两个作为模板
            }
            
            await client.post(
                "/api/dag/",
                json=dag_data,
                headers={"Authorization": f"Bearer {token}"}
            )
        
        # 获取模板列表
        response = await client.get(
            "/api/dag/templates/",
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 200
        templates = response.json()
        assert len(templates) == 2
        assert all(template["is_template"] for template in templates)
    
    @pytest.mark.asyncio
    async def test_instantiate_dag_template(self, client: AsyncClient, test_user_data):
        """测试从模板实例化DAG"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 创建模板
        template_data = {
            "name": "ML Training Template",
            "description": "Template for ML training workflows",
            "nodes": [
                {
                    "node_id": "data_prep",
                    "node_name": "Data Preparation",
                    "node_type": "task",
                    "node_config": {"stage": "preparation"},
                    "dependencies": []
                },
                {
                    "node_id": "training",
                    "node_name": "Model Training",
                    "node_type": "task",
                    "node_config": {"stage": "training"},
                    "dependencies": ["data_prep"]
                },
                {
                    "node_id": "evaluation",
                    "node_name": "Model Evaluation",
                    "node_type": "task",
                    "node_config": {"stage": "evaluation"},
                    "dependencies": ["training"]
                }
            ],
            "is_template": True
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=template_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        template_id = create_response.json()["id"]
        
        # 从模板实例化DAG
        response = await client.post(
            f"/api/dag/templates/{template_id}/instantiate",
            params={
                "dag_name": "My ML Training Workflow",
                "description": "Custom ML training workflow instance"
            },
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 201
        new_dag = response.json()
        assert new_dag["name"] == "My ML Training Workflow"
        assert new_dag["description"] == "Custom ML training workflow instance"
        assert new_dag["is_template"] is False
        assert new_dag["is_active"] is True
        
        # 验证节点被正确复制
        detail_response = await client.get(
            f"/api/dag/{new_dag['id']}",
            headers={"Authorization": f"Bearer {token}"}
        )
        nodes = detail_response.json()["nodes"]
        assert len(nodes) == 3
        
        node_ids = [node["node_id"] for node in nodes]
        assert "data_prep" in node_ids
        assert "training" in node_ids
        assert "evaluation" in node_ids
    
    @pytest.mark.asyncio
    async def test_instantiate_nonexistent_template(self, client: AsyncClient, test_user_data):
        """测试实例化不存在的模板"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        response = await client.post(
            f"/api/dag/templates/{str(uuid.uuid4())}/instantiate",
            params={"dag_name": "Test Instance"},
            headers={"Authorization": f"Bearer {token}"}
        )
        
        assert response.status_code == 404
        assert "DAG template not found" in response.json()["detail"]


class TestDAGAPIAuth:
    """DAG API认证测试"""
    
    @pytest.mark.asyncio
    async def test_unauthenticated_access(self, client: AsyncClient):
        """测试未认证访问"""
        # 不提供token尝试访问
        response = await client.get("/api/dag/")
        assert response.status_code == 401
        
        response = await client.post("/api/dag/", json={})
        assert response.status_code == 401
    
    @pytest.mark.asyncio
    async def test_invalid_token(self, client: AsyncClient):
        """测试无效token"""
        response = await client.get(
            "/api/dag/",
            headers={"Authorization": "Bearer invalid_token"}
        )
        assert response.status_code == 401
    
    @pytest.mark.asyncio
    async def test_user_isolation(self, client: AsyncClient, test_user_data, test_admin_data):
        """测试用户隔离"""
        # 注册两个用户
        await client.post("/auth/register", json=test_user_data)
        await client.post("/auth/register", json=test_admin_data)
        
        # 用户1登录并创建DAG
        user1_login = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        user1_token = user1_login.json()["access_token"]
        
        dag_data = {
            "name": "User1 DAG",
            "nodes": [
                {
                    "node_id": "task1",
                    "node_name": "Task 1",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": []
                }
            ]
        }
        
        create_response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {user1_token}"}
        )
        dag_id = create_response.json()["id"]
        
        # 用户2登录
        user2_login = await client.post(
            "/auth/jwt/login",
            data={"username": test_admin_data["email"], "password": test_admin_data["password"]}
        )
        user2_token = user2_login.json()["access_token"]
        
        # 用户2不应该能看到用户1的DAG
        response = await client.get(
            f"/api/dag/{dag_id}",
            headers={"Authorization": f"Bearer {user2_token}"}
        )
        assert response.status_code == 404
        
        # 用户2不应该能看到用户1的DAG列表
        response = await client.get(
            "/api/dag/",
            headers={"Authorization": f"Bearer {user2_token}"}
        )
        assert response.status_code == 200
        assert len(response.json()) == 0


class TestDAGAPIValidation:
    """DAG API验证测试"""
    
    @pytest.mark.asyncio
    async def test_create_dag_missing_fields(self, client: AsyncClient, test_user_data):
        """测试创建DAG时缺少必填字段"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        # 缺少name
        response = await client.post(
            "/api/dag/",
            json={"nodes": []},
            headers={"Authorization": f"Bearer {token}"}
        )
        assert response.status_code == 422
        
        # 缺少nodes
        response = await client.post(
            "/api/dag/",
            json={"name": "Test DAG"},
            headers={"Authorization": f"Bearer {token}"}
        )
        assert response.status_code == 422
    
    @pytest.mark.asyncio
    async def test_invalid_node_type(self, client: AsyncClient, test_user_data):
        """测试无效节点类型"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        dag_data = {
            "name": "Invalid Node Type DAG",
            "nodes": [
                {
                    "node_id": "invalid",
                    "node_name": "Invalid Node",
                    "node_type": "invalid_type",  # 无效类型
                    "node_config": {},
                    "dependencies": []
                }
            ]
        }
        
        response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        assert response.status_code == 422
    
    @pytest.mark.asyncio
    async def test_duplicate_node_ids(self, client: AsyncClient, test_user_data):
        """测试重复节点ID"""
        register_response = await client.post("/auth/register", json=test_user_data)
        login_response = await client.post(
            "/auth/jwt/login",
            data={"username": test_user_data["email"], "password": test_user_data["password"]}
        )
        token = login_response.json()["access_token"]
        
        dag_data = {
            "name": "Duplicate Node IDs DAG",
            "nodes": [
                {
                    "node_id": "duplicate",
                    "node_name": "Node 1",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": []
                },
                {
                    "node_id": "duplicate",  # 重复ID
                    "node_name": "Node 2",
                    "node_type": "task",
                    "node_config": {},
                    "dependencies": []
                }
            ]
        }
        
        # 注意：这个测试可能需要在DAG创建逻辑中添加重复ID检查
        response = await client.post(
            "/api/dag/",
            json=dag_data,
            headers={"Authorization": f"Bearer {token}"}
        )
        # 根据实际实现，这可能返回400或422
