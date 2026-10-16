import { useAuthStore } from '@/stores/useAuthStore'
import { authApi } from '@/services/authApi'

export function useAuth() {
  const user = useAuthStore((s) => s.user)
  const token = useAuthStore((s) => s.token)
  const isLoggedIn = useAuthStore((s) => s.isLoggedIn)
  const setAuth = useAuthStore((s) => s.setAuth)
  const clearAuth = useAuthStore((s) => s.clearAuth)
  const updateUser = useAuthStore((s) => s.updateUser)

  const login = async (username: string, password: string) => {
    const res = await authApi.login({ username, password })
    if (res.data && res.data.token) {
      setAuth(res.data, res.data.token)
    }
    return res
  }

  const logout = async () => {
    try {
      await authApi.logout()
    } finally {
      clearAuth()
      window.location.href = '/login'
    }
  }

  return {
    user,
    token,
    isLoggedIn,
    login,
    logout,
    updateUser,
  }
}
