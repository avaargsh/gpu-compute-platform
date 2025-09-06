"""Authentication system tests."""
import pytest
from httpx import AsyncClient
import uuid


class TestAuthentication:
    """Test user authentication functionality."""
    
    async def test_user_registration(self, client: AsyncClient, test_user_data):
        """Test user registration endpoint."""
        response = await client.post("/auth/register", json=test_user_data)
        
        assert response.status_code == 201
        data = response.json()
        
        # Check user data
        assert data["email"] == test_user_data["email"]
        assert data["first_name"] == test_user_data["first_name"]
        assert data["last_name"] == test_user_data["last_name"]
        assert data["organization"] == test_user_data["organization"]
        assert data["is_active"] is True
        assert data["is_verified"] is False
        assert data["is_superuser"] is False
        assert "id" in data
        
        # Check UUID format
        assert uuid.UUID(data["id"])
        
        # Check password is not returned
        assert "password" not in data
        assert "hashed_password" not in data
    
    async def test_duplicate_user_registration(self, client: AsyncClient):
        """Test that duplicate user registration fails."""
        test_data = {
            "email": "duplicate@example.com",
            "password": "testpassword123",
            "first_name": "Duplicate",
            "last_name": "User"
        }
        
        # First registration should succeed
        response1 = await client.post("/auth/register", json=test_data)
        assert response1.status_code == 201
        
        # Second registration with same email should fail
        response2 = await client.post("/auth/register", json=test_data)
        assert response2.status_code == 400
    
    async def test_user_login(self, client: AsyncClient, test_user_data):
        """Test user login endpoint."""
        # First register a user
        await client.post("/auth/register", json=test_user_data)
        
        # Now try to login
        login_data = {
            "username": test_user_data["email"],
            "password": test_user_data["password"]
        }
        
        response = await client.post("/auth/jwt/login", data=login_data)
        
        assert response.status_code == 200
        data = response.json()
        
        assert "access_token" in data
        assert "token_type" in data
        assert data["token_type"] == "bearer"
        assert isinstance(data["access_token"], str)
        assert len(data["access_token"]) > 50  # JWT tokens are quite long
    
    async def test_invalid_login(self, client: AsyncClient):
        """Test login with invalid credentials."""
        login_data = {
            "username": "nonexistent@example.com",
            "password": "wrongpassword"
        }
        
        response = await client.post("/auth/jwt/login", data=login_data)
        
        assert response.status_code == 400
    
    async def test_protected_route_without_token(self, client: AsyncClient):
        """Test accessing protected route without authentication token."""
        response = await client.get("/api/protected-route")
        
        assert response.status_code == 401
    
    async def test_protected_route_with_token(self, client: AsyncClient, test_user_data):
        """Test accessing protected route with valid authentication token."""
        # Register and login user
        await client.post("/auth/register", json=test_user_data)
        
        login_data = {
            "username": test_user_data["email"],
            "password": test_user_data["password"]
        }
        
        login_response = await client.post("/auth/jwt/login", data=login_data)
        assert login_response.status_code == 200
        
        token_data = login_response.json()
        access_token = token_data["access_token"]
        
        # Access protected route
        headers = {"Authorization": f"Bearer {access_token}"}
        response = await client.get("/api/protected-route", headers=headers)
        
        assert response.status_code == 200
        data = response.json()
        
        assert "message" in data
        assert "user_id" in data
        assert "user_data" in data
        
        user_data = data["user_data"]
        assert user_data["email"] == test_user_data["email"]
        assert user_data["first_name"] == test_user_data["first_name"]
        assert user_data["last_name"] == test_user_data["last_name"]
    
    async def test_protected_route_with_invalid_token(self, client: AsyncClient):
        """Test accessing protected route with invalid token."""
        headers = {"Authorization": "Bearer invalid_token_here"}
        response = await client.get("/api/protected-route", headers=headers)
        
        assert response.status_code == 401
    
    async def test_user_registration_validation(self, client: AsyncClient):
        """Test user registration input validation."""
        # Test with missing email
        invalid_data = {
            "password": "testpass123",
            "first_name": "Test"
        }
        
        response = await client.post("/auth/register", json=invalid_data)
        assert response.status_code == 422
        
        # Test with invalid email format
        invalid_data = {
            "email": "not-an-email",
            "password": "testpass123"
        }
        
        response = await client.post("/auth/register", json=invalid_data)
        assert response.status_code == 422
        
        # Test with missing password
        invalid_data = {
            "email": "test@example.com"
        }
        
        response = await client.post("/auth/register", json=invalid_data)
        assert response.status_code == 422
