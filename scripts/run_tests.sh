#!/bin/bash

# Run tests with uv

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "🧪 Running GPU Compute Platform Tests"

# Check if uv is installed
if ! command -v uv &> /dev/null; then
    echo "❌ uv is not installed. Please install it first:"
    echo "curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

# Sync dependencies
echo "🔄 Syncing dependencies..."
uv sync

echo "🔬 Running backend tests..."
uv run pytest tests/ -v

echo "🎨 Running frontend tests..."
cd frontend
if [ -d "node_modules" ]; then
    npm run test
else
    echo "⚠️  Frontend dependencies not installed. Skipping frontend tests."
fi
cd ..

echo "✅ All tests completed!"
