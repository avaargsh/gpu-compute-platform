from fastapi import APIRouter, Depends, HTTPException, status
from typing import List, Optional
from app.core.auth import current_active_user
from app.models.user import User
from app.gpu.interface import GpuSpec, JobConfig, JobResult, CostInfo
from app.gpu.providers.runpod import RunPodAdapter
from app.gpu.providers.tencent import TencentCloudAdapter
from app.gpu.providers.alibaba import AlibabaCloudAdapter
import os

router = APIRouter()

# Provider configurations (在实际环境中这些应该来自环境变量或配置文件)
PROVIDERS = {
    "runpod": {
        "api_key": os.getenv("RUNPOD_API_KEY", "demo-api-key"),
        "endpoint_id": os.getenv("RUNPOD_ENDPOINT_ID", "demo-endpoint"),
    },
    "tencent": {
        "secret_id": os.getenv("TENCENT_SECRET_ID", "demo-secret-id"),
        "secret_key": os.getenv("TENCENT_SECRET_KEY", "demo-secret-key"),
        "region": os.getenv("TENCENT_REGION", "ap-shanghai"),
        "cluster_id": os.getenv("TENCENT_CLUSTER_ID", "cls-demo"),
    },
    "alibaba": {
        "access_key_id": os.getenv("ALIBABA_ACCESS_KEY_ID", "demo-access-key"),
        "access_key_secret": os.getenv("ALIBABA_ACCESS_KEY_SECRET", "demo-secret"),
        "region_id": os.getenv("ALIBABA_REGION_ID", "cn-hangzhou"),
    }
}


def get_provider_adapter(provider_name: str):
    """获取指定提供商的适配器实例"""
    if provider_name not in PROVIDERS:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Unsupported provider: {provider_name}"
        )
    
    config = PROVIDERS[provider_name]
    
    if provider_name == "runpod":
        return RunPodAdapter(config)
    elif provider_name == "tencent":
        return TencentCloudAdapter(config)
    elif provider_name == "alibaba":
        return AlibabaCloudAdapter(config)
    else:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Unknown provider: {provider_name}"
        )


@router.get("/providers")
async def list_providers(user: User = Depends(current_active_user)):
    """列出所有支持的GPU提供商"""
    return {
        "providers": [
            {
                "name": "runpod",
                "display_name": "RunPod",
                "description": "RunPod GPU cloud computing platform"
            },
            {
                "name": "tencent",
                "display_name": "Tencent Cloud",
                "description": "Tencent Cloud TKE GPU computing"
            },
            {
                "name": "alibaba",
                "display_name": "Alibaba Cloud",
                "description": "Alibaba Cloud ECS GPU instances"
            }
        ]
    }


@router.get("/providers/{provider_name}/gpus")
async def list_available_gpus(
    provider_name: str,
    user: User = Depends(current_active_user)
):
    """列出指定提供商的可用GPU规格"""
    try:
        adapter = get_provider_adapter(provider_name)
        gpus = await adapter.list_available_gpus()
        return {"provider": provider_name, "available_gpus": gpus}
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to list GPUs: {str(e)}"
        )


@router.get("/providers/{provider_name}/health")
async def check_provider_health(
    provider_name: str,
    user: User = Depends(current_active_user)
):
    """检查指定提供商的健康状态"""
    try:
        adapter = get_provider_adapter(provider_name)
        health = await adapter.health_check()
        return {"provider": provider_name, "health": health}
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Health check failed: {str(e)}"
        )


@router.post("/jobs/submit")
async def submit_job(
    provider_name: str,
    job_config: JobConfig,
    user: User = Depends(current_active_user)
):
    """提交GPU作业"""
    try:
        adapter = get_provider_adapter(provider_name)
        job_id = await adapter.submit_job(job_config)
        
        return {
            "job_id": job_id,
            "provider": provider_name,
            "status": "submitted",
            "message": f"Job submitted successfully to {provider_name}"
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Job submission failed: {str(e)}"
        )


@router.get("/jobs/{provider_name}/{job_id}/status")
async def get_job_status(
    provider_name: str,
    job_id: str,
    user: User = Depends(current_active_user)
):
    """获取作业状态"""
    try:
        adapter = get_provider_adapter(provider_name)
        result = await adapter.get_job_status(job_id)
        return {
            "provider": provider_name,
            "result": result
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get job status: {str(e)}"
        )


@router.get("/jobs/{provider_name}/{job_id}/logs")
async def get_job_logs(
    provider_name: str,
    job_id: str,
    lines: Optional[int] = None,
    user: User = Depends(current_active_user)
):
    """获取作业日志"""
    try:
        adapter = get_provider_adapter(provider_name)
        logs = await adapter.get_job_logs(job_id, lines)
        return {
            "provider": provider_name,
            "job_id": job_id,
            "logs": logs
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get job logs: {str(e)}"
        )


@router.get("/jobs/{provider_name}/{job_id}/cost")
async def get_job_cost(
    provider_name: str,
    job_id: str,
    user: User = Depends(current_active_user)
):
    """获取作业成本信息"""
    try:
        adapter = get_provider_adapter(provider_name)
        cost_info = await adapter.get_cost_info(job_id)
        return {
            "provider": provider_name,
            "cost_info": cost_info
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to get cost info: {str(e)}"
        )


@router.post("/jobs/{provider_name}/{job_id}/cancel")
async def cancel_job(
    provider_name: str,
    job_id: str,
    user: User = Depends(current_active_user)
):
    """取消作业"""
    try:
        adapter = get_provider_adapter(provider_name)
        success = await adapter.cancel_job(job_id)
        return {
            "provider": provider_name,
            "job_id": job_id,
            "cancelled": success,
            "message": "Job cancellation request sent" if success else "Job cancellation failed"
        }
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Failed to cancel job: {str(e)}"
        )
