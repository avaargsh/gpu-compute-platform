from pydantic_settings import BaseSettings
from typing import Optional
import secrets


class Settings(BaseSettings):
    # App settings
    app_name: str = "GPU Compute Platform"
    debug: bool = False
    version: str = "0.1.0"
    
    # Security
    secret_key: str = secrets.token_urlsafe(32)
    access_token_expire_minutes: int = 30
    algorithm: str = "HS256"
    
    # Database
    database_url: str = "sqlite+aiosqlite:///./gpu_platform.db"
    
    # CORS
    allowed_origins: list[str] = ["http://localhost:3000", "http://localhost:8000"]
    
    # Redis (for future Celery integration)
    redis_url: str = "redis://localhost:6379/0"
    
    model_config = {"env_file": ".env", "case_sensitive": False}


settings = Settings()
