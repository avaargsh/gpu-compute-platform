"""Basic API endpoint tests."""
import pytest
from httpx import AsyncClient


class TestBasicAPI:
    """Test basic API endpoints."""
    
    async def test_health_check(self, client: AsyncClient):
        """Test health check endpoint."""
        response = await client.get("/health")
        
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "healthy"
    
    async def test_root_endpoint(self, client: AsyncClient):
        """Test root endpoint."""
        response = await client.get("/")
        
        assert response.status_code == 200
        data = response.json()
        assert "message" in data
        assert "version" in data
        assert "docs" in data
        assert data["docs"] == "/docs"
    
    async def test_docs_endpoint(self, client: AsyncClient):
        """Test API documentation endpoint."""
        response = await client.get("/docs")
        
        assert response.status_code == 200
        assert "text/html" in response.headers["content-type"]
    
    async def test_openapi_endpoint(self, client: AsyncClient):
        """Test OpenAPI schema endpoint."""
        response = await client.get("/openapi.json")
        
        assert response.status_code == 200
        data = response.json()
        assert "openapi" in data
        assert "info" in data
        assert data["info"]["title"] == "GPU Compute Platform"
