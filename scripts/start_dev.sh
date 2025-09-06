#!/bin/bash

# GPU Compute Platform Development Startup Script (using uv for backend)

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "🚀 Starting GPU Compute Platform Development Environment"
echo "Project root: $PROJECT_ROOT"

# Check if uv is installed
if ! command -v uv &> /dev/null; then
    echo "❌ uv is not installed. Please install it first:"
    echo "curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

echo "📋 Checking dependencies..."

# Sync backend dependencies with uv
echo "🔄 Syncing backend dependencies with uv..."
uv sync

# Check if frontend dependencies are installed
if [ ! -d "frontend/node_modules" ]; then
    echo "❌ Frontend dependencies not installed. Installing..."
    cd frontend
    npm install
    cd ..
fi

echo "🔧 Starting services..."

# Start backend with uv
echo "🌟 Starting backend server with uv..."
uv run python scripts/run_dev.py &
BACKEND_PID=$!

# Wait for backend to start
sleep 3

# Start frontend
echo "🎨 Starting frontend development server..."
cd frontend
npm run dev &
FRONTEND_PID=$!
cd ..

echo "✅ Development environment started!"
echo "Backend: http://localhost:8000"
echo "Frontend: http://localhost:3000"
echo "API Docs: http://localhost:8000/docs"
echo ""
echo "Press Ctrl+C to stop all services"

# Function to cleanup on exit
cleanup() {
    echo ""
    echo "🛑 Shutting down services..."
    kill $BACKEND_PID 2>/dev/null || true
    kill $FRONTEND_PID 2>/dev/null || true
    echo "👋 Development environment stopped"
    exit 0
}

trap cleanup SIGINT SIGTERM

# Wait for processes
wait
