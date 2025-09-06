import { io, Socket } from 'socket.io-client'
import type { 
  WebSocketMessage, 
  TaskUpdateMessage, 
  GPUMetricsMessage 
} from '@/types'

export class WebSocketService {
  private socket: Socket | null = null
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private reconnectDelay = 1000
  private listeners: Map<string, Function[]> = new Map()

  constructor(private url: string = 'ws://localhost:8000') {}

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      try {
        this.socket = io(this.url, {
          transports: ['websocket'],
          autoConnect: true,
          reconnection: true,
          reconnectionAttempts: this.maxReconnectAttempts,
          reconnectionDelay: this.reconnectDelay,
          timeout: 5000,
        })

        this.socket.on('connect', () => {
          console.log('WebSocket connected')
          this.reconnectAttempts = 0
          this.emit('connected')
          resolve()
        })

        this.socket.on('disconnect', (reason) => {
          console.log('WebSocket disconnected:', reason)
          this.emit('disconnected', reason)
        })

        this.socket.on('connect_error', (error) => {
          console.error('WebSocket connection error:', error)
          this.emit('error', error)
          if (this.reconnectAttempts === 0) {
            reject(error)
          }
        })

        this.socket.on('reconnect', (attemptNumber) => {
          console.log(`WebSocket reconnected after ${attemptNumber} attempts`)
          this.emit('reconnected', attemptNumber)
        })

        this.socket.on('reconnect_error', (error) => {
          this.reconnectAttempts++
          console.error(`WebSocket reconnection attempt ${this.reconnectAttempts} failed:`, error)
          this.emit('reconnect_error', error)
        })

        this.socket.on('reconnect_failed', () => {
          console.error('WebSocket failed to reconnect after maximum attempts')
          this.emit('reconnect_failed')
        })

        // 监听消息
        this.socket.on('message', (data: WebSocketMessage) => {
          this.handleMessage(data)
        })

        // 监听任务更新
        this.socket.on('task_update', (data: TaskUpdateMessage) => {
          this.emit('task_update', data)
        })

        // 监听 GPU 指标更新
        this.socket.on('gpu_metrics', (data: GPUMetricsMessage) => {
          this.emit('gpu_metrics', data)
        })

        // 监听系统警报
        this.socket.on('system_alert', (data: any) => {
          this.emit('system_alert', data)
        })

      } catch (error) {
        reject(error)
      }
    })
  }

  disconnect(): void {
    if (this.socket) {
      this.socket.disconnect()
      this.socket = null
    }
    this.listeners.clear()
  }

  isConnected(): boolean {
    return this.socket?.connected || false
  }

  // 发送消息
  send(event: string, data: any): void {
    if (this.socket?.connected) {
      this.socket.emit(event, data)
    } else {
      console.warn('WebSocket not connected, unable to send message')
    }
  }

  // 订阅任务更新
  subscribeToTask(taskId: string): void {
    this.send('subscribe_task', { task_id: taskId })
  }

  // 取消订阅任务更新
  unsubscribeFromTask(taskId: string): void {
    this.send('unsubscribe_task', { task_id: taskId })
  }

  // 订阅 GPU 指标
  subscribeToGpuMetrics(): void {
    this.send('subscribe_gpu_metrics', {})
  }

  // 取消订阅 GPU 指标
  unsubscribeFromGpuMetrics(): void {
    this.send('unsubscribe_gpu_metrics', {})
  }

  // 事件监听器管理
  on(event: string, callback: Function): void {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, [])
    }
    this.listeners.get(event)!.push(callback)
  }

  off(event: string, callback?: Function): void {
    if (!this.listeners.has(event)) return

    if (callback) {
      const callbacks = this.listeners.get(event)!
      const index = callbacks.indexOf(callback)
      if (index > -1) {
        callbacks.splice(index, 1)
      }
    } else {
      this.listeners.delete(event)
    }
  }

  // 触发事件
  private emit(event: string, data?: any): void {
    const callbacks = this.listeners.get(event) || []
    callbacks.forEach(callback => {
      try {
        callback(data)
      } catch (error) {
        console.error(`Error in ${event} callback:`, error)
      }
    })
  }

  // 处理接收到的消息
  private handleMessage(message: WebSocketMessage): void {
    console.log('Received WebSocket message:', message)
    
    switch (message.type) {
      case 'task_update':
        this.emit('task_update', message)
        break
      case 'gpu_metrics':
        this.emit('gpu_metrics', message)
        break
      case 'system_alert':
        this.emit('system_alert', message)
        break
      default:
        console.warn('Unknown message type:', message.type)
    }
  }

  // 获取连接状态信息
  getConnectionInfo() {
    if (!this.socket) return null

    return {
      connected: this.socket.connected,
      id: this.socket.id,
      transport: this.socket.io.engine.transport.name,
      reconnectAttempts: this.reconnectAttempts,
    }
  }
}

// 创建全局 WebSocket 服务实例
export const wsService = new WebSocketService()

// Vue 3 插件形式
export default {
  install(app: any) {
    app.config.globalProperties.$ws = wsService
    app.provide('websocket', wsService)
  }
}
