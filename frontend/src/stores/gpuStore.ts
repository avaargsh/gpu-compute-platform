import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { GPUInfo, GPUMetrics } from '@/types'
import { gpuApi } from '@/services/api'

export const useGpuStore = defineStore('gpu', () => {
  // 状态
  const gpus = ref<GPUInfo[]>([])
  const metrics = ref<GPUMetrics | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const isConnected = ref(false)

  // 历史数据存储 (最近 100 个数据点)
  const maxHistoryLength = 100

  // 计算属性
  const totalGpuUsage = computed(() => {
    if (!gpus.value.length) return 0
    const totalUsage = gpus.value.reduce((sum, gpu) => sum + gpu.usage, 0)
    return totalUsage / gpus.value.length
  })

  const totalMemoryUsage = computed(() => {
    if (!gpus.value.length) return 0
    const totalUsed = gpus.value.reduce((sum, gpu) => sum + gpu.memory_usage, 0)
    const totalCapacity = gpus.value.reduce((sum, gpu) => sum + gpu.memory_total, 0)
    return totalCapacity > 0 ? (totalUsed / totalCapacity) * 100 : 0
  })

  const averageTemperature = computed(() => {
    if (!gpus.value.length) return 0
    const totalTemp = gpus.value.reduce((sum, gpu) => sum + gpu.temperature, 0)
    return totalTemp / gpus.value.length
  })

  const totalPowerUsage = computed(() => {
    return gpus.value.reduce((sum, gpu) => sum + gpu.power_usage, 0)
  })

  const availableGpus = computed(() => {
    return gpus.value.filter(gpu => gpu.usage < 80) // 使用率低于80%的GPU视为可用
  })

  const criticalGpus = computed(() => {
    return gpus.value.filter(gpu => gpu.temperature > 80 || gpu.usage > 95)
  })

  // 方法
  const fetchGpuMetrics = async () => {
    loading.value = true
    error.value = null
    try {
      const response = await gpuApi.getMetrics()
      updateMetrics(response.data)
      isConnected.value = true
    } catch (err) {
      error.value = err instanceof Error ? err.message : '获取GPU指标失败'
      console.error('Error fetching GPU metrics:', err)
      isConnected.value = false
    } finally {
      loading.value = false
    }
  }

  const updateMetrics = (newMetrics: GPUMetrics) => {
    metrics.value = newMetrics
    
    // 更新GPU信息，保持历史数据
    newMetrics.gpus.forEach((newGpu) => {
      const existingGpu = gpus.value.find(gpu => gpu.id === newGpu.id)
      
      if (existingGpu) {
        // 更新现有GPU，维护历史数据
        existingGpu.usage = newGpu.usage
        existingGpu.memory_usage = newGpu.memory_usage
        existingGpu.memory_total = newGpu.memory_total
        existingGpu.temperature = newGpu.temperature
        existingGpu.power_usage = newGpu.power_usage
        
        // 添加到历史数据
        existingGpu.utilization_history.push(newGpu.usage)
        existingGpu.memory_history.push((newGpu.memory_usage / newGpu.memory_total) * 100)
        
        // 限制历史数据长度
        if (existingGpu.utilization_history.length > maxHistoryLength) {
          existingGpu.utilization_history = existingGpu.utilization_history.slice(-maxHistoryLength)
        }
        if (existingGpu.memory_history.length > maxHistoryLength) {
          existingGpu.memory_history = existingGpu.memory_history.slice(-maxHistoryLength)
        }
      } else {
        // 新的GPU
        const gpuWithHistory: GPUInfo = {
          ...newGpu,
          utilization_history: [newGpu.usage],
          memory_history: [(newGpu.memory_usage / newGpu.memory_total) * 100]
        }
        gpus.value.push(gpuWithHistory)
      }
    })
  }

  const getGpuById = (gpuId: string) => {
    return gpus.value.find(gpu => gpu.id === gpuId)
  }

  const getGpuUtilizationHistory = (gpuId: string) => {
    const gpu = getGpuById(gpuId)
    return gpu?.utilization_history || []
  }

  const getGpuMemoryHistory = (gpuId: string) => {
    const gpu = getGpuById(gpuId)
    return gpu?.memory_history || []
  }

  const clearHistoryData = () => {
    gpus.value.forEach(gpu => {
      gpu.utilization_history = []
      gpu.memory_history = []
    })
  }

  const clearError = () => {
    error.value = null
  }

  const setConnectionStatus = (status: boolean) => {
    isConnected.value = status
    if (!status) {
      error.value = 'WebSocket连接已断开'
    }
  }

  // 获取GPU健康状态
  const getGpuHealthStatus = (gpu: GPUInfo) => {
    if (gpu.temperature > 85) {
      return { status: 'critical', message: '温度过高' }
    }
    if (gpu.usage > 95) {
      return { status: 'warning', message: '使用率过高' }
    }
    if (gpu.temperature > 75) {
      return { status: 'warning', message: '温度偏高' }
    }
    return { status: 'healthy', message: '正常' }
  }

  return {
    // 状态
    gpus,
    metrics,
    loading,
    error,
    isConnected,
    
    // 计算属性
    totalGpuUsage,
    totalMemoryUsage,
    averageTemperature,
    totalPowerUsage,
    availableGpus,
    criticalGpus,
    
    // 方法
    fetchGpuMetrics,
    updateMetrics,
    getGpuById,
    getGpuUtilizationHistory,
    getGpuMemoryHistory,
    clearHistoryData,
    clearError,
    setConnectionStatus,
    getGpuHealthStatus
  }
})
