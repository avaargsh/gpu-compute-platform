#!/usr/bin/env python3
"""
Development database reset script.
WARNING: This will DROP ALL TABLES and recreate them from models.
Only use in development!
"""

import asyncio
import asyncpg
import sys
import os
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from sqlalchemy.ext.asyncio import create_async_engine
from app.core.config import settings
from app.core.database import Base

# Import all models to ensure they're registered
from app.models.user import User
from app.models.task import GpuTask
from app.models.dag import TaskDAG

async def reset_database():
    """Drop all tables and recreate from models."""
    
    # Connect directly to PostgreSQL to drop/recreate schema
    try:
        # Parse database URL to get connection params
        import urllib.parse as urlparse
        parsed = urlparse.urlparse(settings.database_url.replace("postgresql+asyncpg://", "postgresql://"))
        
        conn = await asyncpg.connect(
            host=parsed.hostname,
            port=parsed.port or 5432,
            user=parsed.username,
            password=parsed.password,
            database=parsed.path[1:]  # Remove leading '/'
        )
        
        print("🗑️  Dropping all tables...")
        
        # Get all table names
        tables = await conn.fetch("""
            SELECT tablename FROM pg_tables 
            WHERE schemaname = 'public' AND tablename != 'alembic_version'
        """)
        
        # Drop all tables with CASCADE to handle dependencies
        for table in tables:
            table_name = table['tablename']
            print(f"   Dropping table: {table_name}")
            await conn.execute(f"DROP TABLE IF EXISTS {table_name} CASCADE")
        
        # Also clean up alembic version table
        await conn.execute("DROP TABLE IF EXISTS alembic_version CASCADE")
        
        await conn.close()
        
        print("✅ All tables dropped successfully")
        
    except Exception as e:
        print(f"❌ Error connecting to database: {e}")
        return False
    
    # Now recreate tables using SQLAlchemy
    try:
        print("🔄 Creating tables from models...")
        
        engine = create_async_engine(
            settings.database_url,
            echo=True
        )
        
        async with engine.begin() as conn:
            await conn.run_sync(Base.metadata.create_all)
        
        await engine.dispose()
        
        print("✅ Tables created successfully from models")
        return True
        
    except Exception as e:
        print(f"❌ Error creating tables: {e}")
        return False

async def main():
    """Main function."""
    print("🚨 WARNING: This will completely reset the database!")
    print("   All data will be lost!")
    print()
    
    confirm = input("Are you sure you want to continue? (yes/no): ").lower().strip()
    if confirm != 'yes':
        print("❌ Database reset cancelled")
        return 1
    
    success = await reset_database()
    
    if success:
        print("\n🎉 Database reset completed successfully!")
        print("   You can now start the application with fresh schema.")
        return 0
    else:
        print("\n💥 Database reset failed!")
        return 1

if __name__ == "__main__":
    import sys
    exit_code = asyncio.run(main())
    sys.exit(exit_code)
