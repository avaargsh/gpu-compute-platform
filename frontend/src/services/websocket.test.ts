import { describe, it, expect, beforeEach, vi } from 'vitest'
import { WebSocketService } from './websocket'
import type { WebSocketMessage, TaskUpdateMessage, GPUMetricsMessage } from '@/types'

// Mock socket.io-client
const mockSocket = {
  connected: false,
  id: 'mock-socket-id',
  on: vi.fn(),
  off: vi.fn(),
  emit: vi.fn(),
  disconnect: vi.fn(),
  connect: vi.fn(),
  io: {
    engine: {
      transport: {
        name: 'websocket'
      }
    }
  }
}

vi.mock('socket.io-client', () => ({
  io: vi.fn(() => mockSocket)
}))

describe('WebSocketService', () => {
  let wsService: WebSocketService

  beforeEach(() => {
    vi.clearAllMocks()
    wsService = new WebSocketService('ws://localhost:8000')
    mockSocket.connected = false
  })

  describe('connection management', () => {
    it('should connect successfully', async () => {
      const connectPromise = wsService.connect()
      
      // Simulate successful connection
      mockSocket.connected = true
      const connectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect')?.[1]
      connectHandler?.()

      await expect(connectPromise).resolves.toBeUndefined()
      
      expect(mockSocket.on).toHaveBeenCalledWith('connect', expect.any(Function))
      expect(mockSocket.on).toHaveBeenCalledWith('disconnect', expect.any(Function))
      expect(mockSocket.on).toHaveBeenCalledWith('connect_error', expect.any(Function))
    })

    it('should handle connection errors', async () => {
      const error = new Error('Connection failed')
      const connectPromise = wsService.connect()
      
      // Simulate connection error
      const errorHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect_error')?.[1]
      errorHandler?.(error)

      await expect(connectPromise).rejects.toThrow('Connection failed')
    })

    it('should disconnect properly', () => {
      mockSocket.connected = true
      wsService.disconnect()

      expect(mockSocket.disconnect).toHaveBeenCalled()
      expect(wsService.isConnected()).toBe(false)
    })

    it('should return connection status', () => {
      mockSocket.connected = false
      expect(wsService.isConnected()).toBe(false)

      mockSocket.connected = true
      expect(wsService.isConnected()).toBe(true)
    })
  })

  describe('message handling', () => {
    beforeEach(() => {
      mockSocket.connected = true
    })

    it('should send message when connected', () => {
      wsService.send('test_event', { data: 'test' })

      expect(mockSocket.emit).toHaveBeenCalledWith('test_event', { data: 'test' })
    })

    it('should not send message when disconnected', () => {
      const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
      mockSocket.connected = false
      
      wsService.send('test_event', { data: 'test' })

      expect(mockSocket.emit).not.toHaveBeenCalled()
      expect(consoleSpy).toHaveBeenCalledWith('WebSocket not connected, unable to send message')
      
      consoleSpy.mockRestore()
    })

    it('should handle task update messages', async () => {
      await wsService.connect()
      
      // Simulate connection
      const connectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect')?.[1]
      connectHandler?.()

      const taskUpdateMessage: TaskUpdateMessage = {
        type: 'task_update',
        data: {
          task_id: 'task-1',
          status: 'completed',
          progress: 100
        },
        timestamp: '2024-01-01T00:00:00Z'
      }

      // Find and call the task_update handler
      const taskUpdateHandler = mockSocket.on.mock.calls.find(call => call[0] === 'task_update')?.[1]
      const callback = vi.fn()
      wsService.on('task_update', callback)
      
      taskUpdateHandler?.(taskUpdateMessage)

      expect(callback).toHaveBeenCalledWith(taskUpdateMessage)
    })

    it('should handle GPU metrics messages', async () => {
      await wsService.connect()
      
      const gpuMetricsMessage: GPUMetricsMessage = {
        type: 'gpu_metrics',
        data: {
          timestamp: '2024-01-01T00:00:00Z',
          gpus: [],
          total_usage: 50,
          total_memory_usage: 60
        },
        timestamp: '2024-01-01T00:00:00Z'
      }

      const gpuMetricsHandler = mockSocket.on.mock.calls.find(call => call[0] === 'gpu_metrics')?.[1]
      const callback = vi.fn()
      wsService.on('gpu_metrics', callback)
      
      gpuMetricsHandler?.(gpuMetricsMessage)

      expect(callback).toHaveBeenCalledWith(gpuMetricsMessage)
    })

    it('should handle system alert messages', async () => {
      await wsService.connect()
      
      const systemAlertData = { message: 'High temperature alert', severity: 'warning' }
      const systemAlertHandler = mockSocket.on.mock.calls.find(call => call[0] === 'system_alert')?.[1]
      const callback = vi.fn()
      wsService.on('system_alert', callback)
      
      systemAlertHandler?.(systemAlertData)

      expect(callback).toHaveBeenCalledWith(systemAlertData)
    })

    it('should handle generic message types', async () => {
      await wsService.connect()
      
      const genericMessage: WebSocketMessage = {
        type: 'task_update',
        data: { test: 'data' },
        timestamp: '2024-01-01T00:00:00Z'
      }

      const messageHandler = mockSocket.on.mock.calls.find(call => call[0] === 'message')?.[1]
      const callback = vi.fn()
      wsService.on('task_update', callback)
      
      messageHandler?.(genericMessage)

      expect(callback).toHaveBeenCalledWith(genericMessage)
    })
  })

  describe('subscription methods', () => {
    beforeEach(() => {
      mockSocket.connected = true
    })

    it('should subscribe to task updates', () => {
      wsService.subscribeToTask('task-1')

      expect(mockSocket.emit).toHaveBeenCalledWith('subscribe_task', { task_id: 'task-1' })
    })

    it('should unsubscribe from task updates', () => {
      wsService.unsubscribeFromTask('task-1')

      expect(mockSocket.emit).toHaveBeenCalledWith('unsubscribe_task', { task_id: 'task-1' })
    })

    it('should subscribe to GPU metrics', () => {
      wsService.subscribeToGpuMetrics()

      expect(mockSocket.emit).toHaveBeenCalledWith('subscribe_gpu_metrics', {})
    })

    it('should unsubscribe from GPU metrics', () => {
      wsService.unsubscribeFromGpuMetrics()

      expect(mockSocket.emit).toHaveBeenCalledWith('unsubscribe_gpu_metrics', {})
    })
  })

  describe('event listener management', () => {
    it('should add event listeners', () => {
      const callback1 = vi.fn()
      const callback2 = vi.fn()

      wsService.on('test_event', callback1)
      wsService.on('test_event', callback2)

      // Manually trigger the event to test listeners
      wsService['emit']('test_event', 'test_data')

      expect(callback1).toHaveBeenCalledWith('test_data')
      expect(callback2).toHaveBeenCalledWith('test_data')
    })

    it('should remove specific event listener', () => {
      const callback1 = vi.fn()
      const callback2 = vi.fn()

      wsService.on('test_event', callback1)
      wsService.on('test_event', callback2)

      wsService.off('test_event', callback1)
      wsService['emit']('test_event', 'test_data')

      expect(callback1).not.toHaveBeenCalled()
      expect(callback2).toHaveBeenCalledWith('test_data')
    })

    it('should remove all listeners for an event', () => {
      const callback1 = vi.fn()
      const callback2 = vi.fn()

      wsService.on('test_event', callback1)
      wsService.on('test_event', callback2)

      wsService.off('test_event')
      wsService['emit']('test_event', 'test_data')

      expect(callback1).not.toHaveBeenCalled()
      expect(callback2).not.toHaveBeenCalled()
    })

    it('should handle errors in event callbacks gracefully', () => {
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const errorCallback = vi.fn(() => { throw new Error('Callback error') })
      const normalCallback = vi.fn()

      wsService.on('test_event', errorCallback)
      wsService.on('test_event', normalCallback)

      wsService['emit']('test_event', 'test_data')

      expect(errorCallback).toHaveBeenCalled()
      expect(normalCallback).toHaveBeenCalled()
      expect(consoleSpy).toHaveBeenCalledWith('Error in test_event callback:', expect.any(Error))

      consoleSpy.mockRestore()
    })
  })

  describe('connection info', () => {
    it('should return null when not connected', () => {
      const info = wsService.getConnectionInfo()
      expect(info).toBeNull()
    })

    it('should return connection info when connected', async () => {
      await wsService.connect()
      
      mockSocket.connected = true
      mockSocket.id = 'test-socket-id'

      const info = wsService.getConnectionInfo()
      
      expect(info).toEqual({
        connected: true,
        id: 'test-socket-id',
        transport: 'websocket',
        reconnectAttempts: 0
      })
    })
  })

  describe('reconnection handling', () => {
    it('should handle reconnection events', async () => {
      const connectPromise = wsService.connect()
      
      // Simulate initial connection
      mockSocket.connected = true
      const connectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect')?.[1]
      connectHandler?.()

      await connectPromise

      // Test reconnect event
      const reconnectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'reconnect')?.[1]
      const callback = vi.fn()
      wsService.on('reconnected', callback)
      
      reconnectHandler?.(3)

      expect(callback).toHaveBeenCalledWith(3)
    })

    it('should handle reconnection errors', async () => {
      const connectPromise = wsService.connect()
      
      mockSocket.connected = true
      const connectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect')?.[1]
      connectHandler?.()

      await connectPromise

      const reconnectErrorHandler = mockSocket.on.mock.calls.find(call => call[0] === 'reconnect_error')?.[1]
      const callback = vi.fn()
      wsService.on('reconnect_error', callback)
      
      const error = new Error('Reconnect failed')
      reconnectErrorHandler?.(error)

      expect(callback).toHaveBeenCalledWith(error)
    })

    it('should handle reconnection failure', async () => {
      const connectPromise = wsService.connect()
      
      mockSocket.connected = true
      const connectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect')?.[1]
      connectHandler?.()

      await connectPromise

      const reconnectFailedHandler = mockSocket.on.mock.calls.find(call => call[0] === 'reconnect_failed')?.[1]
      const callback = vi.fn()
      wsService.on('reconnect_failed', callback)
      
      reconnectFailedHandler?.()

      expect(callback).toHaveBeenCalled()
    })

    it('should handle disconnect events', async () => {
      const connectPromise = wsService.connect()
      
      mockSocket.connected = true
      const connectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'connect')?.[1]
      connectHandler?.()

      await connectPromise

      const disconnectHandler = mockSocket.on.mock.calls.find(call => call[0] === 'disconnect')?.[1]
      const callback = vi.fn()
      wsService.on('disconnected', callback)
      
      disconnectHandler?.('transport close')

      expect(callback).toHaveBeenCalledWith('transport close')
    })
  })

  describe('unknown message handling', () => {
    it('should log warning for unknown message types', async () => {
      const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
      
      await wsService.connect()
      
      const unknownMessage: WebSocketMessage = {
        type: 'unknown_type' as any,
        data: {},
        timestamp: '2024-01-01T00:00:00Z'
      }

      const messageHandler = mockSocket.on.mock.calls.find(call => call[0] === 'message')?.[1]
      messageHandler?.(unknownMessage)

      expect(consoleSpy).toHaveBeenCalledWith('Unknown message type:', 'unknown_type')
      
      consoleSpy.mockRestore()
    })
  })
})
