from pydantic_settings import BaseSettings
from pydantic import Field
from typing import List
import secrets


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
    celery_broker_url: str = "redis://localhost:6379/0"
    celery_result_backend: str = "redis://localhost:6379/0"
    
    # MLflow配置
    mlflow_tracking_uri: str = "http://localhost:5000"
    mlflow_experiment_name: str = "gpu-compute-platform"
    mlflow_artifact_location: str = "./mlruns"
    
    # Redis配置
    redis_url: str = "redis://localhost:6379/0"
    
    model_config = {"env_file": ".env", "case_sensitive": False}


settings = Settings()
