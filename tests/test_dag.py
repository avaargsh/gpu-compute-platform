import pytest
import asyncio
import json
import uuid
from unittest.mock import Mock, patch, AsyncMock
from datetime import datetime, timezone
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.dag import (
    TaskDAG, DAGRun, DAGNode, DAGNodeRun, DAGEdge, DAGTemplate,
    DAGStatus, NodeStatus, NodeType
)
from app.models.user import User
from app.core.dag_engine import (
    DAGEngine, DAGNodeExecutor, DAGTopologyAnalyzer, 
    DAGExecutionContext, execute_dag_task
)


class TestDAGModels:
    """DAG数据模型测试"""
    
    @pytest.mark.asyncio
    async def test_task_dag_creation(self, test_session):
        """测试TaskDAG创建"""
        # 首先创建用户
        user = User(
            email="test@example.com",
            hashed_password="hashedpassword",
            first_name="Test",
            last_name="User"
        )
        test_session.add(user)
        await test_session.commit()
        await test_session.refresh(user)
        
        dag = TaskDAG(
            id=str(uuid.uuid4()),
            name="Test DAG",
            description="Test DAG description",
            user_id=str(user.id),
            dag_config="{}",
            schedule_expression="0 0 * * *",
            is_active=True
        )
        
        test_session.add(dag)
        await test_session.commit()
        await test_session.refresh(dag)
        
        assert dag.id is not None
        assert dag.name == "Test DAG"
        assert dag.description == "Test DAG description"
        assert dag.user_id == str(user.id)
        assert dag.is_active is True
        assert dag.is_template is False
        assert dag.success_rate == 0.0
        assert dag.total_runs == 0
    
    @pytest.mark.asyncio
    async def test_dag_node_creation(self, test_session):
        """测试DAGNode创建"""
        # 创建用户和DAG
        user = User(
            email="test+dag_node@example.com",
            hashed_password="hashedpassword",
            first_name="Test",
            last_name="User"
        )
        test_session.add(user)
        await test_session.commit()
        await test_session.refresh(user)
        
        dag = TaskDAG(
            id=str(uuid.uuid4()),
            name="Test DAG",
            user_id=str(user.id),
            dag_config="{}"
        )
        test_session.add(dag)
        await test_session.commit()
        await test_session.refresh(dag)
        
        # 创建DAG节点
        node = DAGNode(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            node_id="task_1",
            node_name="Task 1",
            node_type=NodeType.TASK,
            node_config=json.dumps({"image": "python:3.9", "command": ["python", "script.py"]}),
            depends_on=json.dumps(["task_0"]),
            position_x=100,
            position_y=200
        )
        
        test_session.add(node)
        await test_session.commit()
        await test_session.refresh(node)
        
        assert node.id is not None
        assert node.dag_id == dag.id
        assert node.node_id == "task_1"
        assert node.node_name == "Task 1"
        assert node.node_type == NodeType.TASK
        assert node.position_x == 100
        assert node.position_y == 200
        
        # 测试依赖关系解析
        dependencies = node.get_dependencies()
        assert dependencies == ["task_0"]
    
    @pytest.mark.asyncio
    async def test_dag_run_creation(self, test_session):
        """测试DAGRun创建"""
        # 创建用户和DAG
        user = User(
            email="test+dag_run@example.com",
            hashed_password="hashedpassword",
            first_name="Test",
            last_name="User"
        )
        test_session.add(user)
        await test_session.commit()
        await test_session.refresh(user)
        
        dag = TaskDAG(
            id=str(uuid.uuid4()),
            name="Test DAG",
            user_id=str(user.id),
            dag_config="{}"
        )
        test_session.add(dag)
        await test_session.commit()
        await test_session.refresh(dag)
        
        # 创建DAG运行
        dag_run = DAGRun(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            run_name="manual_run_20240101_120000",
            status=DAGStatus.PENDING,
            parameters=json.dumps({"param1": "value1"}),
            user_id=str(user.id)
        )
        
        test_session.add(dag_run)
        await test_session.commit()
        await test_session.refresh(dag_run)
        
        assert dag_run.id is not None
        assert dag_run.dag_id == dag.id
        assert dag_run.run_name == "manual_run_20240101_120000"
        assert dag_run.status == DAGStatus.PENDING
        assert dag_run.user_id == str(user.id)
        assert dag_run.duration_seconds is None  # 未完成
        
        # 测试参数解析
        parameters = json.loads(dag_run.parameters)
        assert parameters == {"param1": "value1"}


class TestDAGTopologyAnalyzer:
    """DAG拓扑分析器测试"""
    
    def create_sample_nodes(self):
        """创建示例节点"""
        nodes = [
            DAGNode(
                id="1", dag_id="dag1", node_id="start", node_name="Start",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps([])
            ),
            DAGNode(
                id="2", dag_id="dag1", node_id="task_a", node_name="Task A",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["start"])
            ),
            DAGNode(
                id="3", dag_id="dag1", node_id="task_b", node_name="Task B",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["start"])
            ),
            DAGNode(
                id="4", dag_id="dag1", node_id="end", node_name="End",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["task_a", "task_b"])
            )
        ]
        return nodes
    
    def test_build_dependency_graph(self):
        """测试依赖图构建"""
        nodes = self.create_sample_nodes()
        graph = DAGTopologyAnalyzer.build_dependency_graph(nodes)
        
        assert "start" in graph
        assert "task_a" in graph["start"]
        assert "task_b" in graph["start"]
        assert "end" in graph["task_a"]
        assert "end" in graph["task_b"]
    
    def test_topological_sort(self):
        """测试拓扑排序"""
        nodes = self.create_sample_nodes()
        sorted_nodes = DAGTopologyAnalyzer.topological_sort(nodes)
        
        # 验证排序结果
        assert sorted_nodes[0] == "start"
        assert sorted_nodes[-1] == "end"
        
        # task_a和task_b可以是任意顺序，但都应在start之后、end之前
        task_a_index = sorted_nodes.index("task_a")
        task_b_index = sorted_nodes.index("task_b")
        start_index = sorted_nodes.index("start")
        end_index = sorted_nodes.index("end")
        
        assert start_index < task_a_index < end_index
        assert start_index < task_b_index < end_index
    
    def test_topological_sort_with_cycle(self):
        """测试环形依赖检测"""
        # 创建有环的节点
        cycle_nodes = [
            DAGNode(
                id="1", dag_id="dag1", node_id="task_a", node_name="Task A",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["task_b"])
            ),
            DAGNode(
                id="2", dag_id="dag1", node_id="task_b", node_name="Task B",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["task_a"])
            )
        ]
        
        with pytest.raises(ValueError, match="DAG contains cycles"):
            DAGTopologyAnalyzer.topological_sort(cycle_nodes)
    
    def test_find_ready_nodes(self):
        """测试查找可执行节点"""
        nodes = self.create_sample_nodes()
        
        # 初始状态，只有start可以执行
        completed = set()
        ready = DAGTopologyAnalyzer.find_ready_nodes(nodes, completed)
        assert ready == ["start"]
        
        # start完成后，task_a和task_b都可以执行
        completed = {"start"}
        ready = DAGTopologyAnalyzer.find_ready_nodes(nodes, completed)
        assert set(ready) == {"task_a", "task_b"}
        
        # task_a和task_b都完成后，end可以执行
        completed = {"start", "task_a", "task_b"}
        ready = DAGTopologyAnalyzer.find_ready_nodes(nodes, completed)
        assert ready == ["end"]
    
    def test_validate_dag(self):
        """测试DAG验证"""
        # 有效的DAG
        valid_nodes = self.create_sample_nodes()
        assert DAGTopologyAnalyzer.validate_dag(valid_nodes) is True
        
        # 无效的DAG（有环）
        invalid_nodes = [
            DAGNode(
                id="1", dag_id="dag1", node_id="task_a", node_name="Task A",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["task_b"])
            ),
            DAGNode(
                id="2", dag_id="dag1", node_id="task_b", node_name="Task B",
                node_type=NodeType.TASK, node_config="{}",
                depends_on=json.dumps(["task_a"])
            )
        ]
        assert DAGTopologyAnalyzer.validate_dag(invalid_nodes) is False


class TestDAGExecutionContext:
    """DAG执行上下文测试"""
    
    def test_context_creation(self):
        """测试执行上下文创建"""
        context = DAGExecutionContext(
            dag_run_id="run_123",
            variables={"var1": "value1"},
            outputs={"node1": {"result": "success"}}
        )
        
        assert context.dag_run_id == "run_123"
        assert context.get_variable("var1") == "value1"
        assert context.get_node_output("node1") == {"result": "success"}
    
    def test_context_variable_operations(self):
        """测试上下文变量操作"""
        context = DAGExecutionContext(
            dag_run_id="run_123",
            variables={},
            outputs={}
        )
        
        # 设置和获取变量
        context.set_variable("test_var", "test_value")
        assert context.get_variable("test_var") == "test_value"
        assert context.get_variable("nonexistent") is None
        assert context.get_variable("nonexistent", "default") == "default"
    
    def test_context_output_operations(self):
        """测试上下文输出操作"""
        context = DAGExecutionContext(
            dag_run_id="run_123",
            variables={},
            outputs={}
        )
        
        # 设置和获取节点输出
        context.set_node_output("node1", {"status": "completed", "data": [1, 2, 3]})
        output = context.get_node_output("node1")
        assert output == {"status": "completed", "data": [1, 2, 3]}
        assert context.get_node_output("nonexistent") is None
        assert context.get_node_output("nonexistent", {}) == {}


class TestDAGNodeExecutor:
    """DAG节点执行器测试"""
    
    @pytest.fixture
    def mock_session(self):
        """模拟数据库会话"""
        session = Mock(spec=AsyncSession)
        session.execute = AsyncMock()
        session.commit = AsyncMock()
        return session
    
    @pytest.fixture
    def node_executor(self, mock_session):
        """创建节点执行器"""
        return DAGNodeExecutor(mock_session)
    
    @pytest.fixture
    def sample_context(self):
        """示例执行上下文"""
        return DAGExecutionContext(
            dag_run_id="run_123",
            variables={"test_var": "test_value"},
            outputs={"upstream_node": {"status": "success", "data": "result"}}
        )
    
    def test_evaluate_condition_simple(self, node_executor, sample_context):
        """测试简单条件评估"""
        # 简单的条件评估
        result = node_executor._evaluate_condition("True", sample_context)
        assert result is True
        
        result = node_executor._evaluate_condition("False", sample_context)
        assert result is False
    
    def test_substitute_variables(self, node_executor, sample_context):
        """测试变量替换"""
        # 字符串变量替换
        data = "${node_output.upstream_node.status}"
        result = node_executor._substitute_variables(data, sample_context)
        assert result == "success"
        
        # 字典变量替换
        data = {
            "status": "${node_output.upstream_node.status}",
            "data": "${node_output.upstream_node.data}",
            "static": "value"
        }
        result = node_executor._substitute_variables(data, sample_context)
        assert result == {
            "status": "success",
            "data": "result", 
            "static": "value"
        }
        
        # 列表变量替换
        data = ["${node_output.upstream_node.status}", "static", "${node_output.upstream_node.data}"]
        result = node_executor._substitute_variables(data, sample_context)
        assert result == ["success", "static", "result"]
    
    def test_substitute_variables_nonexistent(self, node_executor, sample_context):
        """测试不存在变量的替换"""
        data = "${node_output.nonexistent.status}"
        result = node_executor._substitute_variables(data, sample_context)
        assert result == ""  # 不存在的变量应返回空字符串
    
    @pytest.mark.asyncio
    async def test_update_node_run_status(self, node_executor, mock_session):
        """测试更新节点运行状态"""
        await node_executor._update_node_run_status(
            "node_run_123",
            NodeStatus.RUNNING,
            started_at=datetime.now(timezone.utc)
        )
        
        # 验证SQL执行
        mock_session.execute.assert_called_once()
        mock_session.commit.assert_called_once()


class TestDAGEngine:
    """DAG引擎测试"""
    
    @pytest.fixture
    def dag_engine(self):
        """创建DAG引擎"""
        return DAGEngine()
    
    @pytest.fixture
    def mock_session(self):
        """模拟数据库会话"""
        session = Mock(spec=AsyncSession)
        session.execute = AsyncMock()
        session.commit = AsyncMock()
        return session
    
    def test_dag_engine_initialization(self, dag_engine):
        """测试DAG引擎初始化"""
        assert dag_engine is not None
        assert isinstance(dag_engine.running_dags, dict)
        assert len(dag_engine.running_dags) == 0
    
    @pytest.mark.asyncio
    async def test_get_dag_run_not_found(self, dag_engine, mock_session):
        """测试获取不存在的DAG运行"""
        mock_result = Mock()
        mock_result.scalar_one_or_none.return_value = None
        mock_session.execute.return_value = mock_result
        
        dag_run = await dag_engine._get_dag_run(mock_session, "nonexistent_run")
        assert dag_run is None
    
    @pytest.mark.asyncio
    async def test_update_dag_run_status(self, dag_engine, mock_session):
        """测试更新DAG运行状态"""
        await dag_engine._update_dag_run_status(
            mock_session,
            "run_123",
            DAGStatus.RUNNING,
            started_at=datetime.now(timezone.utc)
        )
        
        # 验证SQL执行
        mock_session.execute.assert_called_once()
        mock_session.commit.assert_called_once()


class TestDAGIntegration:
    """DAG集成测试"""
    
    @pytest.mark.asyncio
    async def test_dag_creation_and_execution_flow(self, test_session):
        """测试DAG创建和执行流程"""
        # 1. 创建用户
        import uuid as _uuid
        user = User(
            email=f"test+{_uuid.uuid4().hex[:8]}@example.com",
            hashed_password="hashedpassword",
            first_name="Test",
            last_name="User"
        )
        test_session.add(user)
        await test_session.commit()
        await test_session.refresh(user)
        
        # 2. 创建DAG
        dag = TaskDAG(
            id=str(uuid.uuid4()),
            name="Integration Test DAG",
            description="Test DAG for integration testing",
            user_id=str(user.id),
            dag_config="{}",
            is_active=True
        )
        test_session.add(dag)
        await test_session.commit()
        await test_session.refresh(dag)
        
        # 3. 创建DAG节点
        start_node = DAGNode(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            node_id="start",
            node_name="Start Task",
            node_type=NodeType.TASK,
            node_config=json.dumps({
                "user_id": str(user.id),
                "provider_name": "runpod",
                "job_config": {
                    "name": "start_task",
                    "image": "python:3.9",
                    "command": ["echo", "starting"]
                },
                "provider_config": {}
            }),
            dependencies=json.dumps([])
        )
        
        end_node = DAGNode(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            node_id="end",
            node_name="End Task",
            node_type=NodeType.TASK,
            node_config=json.dumps({
                "user_id": str(user.id),
                "provider_name": "runpod",
                "job_config": {
                    "name": "end_task",
                    "image": "python:3.9",
                    "command": ["echo", "finished"]
                },
                "provider_config": {}
            }),
            dependencies=json.dumps(["start"])
        )
        
        test_session.add(start_node)
        test_session.add(end_node)
        await test_session.commit()
        
        # 4. 创建DAG运行
        dag_run = DAGRun(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            run_name="test_run",
            status=DAGStatus.PENDING,
            parameters=json.dumps({}),
            user_id=str(user.id)
        )
        test_session.add(dag_run)
        await test_session.commit()
        await test_session.refresh(dag_run)
        
        # 5. 验证DAG结构
        assert dag.id is not None
        assert len(dag.dag_nodes) == 0  # 需要重新加载关系
        
        # 6. 验证拓扑结构
        nodes = [start_node, end_node]
        is_valid = DAGTopologyAnalyzer.validate_dag(nodes)
        assert is_valid is True
        
        sorted_nodes = DAGTopologyAnalyzer.topological_sort(nodes)
        assert sorted_nodes == ["start", "end"]
    
    @pytest.mark.asyncio 
    async def test_complex_dag_topology(self, test_session):
        """测试复杂DAG拓扑"""
        # 创建用户
        import uuid as _uuid
        user = User(
            email=f"test+{_uuid.uuid4().hex[:8]}@example.com",
            hashed_password="hashedpassword",
            first_name="Test",
            last_name="User"
        )
        test_session.add(user)
        await test_session.commit()
        await test_session.refresh(user)
        
        # 创建DAG
        dag = TaskDAG(
            id=str(uuid.uuid4()),
            name="Complex DAG",
            user_id=str(user.id),
            dag_config="{}"
        )
        test_session.add(dag)
        await test_session.commit()
        await test_session.refresh(dag)
        
        # 创建复杂的节点结构
        # start -> [task1, task2] -> task3 -> end
        #       \-> task4 --------/
        nodes = [
            DAGNode(
                id=str(uuid.uuid4()), dag_id=dag.id, node_id="start", 
                node_name="Start", node_type=NodeType.TASK,
                node_config="{}", dependencies=json.dumps([])
            ),
            DAGNode(
                id=str(uuid.uuid4()), dag_id=dag.id, node_id="task1",
                node_name="Task 1", node_type=NodeType.TASK,
                node_config="{}", dependencies=json.dumps(["start"])
            ),
            DAGNode(
                id=str(uuid.uuid4()), dag_id=dag.id, node_id="task2",
                node_name="Task 2", node_type=NodeType.TASK,
                node_config="{}", dependencies=json.dumps(["start"])
            ),
            DAGNode(
                id=str(uuid.uuid4()), dag_id=dag.id, node_id="task3",
                node_name="Task 3", node_type=NodeType.TASK,
                node_config="{}", dependencies=json.dumps(["task1", "task2"])
            ),
            DAGNode(
                id=str(uuid.uuid4()), dag_id=dag.id, node_id="task4",
                node_name="Task 4", node_type=NodeType.TASK,
                node_config="{}", dependencies=json.dumps(["start"])
            ),
            DAGNode(
                id=str(uuid.uuid4()), dag_id=dag.id, node_id="end",
                node_name="End", node_type=NodeType.TASK,
                node_config="{}", dependencies=json.dumps(["task3", "task4"])
            )
        ]
        
        for node in nodes:
            test_session.add(node)
        await test_session.commit()
        
        # 验证拓扑排序
        sorted_nodes = DAGTopologyAnalyzer.topological_sort(nodes)
        
        # 验证顺序约束
        start_idx = sorted_nodes.index("start")
        task1_idx = sorted_nodes.index("task1") 
        task2_idx = sorted_nodes.index("task2")
        task3_idx = sorted_nodes.index("task3")
        task4_idx = sorted_nodes.index("task4")
        end_idx = sorted_nodes.index("end")
        
        # start应该在最前面
        assert start_idx == 0
        
        # task1, task2, task4都应该在start之后
        assert start_idx < task1_idx
        assert start_idx < task2_idx
        assert start_idx < task4_idx
        
        # task3应该在task1和task2之后
        assert task1_idx < task3_idx
        assert task2_idx < task3_idx
        
        # end应该在task3和task4之后
        assert task3_idx < end_idx
        assert task4_idx < end_idx
        
        # 验证准备就绪的节点
        ready_nodes = DAGTopologyAnalyzer.find_ready_nodes(nodes, set())
        assert ready_nodes == ["start"]
        
        ready_nodes = DAGTopologyAnalyzer.find_ready_nodes(nodes, {"start"})
        assert set(ready_nodes) == {"task1", "task2", "task4"}
        
        ready_nodes = DAGTopologyAnalyzer.find_ready_nodes(nodes, {"start", "task1", "task2", "task4"})
        assert ready_nodes == ["task3"]
        
        ready_nodes = DAGTopologyAnalyzer.find_ready_nodes(nodes, {"start", "task1", "task2", "task3", "task4"})
        assert ready_nodes == ["end"]


@pytest.mark.asyncio
async def test_celery_dag_execution_task():
    """测试Celery DAG执行任务"""
    with patch('app.core.database.get_async_session') as mock_get_session:
        with patch('app.core.dag_engine.DAGEngine.execute_dag') as mock_execute:
            # 模拟数据库会话
            mock_session = AsyncMock()
            mock_get_session.return_value.__anext__ = AsyncMock(return_value=mock_session)
            
            # 模拟DAG执行
            mock_execute.return_value = True
            
            # 执行任务
            result = execute_dag_task("dag_run_123")
            
            # 让事件循环有机会调度任务
            await asyncio.sleep(0)
            
            # 验证执行
            assert result is True
            mock_execute.assert_called_once_with(mock_session, "dag_run_123")


class TestDAGErrorHandling:
    """DAG错误处理测试"""
    
    def test_invalid_json_in_node_config(self):
        """测试节点配置中的无效JSON"""
        with pytest.raises(json.JSONDecodeError):
            node = DAGNode(
                id="1", dag_id="dag1", node_id="task1", node_name="Task 1",
                node_type=NodeType.TASK, node_config="invalid json",
                dependencies="[]"
            )
            json.loads(node.node_config)
    
    def test_invalid_json_in_dependencies(self):
        """测试依赖关系中的无效JSON"""
        node = DAGNode(
            id="1", dag_id="dag1", node_id="task1", node_name="Task 1",
            node_type=NodeType.TASK, node_config="{}",
            dependencies="invalid json"
        )
        
        with pytest.raises(json.JSONDecodeError):
            node.get_dependencies()
    
    def test_circular_dependency_detection(self):
        """测试循环依赖检测"""
        nodes = [
            DAGNode(
                id="1", dag_id="dag1", node_id="task_a", node_name="Task A",
                node_type=NodeType.TASK, node_config="{}",
                dependencies=json.dumps(["task_c"])
            ),
            DAGNode(
                id="2", dag_id="dag1", node_id="task_b", node_name="Task B", 
                node_type=NodeType.TASK, node_config="{}",
                dependencies=json.dumps(["task_a"])
            ),
            DAGNode(
                id="3", dag_id="dag1", node_id="task_c", node_name="Task C",
                node_type=NodeType.TASK, node_config="{}",
                dependencies=json.dumps(["task_b"])
            )
        ]
        
        # 应该检测到循环依赖
        assert DAGTopologyAnalyzer.validate_dag(nodes) is False
        
        with pytest.raises(ValueError, match="DAG contains cycles"):
            DAGTopologyAnalyzer.topological_sort(nodes)
    
    def test_missing_dependency_reference(self):
        """测试缺失的依赖引用"""
        nodes = [
            DAGNode(
                id="1", dag_id="dag1", node_id="task_a", node_name="Task A",
                node_type=NodeType.TASK, node_config="{}",
                dependencies=json.dumps(["nonexistent_task"])
            )
        ]
        
        # 拓扑排序应该能处理不存在的依赖
        result = DAGTopologyAnalyzer.topological_sort(nodes)
        assert result == ["task_a"]  # 由于依赖不存在，task_a被视为没有依赖
    
    @pytest.mark.asyncio
    async def test_dag_execution_with_nonexistent_run(self):
        """测试执行不存在的DAG运行"""
        engine = DAGEngine()
        
        with patch('app.core.dag_engine.DAGEngine._get_dag_run') as mock_get_dag_run:
            mock_get_dag_run.return_value = None
            mock_session = Mock(spec=AsyncSession)
            
            result = await engine.execute_dag(mock_session, "nonexistent_run")
            assert result is False
