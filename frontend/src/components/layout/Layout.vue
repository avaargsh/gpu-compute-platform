<template>
  <el-container class="layout-container">
    <!-- 侧边栏 -->
    <el-aside :width="sidebarCollapsed ? '64px' : '240px'" class="sidebar">
      <div class="logo">
        <img v-if="!sidebarCollapsed" src="/logo.svg" alt="GPU Platform" class="logo-img">
        <img v-else src="/logo-mini.svg" alt="GPU Platform" class="logo-img-mini">
        <span v-if="!sidebarCollapsed" class="logo-text">GPU计算平台</span>
      </div>
      
      <el-menu
        :default-active="currentRoute"
        class="sidebar-menu"
        :collapse="sidebarCollapsed"
        router
        unique-opened
      >
        <el-menu-item
          v-for="route in menuRoutes"
          :key="route.path"
          :index="route.path"
          @click="handleMenuClick(route.path)"
        >
          <el-icon>
            <component :is="getMenuIcon(route.meta?.icon)" />
          </el-icon>
          <span>{{ route.meta?.title }}</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <!-- 主内容区 -->
    <el-container class="main-container">
      <!-- 顶部导航栏 -->
      <el-header class="header">
        <div class="header-left">
          <el-button
            text
            @click="toggleSidebar"
            class="sidebar-toggle"
          >
            <el-icon size="18">
              <component :is="sidebarCollapsed ? 'Expand' : 'Fold'" />
            </el-icon>
          </el-button>
          
          <el-breadcrumb separator="/">
            <el-breadcrumb-item
              v-for="breadcrumb in breadcrumbs"
              :key="breadcrumb.path"
              :to="breadcrumb.path"
            >
              {{ breadcrumb.title }}
            </el-breadcrumb-item>
          </el-breadcrumb>
        </div>

        <div class="header-right">
          <!-- 系统状态 -->
          <div class="system-status">
            <el-tooltip content="WebSocket连接状态">
              <el-badge 
                :is-dot="true" 
                :type="wsConnected ? 'success' : 'danger'"
                class="status-badge"
              >
                <el-icon><Connection /></el-icon>
              </el-badge>
            </el-tooltip>
          </div>
          
          <!-- 通知 -->
          <el-dropdown trigger="click" class="notification-dropdown">
            <el-badge :value="notificationCount" :max="99">
              <el-icon size="18"><Bell /></el-icon>
            </el-badge>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item v-if="notifications.length === 0">
                  <span class="no-notifications">暂无通知</span>
                </el-dropdown-item>
                <el-dropdown-item
                  v-for="notification in notifications"
                  :key="notification.id"
                >
                  <div class="notification-item">
                    <div class="notification-title">{{ notification.title }}</div>
                    <div class="notification-time">{{ notification.time }}</div>
                  </div>
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          
          <!-- 用户菜单 -->
          <el-dropdown trigger="click" class="user-dropdown">
            <div class="user-info">
              <el-avatar size="small">
                <el-icon><User /></el-icon>
              </el-avatar>
              <span v-if="!sidebarCollapsed" class="username">管理员</span>
            </div>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="handleUserSettings">
                  <el-icon><Setting /></el-icon>
                  个人设置
                </el-dropdown-item>
                <el-dropdown-item divided @click="handleLogout">
                  <el-icon><SwitchButton /></el-icon>
                  退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <!-- 主要内容区域 -->
      <el-main class="main-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { 
  Dashboard,
  Management,
  Monitor,
  Money,
  Setting,
  User,
  Bell,
  Connection,
  SwitchButton,
  Expand,
  Fold
} from '@element-plus/icons-vue'
import { wsService } from '@/services/websocket'

const route = useRoute()
const router = useRouter()

// 响应式数据
const sidebarCollapsed = ref(false)
const wsConnected = ref(false)
const notifications = ref<any[]>([])

// 菜单路由配置
const menuRoutes = computed(() => {
  return router.getRoutes().filter(route => 
    !route.meta?.hideInMenu && route.meta?.title
  )
})

// 当前路由
const currentRoute = computed(() => route.path)

// 面包屑导航
const breadcrumbs = computed(() => {
  const matched = route.matched
  return matched.filter(item => item.meta?.title).map(item => ({
    path: item.path,
    title: item.meta?.title as string
  }))
})

// 通知数量
const notificationCount = computed(() => notifications.value.length)

// 图标映射
const iconMap: Record<string, any> = {
  dashboard: Dashboard,
  task: Management,
  monitor: Monitor,
  money: Money,
  setting: Setting
}

const getMenuIcon = (iconName?: string) => {
  return iconMap[iconName || 'dashboard'] || Dashboard
}

// 事件处理
const toggleSidebar = () => {
  sidebarCollapsed.value = !sidebarCollapsed.value
  localStorage.setItem('sidebarCollapsed', String(sidebarCollapsed.value))
}

const handleMenuClick = (path: string) => {
  if (route.path !== path) {
    router.push(path)
  }
}

const handleUserSettings = () => {
  router.push('/settings')
}

const handleLogout = () => {
  // TODO: 实现退出登录逻辑
  console.log('退出登录')
}

// WebSocket 事件监听
const setupWebSocketListeners = () => {
  wsService.on('connected', () => {
    wsConnected.value = true
  })
  
  wsService.on('disconnected', () => {
    wsConnected.value = false
  })
  
  wsService.on('system_alert', (data: any) => {
    notifications.value.unshift({
      id: Date.now(),
      title: data.message,
      time: new Date().toLocaleTimeString('zh-CN'),
      type: data.type
    })
    
    // 限制通知数量
    if (notifications.value.length > 10) {
      notifications.value = notifications.value.slice(0, 10)
    }
  })
}

// 组件生命周期
onMounted(() => {
  // 恢复侧边栏状态
  const savedCollapsed = localStorage.getItem('sidebarCollapsed')
  if (savedCollapsed !== null) {
    sidebarCollapsed.value = savedCollapsed === 'true'
  }
  
  // 设置 WebSocket 状态
  wsConnected.value = wsService.isConnected()
  setupWebSocketListeners()
})

onUnmounted(() => {
  wsService.off('connected')
  wsService.off('disconnected')
  wsService.off('system_alert')
})
</script>

<style scoped>
.layout-container {
  height: 100vh;
}

.sidebar {
  background: #001529;
  transition: width 0.3s ease;
  overflow: hidden;
}

.logo {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  border-bottom: 1px solid #1f2937;
}

.logo-img {
  height: 32px;
  width: 32px;
}

.logo-img-mini {
  height: 24px;
  width: 24px;
}

.logo-text {
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  margin-left: 12px;
  white-space: nowrap;
}

.sidebar-menu {
  border: none;
  background: #001529;
}

:deep(.el-menu-item) {
  color: rgba(255, 255, 255, 0.65);
  transition: all 0.3s;
}

:deep(.el-menu-item:hover) {
  background-color: #1f2937;
  color: #1890ff;
}

:deep(.el-menu-item.is-active) {
  background-color: #1890ff;
  color: #fff;
}

:deep(.el-menu--collapse .el-menu-item span) {
  display: none;
}

.main-container {
  background: #f0f2f5;
}

.header {
  background: #fff;
  border-bottom: 1px solid #e8e8e8;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  box-shadow: 0 1px 4px rgba(0, 21, 41, 0.08);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 24px;
}

.sidebar-toggle {
  padding: 4px;
  margin-right: 8px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.system-status {
  display: flex;
  align-items: center;
}

.status-badge {
  margin-right: 8px;
}

.notification-dropdown,
.user-dropdown {
  cursor: pointer;
  display: flex;
  align-items: center;
  padding: 8px;
  border-radius: 4px;
  transition: background-color 0.2s;
}

.notification-dropdown:hover,
.user-dropdown:hover {
  background-color: #f5f5f5;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.username {
  font-size: 14px;
  color: #333;
}

.notification-item {
  min-width: 200px;
}

.notification-title {
  font-size: 14px;
  margin-bottom: 4px;
}

.notification-time {
  font-size: 12px;
  color: #999;
}

.no-notifications {
  color: #999;
  text-align: center;
  padding: 16px;
}

.main-content {
  padding: 0;
  overflow-y: auto;
  background: #f0f2f5;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .sidebar {
    position: fixed;
    z-index: 1000;
    height: 100vh;
  }
  
  .main-container {
    margin-left: 0;
  }
  
  .header-left .el-breadcrumb {
    display: none;
  }
}
</style>
