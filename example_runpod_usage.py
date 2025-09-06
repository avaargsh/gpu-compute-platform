#!/usr/bin/env python3
"""
RunPod GPU Provider Usage Example

This script demonstrates the complete workflow of using the RunPod GPU provider,
from job submission to cost tracking, showcasing the MVP's core functionality.
"""

import asyncio
import os
from datetime import datetime
from typing import Dict, Any

from app.gpu.interface import JobConfig, GpuSpec
from app.gpu.providers.runpod import RunPodAdapter


def get_runpod_config() -> Dict[str, Any]:
    """Get RunPod configuration from environment variables."""
    api_key = os.getenv("RUNPOD_API_KEY", "demo-api-key-replace-with-real")
    
    return {
        "api_key": api_key,
        "base_url": "https://api.runpod.ai/graphql",
        "timeout": 300,  # 5 minutes timeout
    }


def create_sample_jobs() -> list[JobConfig]:
    """Create sample GPU job configurations for different use cases."""
    
    # Job 1: PyTorch Training
    pytorch_job = JobConfig(
        name="pytorch-training-example",
        image="nvcr.io/nvidia/pytorch:24.02-py3",
        command=[
            "python", "-c", 
            """
import torch
import time
print(f'PyTorch version: {torch.__version__}')
print(f'CUDA available: {torch.cuda.is_available()}')
if torch.cuda.is_available():
    print(f'CUDA devices: {torch.cuda.device_count()}')
    print(f'Current device: {torch.cuda.current_device()}')
    print(f'Device name: {torch.cuda.get_device_name()}')
    
    # Simulate training
    print('Starting training simulation...')
    x = torch.randn(1000, 1000).cuda()
    for i in range(10):
        y = torch.mm(x, x.T)
        print(f'Training step {i+1}/10')
        time.sleep(2)
    print('Training completed!')
else:
    print('No CUDA devices available')
            """
        ],
        gpu_spec=GpuSpec(
            gpu_type="A100",
            gpu_count=1,
            memory_gb=40,
            vcpus=12,
            ram_gb=64
        ),
        environment={
            "NVIDIA_VISIBLE_DEVICES": "all",
            "PYTHONPATH": "/workspace",
            "TORCH_CUDA_ARCH_LIST": "8.0",
        },
        timeout_minutes=30
    )
    
    # Job 2: TensorFlow Inference
    tensorflow_job = JobConfig(
        name="tensorflow-inference-example",
        image="tensorflow/tensorflow:2.15.0-gpu",
        command=[
            "python", "-c",
            """
import tensorflow as tf
import numpy as np
import time

print(f'TensorFlow version: {tf.__version__}')
print(f'GPU available: {tf.test.is_gpu_available()}')
if tf.config.list_physical_devices('GPU'):
    gpus = tf.config.list_physical_devices('GPU')
    print(f'Available GPUs: {len(gpus)}')
    for i, gpu in enumerate(gpus):
        print(f'  GPU {i}: {gpu.name}')
    
    # Simulate inference
    print('Starting inference simulation...')
    with tf.device('/GPU:0'):
        x = tf.random.normal([1000, 1000])
        for i in range(5):
            y = tf.linalg.matmul(x, x, transpose_b=True)
            print(f'Inference batch {i+1}/5')
            time.sleep(3)
    print('Inference completed!')
else:
    print('No GPU devices found')
            """
        ],
        gpu_spec=GpuSpec(
            gpu_type="RTX4090",
            gpu_count=1,
            memory_gb=24,
            vcpus=8,
            ram_gb=32
        ),
        environment={
            "NVIDIA_VISIBLE_DEVICES": "all",
            "TF_FORCE_GPU_ALLOW_GROWTH": "true",
        },
        timeout_minutes=20
    )
    
    # Job 3: Lightweight Testing
    test_job = JobConfig(
        name="gpu-test-example",
        image="nvidia/cuda:12.3-runtime-ubuntu22.04",
        command=[
            "bash", "-c",
            """
echo "=== GPU Test Environment ==="
echo "CUDA Version: $(nvcc --version 2>/dev/null || echo 'nvcc not found')"
echo "NVIDIA-SMI Output:"
nvidia-smi || echo "nvidia-smi not available"
echo ""
echo "Environment Variables:"
env | grep -E "(CUDA|NVIDIA)" | sort
echo ""
echo "GPU test completed successfully!"
sleep 10
            """
        ],
        gpu_spec=GpuSpec(
            gpu_type="T4",
            gpu_count=1,
            memory_gb=16,
            vcpus=4,
            ram_gb=16
        ),
        environment={
            "NVIDIA_VISIBLE_DEVICES": "all",
        },
        timeout_minutes=15
    )
    
    return [pytorch_job, tensorflow_job, test_job]


async def demonstrate_runpod_workflow():
    """Demonstrate complete RunPod workflow with error handling."""
    print("🚀 RunPod GPU Provider Workflow Demo")
    print("=" * 60)
    
    # Initialize RunPod adapter
    config = get_runpod_config()
    
    print(f"📋 Configuration:")
    print(f"   API Key: {'*' * 8}{config['api_key'][-4:] if len(config['api_key']) > 4 else 'demo'}")
    print(f"   Base URL: {config['base_url']}")
    print(f"   Timeout: {config['timeout']}s")
    print()
    
    async with RunPodAdapter(config) as provider:
        # 1. Health Check
        print("🏥 Checking RunPod service health...")
        try:
            health = await provider.health_check()
            print(f"   Status: {health['status']}")
            print(f"   Message: {health['message']}")
            if health.get('user_email'):
                print(f"   User: {health['user_email']}")
                print(f"   Response time: {health.get('response_time_ms', 0):.1f}ms")
        except Exception as e:
            print(f"   ⚠️  Health check failed: {e}")
            print("   Continuing with demo using mock data...")
        print()
        
        # 2. List Available GPUs
        print("📊 Listing available GPU types...")
        try:
            available_gpus = await provider.list_available_gpus()
            print(f"   Found {len(available_gpus)} GPU types:")
            for gpu in available_gpus:
                print(f"     • {gpu.gpu_type}: {gpu.memory_gb}GB VRAM, "
                      f"{gpu.vcpus} vCPUs, {gpu.ram_gb}GB RAM")
        except Exception as e:
            print(f"   ⚠️  Failed to list GPUs: {e}")
            available_gpus = []
        print()
        
        # 3. Submit Sample Jobs
        sample_jobs = create_sample_jobs()
        submitted_jobs = []
        
        for i, job_config in enumerate(sample_jobs, 1):
            print(f"📤 Submitting job {i}/{len(sample_jobs)}: {job_config.name}")
            print(f"   Image: {job_config.image}")
            print(f"   GPU: {job_config.gpu_spec.gpu_type} ({job_config.gpu_spec.memory_gb}GB)")
            print(f"   Timeout: {job_config.timeout_minutes} minutes")
            
            try:
                job_id = await provider.submit_job(job_config)
                submitted_jobs.append((job_id, job_config))
                print(f"   ✅ Job submitted successfully!")
                print(f"   Job ID: {job_id}")
            except Exception as e:
                print(f"   ❌ Job submission failed: {e}")
            print()
        
        if not submitted_jobs:
            print("❌ No jobs were submitted successfully. Demo completed.")
            return
        
        # 4. Monitor Job Status
        print("📋 Monitoring job status...")
        for job_id, job_config in submitted_jobs:
            print(f"   Checking job: {job_config.name} ({job_id[:12]}...)")
            try:
                result = await provider.get_job_status(job_id)
                print(f"     Status: {result.status.value}")
                print(f"     Created: {result.created_at.strftime('%Y-%m-%d %H:%M:%S UTC')}")
                if result.started_at:
                    print(f"     Started: {result.started_at.strftime('%Y-%m-%d %H:%M:%S UTC')}")
                print(f"     Runtime: {result.runtime_seconds}s")
                
                # Additional metadata
                if result.metadata:
                    runpod_status = result.metadata.get('runpod_status')
                    machine_id = result.metadata.get('machine_id')
                    if runpod_status:
                        print(f"     RunPod Status: {runpod_status}")
                    if machine_id:
                        print(f"     Machine ID: {machine_id}")
                        
            except Exception as e:
                print(f"     ❌ Status check failed: {e}")
            print()
        
        # 5. Get Job Logs (for first job)
        if submitted_jobs:
            job_id, job_config = submitted_jobs[0]
            print(f"📜 Getting logs for job: {job_config.name}")
            try:
                logs = await provider.get_job_logs(job_id, lines=20)
                if logs:
                    print("   Last 20 lines:")
                    for line in logs.split('\n')[-20:]:
                        if line.strip():
                            print(f"     {line}")
                else:
                    print("   No logs available yet")
            except Exception as e:
                print(f"   ❌ Log retrieval failed: {e}")
            print()
        
        # 6. Cost Information
        print("💰 Cost analysis...")
        total_estimated_cost = 0
        for job_id, job_config in submitted_jobs:
            print(f"   Job: {job_config.name}")
            try:
                cost_info = await provider.get_cost_info(job_id)
                print(f"     Cost: ${cost_info.total_cost:.4f} {cost_info.currency}")
                if cost_info.breakdown:
                    gpu_type = cost_info.breakdown.get('gpu_type', 'Unknown')
                    runtime_hours = cost_info.breakdown.get('runtime_hours', 0)
                    hourly_rate = cost_info.breakdown.get('hourly_rate', 0)
                    print(f"     GPU: {gpu_type}")
                    print(f"     Runtime: {runtime_hours:.2f} hours")
                    print(f"     Rate: ${hourly_rate:.2f}/hour")
                total_estimated_cost += cost_info.total_cost
            except Exception as e:
                print(f"     ❌ Cost calculation failed: {e}")
            print()
        
        print(f"💵 Total estimated cost: ${total_estimated_cost:.4f} USD")
        print()
        
        # 7. Demonstrate Job Cancellation
        if len(submitted_jobs) > 1:
            job_id, job_config = submitted_jobs[-1]  # Cancel the last job
            print(f"🛑 Cancelling job: {job_config.name}")
            try:
                cancelled = await provider.cancel_job(job_id)
                if cancelled:
                    print("   ✅ Job cancelled successfully")
                else:
                    print("   ⚠️  Job cancellation failed or job already completed")
            except Exception as e:
                print(f"   ❌ Cancellation error: {e}")
        print()
    
    print("🎉 RunPod workflow demonstration completed!")
    print()
    print("💡 Key Features Demonstrated:")
    print("   ✅ Service health checking")
    print("   ✅ GPU resource discovery")
    print("   ✅ Multi-job submission")
    print("   ✅ Real-time status monitoring")
    print("   ✅ Log retrieval and analysis")
    print("   ✅ Cost calculation and tracking")
    print("   ✅ Job lifecycle management")
    print()
    print("🔧 To use with real RunPod account:")
    print("   1. Set RUNPOD_API_KEY environment variable")
    print("   2. Ensure RunPod account has sufficient credits")
    print("   3. Modify job configurations as needed")


async def quick_test():
    """Quick test function for basic functionality."""
    print("🧪 Quick RunPod Test")
    print("=" * 30)
    
    config = get_runpod_config()
    
    # Test basic adapter initialization
    try:
        adapter = RunPodAdapter(config)
        print("✅ Adapter initialization: OK")
    except Exception as e:
        print(f"❌ Adapter initialization failed: {e}")
        return
    
    # Test health check
    async with adapter:
        try:
            health = await adapter.health_check()
            print(f"✅ Health check: {health['status']}")
        except Exception as e:
            print(f"⚠️  Health check: {e}")
    
    print("🎯 Quick test completed!")


if __name__ == "__main__":
    import sys
    
    if len(sys.argv) > 1 and sys.argv[1] == "--quick":
        asyncio.run(quick_test())
    else:
        asyncio.run(demonstrate_runpod_workflow())
