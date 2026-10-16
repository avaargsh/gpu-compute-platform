import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { StaleReceiptReadError, useReceipt } from '@/management/hooks/useReceipt'
import { readReceipt } from '@/management/api'

const sample = (id: string): Awaited<ReturnType<typeof readReceipt>> => ({
  status: 200,
  value: {
    requestId: id,
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
      await expect(result.current.readReceipt('cluster-a', 'proj-a', 'op-123', 'token-123'))
        .rejects.toThrow('Receipt not found')
    })

    expect(result.current.receipt).toBeNull()
    expect(result.current.error).toBe('Receipt not found')
  })

  it('clears the last receipt before a failed query for another operation', async () => {
    vi.mocked(readReceipt)
      .mockResolvedValueOnce(sample('op-old'))
      .mockRejectedValueOnce(new Error('Receipt not found'))
    const { result } = renderHook(() => useReceipt())
    await act(async () => {
      await result.current.readReceipt('cluster-a', 'proj-a', 'op-old', 'token-123')
    })
    expect(result.current.receipt?.requestId).toBe('op-old')

    await act(async () => {
      await expect(result.current.readReceipt('cluster-a', 'proj-a', 'op-new', 'token-123'))
        .rejects.toThrow('Receipt not found')
    })
    expect(result.current.receipt).toBeNull()
    expect(result.current.message).toBe('')
    expect(result.current.error).toBe('Receipt not found')
  })

  it('rejects a late response when an explicit scope invalidation has occurred', async () => {
    let resolvePending!: (value: Awaited<ReturnType<typeof readReceipt>>) => void
    vi.mocked(readReceipt).mockImplementationOnce(
      () => new Promise((resolve) => { resolvePending = resolve })
    )
    const { result } = renderHook(() => useReceipt())
    let pending!: ReturnType<typeof result.current.readReceipt>
    act(() => {
      pending = result.current.readReceipt('cluster-a', 'proj-a', 'op-old', 'token-123')
    })
    expect(result.current.loading).toBe(true)
    act(() => result.current.invalidate())
    expect(result.current.loading).toBe(false)

    await act(async () => {
      resolvePending(sample('op-old'))
      await expect(pending).rejects.toBeInstanceOf(StaleReceiptReadError)
    })
    expect(result.current.receipt).toBeNull()
    expect(result.current.error).toBe('')
  })

  it('does not let an older response replace a newer successful receipt', async () => {
    let resolveOld!: (value: Awaited<ReturnType<typeof readReceipt>>) => void
    vi.mocked(readReceipt)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve }))
      .mockResolvedValueOnce(sample('op-new'))
    const { result } = renderHook(() => useReceipt())
    let pending!: ReturnType<typeof result.current.readReceipt>
    act(() => {
      pending = result.current.readReceipt('cluster-a', 'proj-a', 'op-old', 'token-123')
    })
    await act(async () => {
      await result.current.readReceipt('cluster-a', 'proj-a', 'op-new', 'token-123')
    })
    expect(result.current.receipt?.requestId).toBe('op-new')

    await act(async () => {
      resolveOld(sample('op-old'))
      await expect(pending).rejects.toBeInstanceOf(StaleReceiptReadError)
    })
    expect(result.current.receipt?.requestId).toBe('op-new')
    expect(result.current.loading).toBe(false)
  })
})
