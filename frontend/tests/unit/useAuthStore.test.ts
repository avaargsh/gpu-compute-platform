import { describe, it, expect, beforeEach } from 'vitest'
import { useAuthStore } from '@/stores/useAuthStore'

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, token: null, isLoggedIn: false })
    localStorage.removeItem('auth-storage')
  })

  it('should set auth state correctly', () => {
    const user = { id: '1', username: 'admin', email: 'admin@example.com', nickname: 'Admin', role: 'admin', is_active: true, created_at: '2024-01-01' }
    useAuthStore.getState().setAuth(user, 'test-token')

    const state = useAuthStore.getState()
    expect(state.isLoggedIn).toBe(true)
    expect(state.token).toBe('test-token')
    expect(state.user?.username).toBe('admin')
  })

  it('should clear auth state', () => {
    useAuthStore.getState().setAuth({ id: '1', username: 'admin', email: 'a@b.com', nickname: 'Admin', role: 'admin', is_active: true, created_at: '2024-01-01' }, 'token')
    useAuthStore.getState().clearAuth()

    const state = useAuthStore.getState()
    expect(state.isLoggedIn).toBe(false)
    expect(state.token).toBeNull()
    expect(state.user).toBeNull()
  })
})
