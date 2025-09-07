from pydantic_settings import BaseSettings
from pydantic import Field
from typing import List, Optional, Dict
import secrets
import os


class Settings(BaseSettings):
    # App settings
    app_name: str = "GPU Compute Platform"
    debug: bool = False
    version: str = "0.1.0"
    environment: str = "development"
    
    # Security
    secret_key: str = secrets.token_urlsafe(32)
    access_token_expire_minutes: int = 30
    algorithm: str = "HS256"
    
    # Database
    database_url: str = "sqlite+aiosqlite:///./gpu_platform.db"
    
    # CORS
    allowed_origins: List[str] = ["http://localhost:3000", "http://localhost:8000"]
    
    # Celery配置
    celery_broker_url: str = Field(default="redis://localhost:6379/0")
    celery_result_backend: str = Field(default="redis://localhost:6379/0")
    
    # MLflow配置
    mlflow_tracking_uri: str = "http://localhost:5000"
    mlflow_experiment_name: str = "gpu-compute-platform"
    mlflow_artifact_location: str = "./mlruns"
    
    # Redis配置
    redis_url: str = Field(default="redis://localhost:6379/0")
    redis_password: str = Field(default="")
    
    # GPU提供商配置
    # 阿里云
    alibaba_access_key_id: Optional[str] = Field(default=None, env="ALIBABA_ACCESS_KEY_ID")
    alibaba_access_key_secret: Optional[str] = Field(default=None, env="ALIBABA_ACCESS_KEY_SECRET")
    alibaba_region_id: str = Field(default="cn-hangzhou", env="ALIBABA_REGION_ID")
    alibaba_security_group_id: Optional[str] = Field(default=None, env="ALIBABA_SECURITY_GROUP_ID")
    alibaba_vswitch_id: Optional[str] = Field(default=None, env="ALIBABA_VSWITCH_ID")
    alibaba_key_pair_name: Optional[str] = Field(default=None, env="ALIBABA_KEY_PAIR_NAME")
    
    # 腾讯云
    tencent_secret_id: Optional[str] = Field(default=None, env="TENCENT_SECRET_ID")
    tencent_secret_key: Optional[str] = Field(default=None, env="TENCENT_SECRET_KEY")
    tencent_region: str = Field(default="ap-shanghai", env="TENCENT_REGION")
    tencent_cluster_id: Optional[str] = Field(default=None, env="TENCENT_CLUSTER_ID")
    tencent_kubeconfig: Optional[str] = Field(default=None, env="TENCENT_KUBECONFIG")
    
    # RunPod
    runpod_api_key: Optional[str] = Field(default=None, env="RUNPOD_API_KEY")
    runpod_endpoint_id: Optional[str] = Field(default=None, env="RUNPOD_ENDPOINT_ID")
    
    # 调度配置
    default_scheduling_strategy: str = Field(default="balanced")
    enable_intelligent_scheduling: bool = Field(default=True)
    scheduling_update_interval_seconds: int = Field(default=300)  # 5分钟
    
    # 监控和日志
    enable_mlflow: bool = Field(default=True)
    log_level: str = Field(default="INFO")
    log_format: str = Field(default="%(asctime)s - %(name)s - %(levelname)s - %(message)s")
    
    # 性能配置
    max_concurrent_tasks: int = Field(default=100)
    task_timeout_seconds: int = Field(default=3600)  # 1小时
    health_check_interval_seconds: int = Field(default=60)
    
    @property
    def is_testing(self) -> bool:
        """是否为测试环境"""
        return os.getenv('TESTING', 'false').lower() == 'true'
    
    @property
    def gpu_providers_config(self) -> Dict[str, Dict]:
        """获取GPU提供商配置"""
        return {
            "alibaba": {
                "access_key_id": self.alibaba_access_key_id,
                "access_key_secret": self.alibaba_access_key_secret,
                "region_id": self.alibaba_region_id,
                "security_group_id": self.alibaba_security_group_id,
                "vswitch_id": self.alibaba_vswitch_id,
                "key_pair_name": self.alibaba_key_pair_name,
                "enabled": bool(self.alibaba_access_key_id and self.alibaba_access_key_secret),
            },
            "tencent": {
                "secret_id": self.tencent_secret_id,
                "secret_key": self.tencent_secret_key,
                "region": self.tencent_region,
                "cluster_id": self.tencent_cluster_id,
                "kubeconfig": self.tencent_kubeconfig,
                "enabled": bool(self.tencent_secret_id and self.tencent_secret_key),
            },
            "runpod": {
                "api_key": self.runpod_api_key,
                "endpoint_id": self.runpod_endpoint_id,
                "enabled": bool(self.runpod_api_key),
            }
        }
    
    model_config = {"env_file": ".env", "case_sensitive": False}


settings = Settings()
