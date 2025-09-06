import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useGpuStore } from './gpuStore'
import { gpuApi } from '@/services/api'
import type { GPUInfo, GPUMetrics } from '@/types'

// Mock the API
vi.mock('@/services/api', () => ({
  gpuApi: {
    getMetrics: vi.fn(),
  }
}))

const mockGPUInfo: GPUInfo = {
  id: 'gpu-1',
  name: 'NVIDIA RTX 4090',
  usage: 75,
  memory_usage: 12000,
  memory_total: 24000,
  temperature: 65,
  power_usage: 350,
  utilization_history: [70, 75],
  memory_history: [45, 50]
}

const mockGPUMetrics: GPUMetrics = {
  timestamp: '2024-01-01T00:00:00Z',
  gpus: [mockGPUInfo],
  total_usage: 75,
  total_memory_usage: 50
}

describe('GPUStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('initial state', () => {
    it('should have correct initial state', () => {
      const gpuStore = useGpuStore()
      
      expect(gpuStore.gpus).toEqual([])
      expect(gpuStore.metrics).toBeNull()
      expect(gpuStore.loading).toBe(false)
      expect(gpuStore.error).toBeNull()
      expect(gpuStore.isConnected).toBe(false)
    })
  })

  describe('computed properties', () => {
    beforeEach(() => {
      const gpuStore = useGpuStore()
      gpuStore.gpus = [
        { ...mockGPUInfo, id: 'gpu-1', usage: 80, memory_usage: 10000, memory_total: 20000, temperature: 70, power_usage: 300 },
        { ...mockGPUInfo, id: 'gpu-2', usage: 60, memory_usage: 8000, memory_total: 16000, temperature: 65, power_usage: 250 }
      ]
    })

    it('should calculate total GPU usage correctly', () => {
      const gpuStore = useGpuStore()
      expect(gpuStore.totalGpuUsage).toBe(70) // (80 + 60) / 2
    })

    it('should calculate total memory usage correctly', () => {
      const gpuStore = useGpuStore()
      // GPU 1: 10000/20000 = 50%, GPU 2: 8000/16000 = 50%
      // Total: (10000 + 8000) / (20000 + 16000) = 18000/36000 = 50%
      expect(gpuStore.totalMemoryUsage).toBe(50)
    })

    it('should calculate average temperature correctly', () => {
      const gpuStore = useGpuStore()
      expect(gpuStore.averageTemperature).toBe(67.5) // (70 + 65) / 2
    })

    it('should calculate total power usage correctly', () => {
      const gpuStore = useGpuStore()
      expect(gpuStore.totalPowerUsage).toBe(550) // 300 + 250
    })

    it('should filter available GPUs (usage < 80%)', () => {
      const gpuStore = useGpuStore()
      expect(gpuStore.availableGpus).toHaveLength(1)
      expect(gpuStore.availableGpus[0].id).toBe('gpu-2')
    })

    it('should filter critical GPUs (temp > 80 or usage > 95)', () => {
      const gpuStore = useGpuStore()
      // Add critical GPU
      gpuStore.gpus.push({ ...mockGPUInfo, id: 'gpu-3', temperature: 85, usage: 50 })
      gpuStore.gpus.push({ ...mockGPUInfo, id: 'gpu-4', temperature: 70, usage: 96 })
      
      expect(gpuStore.criticalGpus).toHaveLength(2)
      expect(gpuStore.criticalGpus.map(g => g.id)).toEqual(['gpu-3', 'gpu-4'])
    })

    it('should return 0 values when no GPUs', () => {
      const gpuStore = useGpuStore()
      gpuStore.gpus = []
      
      expect(gpuStore.totalGpuUsage).toBe(0)
      expect(gpuStore.totalMemoryUsage).toBe(0)
      expect(gpuStore.averageTemperature).toBe(0)
      expect(gpuStore.totalPowerUsage).toBe(0)
    })
  })

  describe('fetchGpuMetrics', () => {
    it('should fetch GPU metrics successfully', async () => {
      const mockResponse = { data: mockGPUMetrics }
      vi.mocked(gpuApi.getMetrics).mockResolvedValue(mockResponse)

      const gpuStore = useGpuStore()
      await gpuStore.fetchGpuMetrics()

      expect(gpuApi.getMetrics).toHaveBeenCalled()
      expect(gpuStore.metrics).toEqual(mockGPUMetrics)
      expect(gpuStore.gpus).toHaveLength(1)
      expect(gpuStore.isConnected).toBe(true)
      expect(gpuStore.loading).toBe(false)
      expect(gpuStore.error).toBeNull()
    })

    it('should handle fetch GPU metrics error', async () => {
      const errorMessage = 'Failed to fetch metrics'
      vi.mocked(gpuApi.getMetrics).mockRejectedValue(new Error(errorMessage))

      const gpuStore = useGpuStore()
      await gpuStore.fetchGpuMetrics()

      expect(gpuStore.error).toBe(errorMessage)
      expect(gpuStore.isConnected).toBe(false)
      expect(gpuStore.loading).toBe(false)
    })
  })

  describe('updateMetrics', () => {
    it('should update metrics and add new GPU', () => {
      const gpuStore = useGpuStore()
      gpuStore.updateMetrics(mockGPUMetrics)

      expect(gpuStore.metrics).toEqual(mockGPUMetrics)
      expect(gpuStore.gpus).toHaveLength(1)
      expect(gpuStore.gpus[0].utilization_history).toEqual([75])
      expect(gpuStore.gpus[0].memory_history).toEqual([50]) // 12000/24000 * 100
    })

    it('should update existing GPU and maintain history', () => {
      const gpuStore = useGpuStore()
      // Add existing GPU
      gpuStore.gpus = [{
        ...mockGPUInfo,
        usage: 70,
        utilization_history: [65, 70],
        memory_history: [40, 45]
      }]

      const updatedMetrics = {
        ...mockGPUMetrics,
        gpus: [{
          ...mockGPUInfo,
          usage: 80,
          memory_usage: 14400
        }]
      }

      gpuStore.updateMetrics(updatedMetrics)

      expect(gpuStore.gpus[0].usage).toBe(80)
      expect(gpuStore.gpus[0].utilization_history).toEqual([65, 70, 80])
      expect(gpuStore.gpus[0].memory_history).toEqual([40, 45, 60]) // 14400/24000 * 100
    })

    it('should limit history data length to maxHistoryLength', () => {
      const gpuStore = useGpuStore()
      
      // Create GPU with history at max length
      const longHistory = Array.from({ length: 100 }, (_, i) => i)
      gpuStore.gpus = [{
        ...mockGPUInfo,
        utilization_history: [...longHistory],
        memory_history: [...longHistory]
      }]

      const updatedMetrics = {
        ...mockGPUMetrics,
        gpus: [{
          ...mockGPUInfo,
          usage: 85
        }]
      }

      gpuStore.updateMetrics(updatedMetrics)

      expect(gpuStore.gpus[0].utilization_history).toHaveLength(100)
      expect(gpuStore.gpus[0].utilization_history[99]).toBe(85) // newest value
      expect(gpuStore.gpus[0].utilization_history[0]).toBe(1) // oldest value should be shifted
    })
  })

  describe('GPU helper methods', () => {
    beforeEach(() => {
      const gpuStore = useGpuStore()
      gpuStore.gpus = [mockGPUInfo]
    })

    it('should get GPU by ID', () => {
      const gpuStore = useGpuStore()
      const gpu = gpuStore.getGpuById('gpu-1')
      
      expect(gpu).toEqual(mockGPUInfo)
    })

    it('should return undefined for non-existent GPU ID', () => {
      const gpuStore = useGpuStore()
      const gpu = gpuStore.getGpuById('non-existent')
      
      expect(gpu).toBeUndefined()
    })

    it('should get GPU utilization history', () => {
      const gpuStore = useGpuStore()
      const history = gpuStore.getGpuUtilizationHistory('gpu-1')
      
      expect(history).toEqual([70, 75])
    })

    it('should get GPU memory history', () => {
      const gpuStore = useGpuStore()
      const history = gpuStore.getGpuMemoryHistory('gpu-1')
      
      expect(history).toEqual([45, 50])
    })

    it('should return empty array for non-existent GPU history', () => {
      const gpuStore = useGpuStore()
      const utilizationHistory = gpuStore.getGpuUtilizationHistory('non-existent')
      const memoryHistory = gpuStore.getGpuMemoryHistory('non-existent')
      
      expect(utilizationHistory).toEqual([])
      expect(memoryHistory).toEqual([])
    })
  })

  describe('clearHistoryData', () => {
    it('should clear all GPU history data', () => {
      const gpuStore = useGpuStore()
      gpuStore.gpus = [
        { ...mockGPUInfo, utilization_history: [1, 2, 3], memory_history: [4, 5, 6] }
      ]

      gpuStore.clearHistoryData()

      expect(gpuStore.gpus[0].utilization_history).toEqual([])
      expect(gpuStore.gpus[0].memory_history).toEqual([])
    })
  })

  describe('connection status', () => {
    it('should set connection status to true', () => {
      const gpuStore = useGpuStore()
      gpuStore.setConnectionStatus(true)
      
      expect(gpuStore.isConnected).toBe(true)
    })

    it('should set connection status to false and add error', () => {
      const gpuStore = useGpuStore()
      gpuStore.setConnectionStatus(false)
      
      expect(gpuStore.isConnected).toBe(false)
      expect(gpuStore.error).toBe('WebSocket连接已断开')
    })
  })

  describe('getGpuHealthStatus', () => {
    it('should return critical status for high temperature', () => {
      const gpuStore = useGpuStore()
      const gpu = { ...mockGPUInfo, temperature: 90, usage: 50 }
      
      const status = gpuStore.getGpuHealthStatus(gpu)
      
      expect(status).toEqual({ status: 'critical', message: '温度过高' })
    })

    it('should return critical status for very high usage', () => {
      const gpuStore = useGpuStore()
      const gpu = { ...mockGPUInfo, temperature: 70, usage: 96 }
      
      const status = gpuStore.getGpuHealthStatus(gpu)
      
      expect(status).toEqual({ status: 'critical', message: '使用率过高' })
    })

    it('should return warning status for moderately high temperature', () => {
      const gpuStore = useGpuStore()
      const gpu = { ...mockGPUInfo, temperature: 78, usage: 60 }
      
      const status = gpuStore.getGpuHealthStatus(gpu)
      
      expect(status).toEqual({ status: 'warning', message: '温度偏高' })
    })

    it('should return healthy status for normal conditions', () => {
      const gpuStore = useGpuStore()
      const gpu = { ...mockGPUInfo, temperature: 65, usage: 70 }
      
      const status = gpuStore.getGpuHealthStatus(gpu)
      
      expect(status).toEqual({ status: 'healthy', message: '正常' })
    })
  })

  describe('clearError', () => {
    it('should clear error state', () => {
      const gpuStore = useGpuStore()
      gpuStore.error = 'Some error'

      gpuStore.clearError()

      expect(gpuStore.error).toBeNull()
    })
  })
})
