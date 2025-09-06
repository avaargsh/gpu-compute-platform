#!/bin/bash

# Start only the backend server with uv

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "🌟 Starting GPU Compute Platform Backend"

# Check if uv is installed
if ! command -v uv &> /dev/null; then
    echo "❌ uv is not installed. Please install it first:"
    echo "curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

# Sync dependencies
echo "🔄 Syncing backend dependencies..."
uv sync

# Start backend
echo "🚀 Starting backend server..."
uv run python scripts/run_dev.py
