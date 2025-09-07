import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { useUserStore } from '@/stores/userStore'
import { ElMessage } from 'element-plus'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/dashboard'
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/Login.vue'),
    meta: {
      title: '用户登录',
      hideInMenu: true
    }
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('@/views/auth/Register.vue'),
    meta: {
      title: '用户注册',
      hideInMenu: true
    }
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('@/views/Dashboard.vue'),
    meta: {
      title: '仪表盘',
      icon: 'dashboard'
    }
  },
  {
    path: '/tasks',
    name: 'TaskList',
    component: () => import('@/views/tasks/TaskList.vue'),
    meta: {
      title: '任务管理',
      icon: 'task'
    }
  },
  {
    path: '/tasks/create',
    name: 'TaskCreate',
    component: () => import('@/views/tasks/TaskCreate.vue'),
    meta: {
      title: '创建任务',
      icon: 'plus',
      hideInMenu: true
    }
  },
  {
    path: '/tasks/:id',
    name: 'TaskDetail',
    component: () => import('@/views/tasks/TaskDetail.vue'),
    meta: {
      title: '任务详情',
      icon: 'task',
      hideInMenu: true
    }
  },
  {
    path: '/gpu',
    name: 'GPUMonitor',
    component: () => import('@/views/gpu/GPUMonitor.vue'),
    meta: {
      title: 'GPU监控',
      icon: 'monitor'
    }
  },
  {
    path: '/cost',
    name: 'CostOptimization',
    component: () => import('@/views/cost/CostOptimization.vue'),
    meta: {
      title: '成本优化',
      icon: 'money'
    }
  },
  {
    path: '/settings',
    name: 'Settings',
    component: () => import('@/views/Settings.vue'),
    meta: {
      title: '系统设置',
      icon: 'setting'
    }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFound.vue'),
    meta: {
      title: '页面未找到',
      hideInMenu: true
    }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) {
      return savedPosition
    } else {
      return { top: 0 }
    }
  }
})

// 导航守卫
router.beforeEach(async (to, from, next) => {
  const userStore = useUserStore()
  
  // 设置页面标题
  if (to.meta.title) {
    document.title = `${to.meta.title} - GPU计算平台`
  }
  
  // 白名单页面（无需登录）
  const whiteList = ['/login', '/register']
  
  if (whiteList.includes(to.path)) {
    // 如果已经登录，跳转到仪表盘
    if (userStore.isLoggedIn) {
      next('/dashboard')
    } else {
      next()
    }
    return
  }
  
  // 检查登录状态
  if (!userStore.token) {
    ElMessage.warning('请先登录')
    next(`/login?redirect=${encodeURIComponent(to.fullPath)}`)
    return
  }
  
  // 如果有 token 但没有用户信息，尝试获取
  if (userStore.token && !userStore.user) {
    try {
      await userStore.getCurrentUser()
    } catch (error) {
      console.error('Failed to get user info:', error)
      ElMessage.error('获取用户信息失败，请重新登录')
      userStore.clearAuth()
      next(`/login?redirect=${encodeURIComponent(to.fullPath)}`)
      return
    }
  }
  
  // 检查权限
  if (to.meta.requiresAuth === false) {
    next()
    return
  }
  
  // 检查特定页面的权限
  if (to.path === '/cost' && !userStore.hasPermission('view_cost')) {
    ElMessage.error('您没有访问此页面的权限')
    next('/dashboard')
    return
  }
  
  if (to.path === '/settings' && !userStore.hasPermission('system_settings')) {
    ElMessage.error('您没有访问此页面的权限')
    next('/dashboard')
    return
  }
  
  next()
})

export default router
