#!/bin/bash

# GPU Compute Platform 统一启动脚本

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

# 帮助信息
show_help() {
    echo "GPU Compute Platform 启动脚本"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  backend        仅启动后端服务"
    echo "  frontend       仅启动前端服务 (需要 Node.js)"
    echo "  full, dev      启动完整开发环境 (前端+后端)"
    echo "  docker         使用 Docker 启动"
    echo "  docker-dev     使用 Docker 启动开发环境"
    echo "  test           运行测试"
    echo "  --help, -h     显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  $0 backend            # 仅启动后端"
    echo "  $0 dev                # 启动开发环境"
    echo "  $0 docker             # Docker 生产环境"
    echo "  $0 docker-dev         # Docker 开发环境"
}

# 检查 uv
check_uv() {
    if ! command -v uv &> /dev/null; then
        echo "❌ uv 未安装。请先安装:"
        echo "curl -LsSf https://astral.sh/uv/install.sh | sh"
        exit 1
    fi
}

# 检查前端依赖
check_frontend_deps() {
    if [ ! -d "frontend/node_modules" ]; then
        echo "📦 安装前端依赖..."
        cd frontend
        npm install
        cd ..
    fi
}

# 启动后端
start_backend() {
    echo "🌟 启动后端服务..."
    check_uv
    echo "🔄 同步依赖..."
    uv sync
    echo "🚀 启动 FastAPI 服务器..."
    uv run python scripts/run_dev.py
}

# 启动前端
start_frontend() {
    echo "🎨 启动前端服务..."
    check_frontend_deps
    cd frontend
    npm run dev
}

# 启动完整开发环境
start_dev() {
    echo "🚀 启动 GPU 计算平台开发环境"
    echo "项目根目录: $PROJECT_ROOT"
    
    check_uv
    check_frontend_deps
    
    echo "🔄 同步后端依赖..."
    uv sync
    
    echo "🔧 启动服务..."
    
    # 启动后端
    echo "🌟 启动后端服务..."
    uv run python scripts/run_dev.py &
    BACKEND_PID=$!
    
    # 等待后端启动
    sleep 3
    
    # 启动前端
    echo "🎨 启动前端开发服务器..."
    cd frontend
    npm run dev &
    FRONTEND_PID=$!
    cd ..
    
    echo "✅ 开发环境启动完成!"
    echo "后端: http://localhost:8000"
    echo "前端: http://localhost:3000"
    echo "API 文档: http://localhost:8000/docs"
    echo ""
    echo "按 Ctrl+C 停止所有服务"
    
    # 清理函数
    cleanup() {
        echo ""
        echo "🛑 关闭服务..."
        kill $BACKEND_PID 2>/dev/null || true
        kill $FRONTEND_PID 2>/dev/null || true
        echo "👋 开发环境已停止"
        exit 0
    }
    
    trap cleanup SIGINT SIGTERM
    wait
}

# Docker 启动
start_docker() {
    local mode="${1:-prod}"
    echo "🐳 使用 Docker 启动 (模式: $mode)"
    
    if [ "$mode" = "dev" ]; then
        exec "$PROJECT_ROOT/scripts/start-docker.sh" dev
    else
        exec "$PROJECT_ROOT/scripts/start-docker.sh" prod
    fi
}

# 运行测试
run_tests() {
    echo "🧪 运行测试..."
    exec "$PROJECT_ROOT/scripts/run_tests.sh"
}

# 主逻辑
case "${1:-dev}" in
    backend)
        start_backend
        ;;
    frontend)
        start_frontend
        ;;
    full|dev|development)
        start_dev
        ;;
    docker)
        start_docker prod
        ;;
    docker-dev|docker-development)
        start_docker dev
        ;;
    test|tests)
        run_tests
        ;;
    --help|-h|help)
        show_help
        ;;
    *)
        echo "❌ 未知选项: $1"
        show_help
        exit 1
        ;;
esac
