import axios, { type AxiosInstance, type AxiosResponse } from 'axios'
import type { 
  Task, 
  TaskSubmitData, 
  GPUMetrics, 
  InstanceRecommendation,
  CostAnalysis,
  ApiResponse, 
  PaginatedResponse 
} from '@/types'

// 创建 axios 实例
const apiClient: AxiosInstance = axios.create({
  baseURL: '/api',
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// 请求拦截器
apiClient.interceptors.request.use(
  (config) => {
    // 可以在这里添加认证 token
    // const token = localStorage.getItem('auth_token')
    // if (token) {
    //   config.headers.Authorization = `Bearer ${token}`
    // }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// 响应拦截器
apiClient.interceptors.response.use(
  (response: AxiosResponse<ApiResponse>) => {
    return response
  },
  (error) => {
    if (error.response?.status === 401) {
      // 处理未授权错误
      console.error('Unauthorized access')
      // 可以跳转到登录页面
    }
    return Promise.reject(error)
  }
)

// 任务相关 API
export const taskApi = {
  // 获取任务列表
  getTasks: async (params: { 
    page?: number
    per_page?: number
    status?: string
    search?: string
  }): Promise<AxiosResponse<PaginatedResponse<Task>>> => {
    return apiClient.get('/tasks', { params })
  },

  // 获取单个任务详情
  getTask: async (taskId: string): Promise<AxiosResponse<Task>> => {
    return apiClient.get(`/tasks/${taskId}`)
  },

  // 提交新任务
  submitTask: async (taskData: TaskSubmitData): Promise<AxiosResponse<Task>> => {
    return apiClient.post('/tasks', taskData)
  },

  // 取消任务
  cancelTask: async (taskId: string): Promise<AxiosResponse<void>> => {
    return apiClient.post(`/tasks/${taskId}/cancel`)
  },

  // 获取任务日志
  getTaskLogs: async (taskId: string): Promise<AxiosResponse<any>> => {
    return apiClient.get(`/tasks/${taskId}/logs`)
  },

  // 重启任务
  restartTask: async (taskId: string): Promise<AxiosResponse<Task>> => {
    return apiClient.post(`/tasks/${taskId}/restart`)
  },

  // 获取任务统计
  getTaskStats: async (): Promise<AxiosResponse<any>> => {
    return apiClient.get('/tasks/stats')
  }
}

// GPU 相关 API
export const gpuApi = {
  // 获取 GPU 指标
  getMetrics: async (): Promise<AxiosResponse<GPUMetrics>> => {
    return apiClient.get('/gpu/metrics')
  },

  // 获取 GPU 历史数据
  getHistoryData: async (params: {
    gpu_id?: string
    start_time?: string
    end_time?: string
    interval?: string
  }): Promise<AxiosResponse<any>> => {
    return apiClient.get('/gpu/history', { params })
  },

  // 获取 GPU 信息
  getGpuInfo: async (): Promise<AxiosResponse<any>> => {
    return apiClient.get('/gpu/info')
  }
}

// 成本优化相关 API
export const costApi = {
  // 获取实例推荐
  getRecommendations: async (params?: {
    task_type?: string
    budget_limit?: number
    duration_estimate?: number
    performance_priority?: 'cost' | 'performance' | 'balanced'
  }): Promise<AxiosResponse<InstanceRecommendation[]>> => {
    return apiClient.get('/cost/recommendations', { params })
  },

  // 获取成本分析
  getCostAnalysis: async (taskId?: string): Promise<AxiosResponse<CostAnalysis>> => {
    const url = taskId ? `/cost/analysis/${taskId}` : '/cost/analysis'
    return apiClient.get(url)
  },

  // 获取实例定价
  getPricing: async (): Promise<AxiosResponse<any>> => {
    return apiClient.get('/cost/pricing')
  },

  // 比较实例
  compareInstances: async (instanceIds: string[]): Promise<AxiosResponse<any>> => {
    return apiClient.post('/cost/compare', { instance_ids: instanceIds })
  }
}

// 系统相关 API
export const systemApi = {
  // 获取系统状态
  getStatus: async (): Promise<AxiosResponse<any>> => {
    return apiClient.get('/system/status')
  },

  // 获取系统配置
  getConfig: async (): Promise<AxiosResponse<any>> => {
    return apiClient.get('/system/config')
  },

  // 健康检查
  healthCheck: async (): Promise<AxiosResponse<any>> => {
    return apiClient.get('/health')
  }
}

// 文件上传相关 API
export const fileApi = {
  // 上传文件
  uploadFile: async (file: File, onProgress?: (progress: number) => void): Promise<AxiosResponse<any>> => {
    const formData = new FormData()
    formData.append('file', file)

    return apiClient.post('/files/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
      onUploadProgress: (progressEvent) => {
        if (onProgress && progressEvent.total) {
          const progress = (progressEvent.loaded / progressEvent.total) * 100
          onProgress(Math.round(progress))
        }
      }
    })
  },

  // 下载文件
  downloadFile: async (fileId: string): Promise<AxiosResponse<Blob>> => {
    return apiClient.get(`/files/${fileId}`, {
      responseType: 'blob'
    })
  }
}

export default apiClient
