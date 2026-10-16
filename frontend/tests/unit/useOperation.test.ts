import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useOperation } from '@/management/hooks/useOperation'
import {
  rememberOperation,
  recoverOperation,
  forgetOperation,
} from '@/management/recovery'

vi.mock('@/management/recovery', () => ({
  rememberOperation: vi.fn(),
  recoverOperation: vi.fn(),
  forgetOperation: vi.fn(),
}))

describe('useOperation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should remember operation and set pointer', () => {
    const mockPointer = {
      operationId: 'op-123',
      cluster: 'cluster-a',
      project: 'proj-a',
      kind: 'ComputePool' as const,
      resource: 'pool-1',
      method: 'PUT' as const,
      issuedAt: Date.now(),
    }
    vi.mocked(rememberOperation).mockReturnValue(mockPointer)

    const { result } = renderHook(() => useOperation())
    act(() => {
      result.current.remember(
        { cluster: 'cluster-a', project: 'proj-a', kind: 'ComputePool', resource: 'pool-1' },
        'PUT',
        'op-123'
      )
    })

    expect(rememberOperation).toHaveBeenCalledWith(
      sessionStorage,
      { cluster: 'cluster-a', project: 'proj-a', kind: 'ComputePool', resource: 'pool-1' },
      'PUT',
      'op-123'
    )
    expect(result.current.pointer?.operationId).toBe('op-123')
  })

  it('should recover operation', () => {
    const mockPointer = {
      operationId: 'op-456',
      cluster: 'cluster-b',
      project: 'proj-b',
      kind: 'Workload' as const,
      resource: 'wl-2',
      method: 'DELETE' as const,
      issuedAt: Date.now(),
    }
    vi.mocked(recoverOperation).mockReturnValue(mockPointer)

    const { result } = renderHook(() => useOperation())
    act(() => {
      result.current.recover()
    })

    expect(recoverOperation).toHaveBeenCalledWith(sessionStorage)
    expect(result.current.pointer?.operationId).toBe('op-456')
  })

  it('should forget operation', () => {
    const { result } = renderHook(() => useOperation())
    act(() => {
      result.current.forget()
    })

    expect(forgetOperation).toHaveBeenCalledWith(sessionStorage)
    expect(result.current.pointer).toBeNull()
  })
})
