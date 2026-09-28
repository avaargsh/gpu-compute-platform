import asyncio
import pytest
import pytest_asyncio
from httpx import AsyncClient
from sqlalchemy.ext.asyncio import AsyncSession, create_async_engine, async_sessionmaker
from sqlalchemy.pool import StaticPool
import tempfile
import os

from app.main import app
from app.core.database import get_async_session, Base
from app.models.user import User


# Test database setup
@pytest.fixture(scope="session")
def temp_db():
    """Create a temporary database file for testing."""
    temp_dir = tempfile.mkdtemp()
    db_path = os.path.join(temp_dir, "test.db")
    return f"sqlite+aiosqlite:///{db_path}"


@pytest_asyncio.fixture(scope="session")
async def test_engine(temp_db):
    """Create test database engine."""
    engine = create_async_engine(
        temp_db,
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
        echo=False
    )
    
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
    
    yield engine
    await engine.dispose()


@pytest_asyncio.fixture
async def test_session(test_engine):
    """Create a database session for testing."""
    async_session_maker = async_sessionmaker(
        test_engine,
        class_=AsyncSession,
        expire_on_commit=False
    )
    
    async with async_session_maker() as session:
        yield session


@pytest_asyncio.fixture
async def client(test_session):
    """Create test client with database session override."""
    def get_test_session():
        yield test_session
    
    app.dependency_overrides[get_async_session] = get_test_session
    
    from httpx import ASGITransport
    async with AsyncClient(
        transport=ASGITransport(app=app), 
        base_url="http://test"
    ) as test_client:
        yield test_client
    
    # Clean up
    app.dependency_overrides.clear()


@pytest.fixture
def test_user_data():
    """Test user data with unique email per test to avoid uniqueness conflicts."""
    import uuid
    return {
        "email": f"test+{uuid.uuid4().hex[:8]}@example.com",
        "password": "testpassword123",
        "first_name": "Test",
        "last_name": "User",
        "organization": "Test Organization"
    }


@pytest.fixture
def test_admin_data():
    """Test admin user data with unique email per test."""
    import uuid
    return {
        "email": f"admin+{uuid.uuid4().hex[:8]}@example.com",
        "password": "adminpassword123",
        "first_name": "Admin",
        "last_name": "User",
        "organization": "Admin Organization"
    }
