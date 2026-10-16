import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useProjects } from '@/management/hooks/useProjects'
import { listProjects } from '@/management/api'

vi.mock('@/management/api', () => ({
  listProjects: vi.fn(),
}))

describe('useProjects', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should load projects successfully', async () => {
    vi.mocked(listProjects).mockResolvedValue({
      status: 200,
      value: {
        projects: [
          { projectId: 'proj-a', clusterId: 'cluster-a', role: 'admin' },
          { projectId: 'proj-b', clusterId: 'cluster-b', role: 'viewer' },
        ],
        truncated: false,
      },
    })

    const { result } = renderHook(() => useProjects())
    await act(async () => {
      await result.current.loadProjects('token-123')
    })

    expect(listProjects).toHaveBeenCalledWith('token-123')
    expect(result.current.projects).toHaveLength(2)
    expect(result.current.projects[0].projectId).toBe('proj-a')
    expect(result.current.truncated).toBe(false)
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBe('')
  })

  it('should handle loadProjects error', async () => {
    vi.mocked(listProjects).mockRejectedValue(new Error('OIDC token expired'))

    const { result } = renderHook(() => useProjects())
    await act(async () => {
      await result.current.loadProjects('bad-token')
    })

    expect(result.current.projects).toHaveLength(0)
    expect(result.current.error).toBe('OIDC token expired')
    expect(result.current.loading).toBe(false)
  })
})
