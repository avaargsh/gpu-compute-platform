import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useEvidence, StaleEvidenceReadError } from '@/management/hooks/useEvidence'
import {
  readReceipt,
  readResource,
  readExecution,
} from '@/management/api'
import {
  classifyDesiredReadback,
} from '@/management/readback'
import {
  classifyScopedExecution,
} from '@/management/execution'
import {
  verifyEvidencePair,
} from '@/management/evidence-check'

vi.mock('@/management/api', () => ({
  readReceipt: vi.fn(),
  readResource: vi.fn(),
  readExecution: vi.fn(),
}))
vi.mock('@/management/readback', () => ({
  classifyDesiredReadback: vi.fn(),
}))
vi.mock('@/management/execution', () => ({
  classifyScopedExecution: vi.fn(),
}))
vi.mock('@/management/evidence-check', () => ({
  verifyEvidencePair: vi.fn(),
}))

describe('useEvidence', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('should refresh evidence chain successfully', async () => {
    const mockReceipt = {
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
    }
    const mockReadback = {
      status: 'desired-visible' as const,
      detail: 'OK',
      desired: { generation: 1 },
    }
    const mockExecution = {
      code: 'READY' as const,
      label: '观察已过期/不可验证',
      detail: 'OK',
      projectId: 'proj-a',
      workloadId: 'wl-1',
      generation: 1,
      conditions: [],
      evidenceRefs: [],
      workspaceClaim: null,
      observedAt: '2024-01-01T00:00:00Z',
      newerThanReceipt: false,
    }

    vi.mocked(readReceipt).mockResolvedValue({ status: 200, value: mockReceipt })
    vi.mocked(readResource).mockResolvedValue({ status: 200, value: { generation: 1 } })
    vi.mocked(readExecution).mockResolvedValue({ status: 200, value: {} as any })
    vi.mocked(classifyDesiredReadback).mockReturnValue(mockReadback as any)
    vi.mocked(classifyScopedExecution).mockReturnValue(mockExecution as any)
    vi.mocked(verifyEvidencePair).mockReturnValue({
      readback: mockReadback as any,
      execution: mockExecution as any,
    } as any)

    const { result } = renderHook(() => useEvidence())
    await act(async () => {
      await result.current.refreshEvidenceChain(
        'cluster-a',
        'proj-a',
        'op-123',
        'token-123'
      )
    })

    expect(readReceipt).toHaveBeenCalledWith('cluster-a', 'proj-a', 'op-123', 'token-123')
    expect(verifyEvidencePair).toHaveBeenCalled()
    expect(result.current.loading).toBe(false)
  })

  it('should handle evidence chain error', async () => {
    vi.mocked(readReceipt).mockRejectedValue(new Error('Receipt not found'))

    const { result } = renderHook(() => useEvidence())
    await act(async () => {
      await expect(result.current.refreshEvidenceChain(
        'cluster-a',
        'proj-a',
        'op-123',
        'token-123'
      )).rejects.toThrow('Receipt not found')
    })

    expect(result.current.error).toBe('Receipt not found')
    expect(result.current.loading).toBe(false)
  })

  it('revoked scope discards an in-flight Desired read without resurrecting evidence', async () => {
    let resolveOld!: (value: Awaited<ReturnType<typeof readResource>>) => void
    vi.mocked(readResource).mockImplementationOnce(
      () => new Promise(resolve => { resolveOld = resolve })
    )
    const { result } = renderHook(() => useEvidence())
    let pending!: ReturnType<typeof result.current.readBackDesired>
    act(() => {
      pending = result.current.readBackDesired(
        {} as any, 'cluster-a', 'proj-a', 'op-old', 'token-123'
      )
    })
    expect(result.current.loading).toBe(true)
    act(() => result.current.invalidate())
    await act(async () => {
      resolveOld({ status: 200, value: null })
      await expect(pending).rejects.toBeInstanceOf(StaleEvidenceReadError)
    })
    expect(result.current.check).toBeNull()
    expect(result.current.message).toBe('')
    expect(result.current.error).toBe('')
    expect(result.current.loading).toBe(false)
  })

  it('after invalidation, an old multi-hop receipt cannot cause a further scoped request', async () => {
    let resolveReceipt!: (value: Awaited<ReturnType<typeof readReceipt>>) => void
    vi.mocked(readReceipt).mockImplementationOnce(
      () => new Promise(resolve => { resolveReceipt = resolve })
    )
    const { result } = renderHook(() => useEvidence())
    let pending!: ReturnType<typeof result.current.refreshEvidenceChain>
    act(() => {
      pending = result.current.refreshEvidenceChain(
        'cluster-a', 'proj-a', 'op-old', 'token-123'
      )
    })
    act(() => result.current.invalidate())
    await act(async () => {
      resolveReceipt({ status: 200, value: null })
      await expect(pending).rejects.toBeInstanceOf(StaleEvidenceReadError)
    })
    expect(readResource).not.toHaveBeenCalled()
    expect(readExecution).not.toHaveBeenCalled()
    expect(result.current.check).toBeNull()
  })

  it('an older completed request cannot overwrite a newer successful readback', async () => {
    const newReadback = { status: 'desired-visible' as const, detail: 'new', desired: { generation: 2 } }
    vi.mocked(classifyDesiredReadback).mockReturnValue(newReadback as any)
    let resolveOld!: (value: Awaited<ReturnType<typeof readResource>>) => void
    vi.mocked(readResource)
      .mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce({ status: 200, value: null })
    const { result } = renderHook(() => useEvidence())
    let pending!: ReturnType<typeof result.current.readBackDesired>
    act(() => {
      pending = result.current.readBackDesired(
        {} as any, 'cluster-a', 'proj-a', 'op-old', 'token-123'
      )
    })
    await act(async () => {
      await result.current.readBackDesired(
        {} as any, 'cluster-a', 'proj-a', 'op-new', 'token-123'
      )
    })
    expect(result.current.check?.readback.detail).toBe('new')
    await act(async () => {
      resolveOld({ status: 200, value: null })
      await expect(pending).rejects.toBeInstanceOf(StaleEvidenceReadError)
    })
    expect(result.current.check?.readback.detail).toBe('new')
    expect(result.current.loading).toBe(false)
  })

  it('a denied new read clears an older previously successful evidence result', async () => {
    vi.mocked(classifyDesiredReadback).mockReturnValue({
      status: 'desired-visible', detail: 'old', desired: { generation: 1 },
    } as any)
    vi.mocked(readResource)
      .mockResolvedValueOnce({ status: 200, value: null })
      .mockRejectedValueOnce(new Error('membership revoked'))
    const { result } = renderHook(() => useEvidence())
    await act(async () => {
      await result.current.readBackDesired(
        {} as any, 'cluster-a', 'proj-a', 'op-a', 'token-123'
      )
    })
    expect(result.current.check).not.toBeNull()
    await act(async () => {
      await expect(result.current.readBackDesired(
        {} as any, 'cluster-a', 'proj-a', 'op-b', 'token-123'
      )).rejects.toThrow('membership revoked')
    })
    expect(result.current.check).toBeNull()
    expect(result.current.message).toBe('')
    expect(result.current.error).toBe('membership revoked')
  })
})
