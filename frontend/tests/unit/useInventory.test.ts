import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useInventory } from '@/management/hooks/useInventory'
import {
  listWorkloads,
  listComputePools,
} from '@/management/api'

vi.mock('@/management/api', () => ({
  listWorkloads: vi.fn(),
  listComputePools: vi.fn(),
}))

describe('useInventory', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should list workloads successfully', async () => {
    vi.mocked(listWorkloads).mockResolvedValue({
      status: 200,
      value: {
        items: [
          { id: 'wl-1', generation: 1, deleting: false },
          { id: 'wl-2', generation: 2, deleting: false },
        ],
        nextCursor: 'cursor-2',
        hasMore: true,
      },
    })

    const { result } = renderHook(() => useInventory())
    await act(async () => {
      await result.current.listWorkloads('cluster-a', 'proj-a', 'token-123')
    })

    expect(listWorkloads).toHaveBeenCalledWith('cluster-a', 'proj-a', 'token-123', undefined)
    expect(result.current.items).toHaveLength(2)
    expect(result.current.nextCursor).toBe('cursor-2')
    expect(result.current.hasMore).toBe(true)
    expect(result.current.loading).toBe(false)
  })

  it('should list compute pools successfully', async () => {
    vi.mocked(listComputePools).mockResolvedValue({
      status: 200,
      value: {
        items: [{ id: 'pool-1', generation: 1, deleting: false }],
        nextCursor: null,
        hasMore: false,
      },
    })

    const { result } = renderHook(() => useInventory())
    await act(async () => {
      await result.current.listComputePools('cluster-a', 'proj-a', 'token-123')
    })

    expect(listComputePools).toHaveBeenCalledWith('cluster-a', 'proj-a', 'token-123', undefined)
    expect(result.current.items).toHaveLength(1)
    expect(result.current.items[0].id).toBe('pool-1')
  })

  it('should handle listWorkloads error', async () => {
    vi.mocked(listWorkloads).mockRejectedValue(new Error('Project not found'))

    const { result } = renderHook(() => useInventory())
    await act(async () => {
      await result.current.listWorkloads('cluster-a', 'proj-a', 'token-123')
    })

    expect(result.current.items).toHaveLength(0)
    expect(result.current.error).toBe('Project not found')
    expect(result.current.loading).toBe(false)
  })
})
