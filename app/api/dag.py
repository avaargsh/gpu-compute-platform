import json
import uuid
from datetime import datetime, timezone
from typing import List, Optional, Dict, Any
from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select, delete
from sqlalchemy.orm import selectinload
from pydantic import BaseModel, Field

from app.core.database import get_async_session
from app.core.auth import current_active_user
from app.core.dag_engine import get_dag_engine, execute_dag_task
from app.models.user import User
from app.models.dag import (
    TaskDAG, DAGRun, DAGNode, DAGNodeRun, DAGEdge, DAGTemplate,
    DAGStatus, NodeStatus, NodeType
)

router = APIRouter(prefix="/dag", tags=["DAG"])


# Pydantic模型定义
class GpuSpecRequest(BaseModel):
    gpu_type: str = Field(..., description="GPU类型")
    gpu_count: int = Field(1, description="GPU数量")
    memory_gb: Optional[int] = Field(None, description="GPU内存(GB)")
    vcpus: int = Field(..., description="CPU核心数")
    ram_gb: int = Field(..., description="内存(GB)")


class JobConfigRequest(BaseModel):
    name: str = Field(..., description="任务名称")
    image: str = Field(..., description="Docker镜像")
    command: List[str] = Field(..., description="执行命令")
    env_vars: Optional[Dict[str, str]] = Field(default_factory=dict, description="环境变量")
    gpu_spec: GpuSpecRequest = Field(..., description="GPU规格")


class DAGNodeRequest(BaseModel):
    node_id: str = Field(..., description="节点ID")
    node_name: str = Field(..., description="节点名称")
    node_type: NodeType = Field(..., description="节点类型")
    node_config: Dict[str, Any] = Field(..., description="节点配置")
    dependencies: List[str] = Field(default_factory=list, description="依赖的节点ID列表")
    position_x: Optional[float] = Field(0, description="UI位置X坐标")
    position_y: Optional[float] = Field(0, description="UI位置Y坐标")


class DAGRequest(BaseModel):
    name: str = Field(..., description="DAG名称")
    description: Optional[str] = Field(None, description="DAG描述")
    schedule: Optional[str] = Field(None, description="调度表达式")
    nodes: List[DAGNodeRequest] = Field(..., description="DAG节点列表")
    is_template: bool = Field(False, description="是否为模板")


class DAGRunRequest(BaseModel):
    dag_id: str = Field(..., description="DAG ID")
    run_name: Optional[str] = Field(None, description="运行名称")
    parameters: Optional[Dict[str, Any]] = Field(default_factory=dict, description="运行参数")


class DAGResponse(BaseModel):
    id: str
    name: str
    description: Optional[str]
    schedule: Optional[str]
    is_active: bool
    is_template: bool
    created_at: datetime
    updated_at: Optional[datetime]
    success_rate: float
    total_runs: int
    
    class Config:
        from_attributes = True


class DAGNodeResponse(BaseModel):
    id: str
    node_id: str
    node_name: str
    node_type: NodeType
    node_config: Dict[str, Any]
    dependencies: List[str]
    position_x: float
    position_y: float
    
    class Config:
        from_attributes = True


class DAGDetailResponse(DAGResponse):
    nodes: List[DAGNodeResponse]


class DAGRunResponse(BaseModel):
    id: str
    dag_id: str
    run_name: Optional[str]
    status: DAGStatus
    started_at: Optional[datetime]
    completed_at: Optional[datetime]
    duration_seconds: Optional[float]
    parameters: str
    error_message: Optional[str]
    
    class Config:
        from_attributes = True


class DAGNodeRunResponse(BaseModel):
    id: str
    node_id: str
    status: NodeStatus
    started_at: Optional[datetime]
    completed_at: Optional[datetime]
    duration_seconds: Optional[float]
    error_message: Optional[str]
    celery_task_id: Optional[str]
    gpu_task_id: Optional[str]
    
    class Config:
        from_attributes = True


class DAGRunDetailResponse(DAGRunResponse):
    node_runs: List[DAGNodeRunResponse]


# API端点
@router.post("/", response_model=DAGResponse, status_code=status.HTTP_201_CREATED)
async def create_dag(
    dag_request: DAGRequest,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """创建DAG"""
    
    # 验证节点依赖关系
    node_ids = {node.node_id for node in dag_request.nodes}
    for node in dag_request.nodes:
        for dep in node.dependencies:
            if dep not in node_ids:
                raise HTTPException(
                    status_code=status.HTTP_400_BAD_REQUEST,
                    detail=f"Invalid dependency: {dep} not found in nodes"
                )
    
    # 创建DAG
    dag = TaskDAG(
        id=str(uuid.uuid4()),
        name=dag_request.name,
        description=dag_request.description,
        schedule_expression=dag_request.schedule,
        dag_config="{}",  # 默认空配置
        user_id=str(user.id),
        is_active=True,
        is_template=dag_request.is_template
    )
    
    session.add(dag)
    await session.flush()  # 获取DAG ID
    
    # 创建节点
    for node_req in dag_request.nodes:
        node = DAGNode(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            node_id=node_req.node_id,
            node_name=node_req.node_name,
            node_type=node_req.node_type,
            node_config=json.dumps(node_req.node_config),
            dependencies=json.dumps(node_req.dependencies),
            depends_on=json.dumps(node_req.dependencies),  # 同时设置两个字段
            position_x=node_req.position_x,
            position_y=node_req.position_y
        )
        session.add(node)
    
    await session.commit()
    await session.refresh(dag)
    
    return DAGResponse.model_validate(dag)


@router.get("/", response_model=List[DAGResponse])
async def list_dags(
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session),
    include_templates: bool = False
):
    """获取DAG列表"""
    
    query = select(TaskDAG).where(TaskDAG.user_id == str(user.id))
    if not include_templates:
        query = query.where(TaskDAG.is_template == False)
    
    result = await session.execute(query.order_by(TaskDAG.created_at.desc()))
    dags = result.scalars().all()
    
    return [DAGResponse.model_validate(dag) for dag in dags]


@router.get("/{dag_id}", response_model=DAGDetailResponse)
async def get_dag(
    dag_id: str,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """获取DAG详情"""
    
    query = select(TaskDAG).options(
        selectinload(TaskDAG.dag_nodes)
    ).where(
        TaskDAG.id == dag_id,
        TaskDAG.user_id == str(user.id)
    )
    
    result = await session.execute(query)
    dag = result.scalar_one_or_none()
    
    if not dag:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG not found"
        )
    
    # 转换节点数据
    nodes = []
    for node in dag.dag_nodes:
        deps_raw = node.depends_on or node.dependencies or "[]"
        node_data = DAGNodeResponse(
            id=node.id,
            node_id=node.node_id,
            node_name=node.node_name,
            node_type=node.node_type,
            node_config=json.loads(node.node_config),
            dependencies=json.loads(deps_raw),
            position_x=float(node.position_x or 0),
            position_y=float(node.position_y or 0)
        )
        nodes.append(node_data)
    
    dag_response = DAGResponse.model_validate(dag)
    return DAGDetailResponse(**dag_response.model_dump(), nodes=nodes)


@router.put("/{dag_id}", response_model=DAGResponse)
async def update_dag(
    dag_id: str,
    dag_request: DAGRequest,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """更新DAG"""
    
    # 获取现有DAG
    query = select(TaskDAG).where(
        TaskDAG.id == dag_id,
        TaskDAG.user_id == str(user.id)
    )
    result = await session.execute(query)
    dag = result.scalar_one_or_none()
    
    if not dag:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG not found"
        )
    
    # 更新DAG属性
    dag.name = dag_request.name
    dag.description = dag_request.description
    dag.schedule = dag_request.schedule
    dag.is_template = dag_request.is_template
    dag.updated_at = datetime.now(timezone.utc)
    
    # 删除现有节点
    await session.execute(delete(DAGNode).where(DAGNode.dag_id == dag_id))
    
    # 创建新节点
    for node_req in dag_request.nodes:
        node = DAGNode(
            id=str(uuid.uuid4()),
            dag_id=dag.id,
            node_id=node_req.node_id,
            node_name=node_req.node_name,
            node_type=node_req.node_type,
            node_config=json.dumps(node_req.node_config),
            dependencies=json.dumps(node_req.dependencies),
            position_x=node_req.position_x,
            position_y=node_req.position_y
        )
        session.add(node)
    
    await session.commit()
    await session.refresh(dag)
    
    return DAGResponse.model_validate(dag)


@router.delete("/{dag_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_dag(
    dag_id: str,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """删除DAG"""
    
    # 先按ID获取，再验证归属，避免不同数据库后端的UUID/字符串比较差异
    query = select(TaskDAG).where(TaskDAG.id == dag_id)
    result = await session.execute(query)
    dag = result.scalar_one_or_none()
    
    if not dag or dag.user_id != str(user.id):
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG not found"
        )
    
    await session.delete(dag)
    await session.commit()


@router.post("/{dag_id}/runs", response_model=DAGRunResponse, status_code=status.HTTP_201_CREATED)
async def create_dag_run(
    dag_id: str,
    run_request: DAGRunRequest,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """创建DAG运行"""
    
    # 验证DAG存在
    query = select(TaskDAG).where(
        TaskDAG.id == dag_id,
        TaskDAG.user_id == str(user.id),
        TaskDAG.is_active == True
    )
    result = await session.execute(query)
    dag = result.scalar_one_or_none()
    
    if not dag:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG not found or inactive"
        )
    
    # 创建DAG运行
    dag_run = DAGRun(
        id=str(uuid.uuid4()),
        dag_id=dag_id,
        run_name=run_request.run_name or f"manual_run_{datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')}",
        status=DAGStatus.PENDING,
        parameters=json.dumps(run_request.parameters),
        user_id=str(user.id)
    )
    
    session.add(dag_run)
    await session.commit()
    await session.refresh(dag_run)
    
    # 异步提交DAG执行任务
    execute_dag_task.delay(dag_run.id)
    
    return DAGRunResponse.model_validate(dag_run)


@router.get("/{dag_id}/runs", response_model=List[DAGRunResponse])
async def list_dag_runs(
    dag_id: str,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session),
    limit: int = 20,
    offset: int = 0
):
    """获取DAG运行列表"""
    
    # 验证DAG存在
    query = select(TaskDAG).where(
        TaskDAG.id == dag_id,
        TaskDAG.user_id == str(user.id)
    )
    result = await session.execute(query)
    dag = result.scalar_one_or_none()
    
    if not dag:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG not found"
        )
    
    # 获取运行列表
    query = select(DAGRun).where(
        DAGRun.dag_id == dag_id
    ).order_by(DAGRun.created_at.desc()).limit(limit).offset(offset)
    
    result = await session.execute(query)
    runs = result.scalars().all()
    
    return [DAGRunResponse.model_validate(run) for run in runs]


@router.get("/{dag_id}/runs/{run_id}", response_model=DAGRunDetailResponse)
async def get_dag_run(
    dag_id: str,
    run_id: str,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """获取DAG运行详情"""
    
    query = select(DAGRun).options(
        selectinload(DAGRun.node_runs)
    ).where(
        DAGRun.id == run_id,
        DAGRun.dag_id == dag_id,
        DAGRun.user_id == str(user.id)
    )
    
    result = await session.execute(query)
    dag_run = result.scalar_one_or_none()
    
    if not dag_run:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG run not found"
        )
    
    # 转换节点运行数据
    node_runs = [DAGNodeRunResponse.model_validate(nr) for nr in dag_run.node_runs]
    
    run_response = DAGRunResponse.model_validate(dag_run)
    return DAGRunDetailResponse(**run_response.model_dump(), node_runs=node_runs)


@router.post("/{dag_id}/runs/{run_id}/cancel", status_code=status.HTTP_200_OK)
async def cancel_dag_run(
    dag_id: str,
    run_id: str,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """取消DAG运行"""
    
    query = select(DAGRun).where(
        DAGRun.id == run_id,
        DAGRun.dag_id == dag_id,
        DAGRun.user_id == str(user.id),
        DAGRun.status.in_([DAGStatus.PENDING, DAGStatus.RUNNING])
    )
    
    result = await session.execute(query)
    dag_run = result.scalar_one_or_none()
    
    if not dag_run:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG run not found or cannot be cancelled"
        )
    
    # 更新状态为已取消
    dag_run.status = DAGStatus.CANCELLED
    dag_run.completed_at = datetime.now(timezone.utc)
    
    await session.commit()
    
    return {"message": "DAG run cancelled successfully"}


@router.post("/{dag_id}/toggle", response_model=DAGResponse)
async def toggle_dag_status(
    dag_id: str,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """切换DAG激活状态"""
    
    query = select(TaskDAG).where(
        TaskDAG.id == dag_id,
        TaskDAG.user_id == str(user.id)
    )
    result = await session.execute(query)
    dag = result.scalar_one_or_none()
    
    if not dag:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG not found"
        )
    
    # 切换激活状态
    dag.is_active = not dag.is_active
    dag.updated_at = datetime.now(timezone.utc)
    
    await session.commit()
    await session.refresh(dag)
    
    return DAGResponse.model_validate(dag)


# 模板相关端点
@router.get("/templates/", response_model=List[DAGResponse])
async def list_dag_templates(
    session: AsyncSession = Depends(get_async_session),
    user: User = Depends(current_active_user)
):
    """获取DAG模板列表"""
    
    query = select(TaskDAG).where(
        TaskDAG.is_template == True,
        TaskDAG.user_id == str(user.id)  # 只显示用户自己的模板，可以根据需要调整
    ).order_by(TaskDAG.created_at.desc())
    
    result = await session.execute(query)
    templates = result.scalars().all()
    
    return [DAGResponse.model_validate(template) for template in templates]


@router.post("/templates/{template_id}/instantiate", response_model=DAGResponse, status_code=status.HTTP_201_CREATED)
async def instantiate_dag_template(
    template_id: str,
    dag_name: str,
    description: Optional[str] = None,
    user: User = Depends(current_active_user),
    session: AsyncSession = Depends(get_async_session)
):
    """从模板实例化DAG"""
    
    # 获取模板
    query = select(TaskDAG).options(
        selectinload(TaskDAG.dag_nodes)
    ).where(
        TaskDAG.id == template_id,
        TaskDAG.is_template == True
    )
    
    result = await session.execute(query)
    template = result.scalar_one_or_none()
    
    if not template:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="DAG template not found"
        )
    
    # 创建新的DAG
    new_dag = TaskDAG(
        id=str(uuid.uuid4()),
        name=dag_name,
        description=description or template.description,
        schedule_expression=template.schedule,
        dag_config="{}",
        user_id=str(user.id),
        is_active=True,
        is_template=False
    )
    
    session.add(new_dag)
    await session.flush()
    
    # 复制节点
    for template_node in template.dag_nodes:
        new_node = DAGNode(
            id=str(uuid.uuid4()),
            dag_id=new_dag.id,
            node_id=template_node.node_id,
            node_name=template_node.node_name,
            node_type=template_node.node_type,
            node_config=template_node.node_config,
            dependencies=template_node.dependencies,
            position_x=template_node.position_x,
            position_y=template_node.position_y
        )
        session.add(new_node)
    
    await session.commit()
    await session.refresh(new_dag)
    
    return DAGResponse.model_validate(new_dag)
