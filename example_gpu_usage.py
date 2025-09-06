#!/usr/bin/env python3
"""
Example usage of GPU provider adapters.

This demonstrates how to use the unified GPU provider interface
with different cloud platforms (Alibaba Cloud and Tencent Cloud).
"""

import asyncio
import os
from app.gpu.interface import JobConfig, GpuSpec
from app.gpu.providers.alibaba import AlibabaCloudAdapter
from app.gpu.providers.tencent import TencentCloudAdapter


async def example_tencent_usage():
    """Example using Tencent Cloud TKE adapter."""
    print("=== Tencent Cloud TKE Example ===")
    
    # Configuration for Tencent Cloud
    config = {
        "secret_id": os.getenv("TENCENT_SECRET_ID", "demo_secret_id"),
        "secret_key": os.getenv("TENCENT_SECRET_KEY", "demo_secret_key"),
        "region": "ap-shanghai",
        "cluster_id": "cls-xxxxxx",  # Replace with actual cluster ID
        # Optional: provide base64-encoded kubeconfig
        # "kubeconfig": "base64_encoded_kubeconfig_content"
    }
    
    try:
        # Initialize the provider
        provider = TencentCloudAdapter(config)
        
        # Check provider health
        health = await provider.health_check()
        print(f"Provider health: {health['status']}")
        
        # List available GPU types
        available_gpus = await provider.list_available_gpus()
        print(f"Available GPU types: {len(available_gpus)}")
        for gpu in available_gpus:
            print(f"  - {gpu.gpu_type}: {gpu.gpu_count}x GPU, {gpu.vcpus} vCPUs, {gpu.ram_gb}GB RAM")
        
        # Define a simple GPU job
        job_config = JobConfig(
            name="pytorch-training",
            image="nvcr.io/nvidia/pytorch:24.02-py3",
            command=["python", "-c", "import torch; print(f'CUDA available: {torch.cuda.is_available()}'); print(f'GPU count: {torch.cuda.device_count()}')"],
            gpu_spec=GpuSpec(
                gpu_type="A100",
                gpu_count=1,
                memory_gb=40,
                vcpus=12,
                ram_gb=48
            ),
            environment={
                "NVIDIA_VISIBLE_DEVICES": "all",
                "CUDA_VISIBLE_DEVICES": "0"
            },
            timeout_minutes=30
        )
        
        # Submit the job
        print("\\nSubmitting job...")
        job_id = await provider.submit_job(job_config)
        print(f"Job submitted with ID: {job_id}")
        
        # Check job status
        result = await provider.get_job_status(job_id)
        print(f"Job status: {result.status}")
        print(f"Created at: {result.created_at}")
        
        # Get cost information (mock implementation)
        cost_info = await provider.get_cost_info(job_id)
        print(f"Estimated cost: {cost_info.total_cost} {cost_info.currency}")
        
        return job_id
        
    except Exception as e:
        print(f"Tencent Cloud example failed: {e}")
        return None


async def example_alibaba_usage():
    """Example using Alibaba Cloud ECS adapter."""
    print("\\n=== Alibaba Cloud ECS Example ===")
    
    # Configuration for Alibaba Cloud
    config = {
        "access_key_id": os.getenv("ALIBABA_ACCESS_KEY_ID", "demo_access_key"),
        "access_key_secret": os.getenv("ALIBABA_ACCESS_KEY_SECRET", "demo_secret"),
        "region_id": "cn-hangzhou",
        "security_group_id": "sg-xxxxxxxxx",  # Replace with actual security group
        "vswitch_id": "vsw-xxxxxxxxx",        # Replace with actual vswitch
        "key_pair_name": "gpu-compute-keypair"  # Replace with actual key pair
    }
    
    try:
        # Initialize the provider
        provider = AlibabaCloudAdapter(config)
        
        # Check provider health
        health = await provider.health_check()
        print(f"Provider health: {health['status']}")
        
        # List available GPU types
        available_gpus = await provider.list_available_gpus()
        print(f"Available GPU types: {len(available_gpus)}")
        for gpu in available_gpus:
            print(f"  - {gpu.gpu_type}: {gpu.gpu_count}x GPU, {gpu.vcpus} vCPUs, {gpu.ram_gb}GB RAM")
        
        # Define a simple GPU job
        job_config = JobConfig(
            name="tensorflow-inference",
            image="tensorflow/tensorflow:2.13.0-gpu",
            command=["python", "-c", "import tensorflow as tf; print(f'TensorFlow version: {tf.__version__}'); print(f'GPU available: {tf.test.is_gpu_available()}')"],
            gpu_spec=GpuSpec(
                gpu_type="T4",
                gpu_count=1,
                memory_gb=15,
                vcpus=4,
                ram_gb=15
            ),
            environment={
                "NVIDIA_VISIBLE_DEVICES": "all"
            },
            timeout_minutes=20
        )
        
        # Submit the job
        print("\\nSubmitting job...")
        job_id = await provider.submit_job(job_config)
        print(f"Job submitted with ID: {job_id}")
        
        # Check job status
        result = await provider.get_job_status(job_id)
        print(f"Job status: {result.status}")
        print(f"Created at: {result.created_at}")
        
        # Get cost information (mock implementation)
        cost_info = await provider.get_cost_info(job_id)
        print(f"Estimated cost: {cost_info.total_cost} {cost_info.currency}")
        
        return job_id
        
    except Exception as e:
        print(f"Alibaba Cloud example failed: {e}")
        return None


async def main():
    """Run examples for both cloud providers."""
    print("GPU Provider Adapter Examples")
    print("=" * 50)
    
    # Run Tencent Cloud example
    tencent_job_id = await example_tencent_usage()
    
    # Run Alibaba Cloud example  
    alibaba_job_id = await example_alibaba_usage()
    
    print("\\n=== Summary ===")
    print(f"Tencent job ID: {tencent_job_id}")
    print(f"Alibaba job ID: {alibaba_job_id}")
    
    print("\\nNote: These are mock examples. To run with real cloud resources:")
    print("1. Set proper environment variables for credentials")
    print("2. Configure actual cloud resources (clusters, security groups, etc.)")
    print("3. Install required cloud SDK dependencies: uv sync")


if __name__ == "__main__":
    asyncio.run(main())
