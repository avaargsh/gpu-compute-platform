// 任务相关类型
export interface Task {
  id: string
  name: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  progress: number
  gpu_usage: number
  memory_usage: number
  created_at: string
  started_at?: string
  completed_at?: string
  error_message?: string
  logs: TaskLog[]
}

export interface TaskLog {
  timestamp: string
  level: 'info' | 'warning' | 'error'
  message: string
}

export interface TaskSubmitData {
  name: string
  script_path: string
  requirements: string[]
  gpu_type?: string
  max_duration?: number
}

// GPU 相关类型
export interface GPUInfo {
  id: string
  name: string
  usage: number
  memory_usage: number
  memory_total: number
  temperature: number
  power_usage: number
  utilization_history: number[]
  memory_history: number[]
}

export interface GPUMetrics {
  timestamp: string
  gpus: GPUInfo[]
  total_usage: number
  total_memory_usage: number
}

// 成本优化相关类型
export interface InstanceRecommendation {
  id: string
  name: string
  gpu_type: string
  gpu_count: number
  vcpus: number
  memory_gb: number
  storage_gb: number
  cost_per_hour: number
  estimated_runtime_hours: number
  total_cost: number
  performance_score: number
  availability: 'high' | 'medium' | 'low'
}

export interface CostAnalysis {
  current_cost: number
  optimized_cost: number
  savings: number
  recommendations: InstanceRecommendation[]
}

// WebSocket 相关类型
export interface WebSocketMessage {
  type: 'task_update' | 'gpu_metrics' | 'system_alert'
  data: any
  timestamp: string
}

export interface TaskUpdateMessage extends WebSocketMessage {
  type: 'task_update'
  data: {
    task_id: string
    status?: Task['status']
    progress?: number
    gpu_usage?: number
    memory_usage?: number
    logs?: TaskLog[]
  }
}

export interface GPUMetricsMessage extends WebSocketMessage {
  type: 'gpu_metrics'
  data: GPUMetrics
}

// API 响应类型
export interface ApiResponse<T = any> {
  success: boolean
  data?: T
  message?: string
  error?: string
}

export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  per_page: number
  pages: number
}
