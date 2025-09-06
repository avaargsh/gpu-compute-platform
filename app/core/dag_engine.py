import json
import logging
import asyncio
from datetime import datetime, timezone
from typing import Dict, List, Optional, Any, Set, Tuple
from dataclasses import dataclass
from collections import defaultdict, deque

from celery import chain, group, chord
from celery.result import GroupResult
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select, update
from app.core.database import get_async_session

from app.core.celery_app import celery_app
from app.models.dag import (
    TaskDAG, DAGRun, DAGNode, DAGNodeRun, DAGEdge,
    DAGStatus, NodeStatus, NodeType
)
from app.models.task import GpuTask, TaskStatus
from app.tasks.gpu_tasks import execute_gpu_task
from app.core.mlflow_config import MLflowTaskTracker

logger = logging.getLogger(__name__)


@dataclass
class DAGExecutionContext:
    """DAG执行上下文"""
    dag_run_id: str
    variables: Dict[str, Any]
    outputs: Dict[str, Any]  # 节点输出数据
    
    def set_variable(self, key: str, value: Any):
        """设置变量"""
        self.variables[key] = value
    
    def get_variable(self, key: str, default: Any = None) -> Any:
        """获取变量"""
        return self.variables.get(key, default)
    
    def set_node_output(self, node_id: str, output: Any):
        """设置节点输出"""
        self.outputs[node_id] = output
    
    def get_node_output(self, node_id: str, default: Any = None) -> Any:
        """获取节点输出"""
        return self.outputs.get(node_id, default)


class DAGTopologyAnalyzer:
    """DAG拓扑分析器"""
    
    @staticmethod
    def build_dependency_graph(nodes: List[DAGNode]) -> Dict[str, Set[str]]:
        """构建依赖图"""
        graph = defaultdict(set)
        for node in nodes:
            dependencies = node.get_dependencies()
            for dep in dependencies:
                graph[dep].add(node.node_id)
        return graph
    
    @staticmethod
    def topological_sort(nodes: List[DAGNode]) -> List[str]:
        """拓扑排序，返回执行顺序。
        忽略对不存在节点的依赖，使其不影响入度计算。
        """
        # 构建入度表和邻接表
        in_degree = defaultdict(int)
        graph = defaultdict(list)
        node_ids = {n.node_id for n in nodes}
        
        # 初始化所有节点的入度为0
        for node in nodes:
            in_degree[node.node_id] = 0
        
        # 构建图和计算入度（忽略不存在的依赖）
        for node in nodes:
            dependencies = [d for d in node.get_dependencies() if d in node_ids]
            in_degree[node.node_id] = len(dependencies)
            for dep in dependencies:
                graph[dep].append(node.node_id)
        
        # Kahn算法进行拓扑排序
        queue = deque([node_id for node_id in in_degree if in_degree[node_id] == 0])
        result = []
        
        while queue:
            current = queue.popleft()
            result.append(current)
            
            for neighbor in graph[current]:
                in_degree[neighbor] -= 1
                if in_degree[neighbor] == 0:
                    queue.append(neighbor)
        
        # 检查是否存在环
        if len(result) != len(nodes):
            raise ValueError("DAG contains cycles")
        
        return result
    
    @staticmethod
    def find_ready_nodes(nodes: List[DAGNode], completed_nodes: Set[str]) -> List[str]:
        """查找可以执行的节点（所有依赖都已完成）"""
        ready = []
        for node in nodes:
            dependencies = set(node.get_dependencies())
            if dependencies.issubset(completed_nodes) and node.node_id not in completed_nodes:
                ready.append(node.node_id)
        return ready
    
    @staticmethod
    def validate_dag(nodes: List[DAGNode]) -> bool:
        """验证DAG的有效性"""
        try:
            DAGTopologyAnalyzer.topological_sort(nodes)
            return True
        except ValueError:
            return False


class DAGNodeExecutor:
    """DAG节点执行器"""
    
    def __init__(self, session: AsyncSession):
        self.session = session
    
    async def execute_node(
        self, 
        node: DAGNode, 
        node_run: DAGNodeRun, 
        context: DAGExecutionContext
    ) -> bool:
        """执行DAG节点"""
        
        try:
            # 更新节点状态为运行中
            await self._update_node_run_status(
                node_run.id, 
                NodeStatus.RUNNING, 
                started_at=datetime.now(timezone.utc)
            )
            
            success = False
            
            if node.node_type == NodeType.TASK:
                success = await self._execute_task_node(node, node_run, context)
            elif node.node_type == NodeType.CONDITION:
                success = await self._execute_condition_node(node, node_run, context)
            elif node.node_type == NodeType.WEBHOOK:
                success = await self._execute_webhook_node(node, node_run, context)
            else:
                logger.warning(f"Unsupported node type: {node.node_type}")
                success = True  # 跳过未支持的节点类型
            
            # 更新最终状态
            final_status = NodeStatus.COMPLETED if success else NodeStatus.FAILED
            await self._update_node_run_status(
                node_run.id,
                final_status,
                completed_at=datetime.now(timezone.utc)
            )
            
            return success
            
        except Exception as e:
            logger.error(f"Node execution failed: {e}")
            await self._update_node_run_status(
                node_run.id,
                NodeStatus.FAILED,
                completed_at=datetime.now(timezone.utc),
                error_message=str(e)
            )
            return False
    
    async def _execute_task_node(
        self, 
        node: DAGNode, 
        node_run: DAGNodeRun, 
        context: DAGExecutionContext
    ) -> bool:
        """执行GPU任务节点"""
        try:
            # 解析节点配置
            config = json.loads(node.node_config)
            
            # 创建GPU任务
            gpu_task = GpuTask(
                name=f"{node.node_name}",
                description=f"DAG节点任务: {node.node_name}",
                user_id=config.get("user_id"),
                provider_name=config.get("provider_name"),
                job_config=json.dumps(config.get("job_config", {})),
                status=TaskStatus.PENDING
            )
            
            self.session.add(gpu_task)
            await self.session.commit()
            await self.session.refresh(gpu_task)
            
            # 关联GPU任务到节点运行
            node_run.gpu_task_id = gpu_task.id
            await self.session.commit()
            
            # 提交Celery任务
            provider_config = config.get("provider_config", {})
            celery_task = execute_gpu_task.delay(gpu_task.id, provider_config)
            
            # 更新Celery任务ID
            node_run.celery_task_id = celery_task.id
            gpu_task.celery_task_id = celery_task.id
            await self.session.commit()
            
            # 等待任务完成（这里可以异步轮询）
            result = celery_task.get(timeout=3600)  # 1小时超时
            
            # 设置节点输出
            context.set_node_output(node.node_id, result)
            
            return result.get("status") in ["completed", "success"]
            
        except Exception as e:
            logger.error(f"Task node execution failed: {e}")
            return False
    
    async def _execute_condition_node(
        self, 
        node: DAGNode, 
        node_run: DAGNodeRun, 
        context: DAGExecutionContext
    ) -> bool:
        """执行条件节点"""
        try:
            config = json.loads(node.node_config)
            condition = config.get("condition", "")
            
            # 简单的条件评估（可以扩展为更复杂的表达式）
            # 支持的格式: "node_output.task1.status == 'success'"
            result = self._evaluate_condition(condition, context)
            
            # 设置节点输出
            context.set_node_output(node.node_id, {"condition_result": result})
            
            return result
            
        except Exception as e:
            logger.error(f"Condition node execution failed: {e}")
            return False
    
    async def _execute_webhook_node(
        self, 
        node: DAGNode, 
        node_run: DAGNodeRun, 
        context: DAGExecutionContext
    ) -> bool:
        """执行Webhook节点"""
        try:
            import httpx
            
            config = json.loads(node.node_config)
            url = config.get("url")
            method = config.get("method", "POST")
            headers = config.get("headers", {})
            payload = config.get("payload", {})
            
            # 替换payload中的变量
            payload = self._substitute_variables(payload, context)
            
            async with httpx.AsyncClient() as client:
                response = await client.request(
                    method=method,
                    url=url,
                    headers=headers,
                    json=payload,
                    timeout=30
                )
                
                success = 200 <= response.status_code < 300
                
                # 设置节点输出
                context.set_node_output(node.node_id, {
                    "status_code": response.status_code,
                    "response": response.json() if success else response.text
                })
                
                return success
                
        except Exception as e:
            logger.error(f"Webhook node execution failed: {e}")
            return False
    
    def _evaluate_condition(self, condition: str, context: DAGExecutionContext) -> bool:
        """评估条件表达式"""
        # 简单的条件评估实现
        # 在生产环境中应该使用更安全的表达式评估器
        try:
            # 替换变量
            eval_condition = self._substitute_condition_variables(condition, context)
            return eval(eval_condition)  # 注意：在生产环境中不要使用eval
        except:
            return False
    
    def _substitute_variables(self, data: Any, context: DAGExecutionContext) -> Any:
        """替换数据中的变量"""
        if isinstance(data, dict):
            return {k: self._substitute_variables(v, context) for k, v in data.items()}
        elif isinstance(data, list):
            return [self._substitute_variables(item, context) for item in data]
        elif isinstance(data, str):
            # 简单的变量替换: ${node_output.task1.status}
            if data.startswith("${") and data.endswith("}"):
                var_path = data[2:-1].split(".")
                if len(var_path) >= 2 and var_path[0] == "node_output":
                    node_id = var_path[1]
                    output = context.get_node_output(node_id, {})
                    for key in var_path[2:]:
                        output = output.get(key, "") if isinstance(output, dict) else ""
                    return output
            return data
        else:
            return data
    
    def _substitute_condition_variables(self, condition: str, context: DAGExecutionContext) -> str:
        """替换条件表达式中的变量"""
        # 这里应该实现更安全的变量替换
        # 暂时使用简单的字符串替换
        return condition
    
    async def _update_node_run_status(
        self, 
        node_run_id: str, 
        status: NodeStatus, 
        **kwargs
    ):
        """更新节点运行状态"""
        update_data = {"status": status, **kwargs}
        stmt = update(DAGNodeRun).where(DAGNodeRun.id == node_run_id).values(**update_data)
        await self.session.execute(stmt)
        await self.session.commit()


class DAGEngine:
    """DAG执行引擎"""
    
    def __init__(self):
        self.running_dags = {}  # dag_run_id -> execution_info
    
    async def execute_dag(self, session: AsyncSession, dag_run_id: str) -> bool:
        """执行DAG"""
        try:
            # 获取DAG运行信息
            dag_run = await self._get_dag_run(session, dag_run_id)
            if not dag_run:
                raise ValueError(f"DAG run {dag_run_id} not found")
            
            # 获取DAG定义
            dag = dag_run.dag
            nodes = dag.dag_nodes
            
            # 验证DAG
            if not DAGTopologyAnalyzer.validate_dag(nodes):
                raise ValueError("Invalid DAG: contains cycles")
            
            # 更新DAG运行状态
            await self._update_dag_run_status(
                session, 
                dag_run_id, 
                DAGStatus.RUNNING,
                started_at=datetime.now(timezone.utc)
            )
            
            # 创建执行上下文
            context = DAGExecutionContext(
                dag_run_id=dag_run_id,
                variables={},
                outputs={}
            )
            
            # 创建节点执行器
            node_executor = DAGNodeExecutor(session)
            
            # 执行DAG
            success = await self._execute_dag_nodes(session, nodes, node_executor, context)
            
            # 更新最终状态
            final_status = DAGStatus.COMPLETED if success else DAGStatus.FAILED
            await self._update_dag_run_status(
                session,
                dag_run_id,
                final_status,
                completed_at=datetime.now(timezone.utc)
            )
            
            return success
            
        except Exception as e:
            logger.error(f"DAG execution failed: {e}")
            await self._update_dag_run_status(
                session,
                dag_run_id,
                DAGStatus.FAILED,
                completed_at=datetime.now(timezone.utc),
                error_message=str(e)
            )
            return False
    
    async def _execute_dag_nodes(
        self, 
        session: AsyncSession,
        nodes: List[DAGNode], 
        node_executor: DAGNodeExecutor, 
        context: DAGExecutionContext
    ) -> bool:
        """执行DAG节点"""
        
        completed_nodes = set()
        failed_nodes = set()
        node_map = {node.node_id: node for node in nodes}
        
        # 创建节点运行记录
        node_runs = {}
        for node in nodes:
            node_run = DAGNodeRun(
                dag_run_id=context.dag_run_id,
                node_id=node.id,
                status=NodeStatus.PENDING
            )
            session.add(node_run)
            node_runs[node.node_id] = node_run
        
        await session.commit()
        
        # 循环执行直到所有节点完成
        while len(completed_nodes) + len(failed_nodes) < len(nodes):
            # 查找可以执行的节点
            ready_nodes = DAGTopologyAnalyzer.find_ready_nodes(nodes, completed_nodes)
            ready_nodes = [nid for nid in ready_nodes if nid not in failed_nodes]
            
            if not ready_nodes:
                # 没有可执行的节点，可能存在循环依赖或者所有剩余节点都被阻塞
                logger.error("No ready nodes found, DAG execution stuck")
                return False
            
            # 并行执行准备就绪的节点
            tasks = []
            for node_id in ready_nodes:
                node = node_map[node_id]
                node_run = node_runs[node_id]
                task = node_executor.execute_node(node, node_run, context)
                tasks.append((node_id, task))
            
            # 等待所有任务完成
            for node_id, task in tasks:
                try:
                    success = await task
                    if success:
                        completed_nodes.add(node_id)
                    else:
                        failed_nodes.add(node_id)
                except Exception as e:
                    logger.error(f"Node {node_id} execution failed: {e}")
                    failed_nodes.add(node_id)
        
        # 检查是否所有节点都成功完成
        return len(failed_nodes) == 0
    
    async def _get_dag_run(self, session: AsyncSession, dag_run_id: str) -> Optional[DAGRun]:
        """获取DAG运行"""
        stmt = select(DAGRun).where(DAGRun.id == dag_run_id)
        result = await session.execute(stmt)
        return result.scalar_one_or_none()
    
    async def _update_dag_run_status(
        self, 
        session: AsyncSession, 
        dag_run_id: str, 
        status: DAGStatus, 
        **kwargs
    ):
        """更新DAG运行状态"""
        update_data = {"status": status, **kwargs}
        stmt = update(DAGRun).where(DAGRun.id == dag_run_id).values(**update_data)
        await session.execute(stmt)
        await session.commit()


# Celery任务定义
@celery_app.task(bind=True, name="app.tasks.dag_tasks.execute_dag")
def execute_dag_task(self, dag_run_id: str):
    """执行DAG的Celery任务"""
    
    async def _execute():
        from app.core.database import get_async_session
        
        session_gen = get_async_session()
        session = await anext(session_gen)
        
        try:
            engine = DAGEngine()
            return await engine.execute_dag(session, dag_run_id)
        finally:
            await session.close()
    
    # 在Celery任务中运行异步函数
    try:
        # 如果已有事件循环在运行（例如pytest-asyncio环境），避免嵌套运行
        running_loop = asyncio.get_running_loop()
        running_loop.create_task(_execute())
        return True
    except RuntimeError:
        # 无运行中的loop，创建新的事件循环执行
        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)
        try:
            return loop.run_until_complete(_execute())
        finally:
            loop.close()


# 全局DAG引擎实例
dag_engine = DAGEngine()


def get_dag_engine() -> DAGEngine:
    """获取DAG引擎实例"""
    return dag_engine
