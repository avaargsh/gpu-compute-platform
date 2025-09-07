# GPU Compute Platform Dockerfile
# Multi-stage build for optimized image size

# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

# Copy frontend package files
COPY frontend/package*.json ./
RUN npm ci --omit=dev

# Copy frontend source and build
COPY frontend/ ./
RUN npm run build

# Stage 2: Python base with GPU support
FROM nvidia/cuda:12.1-runtime-ubuntu22.04 AS python-base

# Install system dependencies
RUN apt-get update && apt-get install -y \
    python3.12 \
    python3.12-venv \
    python3-pip \
    curl \
    git \
    build-essential \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

# Install uv package manager
RUN pip3 install uv

# Create non-root user
RUN useradd -m -u 1000 appuser && \
    mkdir -p /app && \
    chown -R appuser:appuser /app

USER appuser
WORKDIR /app

# Stage 3: Dependencies
FROM python-base AS deps

# Copy Python project configuration
COPY --chown=appuser:appuser pyproject.toml ./
COPY --chown=appuser:appuser README.md ./

# Install Python dependencies with uv
RUN uv venv .venv && \
    . .venv/bin/activate && \
    uv pip install -e .

# Stage 4: Application
FROM python-base AS app

# Copy virtual environment from deps stage
COPY --from=deps --chown=appuser:appuser /app/.venv /app/.venv

# Copy application code
COPY --chown=appuser:appuser app/ ./app/
COPY --chown=appuser:appuser alembic/ ./alembic/
COPY --chown=appuser:appuser alembic.ini ./
COPY --chown=appuser:appuser main.py ./
COPY --chown=appuser:appuser scripts/ ./scripts/
COPY --chown=appuser:appuser examples/ ./examples/
COPY --chown=appuser:appuser tests/ ./tests/
COPY --chown=appuser:appuser pytest.ini ./

# Copy built frontend from frontend-builder
COPY --from=frontend-builder --chown=appuser:appuser /app/frontend/dist ./frontend/dist

# Make scripts executable
RUN chmod +x scripts/*.sh

# Set environment variables
ENV PATH="/app/.venv/bin:$PATH"
ENV PYTHONPATH="/app"
ENV ENVIRONMENT="production"
ENV DATABASE_URL="postgresql://postgres:postgres@postgres:5432/gpu_platform"
ENV CELERY_BROKER_URL="redis://redis:6379/0"
ENV CELERY_RESULT_BACKEND="redis://redis:6379/0"
ENV REDIS_URL="redis://redis:6379/0"
ENV MLFLOW_TRACKING_URI="http://mlflow:5000"

# Health check
HEALTHCHECK --interval=30s --timeout=30s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8000/health || exit 1

# Expose port
EXPOSE 8000

# Default command
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000", "--workers", "4"]
