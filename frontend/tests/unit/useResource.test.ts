import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { StaleResourceReadError, useResource } from '@/management/hooks/useResource'
import {
  readResource,
  writeResource,
  deleteResource,
  type DesiredView,
  type Scope,
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
      await expect(result.current.readResource(
        { cluster: 'cluster-a', project: 'proj-a', kind: 'ComputePool', resource: 'pool-1' },
        'token-123'
      )).rejects.toThrow('404 Not Found')
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


type ReadReply = Awaited<ReturnType<typeof readResource>>
function deferredRead() {
  let resolve!: (value: ReadReply) => void
  let reject!: (error: Error) => void
  const promise = new Promise<ReadReply>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const targetA: Scope = { cluster: 'cluster-a', project: 'proj-a', kind: 'Workload', resource: 'train-a' }
const targetB: Scope = { ...targetA, resource: 'train-b' }
const readA: DesiredView = {
  clusterId: targetA.cluster, projectId: targetA.project, kind: targetA.kind,
  id: targetA.resource, generation: 1, deleting: false, spec: {},
}
const readB: DesiredView = { ...readA, id: targetB.resource }

describe('useResource stale read safety', () => {
  it('does not let late GET A overwrite newer GET B', async () => {
    const first = deferredRead()
    const second = deferredRead()
    vi.mocked(readResource).mockImplementationOnce(() => first.promise).mockImplementationOnce(() => second.promise)
    const { result } = renderHook(() => useResource())
    let oldRead!: Promise<unknown>
    let newRead!: Promise<unknown>
    act(() => {
      oldRead = result.current.readResource(targetA, 'token-a')
      newRead = result.current.readResource(targetB, 'token-a')
    })
    const stale = expect(oldRead).rejects.toBeInstanceOf(StaleResourceReadError)
    await act(async () => {
      second.resolve({ status: 200, value: readB })
      await newRead
    })
    await act(async () => {
      first.resolve({ status: 200, value: readA })
      await stale
    })
    expect(result.current.desired).toEqual(readB)
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBe('')
  })

  it('clearDesired invalidates an in-flight GET and prevents repopulating a previous session', async () => {
    const pending = deferredRead()
    vi.mocked(readResource).mockImplementationOnce(() => pending.promise)
    const { result } = renderHook(() => useResource())
    let oldRead!: Promise<unknown>
    act(() => { oldRead = result.current.readResource(targetA, 'old-session-token') })
    const stale = expect(oldRead).rejects.toBeInstanceOf(StaleResourceReadError)
    act(() => { result.current.clearDesired() })
    await act(async () => {
      pending.resolve({ status: 200, value: readA })
      await stale
    })
    expect(result.current.desired).toBeNull()
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBe('')
  })

  it('late failing GET cannot replace the newer successful resource or show stale errors', async () => {
    const oldPending = deferredRead()
    const newPending = deferredRead()
    vi.mocked(readResource).mockImplementationOnce(() => oldPending.promise).mockImplementationOnce(() => newPending.promise)
    const { result } = renderHook(() => useResource())
    let oldRead!: Promise<unknown>
    let newRead!: Promise<unknown>
    act(() => {
      oldRead = result.current.readResource(targetA, 'token-a')
      newRead = result.current.readResource(targetB, 'token-a')
    })
    const stale = expect(oldRead).rejects.toBeInstanceOf(StaleResourceReadError)
    await act(async () => {
      newPending.resolve({ status: 200, value: readB })
      await newRead
    })
    await act(async () => {
      oldPending.reject(new Error('old project read forbidden'))
      await stale
    })
    expect(result.current.desired).toEqual(readB)
    expect(result.current.error).toBe('')
  })

  it('a dispatched PUT invalidates a preexisting GET even when the GET settles after the write', async () => {
    const oldPending = deferredRead()
    vi.mocked(readResource).mockImplementationOnce(() => oldPending.promise)
    vi.mocked(writeResource).mockResolvedValueOnce({ status: 204, value: null })
    const { result } = renderHook(() => useResource())
    let oldRead!: Promise<unknown>
    act(() => { oldRead = result.current.readResource(targetA, 'token-a') })
    const stale = expect(oldRead).rejects.toBeInstanceOf(StaleResourceReadError)
    await act(async () => {
      await result.current.writeResource(targetA, 'token-a',
        '{"expectedGeneration":0,"spec":{}}', '0123456789abcdef0123456789abcdef')
    })
    await act(async () => {
      oldPending.resolve({ status: 200, value: readA })
      await stale
    })
    expect(result.current.desired).toBeNull()
    expect(result.current.loading).toBe(false)
  })
})
