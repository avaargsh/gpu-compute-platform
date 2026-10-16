import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useReceipt } from '@/management/hooks/useReceipt'
import { readReceipt } from '@/management/api'

vi.mock('@/management/api', () => ({
  readReceipt: vi.fn(),
}))

describe('useReceipt', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should read receipt successfully', async () => {
    vi.mocked(readReceipt).mockResolvedValue({
      status: 200,
      value: {
        requestId: 'op-123',
        status: 'desired-committed',
        clusterId: 'cluster-a',
        projectId: 'proj-a',
        kind: 'Workload',
        resourceId: 'wl-1',
        action: 'create',
        expectedGeneration: 0,
        resultingGeneration: 1,
        committedAt: '2024-01-01T00:00:00Z',
        requestDigest: 'abc123',
      },
    })

    const { result } = renderHook(() => useReceipt())
    await act(async () => {
      await result.current.readReceipt('cluster-a', 'proj-a', 'op-123', 'token-123')
    })

    expect(readReceipt).toHaveBeenCalledWith('cluster-a', 'proj-a', 'op-123', 'token-123')
    expect(result.current.receipt?.requestId).toBe('op-123')
    expect(result.current.receipt?.status).toBe('desired-committed')
    expect(result.current.loading).toBe(false)
  })

  it('should handle read receipt error', async () => {
    vi.mocked(readReceipt).mockRejectedValue(new Error('Receipt not found'))

    const { result } = renderHook(() => useReceipt())
    await act(async () => {
      await result.current.readReceipt('cluster-a', 'proj-a', 'op-123', 'token-123')
    })

    expect(result.current.receipt).toBeNull()
    expect(result.current.error).toBe('Receipt not found')
  })
})
