#!/usr/bin/env python3
import asyncio
import websockets
import json

async def test_websocket():
    uri = "ws://localhost:8000/api/gpu/ws"
    try:
        async with websockets.connect(uri) as websocket:
            print("WebSocket connected successfully!")
            
            # Send a test message
            test_message = {
                "type": "subscribe",
                "task_id": "test-task-123"
            }
            await websocket.send(json.dumps(test_message))
            print(f"Sent: {test_message}")
            
            # Wait for response with timeout
            try:
                response = await asyncio.wait_for(websocket.recv(), timeout=5.0)
                print(f"Received: {response}")
            except asyncio.TimeoutError:
                print("No response received within 5 seconds")
                
    except Exception as e:
        print(f"WebSocket connection failed: {e}")

if __name__ == "__main__":
    asyncio.run(test_websocket())
