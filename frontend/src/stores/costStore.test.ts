import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useCostStore } from './costStore'
import { costApi } from '@/services/api'
import type { InstanceRecommendation, CostAnalysis } from '@/types'

// Mock the API
vi.mock('@/services/api', () => ({
  costApi: {
    getRecommendations: vi.fn(),
    getCostAnalysis: vi.fn(),
  }
}))

const mockRecommendation1: InstanceRecommendation = {
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

const mockRecommendation2: InstanceRecommendation = {
  id: 'instance-2',
  name: 'High Performance Instance',
  gpu_type: 'RTX 4090',
  gpu_count: 2,
  vcpus: 16,
  memory_gb: 64,
  storage_gb: 1000,
  cost_per_hour: 4.0,
  estimated_runtime_hours: 2,
  total_cost: 8,
  performance_score: 95,
  availability: 'medium'
}

const mockCostAnalysis: CostAnalysis = {
  current_cost: 100,
  optimized_cost: 75,
  savings: 25,
  recommendations: [mockRecommendation1]
}

describe('CostStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('initial state', () => {
    it('should have correct initial state', () => {
      const costStore = useCostStore()
      
      expect(costStore.recommendations).toEqual([])
      expect(costStore.costAnalysis).toBeNull()
      expect(costStore.loading).toBe(false)
      expect(costStore.error).toBeNull()
    })
  })

  describe('computed properties', () => {
    beforeEach(() => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1, mockRecommendation2]
    })

    it('should find best recommendation by performance score', () => {
      const costStore = useCostStore()
      expect(costStore.bestRecommendation).toEqual(mockRecommendation2) // higher performance score
    })

    it('should find best recommendation by cost when performance is equal', () => {
      const costStore = useCostStore()
      const equalPerformanceRecommendations = [
        { ...mockRecommendation1, performance_score: 90, total_cost: 12 },
        { ...mockRecommendation2, performance_score: 90, total_cost: 8 }
      ]
      costStore.recommendations = equalPerformanceRecommendations
      
      expect(costStore.bestRecommendation?.id).toBe('instance-2') // lower cost
    })

    it('should find cheapest option', () => {
      const costStore = useCostStore()
      expect(costStore.cheapestOption).toEqual(mockRecommendation2) // total_cost: 8
    })

    it('should find fastest option', () => {
      const costStore = useCostStore()
      expect(costStore.fastestOption).toEqual(mockRecommendation2) // performance_score: 95
    })

    it('should calculate total potential savings', () => {
      const costStore = useCostStore()
      costStore.costAnalysis = mockCostAnalysis
      
      expect(costStore.totalPotentialSavings).toBe(25)
    })

    it('should calculate savings percentage', () => {
      const costStore = useCostStore()
      costStore.costAnalysis = mockCostAnalysis
      
      expect(costStore.savingsPercentage).toBe(25) // 25/100 * 100
    })

    it('should return 0 savings percentage when current cost is 0', () => {
      const costStore = useCostStore()
      costStore.costAnalysis = { ...mockCostAnalysis, current_cost: 0 }
      
      expect(costStore.savingsPercentage).toBe(0)
    })

    it('should group recommendations by availability', () => {
      const costStore = useCostStore()
      const lowAvailabilityRecommendation = { ...mockRecommendation1, availability: 'low' as const, id: 'instance-3' }
      costStore.recommendations = [mockRecommendation1, mockRecommendation2, lowAvailabilityRecommendation]
      
      const grouped = costStore.recommendationsByAvailability
      
      expect(grouped.high).toHaveLength(1)
      expect(grouped.medium).toHaveLength(1)
      expect(grouped.low).toHaveLength(1)
    })

    it('should get unique GPU type options', () => {
      const costStore = useCostStore()
      const differentGpuRecommendation = { ...mockRecommendation1, gpu_type: 'RTX 3080', id: 'instance-3' }
      costStore.recommendations = [mockRecommendation1, mockRecommendation2, differentGpuRecommendation]
      
      expect(costStore.gpuTypeOptions).toEqual(['RTX 4090', 'RTX 3080'])
    })

    it('should sort recommendations by value score', () => {
      const costStore = useCostStore()
      // recommendation1: (85/10) * 100 = 850
      // recommendation2: (95/8) * 100 = 1187.5
      
      expect(costStore.getRecommendationsByValue[0]).toEqual(mockRecommendation2)
      expect(costStore.getRecommendationsByValue[1]).toEqual(mockRecommendation1)
    })

    it('should return null/empty for computed properties when no data', () => {
      const costStore = useCostStore()
      costStore.recommendations = []
      
      expect(costStore.bestRecommendation).toBeNull()
      expect(costStore.cheapestOption).toBeNull()
      expect(costStore.fastestOption).toBeNull()
      expect(costStore.totalPotentialSavings).toBe(0)
    })
  })

  describe('fetchRecommendations', () => {
    it('should fetch recommendations successfully', async () => {
      const mockResponse = { data: [mockRecommendation1, mockRecommendation2] }
      vi.mocked(costApi.getRecommendations).mockResolvedValue(mockResponse)

      const costStore = useCostStore()
      const params = { task_type: 'training', budget_limit: 50 }
      
      await costStore.fetchRecommendations(params)

      expect(costApi.getRecommendations).toHaveBeenCalledWith(params)
      expect(costStore.recommendations).toEqual([mockRecommendation1, mockRecommendation2])
      expect(costStore.loading).toBe(false)
      expect(costStore.error).toBeNull()
    })

    it('should handle fetch recommendations error', async () => {
      const errorMessage = 'Failed to fetch recommendations'
      vi.mocked(costApi.getRecommendations).mockRejectedValue(new Error(errorMessage))

      const costStore = useCostStore()
      await costStore.fetchRecommendations()

      expect(costStore.error).toBe(errorMessage)
      expect(costStore.loading).toBe(false)
      expect(costStore.recommendations).toEqual([])
    })
  })

  describe('fetchCostAnalysis', () => {
    it('should fetch cost analysis successfully', async () => {
      const mockResponse = { data: mockCostAnalysis }
      vi.mocked(costApi.getCostAnalysis).mockResolvedValue(mockResponse)

      const costStore = useCostStore()
      await costStore.fetchCostAnalysis('task-1')

      expect(costApi.getCostAnalysis).toHaveBeenCalledWith('task-1')
      expect(costStore.costAnalysis).toEqual(mockCostAnalysis)
      expect(costStore.loading).toBe(false)
      expect(costStore.error).toBeNull()
    })

    it('should handle fetch cost analysis error', async () => {
      const errorMessage = 'Failed to fetch cost analysis'
      vi.mocked(costApi.getCostAnalysis).mockRejectedValue(new Error(errorMessage))

      const costStore = useCostStore()
      await costStore.fetchCostAnalysis()

      expect(costStore.error).toBe(errorMessage)
      expect(costStore.loading).toBe(false)
    })
  })

  describe('calculateEstimatedCost', () => {
    it('should calculate estimated cost correctly', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1] // cost_per_hour: 2.5
      
      const estimatedCost = costStore.calculateEstimatedCost('instance-1', 6)
      
      expect(estimatedCost).toBe(15) // 2.5 * 6
    })

    it('should return 0 for non-existent instance', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      
      const estimatedCost = costStore.calculateEstimatedCost('non-existent', 6)
      
      expect(estimatedCost).toBe(0)
    })
  })

  describe('compareInstances', () => {
    it('should compare instances correctly', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1, mockRecommendation2]
      
      const comparison = costStore.compareInstances(['instance-1', 'instance-2'])
      
      expect(comparison).toEqual({
        instances: [mockRecommendation1, mockRecommendation2],
        cheapest: mockRecommendation2, // total_cost: 8
        fastest: mockRecommendation2, // performance_score: 95
        avgCost: 9, // (10 + 8) / 2
        avgPerformance: 90 // (85 + 95) / 2
      })
    })

    it('should return null for empty instance list', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      
      const comparison = costStore.compareInstances([])
      
      expect(comparison).toBeNull()
    })

    it('should return null for non-existent instances', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      
      const comparison = costStore.compareInstances(['non-existent'])
      
      expect(comparison).toBeNull()
    })
  })

  describe('filterRecommendations', () => {
    beforeEach(() => {
      const costStore = useCostStore()
      costStore.recommendations = [
        mockRecommendation1, // cost: 10, performance: 85, gpu: RTX 4090, availability: high
        mockRecommendation2, // cost: 8, performance: 95, gpu: RTX 4090, availability: medium
        { ...mockRecommendation1, id: 'instance-3', total_cost: 15, performance_score: 75, gpu_type: 'RTX 3080', availability: 'low' }
      ]
    })

    it('should filter by max cost', () => {
      const costStore = useCostStore()
      const filtered = costStore.filterRecommendations({ maxCost: 10 })
      
      expect(filtered).toHaveLength(2)
      expect(filtered.every(r => r.total_cost <= 10)).toBe(true)
    })

    it('should filter by min performance', () => {
      const costStore = useCostStore()
      const filtered = costStore.filterRecommendations({ minPerformance: 85 })
      
      expect(filtered).toHaveLength(2)
      expect(filtered.every(r => r.performance_score >= 85)).toBe(true)
    })

    it('should filter by GPU types', () => {
      const costStore = useCostStore()
      const filtered = costStore.filterRecommendations({ gpuTypes: ['RTX 3080'] })
      
      expect(filtered).toHaveLength(1)
      expect(filtered[0].gpu_type).toBe('RTX 3080')
    })

    it('should filter by availability', () => {
      const costStore = useCostStore()
      const filtered = costStore.filterRecommendations({ availability: ['high', 'medium'] })
      
      expect(filtered).toHaveLength(2)
      expect(filtered.every(r => ['high', 'medium'].includes(r.availability))).toBe(true)
    })

    it('should filter by multiple criteria', () => {
      const costStore = useCostStore()
      const filtered = costStore.filterRecommendations({ 
        maxCost: 12, 
        minPerformance: 80,
        gpuTypes: ['RTX 4090'],
        availability: ['high', 'medium']
      })
      
      expect(filtered).toHaveLength(2) // Both RTX 4090 instances meet all criteria
    })
  })

  describe('getRecommendationById', () => {
    it('should get recommendation by ID', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      
      const recommendation = costStore.getRecommendationById('instance-1')
      
      expect(recommendation).toEqual(mockRecommendation1)
    })

    it('should return undefined for non-existent ID', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      
      const recommendation = costStore.getRecommendationById('non-existent')
      
      expect(recommendation).toBeUndefined()
    })
  })

  describe('updateRecommendation', () => {
    it('should update existing recommendation', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      
      costStore.updateRecommendation('instance-1', { total_cost: 15 })
      
      expect(costStore.recommendations[0].total_cost).toBe(15)
    })

    it('should not affect other properties when updating', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      const originalName = mockRecommendation1.name
      
      costStore.updateRecommendation('instance-1', { total_cost: 15 })
      
      expect(costStore.recommendations[0].name).toBe(originalName)
    })
  })

  describe('getValueScore', () => {
    it('should calculate value score correctly', () => {
      const costStore = useCostStore()
      
      const score = costStore.getValueScore(mockRecommendation1)
      
      expect(score).toBe(850) // (85 / 10) * 100
    })

    it('should return 0 when cost is 0', () => {
      const costStore = useCostStore()
      const zeroCostrecommendation = { ...mockRecommendation1, total_cost: 0 }
      
      const score = costStore.getValueScore(zeroCostrecommendation)
      
      expect(score).toBe(0)
    })
  })

  describe('clearError and reset', () => {
    it('should clear error state', () => {
      const costStore = useCostStore()
      costStore.error = 'Some error'

      costStore.clearError()

      expect(costStore.error).toBeNull()
    })

    it('should reset all state', () => {
      const costStore = useCostStore()
      costStore.recommendations = [mockRecommendation1]
      costStore.costAnalysis = mockCostAnalysis
      costStore.error = 'Some error'

      costStore.reset()

      expect(costStore.recommendations).toEqual([])
      expect(costStore.costAnalysis).toBeNull()
      expect(costStore.error).toBeNull()
    })
  })
})
