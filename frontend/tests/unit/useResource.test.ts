import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useResource } from '@/management/hooks/useResource'
import {
  readResource,
  writeResource,
  deleteResource,
} from '@/management/api'

vi.mock('@/management/api', () => ({
  readResource: vi.fn(),
  writeResource: vi.fn(),
  deleteResource: vi.fn(),
}))

describe('useResource', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubEnv('VITE_MANAGEMENT_OIDC_ISSUER', '', true)
    vi.stubEnv('VITE_MANAGEMENT_OIDC_CLIENT_ID', '', true)
  })

  it('should read resource successfully', async () => {
    vi.mocked(readResource).mockResolvedValue({
      status: 200,
      value: {
        clusterId: 'cluster-a',
        projectId: 'proj-a',
        kind: 'ComputePool',
        id: 'pool-1',
        generation: 3,
        deleting: false,
        spec: { accelerators: [] },
      },
    })

    const { result } = renderHook(() => useResource())
    await act(async () => {
      await result.current.readResource(
        { cluster: 'cluster-a', project: 'proj-a', kind: 'ComputePool', resource: 'pool-1' },
        'token-123'
      )
    })

    expect(readResource).toHaveBeenCalled()
    expect(result.current.desired?.id).toBe('pool-1')
    expect(result.current.desired?.generation).toBe(3)
    expect(result.current.loading).toBe(false)
  })

  it('should handle read error', async () => {
    vi.mocked(readResource).mockRejectedValue(new Error('404 Not Found'))

    const { result } = renderHook(() => useResource())
    await act(async () => {
      await result.current.readResource(
        { cluster: 'cluster-a', project: 'proj-a', kind: 'ComputePool', resource: 'pool-1' },
        'token-123'
      )
    })

    expect(result.current.desired).toBeNull()
    expect(result.current.error).toBe('404 Not Found')
  })

  it('should write resource successfully', async () => {
    vi.mocked(writeResource).mockResolvedValue({ status: 202, value: null })
    vi.stubGlobal('window', Object.create(window))
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))

    const { result } = renderHook(() => useResource())
    await act(async () => {
      await result.current.writeResource(
        { cluster: 'cluster-a', project: 'proj-a', kind: 'Workload', resource: 'wl-1' },
        'token-123',
        JSON.stringify({ expectedGeneration: 0, spec: {} }),
        'op-id-123'
      )
    })

    expect(writeResource).toHaveBeenCalled()
    expect(result.current.loading).toBe(false)
  })

  it('should clear desired state', () => {
    const { result } = renderHook(() => useResource())
    act(() => {
      result.current.clearDesired()
    })
    expect(result.current.desired).toBeNull()
    expect(result.current.error).toBe('')
  })
})
