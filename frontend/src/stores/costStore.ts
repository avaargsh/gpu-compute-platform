import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { InstanceRecommendation, CostAnalysis } from '@/types'
import { costApi } from '@/services/api'

export const useCostStore = defineStore('cost', () => {
  // 状态
  const recommendations = ref<InstanceRecommendation[]>([])
  const costAnalysis = ref<CostAnalysis | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  // 计算属性
  const bestRecommendation = computed(() => {
    if (!recommendations.value.length) return null
    return recommendations.value.reduce((best, current) => {
      if (current.performance_score > best.performance_score) return current
      if (current.performance_score === best.performance_score && current.total_cost < best.total_cost) {
        return current
      }
      return best
    })
  })

  const cheapestOption = computed(() => {
    if (!recommendations.value.length) return null
    return recommendations.value.reduce((cheapest, current) => 
      current.total_cost < cheapest.total_cost ? current : cheapest
    )
  })

  const fastestOption = computed(() => {
    if (!recommendations.value.length) return null
    return recommendations.value.reduce((fastest, current) => 
      current.performance_score > fastest.performance_score ? current : fastest
    )
  })

  const totalPotentialSavings = computed(() => {
    return costAnalysis.value?.savings || 0
  })

  const savingsPercentage = computed(() => {
    if (!costAnalysis.value || costAnalysis.value.current_cost === 0) return 0
    return (costAnalysis.value.savings / costAnalysis.value.current_cost) * 100
  })

  const recommendationsByAvailability = computed(() => {
    const grouped = {
      high: recommendations.value.filter(r => r.availability === 'high'),
      medium: recommendations.value.filter(r => r.availability === 'medium'),
      low: recommendations.value.filter(r => r.availability === 'low')
    }
    return grouped
  })

  const gpuTypeOptions = computed(() => {
    const types = new Set(recommendations.value.map(r => r.gpu_type))
    return Array.from(types)
  })

  // 方法
  const fetchRecommendations = async (params?: {
    task_type?: string
    budget_limit?: number
    duration_estimate?: number
    performance_priority?: 'cost' | 'performance' | 'balanced'
  }) => {
    loading.value = true
    error.value = null
    try {
      const response = await costApi.getRecommendations(params)
      recommendations.value = response.data
    } catch (err) {
      error.value = err instanceof Error ? err.message : '获取推荐实例失败'
      console.error('Error fetching recommendations:', err)
    } finally {
      loading.value = false
    }
  }

  const fetchCostAnalysis = async (taskId?: string) => {
    loading.value = true
    error.value = null
    try {
      const response = await costApi.getCostAnalysis(taskId)
      costAnalysis.value = response.data
    } catch (err) {
      error.value = err instanceof Error ? err.message : '获取成本分析失败'
      console.error('Error fetching cost analysis:', err)
    } finally {
      loading.value = false
    }
  }

  const calculateEstimatedCost = (
    instanceId: string,
    estimatedHours: number
  ): number => {
    const instance = recommendations.value.find(r => r.id === instanceId)
    if (!instance) return 0
    return instance.cost_per_hour * estimatedHours
  }

  const compareInstances = (instanceIds: string[]) => {
    const instances = recommendations.value.filter(r => 
      instanceIds.includes(r.id)
    )
    
    if (instances.length === 0) return null

    return {
      instances,
      cheapest: instances.reduce((min, current) => 
        current.total_cost < min.total_cost ? current : min
      ),
      fastest: instances.reduce((max, current) => 
        current.performance_score > max.performance_score ? current : max
      ),
      avgCost: instances.reduce((sum, instance) => sum + instance.total_cost, 0) / instances.length,
      avgPerformance: instances.reduce((sum, instance) => sum + instance.performance_score, 0) / instances.length
    }
  }

  const filterRecommendations = (filters: {
    maxCost?: number
    minPerformance?: number
    gpuTypes?: string[]
    availability?: ('high' | 'medium' | 'low')[]
  }) => {
    return recommendations.value.filter(recommendation => {
      if (filters.maxCost && recommendation.total_cost > filters.maxCost) {
        return false
      }
      if (filters.minPerformance && recommendation.performance_score < filters.minPerformance) {
        return false
      }
      if (filters.gpuTypes && !filters.gpuTypes.includes(recommendation.gpu_type)) {
        return false
      }
      if (filters.availability && !filters.availability.includes(recommendation.availability)) {
        return false
      }
      return true
    })
  }

  const getRecommendationById = (instanceId: string) => {
    return recommendations.value.find(r => r.id === instanceId)
  }

  const updateRecommendation = (instanceId: string, updates: Partial<InstanceRecommendation>) => {
    const index = recommendations.value.findIndex(r => r.id === instanceId)
    if (index !== -1) {
      recommendations.value[index] = { ...recommendations.value[index], ...updates }
    }
  }

  const clearError = () => {
    error.value = null
  }

  const reset = () => {
    recommendations.value = []
    costAnalysis.value = null
    error.value = null
  }

  // 获取性价比评分
  const getValueScore = (recommendation: InstanceRecommendation) => {
    // 性价比 = 性能分数 / 成本 * 100
    return recommendation.total_cost > 0 
      ? (recommendation.performance_score / recommendation.total_cost) * 100 
      : 0
  }

  const getRecommendationsByValue = computed(() => {
    return [...recommendations.value]
      .sort((a, b) => getValueScore(b) - getValueScore(a))
  })

  return {
    // 状态
    recommendations,
    costAnalysis,
    loading,
    error,
    
    // 计算属性
    bestRecommendation,
    cheapestOption,
    fastestOption,
    totalPotentialSavings,
    savingsPercentage,
    recommendationsByAvailability,
    gpuTypeOptions,
    getRecommendationsByValue,
    
    // 方法
    fetchRecommendations,
    fetchCostAnalysis,
    calculateEstimatedCost,
    compareInstances,
    filterRecommendations,
    getRecommendationById,
    updateRecommendation,
    clearError,
    reset,
    getValueScore
  }
})
