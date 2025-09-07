import asyncio
from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.api.auth import router as auth_router
# 尝试导入增强的API，如果失败则跳过
try:
    from app.api.enhanced_auth import router as enhanced_auth_router
    HAS_ENHANCED_AUTH = True
except ImportError:
    HAS_ENHANCED_AUTH = False

try:
    from app.api.enhanced_tasks import router as enhanced_tasks_router
    HAS_ENHANCED_TASKS = True
except ImportError:
    HAS_ENHANCED_TASKS = False

try:
    from app.api.providers import router as providers_router
    HAS_PROVIDERS = True
except ImportError:
    HAS_PROVIDERS = False

try:
    from app.api.v1.websocket import router as websocket_router
    HAS_WEBSOCKET = True
except ImportError:
    HAS_WEBSOCKET = False
from app.api.protected import router as protected_router
from app.api.gpu_jobs import router as gpu_jobs_router
from app.api.dag import router as dag_router
from app.core.config import settings
from app.core.database import create_db_and_tables
# from app.core.websocket_manager import cleanup_stale_connections_periodic


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Create database tables on startup
    await create_db_and_tables()
    
    # Start WebSocket cleanup task (disabled for now)
    # cleanup_task = None
    # if settings.environment != "testing":
    #     cleanup_task = asyncio.create_task(cleanup_stale_connections_periodic())
    
    yield
    
    # Cleanup on shutdown
    # if cleanup_task:
    #     cleanup_task.cancel()
    #     try:
    #         await cleanup_task
    #     except asyncio.CancelledError:
    #         pass


# Create FastAPI app
app = FastAPI(
    title=settings.app_name,
    version=settings.version,
    debug=settings.debug,
    lifespan=lifespan,
)

# Add CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=settings.allowed_origins,
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Include routers
app.include_router(auth_router, prefix="/auth", tags=["authentication"])

if HAS_ENHANCED_AUTH:
    app.include_router(enhanced_auth_router, prefix="/auth", tags=["enhanced-auth"])

if HAS_ENHANCED_TASKS:
    app.include_router(enhanced_tasks_router, prefix="/tasks", tags=["tasks"])

if HAS_PROVIDERS:
    app.include_router(providers_router, prefix="/providers", tags=["providers"])

if HAS_WEBSOCKET:
    app.include_router(websocket_router, prefix="/api/v1", tags=["websocket"])

# 包含原有的路由
try:
    app.include_router(protected_router, prefix="/api", tags=["protected"])
except:
    pass

try:
    app.include_router(gpu_jobs_router, prefix="/api/gpu", tags=["gpu-jobs"])
except:
    pass

try:
    app.include_router(dag_router, prefix="/api", tags=["dag"])
except:
    pass


@app.get("/")
async def root():
    """Root endpoint with basic info."""
    return {
        "message": "Welcome to GPU Compute Platform API",
        "version": settings.version,
        "docs": "/docs",
        "redoc": "/redoc"
    }


@app.get("/health")
async def health_check():
    """Health check endpoint."""
    return {"status": "healthy"}
