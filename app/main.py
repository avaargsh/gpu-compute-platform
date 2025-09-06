import asyncio
from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.api.auth import router as auth_router
from app.api.protected import router as protected_router
from app.api.gpu_jobs import router as gpu_jobs_router
from app.api.dag import router as dag_router
from app.core.config import settings
from app.core.database import create_db_and_tables
from app.core.websocket_manager import cleanup_stale_connections_periodic


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Create database tables on startup
    await create_db_and_tables()
    
    # Start WebSocket cleanup task
    cleanup_task = None
    if settings.environment != "testing":
        cleanup_task = asyncio.create_task(cleanup_stale_connections_periodic())
    
    yield
    
    # Cleanup on shutdown
    if cleanup_task:
        cleanup_task.cancel()
        try:
            await cleanup_task
        except asyncio.CancelledError:
            pass


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
app.include_router(protected_router, prefix="/api", tags=["protected"])
app.include_router(gpu_jobs_router, prefix="/api/gpu", tags=["gpu-jobs"])
app.include_router(dag_router, prefix="/api", tags=["dag"])


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
