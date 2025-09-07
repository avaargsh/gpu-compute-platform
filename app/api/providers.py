from fastapi import APIRouter, Depends, Query
from typing import List, Optional
from app.core.auth import current_active_user
from app.models.user import User
from app.schemas.task import Provider, GPUModel, DockerImage, ApiResponse

router = APIRouter()


@router.get("/providers", response_model=List[Provider])
async def get_providers(current_user: User = Depends(current_active_user)):
    """
    获取所有云服务商列表
    """
    providers = [
        Provider(
            id="alibaba",
            name="alibaba",
            display_name="阿里云 (Alibaba Cloud)",
            is_available=True,
            regions=["cn-hangzhou", "cn-beijing", "cn-shenzhen", "cn-shanghai", "ap-southeast-1"]
        ),
        Provider(
            id="tencent",
            name="tencent", 
            display_name="腾讯云 (Tencent Cloud)",
            is_available=True,
            regions=["ap-beijing", "ap-shanghai", "ap-guangzhou", "ap-chengdu", "ap-singapore"]
        ),
        Provider(
            id="runpod",
            name="runpod",
            display_name="RunPod",
            is_available=True,
            regions=["us-east", "us-west", "eu-central", "ap-southeast"]
        )
    ]
    return providers


@router.get("/gpu/models", response_model=List[GPUModel])
async def get_gpu_models(
    provider: Optional[str] = Query(None, description="云服务商过滤"),
    current_user: User = Depends(current_active_user)
):
    """
    获取GPU型号列表
    """
    # 模拟GPU型号数据
    gpu_models = [
        # 阿里云 GPU型号
        GPUModel(
            id="alibaba-v100",
            name="Tesla V100",
            provider="alibaba",
            memory_gb=16,
            compute_capability="7.0",
            cost_per_hour=2.8,
            availability="high"
        ),
        GPUModel(
            id="alibaba-a100",
            name="Tesla A100",
            provider="alibaba",
            memory_gb=40,
            compute_capability="8.0",
            cost_per_hour=3.9,
            availability="medium"
        ),
        GPUModel(
            id="alibaba-t4",
            name="Tesla T4",
            provider="alibaba",
            memory_gb=16,
            compute_capability="7.5",
            cost_per_hour=0.48,
            availability="high"
        ),
        GPUModel(
            id="alibaba-rtx3090",
            name="RTX 3090",
            provider="alibaba",
            memory_gb=24,
            compute_capability="8.6",
            cost_per_hour=1.25,
            availability="medium"
        ),
        
        # 腾讯云 GPU型号
        GPUModel(
            id="tencent-v100",
            name="Tesla V100",
            provider="tencent",
            memory_gb=16,
            compute_capability="7.0",
            cost_per_hour=2.5,
            availability="high"
        ),
        GPUModel(
            id="tencent-a100",
            name="Tesla A100",
            provider="tencent",
            memory_gb=40,
            compute_capability="8.0",
            cost_per_hour=3.8,
            availability="medium"
        ),
        GPUModel(
            id="tencent-t4",
            name="Tesla T4",
            provider="tencent",
            memory_gb=16,
            compute_capability="7.5",
            cost_per_hour=0.42,
            availability="high"
        ),
        GPUModel(
            id="tencent-k80",
            name="Tesla K80",
            provider="tencent",
            memory_gb=12,
            compute_capability="3.7",
            cost_per_hour=0.38,
            availability="high"
        ),
        
        # RunPod GPU型号
        GPUModel(
            id="runpod-v100",
            name="Tesla V100",
            provider="runpod",
            memory_gb=16,
            compute_capability="7.0",
            cost_per_hour=1.89,
            availability="high"
        ),
        GPUModel(
            id="runpod-a100",
            name="Tesla A100",
            provider="runpod",
            memory_gb=80,
            compute_capability="8.0",
            cost_per_hour=2.89,
            availability="medium"
        ),
        GPUModel(
            id="runpod-rtx4090",
            name="RTX 4090",
            provider="runpod",
            memory_gb=24,
            compute_capability="8.9",
            cost_per_hour=0.79,
            availability="high"
        ),
        GPUModel(
            id="runpod-rtx3090",
            name="RTX 3090",
            provider="runpod",
            memory_gb=24,
            compute_capability="8.6",
            cost_per_hour=0.59,
            availability="high"
        )
    ]
    
    # 根据provider过滤
    if provider:
        gpu_models = [gpu for gpu in gpu_models if gpu.provider == provider]
    
    return gpu_models


@router.get("/images", response_model=List[str])
async def get_docker_images(current_user: User = Depends(current_active_user)):
    """
    获取可用的Docker镜像列表
    """
    images = [
        # 机器学习框架镜像
        "pytorch/pytorch:2.0.1-cuda11.7-cudnn8-runtime",
        "pytorch/pytorch:2.0.1-cuda11.7-cudnn8-devel",
        "pytorch/pytorch:1.13.1-cuda11.6-cudnn8-runtime",
        "tensorflow/tensorflow:2.12.0-gpu",
        "tensorflow/tensorflow:2.11.0-gpu",
        "tensorflow/tensorflow:2.10.1-gpu",
        
        # 深度学习专用镜像
        "nvcr.io/nvidia/pytorch:23.03-py3",
        "nvcr.io/nvidia/tensorflow:23.03-tf2-py3",
        "nvcr.io/nvidia/jax:23.03-py3",
        
        # 数据科学镜像
        "jupyter/tensorflow-notebook:latest",
        "jupyter/pytorch-notebook:latest",
        "jupyter/datascience-notebook:latest",
        
        # 自定义镜像
        "gpu-platform/python:3.9-cuda11.7",
        "gpu-platform/python:3.10-cuda11.8",
        "gpu-platform/ml-base:latest",
        "gpu-platform/deep-learning:v1.0",
        
        # 基础镜像
        "nvidia/cuda:11.7-cudnn8-devel-ubuntu20.04",
        "nvidia/cuda:11.8-cudnn8-devel-ubuntu22.04",
        "nvidia/cuda:12.0-cudnn8-devel-ubuntu20.04",
        
        # 专业应用镜像
        "huggingface/transformers-pytorch-gpu:latest",
        "rapidsai/rapidsai:23.04-cuda11.7-runtime-ubuntu20.04-py3.9",
        "python:3.9-slim",
        "python:3.10-slim",
        "ubuntu:20.04",
        "ubuntu:22.04"
    ]
    
    return images


@router.get("/gpu/pricing", response_model=dict)
async def get_gpu_pricing(current_user: User = Depends(current_active_user)):
    """
    获取GPU定价信息（管理员或高级用户可见详细信息）
    """
    
    # 基础定价信息（所有用户可见）
    pricing_info = {
        "currency": "USD",
        "unit": "per_hour",
        "last_updated": "2024-01-01T00:00:00Z",
        "price_ranges": {
            "basic": {"min": 0.30, "max": 1.50, "description": "入门级GPU，适合学习和轻量任务"},
            "professional": {"min": 1.50, "max": 4.00, "description": "专业级GPU，适合生产环境"},
            "enterprise": {"min": 4.00, "max": 8.00, "description": "企业级GPU，适合大规模计算"}
        }
    }
    
    # 管理员可以看到详细定价
    if current_user.role.value == "admin":
        pricing_info["detailed_pricing"] = {
            "aws": {
                "Tesla V100": {"base_price": 3.06, "discount_rates": {"weekly": 0.1, "monthly": 0.2}},
                "Tesla A100": {"base_price": 4.10, "discount_rates": {"weekly": 0.1, "monthly": 0.2}},
                "Tesla T4": {"base_price": 0.526, "discount_rates": {"weekly": 0.05, "monthly": 0.15}}
            },
            "gcp": {
                "Tesla V100": {"base_price": 2.48, "discount_rates": {"weekly": 0.1, "monthly": 0.2}},
                "Tesla A100": {"base_price": 3.67, "discount_rates": {"weekly": 0.1, "monthly": 0.2}},
                "Tesla T4": {"base_price": 0.35, "discount_rates": {"weekly": 0.05, "monthly": 0.15}}
            },
            "azure": {
                "Tesla V100": {"base_price": 3.20, "discount_rates": {"weekly": 0.1, "monthly": 0.2}},
                "Tesla A100": {"base_price": 4.50, "discount_rates": {"weekly": 0.1, "monthly": 0.2}}
            },
            "local": {
                "RTX 4090": {"base_price": 0.50, "discount_rates": {"weekly": 0.0, "monthly": 0.0}},
                "RTX 3090": {"base_price": 0.40, "discount_rates": {"weekly": 0.0, "monthly": 0.0}},
                "Tesla V100": {"base_price": 0.30, "discount_rates": {"weekly": 0.0, "monthly": 0.0}}
            }
        }
    
    return {
        "success": True,
        "data": pricing_info
    }


@router.post("/cost/compare", response_model=dict)
async def compare_instances(
    instance_data: dict,
    current_user: User = Depends(current_active_user)
):
    """
    比较不同实例的成本效益
    """
    instance_ids = instance_data.get("instance_ids", [])
    
    if not instance_ids:
        return {
            "success": False,
            "error": "请提供要比较的实例ID",
            "message": "请提供要比较的实例ID"
        }
    
    # 模拟比较结果
    comparison_results = []
    
    # 获取所有GPU型号
    all_gpus = await get_gpu_models(current_user=current_user)
    gpu_dict = {gpu.id: gpu for gpu in all_gpus}
    
    for instance_id in instance_ids:
        if instance_id in gpu_dict:
            gpu = gpu_dict[instance_id]
            
            # 计算性能分数（简化算法）
            performance_score = gpu.memory_gb * 10 + float(gpu.compute_capability) * 50
            cost_efficiency = performance_score / gpu.cost_per_hour
            
            comparison_results.append({
                "instance_id": instance_id,
                "name": gpu.name,
                "provider": gpu.provider,
                "cost_per_hour": gpu.cost_per_hour,
                "memory_gb": gpu.memory_gb,
                "performance_score": round(performance_score, 2),
                "cost_efficiency": round(cost_efficiency, 2),
                "availability": gpu.availability,
                "recommendations": {
                    "suitable_for": ["机器学习训练", "深度学习推理"] if gpu.memory_gb >= 16 else ["轻量级计算", "开发测试"],
                    "best_use_cases": ["长时间训练任务"] if gpu.cost_per_hour < 2.0 else ["短期高性能计算"]
                }
            })
    
    # 按成本效益排序
    comparison_results.sort(key=lambda x: x["cost_efficiency"], reverse=True)
    
    return {
        "success": True,
        "data": {
            "comparison": comparison_results,
            "summary": {
                "total_instances": len(comparison_results),
                "best_value": comparison_results[0]["instance_id"] if comparison_results else None,
                "lowest_cost": min(comparison_results, key=lambda x: x["cost_per_hour"])["instance_id"] if comparison_results else None,
                "highest_performance": max(comparison_results, key=lambda x: x["performance_score"])["instance_id"] if comparison_results else None
            }
        }
    }


@router.get("/system/status", response_model=dict)
async def get_system_status(current_user: User = Depends(current_active_user)):
    """
    获取系统状态信息
    """
    status_info = {
        "system": "healthy",
        "database": "connected",
        "gpu_providers": {
            "aws": "available",
            "gcp": "available", 
            "azure": "limited",  # 模拟部分不可用
            "local": "available"
        },
        "active_tasks": 12,  # 模拟数据
        "queued_tasks": 3,
        "available_gpus": 45,
        "last_check": "2024-01-01T12:00:00Z"
    }
    
    return {
        "success": True,
        "data": status_info
    }


@router.get("/system/config", response_model=dict) 
async def get_system_config(current_user: User = Depends(current_active_user)):
    """
    获取系统配置信息（管理员专用）
    """
    if current_user.role.value != "admin":
        return {
            "success": False,
            "error": "需要管理员权限",
            "message": "需要管理员权限"
        }
    
    config_info = {
        "max_concurrent_tasks": 100,
        "default_timeout": 3600,
        "supported_regions": {
            "aws": ["us-east-1", "us-west-2", "eu-west-1"],
            "gcp": ["us-central1", "europe-west1"],
            "azure": ["eastus", "westeurope"]
        },
        "resource_limits": {
            "max_gpu_hours_per_user": 168,  # 一周
            "max_tasks_per_user": 50,
            "max_budget_per_task": 1000
        },
        "features": {
            "auto_scaling": True,
            "cost_optimization": True,
            "real_time_monitoring": True,
            "multi_region": True
        }
    }
    
    return {
        "success": True,
        "data": config_info
    }
