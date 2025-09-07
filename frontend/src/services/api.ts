import axios, { type AxiosInstance, type AxiosResponse } from 'axios'
import type { 
  Task, 
  TaskSubmitData, 
  GPUMetrics, 
  InstanceRecommendation,
  CostAnalysis,
  ApiResponse, 
  PaginatedResponse,
  User,
  LoginForm,
  RegisterForm,
  UserProfile,
  ChangePasswordForm,
  TaskWithUser,
  TaskCreateForm,
  Provider,
  GPUModel
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
    // 添加认证 token
    const token = localStorage.getItem('auth_token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
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
      // 处理未授权错误，清除 token 并跳转到登录页面
      localStorage.removeItem('auth_token')
      localStorage.removeItem('user_info')
      window.location.href = '/login'
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

// 用户认证相关 API
export const authApi = {
  // 用户登录
  login: async (loginData: LoginForm): Promise<AxiosResponse<ApiResponse<User>>> => {
    return apiClient.post('/auth/login', loginData)
  },

  // 用户注册
  register: async (registerData: Omit<RegisterForm, 'confirmPassword' | 'agreement'>): Promise<AxiosResponse<ApiResponse<User>>> => {
    return apiClient.post('/auth/register', registerData)
  },

  // 用户登出
  logout: async (): Promise<AxiosResponse<ApiResponse<void>>> => {
    return apiClient.post('/auth/logout')
  },

  // 获取当前用户信息
  getCurrentUser: async (): Promise<AxiosResponse<ApiResponse<User>>> => {
    return apiClient.get('/auth/user')
  },

  // 更新用户资料
  updateProfile: async (profileData: UserProfile): Promise<AxiosResponse<ApiResponse<User>>> => {
    return apiClient.put('/auth/profile', profileData)
  },

  // 修改密码
  changePassword: async (passwordData: ChangePasswordForm): Promise<AxiosResponse<ApiResponse<void>>> => {
    return apiClient.put('/auth/password', passwordData)
  },

  // 刷新 token
  refreshToken: async (): Promise<AxiosResponse<ApiResponse<{ token: string }>>> => {
    return apiClient.post('/auth/refresh')
  }
}

// 更新后的任务相关 API
export const enhancedTaskApi = {
  // 获取任务列表（包含用户信息）
  getTasks: async (params: { 
    page?: number
    per_page?: number
    status?: string
    provider?: string
    search?: string
    user_id?: string
    sort_by?: 'created_at' | 'cost' | 'status'
    sort_order?: 'asc' | 'desc'
  }): Promise<AxiosResponse<PaginatedResponse<TaskWithUser>>> => {
    return apiClient.get('/tasks', { params })
  },

  // 获取单个任务详情
  getTask: async (taskId: string): Promise<AxiosResponse<TaskWithUser>> => {
    return apiClient.get(`/tasks/${taskId}`)
  },

  // 提交新任务
  createTask: async (taskData: TaskCreateForm): Promise<AxiosResponse<TaskWithUser>> => {
    return apiClient.post('/tasks', taskData)
  },

  // 取消任务
  cancelTask: async (taskId: string): Promise<AxiosResponse<void>> => {
    return apiClient.post(`/tasks/${taskId}/cancel`)
  },

  // 删除任务
  deleteTask: async (taskId: string): Promise<AxiosResponse<void>> => {
    return apiClient.delete(`/tasks/${taskId}`)
  },

  // 获取任务日志
  getTaskLogs: async (taskId: string): Promise<AxiosResponse<any>> => {
    return apiClient.get(`/tasks/${taskId}/logs`)
  },

  // 重启任务
  restartTask: async (taskId: string): Promise<AxiosResponse<TaskWithUser>> => {
    return apiClient.post(`/tasks/${taskId}/restart`)
  },

  // 获取任务统计
  getTaskStats: async (userId?: string): Promise<AxiosResponse<any>> => {
    const params = userId ? { user_id: userId } : {}
    return apiClient.get('/tasks/stats', { params })
  }
}

// Provider 和 GPU 相关 API
export const providerApi = {
  // 获取所有 Provider
  getProviders: async (): Promise<AxiosResponse<Provider[]>> => {
    return apiClient.get('/providers')
  },

  // 获取 GPU 型号列表
  getGPUModels: async (provider?: string): Promise<AxiosResponse<GPUModel[]>> => {
    const params = provider ? { provider } : {}
    return apiClient.get('/gpu/models', { params })
  },

  // 获取可用的 Docker 镜像
  getImages: async (): Promise<AxiosResponse<string[]>> => {
    return apiClient.get('/images')
  }
}

export default apiClient
