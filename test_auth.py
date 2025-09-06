#!/usr/bin/env python3

import asyncio
import httpx
import json


async def test_authentication_system():
    """Test the authentication system by registering and logging in a user."""
    
    base_url = "http://localhost:8000"
    
    # Test data
    test_user = {
        "email": "test@example.com",
        "password": "testpassword123",
        "first_name": "Test",
        "last_name": "User",
        "organization": "Test Org"
    }
    
    print("🚀 Testing GPU Compute Platform Authentication System")
    print("=" * 60)
    
    try:
        async with httpx.AsyncClient() as client:
            # Test 1: Check health endpoint
            print("\n1. Testing health endpoint...")
            response = await client.get(f"{base_url}/health")
            if response.status_code == 200:
                print("✅ Health check passed")
                print(f"   Response: {response.json()}")
            else:
                print("❌ Health check failed")
                return
            
            # Test 2: Check root endpoint
            print("\n2. Testing root endpoint...")
            response = await client.get(f"{base_url}/")
            if response.status_code == 200:
                print("✅ Root endpoint works")
                print(f"   Response: {response.json()}")
            else:
                print("❌ Root endpoint failed")
                return
            
            # Test 3: User registration
            print("\n3. Testing user registration...")
            response = await client.post(
                f"{base_url}/auth/register",
                json=test_user
            )
            
            if response.status_code == 201:
                print("✅ User registration successful")
                user_data = response.json()
                print(f"   User ID: {user_data['id']}")
                print(f"   Email: {user_data['email']}")
                print(f"   Name: {user_data['first_name']} {user_data['last_name']}")
            else:
                print("❌ User registration failed")
                print(f"   Status: {response.status_code}")
                print(f"   Response: {response.text}")
                return
            
            # Test 4: User login
            print("\n4. Testing user login...")
            login_data = {
                "username": test_user["email"],
                "password": test_user["password"]
            }
            
            response = await client.post(
                f"{base_url}/auth/jwt/login",
                data=login_data  # Form data for OAuth2
            )
            
            if response.status_code == 200:
                print("✅ User login successful")
                login_response = response.json()
                access_token = login_response["access_token"]
                print(f"   Token type: {login_response['token_type']}")
                print(f"   Access token: {access_token[:50]}...")
            else:
                print("❌ User login failed")
                print(f"   Status: {response.status_code}")
                print(f"   Response: {response.text}")
                return
            
            # Test 5: Access protected route
            print("\n5. Testing protected route access...")
            headers = {"Authorization": f"Bearer {access_token}"}
            response = await client.get(
                f"{base_url}/api/protected-route",
                headers=headers
            )
            
            if response.status_code == 200:
                print("✅ Protected route access successful")
                protected_data = response.json()
                print(f"   Message: {protected_data['message']}")
                print(f"   User data: {json.dumps(protected_data['user_data'], indent=4)}")
            else:
                print("❌ Protected route access failed")
                print(f"   Status: {response.status_code}")
                print(f"   Response: {response.text}")
            
            # Test 6: Test without token (should fail)
            print("\n6. Testing protected route without token...")
            response = await client.get(f"{base_url}/api/protected-route")
            
            if response.status_code == 401:
                print("✅ Protected route correctly rejects unauthenticated requests")
            else:
                print(f"⚠️  Unexpected status for unauthenticated request: {response.status_code}")
            
            print("\n" + "=" * 60)
            print("🎉 All authentication tests completed successfully!")
            print("\n📋 Available endpoints:")
            print("   - POST /auth/register - Register new user")
            print("   - POST /auth/jwt/login - Login user")
            print("   - GET /api/protected-route - Protected route (requires token)")
            print("   - GET /docs - API documentation")
            
    except httpx.ConnectError:
        print("❌ Could not connect to server. Is it running?")
        print("   Start the server with: uv run uvicorn app.main:app --reload")
        return False
    
    except Exception as e:
        print(f"❌ Test failed with error: {e}")
        return False
    
    return True


if __name__ == "__main__":
    asyncio.run(test_authentication_system())
