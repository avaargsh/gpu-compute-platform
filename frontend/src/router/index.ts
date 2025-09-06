import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/dashboard'
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
router.beforeEach((to, from, next) => {
  // 设置页面标题
  if (to.meta.title) {
    document.title = `${to.meta.title} - GPU计算平台`
  }
  
  next()
})

export default router
