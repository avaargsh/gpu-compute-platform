"""Database models tests."""
import pytest
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.user import User
from fastapi_users.db import SQLAlchemyUserDatabase
import uuid


class TestUserModel:
    """Test User model functionality."""
    
    async def test_create_user(self, test_session: AsyncSession):
        """Test creating a user directly in database."""
        user_data = {
            "id": uuid.uuid4(),
            "email": "testuser@example.com",
            "hashed_password": "hashed_password_here",
            "first_name": "Test",
            "last_name": "User",
            "organization": "Test Org",
            "is_active": True,
            "is_verified": False,
            "is_superuser": False
        }
        
        user = User(**user_data)
        test_session.add(user)
        await test_session.commit()
        
        # Fetch the user back
        result = await test_session.execute(
            select(User).where(User.email == "testuser@example.com")
        )
        saved_user = result.scalar_one()
        
        assert saved_user.email == user_data["email"]
        assert saved_user.first_name == user_data["first_name"]
        assert saved_user.last_name == user_data["last_name"]
        assert saved_user.organization == user_data["organization"]
        assert saved_user.is_active is True
        assert saved_user.is_verified is False
        assert saved_user.is_superuser is False
        assert saved_user.total_compute_hours == "0.0"
        assert saved_user.total_cost == "0.0"
    
    async def test_user_repr(self, test_session: AsyncSession):
        """Test User model string representation."""
        user_id = uuid.uuid4()
        user = User(
            id=user_id,
            email="repr@example.com",
            hashed_password="hashed"
        )
        
        expected_repr = f"<User(id={user_id}, email=repr@example.com)>"
        assert str(user) == expected_repr
    
    async def test_user_defaults(self, test_session: AsyncSession):
        """Test User model default values."""
        user = User(
            id=uuid.uuid4(),
            email="defaults@example.com",
            hashed_password="hashed"
        )
        
        test_session.add(user)
        await test_session.commit()
        
        # Refresh to get server defaults
        await test_session.refresh(user)
        
        assert user.is_active is True
        assert user.is_verified is False
        assert user.is_superuser is False
        assert user.total_compute_hours == "0.0"
        assert user.total_cost == "0.0"
        assert user.created_at is not None
    
    async def test_sqlalchemy_user_database(self, test_session: AsyncSession):
        """Test SQLAlchemy user database integration."""
        user_db = SQLAlchemyUserDatabase(test_session, User)
        
        # Test creating user through FastAPI Users DB interface
        user_create_data = {
            "email": "fastapi_user@example.com",
            "hashed_password": "hashed_password",
            "is_active": True,
            "is_verified": False,
            "is_superuser": False
        }
        
        created_user = await user_db.create(user_create_data)
        
        assert created_user.email == "fastapi_user@example.com"
        assert created_user.is_active is True
        assert isinstance(created_user.id, uuid.UUID)
        
        # Test fetching user by ID
        fetched_user = await user_db.get(created_user.id)
        assert fetched_user is not None
        assert fetched_user.email == created_user.email
        
        # Test fetching user by email
        fetched_by_email = await user_db.get_by_email("fastapi_user@example.com")
        assert fetched_by_email is not None
        assert fetched_by_email.id == created_user.id
