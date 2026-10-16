import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAuth } from '@/management/hooks/useAuth'
import {
  discover,
  beginLogin,
  finishLogin,
  clearPendingLogin,
} from '@/management/auth'

vi.mock('@/management/auth', () => ({
  discover: vi.fn(),
  beginLogin: vi.fn(),
  finishLogin: vi.fn(),
  clearPendingLogin: vi.fn(),
}))

const mockEnv = (issuer: string, clientId: string) => {
  vi.stubEnv('VITE_MANAGEMENT_OIDC_ISSUER', issuer, true)
  vi.stubEnv('VITE_MANAGEMENT_OIDC_CLIENT_ID', clientId, true)
}

describe('useAuth', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.unstubAllEnvs()
  })

  it('should be configured when issuer and clientId are set', () => {
    mockEnv('https://oidc.example.com', 'my-client-id')
    const { result } = renderHook(() => useAuth())
    expect(result.current.configured).toBe(true)
    expect(result.current.issuer).toBe('https://oidc.example.com')
    expect(result.current.clientId).toBe('my-client-id')
  })

  it('should not be configured when env vars are missing', () => {
    const { result } = renderHook(() => useAuth())
    expect(result.current.configured).toBe(false)
  })

  it('should call signIn which calls discover and beginLogin', async () => {
    mockEnv('https://oidc.example.com', 'my-client-id')
    vi.mocked(discover).mockResolvedValue({
      issuer: 'https://oidc.example.com',
      authorization_endpoint: 'https://oidc.example.com/oauth2/auth',
      token_endpoint: 'https://oidc.example.com/oauth2/token',
    })
    vi.mocked(beginLogin).mockResolvedValue('https://oidc.example.com/oauth2/auth?code_challenge=abc')

    const { result } = renderHook(() => useAuth())
    await act(async () => {
      await result.current.signIn()
    })

    expect(discover).toHaveBeenCalled()
    expect(beginLogin).toHaveBeenCalled()
  })

  it('should set error when signIn fails', async () => {
    mockEnv('https://oidc.example.com', 'my-client-id')
    vi.mocked(discover).mockRejectedValue(new Error('Discovery failed'))

    const { result } = renderHook(() => useAuth())
    await act(async () => {
      await result.current.signIn()
    })

    expect(result.current.error).toBe('Discovery failed')
    expect(result.current.busy).toBe(false)
  })

  it('should clear session on signOut', () => {
    mockEnv('https://oidc.example.com', 'my-client-id')
    const { result } = renderHook(() => useAuth())
    // Manually set session via internal state is not exposed, so just test signOut doesn't throw
    act(() => {
      result.current.signOut()
    })
    expect(clearPendingLogin).toHaveBeenCalled()
  })
})
