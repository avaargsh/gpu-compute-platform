import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useEvidence } from '@/management/hooks/useEvidence'
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
    vi.clearAllMocks()
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
      await result.current.refreshEvidenceChain(
        'cluster-a',
        'proj-a',
        'op-123',
        'token-123'
      )
    })

    expect(result.current.error).toBe('Receipt not found')
    expect(result.current.loading).toBe(false)
  })
})
