import { describe, it, expect, beforeEach, vi } from 'vitest'
import axios from 'axios'
import { taskApi, gpuApi, costApi, systemApi, fileApi } from './api'
import type { Task, TaskSubmitData, GPUMetrics, InstanceRecommendation, CostAnalysis } from '@/types'

// Mock axios
vi.mock('axios')
const mockedAxios = vi.mocked(axios, true)

const mockAxiosInstance = {
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
  interceptors: {
    request: { use: vi.fn() },
    response: { use: vi.fn() }
  }
}

mockedAxios.create.mockReturnValue(mockAxiosInstance as any)

describe('API Services', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('taskApi', () => {
    const mockTask: Task = {
      id: 'task-1',
      name: 'Test Task',
      status: 'running',
      progress: 50,
      gpu_usage: 75,
      memory_usage: 2048,
      created_at: '2024-01-01T00:00:00Z',
      logs: []
    }

    describe('getTasks', () => {
      it('should fetch tasks with parameters', async () => {
        const mockResponse = { data: { items: [mockTask], total: 1 } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const params = { page: 1, per_page: 10, status: 'running' }
        const result = await taskApi.getTasks(params)

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/tasks', { params })
        expect(result).toEqual(mockResponse)
      })

      it('should fetch tasks without parameters', async () => {
        const mockResponse = { data: { items: [], total: 0 } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        await taskApi.getTasks({})

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/tasks', { params: {} })
      })
    })

    describe('getTask', () => {
      it('should fetch single task by ID', async () => {
        const mockResponse = { data: mockTask }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await taskApi.getTask('task-1')

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/tasks/task-1')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('submitTask', () => {
      it('should submit new task', async () => {
        const taskData: TaskSubmitData = {
          name: 'New Task',
          script_path: '/path/to/script.py',
          requirements: ['torch', 'numpy']
        }
        const mockResponse = { data: mockTask }
        mockAxiosInstance.post.mockResolvedValue(mockResponse)

        const result = await taskApi.submitTask(taskData)

        expect(mockAxiosInstance.post).toHaveBeenCalledWith('/tasks', taskData)
        expect(result).toEqual(mockResponse)
      })
    })

    describe('cancelTask', () => {
      it('should cancel task', async () => {
        const mockResponse = { data: undefined }
        mockAxiosInstance.post.mockResolvedValue(mockResponse)

        const result = await taskApi.cancelTask('task-1')

        expect(mockAxiosInstance.post).toHaveBeenCalledWith('/tasks/task-1/cancel')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('getTaskLogs', () => {
      it('should fetch task logs', async () => {
        const mockResponse = { data: [{ message: 'log entry' }] }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await taskApi.getTaskLogs('task-1')

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/tasks/task-1/logs')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('restartTask', () => {
      it('should restart task', async () => {
        const mockResponse = { data: mockTask }
        mockAxiosInstance.post.mockResolvedValue(mockResponse)

        const result = await taskApi.restartTask('task-1')

        expect(mockAxiosInstance.post).toHaveBeenCalledWith('/tasks/task-1/restart')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('getTaskStats', () => {
      it('should fetch task statistics', async () => {
        const mockResponse = { data: { total: 10, running: 3, completed: 7 } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await taskApi.getTaskStats()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/tasks/stats')
        expect(result).toEqual(mockResponse)
      })
    })
  })

  describe('gpuApi', () => {
    const mockGPUMetrics: GPUMetrics = {
      timestamp: '2024-01-01T00:00:00Z',
      gpus: [{
        id: 'gpu-1',
        name: 'RTX 4090',
        usage: 75,
        memory_usage: 12000,
        memory_total: 24000,
        temperature: 65,
        power_usage: 350,
        utilization_history: [],
        memory_history: []
      }],
      total_usage: 75,
      total_memory_usage: 50
    }

    describe('getMetrics', () => {
      it('should fetch GPU metrics', async () => {
        const mockResponse = { data: mockGPUMetrics }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await gpuApi.getMetrics()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/gpu/metrics')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('getHistoryData', () => {
      it('should fetch GPU history data with parameters', async () => {
        const mockResponse = { data: [] }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const params = {
          gpu_id: 'gpu-1',
          start_time: '2024-01-01T00:00:00Z',
          end_time: '2024-01-01T01:00:00Z',
          interval: '1m'
        }

        const result = await gpuApi.getHistoryData(params)

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/gpu/history', { params })
        expect(result).toEqual(mockResponse)
      })
    })

    describe('getGpuInfo', () => {
      it('should fetch GPU information', async () => {
        const mockResponse = { data: { gpus: [] } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await gpuApi.getGpuInfo()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/gpu/info')
        expect(result).toEqual(mockResponse)
      })
    })
  })

  describe('costApi', () => {
    const mockRecommendation: InstanceRecommendation = {
      id: 'instance-1',
      name: 'Standard Instance',
      gpu_type: 'RTX 4090',
      gpu_count: 1,
      vcpus: 8,
      memory_gb: 32,
      storage_gb: 500,
      cost_per_hour: 2.5,
      estimated_runtime_hours: 4,
      total_cost: 10,
      performance_score: 85,
      availability: 'high'
    }

    const mockCostAnalysis: CostAnalysis = {
      current_cost: 100,
      optimized_cost: 75,
      savings: 25,
      recommendations: [mockRecommendation]
    }

    describe('getRecommendations', () => {
      it('should fetch recommendations with parameters', async () => {
        const mockResponse = { data: [mockRecommendation] }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const params = {
          task_type: 'training',
          budget_limit: 50,
          performance_priority: 'balanced' as const
        }

        const result = await costApi.getRecommendations(params)

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/cost/recommendations', { params })
        expect(result).toEqual(mockResponse)
      })

      it('should fetch recommendations without parameters', async () => {
        const mockResponse = { data: [] }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        await costApi.getRecommendations()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/cost/recommendations', { params: undefined })
      })
    })

    describe('getCostAnalysis', () => {
      it('should fetch cost analysis with task ID', async () => {
        const mockResponse = { data: mockCostAnalysis }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await costApi.getCostAnalysis('task-1')

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/cost/analysis/task-1')
        expect(result).toEqual(mockResponse)
      })

      it('should fetch cost analysis without task ID', async () => {
        const mockResponse = { data: mockCostAnalysis }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await costApi.getCostAnalysis()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/cost/analysis')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('getPricing', () => {
      it('should fetch pricing information', async () => {
        const mockResponse = { data: { instances: [] } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await costApi.getPricing()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/cost/pricing')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('compareInstances', () => {
      it('should compare instances', async () => {
        const mockResponse = { data: { comparison: [] } }
        mockAxiosInstance.post.mockResolvedValue(mockResponse)

        const instanceIds = ['instance-1', 'instance-2']
        const result = await costApi.compareInstances(instanceIds)

        expect(mockAxiosInstance.post).toHaveBeenCalledWith('/cost/compare', {
          instance_ids: instanceIds
        })
        expect(result).toEqual(mockResponse)
      })
    })
  })

  describe('systemApi', () => {
    describe('getStatus', () => {
      it('should fetch system status', async () => {
        const mockResponse = { data: { status: 'healthy' } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await systemApi.getStatus()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/system/status')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('getConfig', () => {
      it('should fetch system configuration', async () => {
        const mockResponse = { data: { config: {} } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await systemApi.getConfig()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/system/config')
        expect(result).toEqual(mockResponse)
      })
    })

    describe('healthCheck', () => {
      it('should perform health check', async () => {
        const mockResponse = { data: { healthy: true } }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await systemApi.healthCheck()

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/health')
        expect(result).toEqual(mockResponse)
      })
    })
  })

  describe('fileApi', () => {
    describe('uploadFile', () => {
      it('should upload file with progress callback', async () => {
        const mockResponse = { data: { file_id: 'file-1' } }
        mockAxiosInstance.post.mockResolvedValue(mockResponse)

        const file = new File(['content'], 'test.txt', { type: 'text/plain' })
        const onProgress = vi.fn()

        const result = await fileApi.uploadFile(file, onProgress)

        expect(mockAxiosInstance.post).toHaveBeenCalledWith(
          '/files/upload',
          expect.any(FormData),
          expect.objectContaining({
            headers: { 'Content-Type': 'multipart/form-data' },
            onUploadProgress: expect.any(Function)
          })
        )
        expect(result).toEqual(mockResponse)
      })

      it('should upload file without progress callback', async () => {
        const mockResponse = { data: { file_id: 'file-1' } }
        mockAxiosInstance.post.mockResolvedValue(mockResponse)

        const file = new File(['content'], 'test.txt', { type: 'text/plain' })

        const result = await fileApi.uploadFile(file)

        expect(mockAxiosInstance.post).toHaveBeenCalled()
        expect(result).toEqual(mockResponse)
      })
    })

    describe('downloadFile', () => {
      it('should download file', async () => {
        const mockBlob = new Blob(['file content'])
        const mockResponse = { data: mockBlob }
        mockAxiosInstance.get.mockResolvedValue(mockResponse)

        const result = await fileApi.downloadFile('file-1')

        expect(mockAxiosInstance.get).toHaveBeenCalledWith('/files/file-1', {
          responseType: 'blob'
        })
        expect(result).toEqual(mockResponse)
      })
    })
  })

  describe('Error Handling', () => {
    it('should handle network errors', async () => {
      const error = new Error('Network Error')
      mockAxiosInstance.get.mockRejectedValue(error)

      await expect(taskApi.getTasks({})).rejects.toThrow('Network Error')
    })

    it('should handle API errors', async () => {
      const apiError = {
        response: {
          status: 404,
          data: { error: 'Not Found' }
        }
      }
      mockAxiosInstance.get.mockRejectedValue(apiError)

      await expect(taskApi.getTask('non-existent')).rejects.toEqual(apiError)
    })
  })
})
