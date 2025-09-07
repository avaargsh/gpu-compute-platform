import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { authApi } from '@/services/api'
import type { User, LoginForm, RegisterForm, UserProfile, ChangePasswordForm } from '@/types'

export const useUserStore = defineStore('user', () => {
  // 状态
  const user = ref<User | null>(null)
  const token = ref<string | null>(localStorage.getItem('auth_token'))
  const isLoggedIn = computed(() => !!user.value && !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // 初始化用户信息
  const initUser = () => {
    const savedUser = localStorage.getItem('user_info')
    if (savedUser && token.value) {
      try {
        user.value = JSON.parse(savedUser)
      } catch (error) {
        console.error('Failed to parse saved user info:', error)
        localStorage.removeItem('user_info')
      }
    }
  }

  // 设置用户信息和 token
  const setAuth = (userData: User, authToken: string) => {
    user.value = userData
    token.value = authToken
    localStorage.setItem('user_info', JSON.stringify(userData))
    localStorage.setItem('auth_token', authToken)
  }

  // 清除用户信息和 token
  const clearAuth = () => {
    user.value = null
    token.value = null
    localStorage.removeItem('user_info')
    localStorage.removeItem('auth_token')
  }

  // 用户登录
  const login = async (loginData: LoginForm): Promise<boolean> => {
    try {
      const response = await authApi.login(loginData)
      
      if (response.data.success && response.data.data) {
        const userData = response.data.data
        const authToken = userData.token
        
        if (authToken) {
          setAuth(userData, authToken)
          
          // 记住用户选项
          if (loginData.remember) {
            localStorage.setItem('remember_user', 'true')
          }
          
          ElMessage.success('登录成功')
          return true
        } else {
          throw new Error('未获取到认证令牌')
        }
      } else {
        throw new Error(response.data.message || '登录失败')
      }
    } catch (error: any) {
      const message = error.response?.data?.message || error.message || '登录失败'
      ElMessage.error(message)
      console.error('Login error:', error)
      return false
    }
  }

  // 用户注册
  const register = async (registerData: RegisterForm): Promise<boolean> => {
    try {
      const { confirmPassword, agreement, ...apiData } = registerData
      
      if (!agreement) {
        ElMessage.warning('请先阅读并同意用户协议')
        return false
      }
      
      if (registerData.password !== confirmPassword) {
        ElMessage.warning('密码和确认密码不一致')
        return false
      }

      const response = await authApi.register(apiData)
      
      if (response.data.success && response.data.data) {
        const userData = response.data.data
        const authToken = userData.token
        
        if (authToken) {
          setAuth(userData, authToken)
          ElMessage.success('注册成功')
          return true
        } else {
          throw new Error('未获取到认证令牌')
        }
      } else {
        throw new Error(response.data.message || '注册失败')
      }
    } catch (error: any) {
      const message = error.response?.data?.message || error.message || '注册失败'
      ElMessage.error(message)
      console.error('Register error:', error)
      return false
    }
  }

  // 用户登出
  const logout = async (): Promise<void> => {
    try {
      if (token.value) {
        await authApi.logout()
      }
    } catch (error) {
      console.error('Logout API error:', error)
      // 即使 API 调用失败，也要清除本地状态
    } finally {
      clearAuth()
      ElMessage.success('已退出登录')
    }
  }

  // 获取当前用户信息
  const getCurrentUser = async (): Promise<boolean> => {
    try {
      if (!token.value) {
        return false
      }

      const response = await authApi.getCurrentUser()
      
      if (response.data.success && response.data.data) {
        user.value = response.data.data
        localStorage.setItem('user_info', JSON.stringify(response.data.data))
        return true
      } else {
        throw new Error(response.data.message || '获取用户信息失败')
      }
    } catch (error: any) {
      console.error('Get current user error:', error)
      
      // 如果是认证错误，清除本地状态
      if (error.response?.status === 401) {
        clearAuth()
      }
      
      return false
    }
  }

  // 更新用户资料
  const updateProfile = async (profileData: UserProfile): Promise<boolean> => {
    try {
      const response = await authApi.updateProfile(profileData)
      
      if (response.data.success && response.data.data) {
        user.value = response.data.data
        localStorage.setItem('user_info', JSON.stringify(response.data.data))
        ElMessage.success('资料更新成功')
        return true
      } else {
        throw new Error(response.data.message || '资料更新失败')
      }
    } catch (error: any) {
      const message = error.response?.data?.message || error.message || '资料更新失败'
      ElMessage.error(message)
      console.error('Update profile error:', error)
      return false
    }
  }

  // 修改密码
  const changePassword = async (passwordData: ChangePasswordForm): Promise<boolean> => {
    try {
      if (passwordData.new_password !== passwordData.confirm_password) {
        ElMessage.warning('新密码和确认密码不一致')
        return false
      }

      const response = await authApi.changePassword(passwordData)
      
      if (response.data.success) {
        ElMessage.success('密码修改成功')
        return true
      } else {
        throw new Error(response.data.message || '密码修改失败')
      }
    } catch (error: any) {
      const message = error.response?.data?.message || error.message || '密码修改失败'
      ElMessage.error(message)
      console.error('Change password error:', error)
      return false
    }
  }

  // 检查权限
  const hasPermission = (permission: string): boolean => {
    if (!user.value) return false
    
    // 管理员拥有所有权限
    if (user.value.role === 'admin') return true
    
    // 这里可以根据实际需求扩展权限逻辑
    const userPermissions = {
      'view_tasks': true,
      'create_tasks': true,
      'manage_own_tasks': true,
      'view_gpu': true,
      'view_cost': user.value.role === 'admin', // 只有管理员可以查看成本
      'manage_users': user.value.role === 'admin',
      'system_settings': user.value.role === 'admin'
    }
    
    return userPermissions[permission as keyof typeof userPermissions] || false
  }

  // 刷新 token
  const refreshToken = async (): Promise<boolean> => {
    try {
      const response = await authApi.refreshToken()
      
      if (response.data.success && response.data.data) {
        token.value = response.data.data.token
        localStorage.setItem('auth_token', response.data.data.token)
        return true
      } else {
        throw new Error('Token 刷新失败')
      }
    } catch (error) {
      console.error('Refresh token error:', error)
      clearAuth()
      return false
    }
  }

  return {
    // 状态
    user: readonly(user),
    token: readonly(token),
    isLoggedIn,
    isAdmin,
    
    // 方法
    initUser,
    login,
    register,
    logout,
    getCurrentUser,
    updateProfile,
    changePassword,
    hasPermission,
    refreshToken,
    clearAuth
  }
})
