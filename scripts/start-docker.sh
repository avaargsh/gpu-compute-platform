#!/bin/bash

# GPU Compute Platform Docker Startup Script

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}}")/.. && pwd)"
cd "$PROJECT_ROOT"

echo "🚀 Starting GPU Compute Platform with Docker"
echo "Project root: $PROJECT_ROOT"

# Function to check if Docker and Docker Compose are available
check_docker() {
    if ! command -v docker &> /dev/null; then
        echo "❌ Docker is not installed. Please install Docker first."
        exit 1
    fi

    if ! docker info &> /dev/null; then
        echo "❌ Docker is not running. Please start Docker first."
        exit 1
    fi

    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
        echo "❌ Docker Compose is not available. Please install Docker Compose."
        exit 1
    fi
}

# Function to check if NVIDIA Docker is available (for GPU support)
check_nvidia_docker() {
    if command -v nvidia-docker &> /dev/null || docker info | grep -q nvidia; then
        echo "✅ NVIDIA Docker support detected"
        GPU_SUPPORT="true"
    else
        echo "⚠️  NVIDIA Docker support not detected. Running without GPU acceleration."
        echo "   For GPU support, install nvidia-container-toolkit:"
        echo "   https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html"
        GPU_SUPPORT="false"
    fi
}

# Function to start services
start_services() {
    local compose_command=""
    
    # Determine which docker-compose command to use
    if command -v docker-compose &> /dev/null; then
        compose_command="docker-compose"
    else
        compose_command="docker compose"
    fi

    # Choose compose file based on mode
    local compose_file="docker-compose.yml"
    local mode="production"
    
    # Check if development mode is requested
    if [[ "${1:-}" == "dev" || "${1:-}" == "development" ]]; then
        compose_file="docker-compose.dev.yml"
        mode="development"
        echo "🛠️  Starting in development mode with hot reload"
    else
        echo "🏭 Starting in production mode"
    fi

    echo "📋 Using compose file: $compose_file"

    # Build and start services
    echo "🔨 Building Docker images..."
    $compose_command -f $compose_file build

    echo "🚀 Starting services..."
    $compose_command -f $compose_file up -d

    echo "⏳ Waiting for services to be ready..."
    sleep 10

    # Show service status
    echo "📊 Service Status:"
    $compose_command -f $compose_file ps

    # Show service URLs
    echo ""
    echo "✅ GPU Compute Platform started successfully!"
    echo ""
    echo "📱 Available Services:"
    echo "   🔗 API Server: http://localhost:8000"
    echo "   📚 API Docs: http://localhost:8000/docs"
    echo "   🏥 Health Check: http://localhost:8000/health"
    
    if [[ "$mode" == "development" ]]; then
        echo "   🎨 Frontend Dev: http://localhost:3000"
    fi
    
    echo "   🗄️  Database: postgresql://postgres:postgres@localhost:5432/gpu_platform"
    echo "   🔄 Redis: redis://localhost:6379"
    echo "   📊 MLflow: http://localhost:5000"
    echo ""
    
    if [[ "${2:-}" == "--with-monitoring" ]]; then
        echo "   📈 Prometheus: http://localhost:9090"
        echo "   📊 Grafana: http://localhost:3000 (admin/admin)"
        echo ""
        echo "🎯 Starting monitoring services..."
        $compose_command -f $compose_file --profile monitoring up -d
    fi

    echo "🛠️  Development Commands:"
    echo "   📋 View logs: $compose_command -f $compose_file logs -f"
    echo "   ⛔ Stop services: $compose_command -f $compose_file down"
    echo "   🗑️  Clean up: $compose_command -f $compose_file down -v"
    echo "   🔧 Rebuild: $compose_command -f $compose_file build --no-cache"
}

# Function to display help
show_help() {
    echo "Usage: $0 [MODE] [OPTIONS]"
    echo ""
    echo "MODES:"
    echo "  prod, production    Start in production mode (default)"
    echo "  dev, development    Start in development mode with hot reload"
    echo ""
    echo "OPTIONS:"
    echo "  --with-monitoring   Include Prometheus and Grafana monitoring"
    echo "  --help, -h          Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                           # Start in production mode"
    echo "  $0 dev                       # Start in development mode"
    echo "  $0 dev --with-monitoring     # Start dev mode with monitoring"
}

# Parse command line arguments
case "${1:-}" in
    --help|-h|help)
        show_help
        exit 0
        ;;
esac

# Check prerequisites
check_docker
check_nvidia_docker

# Start services
start_services "$@"
