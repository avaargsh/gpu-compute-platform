from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.api.auth import router as auth_router
from app.api.v2_tenancy import router as tenancy_router
from app.api.v2_workloads import router as workloads_router
from app.api.v1_compute_pools import router as compute_pools_router
from app.core.config import settings
import app.models.control_plane_resource
import app.models.control_plane_revision
import app.models.tenancy


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Production schema is managed exclusively by Alembic.
    yield


app = FastAPI(
    title="AI Compute Control Plane",
    version="1.0.0-alpha.1",
    debug=settings.debug,
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=settings.allowed_origins,
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(auth_router, prefix="/auth", tags=["authentication"])
app.include_router(tenancy_router, prefix="/api/v1", tags=["tenancy"])
app.include_router(workloads_router, prefix="/api/v1", tags=["workloads"])
app.include_router(compute_pools_router, prefix="/api/v1", tags=["compute-pools"])


@app.get("/")
async def root():
    return {
        "name": "AI Compute Control Plane",
        "api_version": "v1",
        "docs": "/docs",
        "health": "/healthz",
    }


@app.get("/healthz")
async def healthz():
    return {"status": "ok"}
