<template>
  <el-drawer
    v-model="visible"
    title="系统通知"
    :size="400"
    direction="rtl"
    :modal="true"
    @close="handleClose"
  >
    <div class="notification-drawer">
      <!-- 操作栏 -->
      <div class="notification-actions">
        <el-button 
          size="small" 
          type="primary" 
          @click="markAllAsRead"
          :disabled="unreadCount === 0"
        >
          全部标记为已读
        </el-button>
        <el-button 
          size="small" 
          @click="clearAllNotifications"
          :disabled="notifications.length === 0"
        >
          清空通知
        </el-button>
      </div>

      <!-- 通知筛选 -->
      <div class="notification-filter">
        <el-radio-group v-model="filterType" size="small">
          <el-radio-button value="all">全部 ({{ notifications.length }})</el-radio-button>
          <el-radio-button value="unread">未读 ({{ unreadCount }})</el-radio-button>
          <el-radio-button value="system">系统通知</el-radio-button>
          <el-radio-button value="task">任务通知</el-radio-button>
        </el-radio-group>
      </div>

      <!-- 通知列表 -->
      <div class="notification-list">
        <el-empty 
          v-if="filteredNotifications.length === 0" 
          description="暂无通知"
          :image-size="120"
        />
        
        <div
          v-for="notification in filteredNotifications"
          :key="notification.id"
          class="notification-item"
          :class="{ unread: !notification.read }"
          @click="markAsRead(notification.id)"
        >
          <!-- 通知图标 -->
          <div class="notification-icon" :class="`icon-${notification.type}`">
            <el-icon>
              <component :is="getNotificationIcon(notification.type)" />
            </el-icon>
          </div>

          <!-- 通知内容 -->
          <div class="notification-content">
            <div class="notification-header">
              <h4 class="notification-title">{{ notification.title }}</h4>
              <span class="notification-time">{{ formatTime(notification.time) }}</span>
            </div>
            
            <p class="notification-message">{{ notification.message }}</p>
            
            <!-- 操作按钮 -->
            <div class="notification-actions-inline" v-if="notification.action">
              <el-button 
                size="small" 
                type="primary" 
                text 
                @click.stop="handleNotificationAction(notification)"
              >
                {{ notification.action.text }}
              </el-button>
            </div>
          </div>

          <!-- 删除按钮 -->
          <div class="notification-delete">
            <el-button 
              size="small" 
              type="danger" 
              text 
              @click.stop="removeNotification(notification.id)"
            >
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Delete,
  InfoFilled,
  WarningFilled,
  CircleCheckFilled,
  CircleCloseFilled,
  Bell,
  Monitor,
  Setting
} from '@element-plus/icons-vue'

interface Props {
  modelValue: boolean
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const router = useRouter()

// 计算属性
const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

// 状态
const filterType = ref('all')
const notifications = ref([
  {
    id: 1,
    type: 'system',
    title: '系统维护通知',
    message: '系统将于今晚23:00-24:00进行例行维护，请提前保存工作',
    time: new Date(Date.now() - 1000 * 60 * 30).toISOString(), // 30分钟前
    read: false,
    action: {
      text: '查看详情',
      type: 'route',
      value: '/system/maintenance'
    }
  },
  {
    id: 2,
    type: 'task',
    title: '任务执行完成',
    message: '您的训练任务 "BERT模型训练" 已执行完成，请查看结果',
    time: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(), // 2小时前
    read: false,
    action: {
      text: '查看任务',
      type: 'route', 
      value: '/tasks/task-123'
    }
  },
  {
    id: 3,
    type: 'warning',
    title: 'GPU使用率过高',
    message: 'GPU-0 使用率持续90%以上，建议检查任务配置',
    time: new Date(Date.now() - 1000 * 60 * 60 * 5).toISOString(), // 5小时前
    read: true
  },
  {
    id: 4,
    type: 'success',
    title: '账户余额充值成功',
    message: '您的账户成功充值￥500.00，当前余额￥1,200.00',
    time: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(), // 1天前
    read: true
  },
  {
    id: 5,
    type: 'error',
    title: '任务执行失败',
    message: '任务 "数据预处理" 执行失败：内存不足',
    time: new Date(Date.now() - 1000 * 60 * 60 * 24 * 2).toISOString(), // 2天前
    read: true,
    action: {
      text: '重新运行',
      type: 'action',
      value: 'retry-task'
    }
  }
])

// 计算属性
const unreadCount = computed(() => 
  notifications.value.filter(n => !n.read).length
)

const filteredNotifications = computed(() => {
  let filtered = notifications.value

  switch (filterType.value) {
    case 'unread':
      filtered = filtered.filter(n => !n.read)
      break
    case 'system':
      filtered = filtered.filter(n => n.type === 'system')
      break
    case 'task':
      filtered = filtered.filter(n => n.type === 'task')
      break
    default:
      // 显示全部
      break
  }

  return filtered.sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime())
})

// 获取通知图标
const getNotificationIcon = (type: string) => {
  const iconMap: Record<string, any> = {
    system: Setting,
    task: Monitor,
    success: CircleCheckFilled,
    warning: WarningFilled,
    error: CircleCloseFilled,
    info: InfoFilled
  }
  return iconMap[type] || Bell
}

// 格式化时间
const formatTime = (timeStr: string) => {
  const time = new Date(timeStr)
  const now = new Date()
  const diff = now.getTime() - time.getTime()

  // 小于1分钟
  if (diff < 60 * 1000) {
    return '刚刚'
  }
  
  // 小于1小时
  if (diff < 60 * 60 * 1000) {
    const minutes = Math.floor(diff / (60 * 1000))
    return `${minutes}分钟前`
  }
  
  // 小于24小时
  if (diff < 24 * 60 * 60 * 1000) {
    const hours = Math.floor(diff / (60 * 60 * 1000))
    return `${hours}小时前`
  }
  
  // 小于7天
  if (diff < 7 * 24 * 60 * 60 * 1000) {
    const days = Math.floor(diff / (24 * 60 * 60 * 1000))
    return `${days}天前`
  }
  
  // 超过7天显示具体日期
  return time.toLocaleDateString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

// 标记为已读
const markAsRead = (id: number) => {
  const notification = notifications.value.find(n => n.id === id)
  if (notification) {
    notification.read = true
  }
}

// 全部标记为已读
const markAllAsRead = () => {
  notifications.value.forEach(n => n.read = true)
  ElMessage.success('已将所有通知标记为已读')
}

// 删除通知
const removeNotification = (id: number) => {
  const index = notifications.value.findIndex(n => n.id === id)
  if (index > -1) {
    notifications.value.splice(index, 1)
    ElMessage.success('通知已删除')
  }
}

// 清空所有通知
const clearAllNotifications = () => {
  ElMessageBox.confirm(
    '确认要清空所有通知吗？此操作不可恢复。',
    '确认清空',
    {
      confirmButtonText: '确认',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(() => {
    notifications.value = []
    ElMessage.success('所有通知已清空')
  }).catch(() => {
    // 取消操作
  })
}

// 处理通知操作
const handleNotificationAction = (notification: any) => {
  if (!notification.action) return

  switch (notification.action.type) {
    case 'route':
      visible.value = false
      router.push(notification.action.value)
      break
    case 'action':
      // 处理自定义操作
      handleCustomAction(notification.action.value, notification)
      break
  }
  
  // 标记为已读
  markAsRead(notification.id)
}

// 处理自定义操作
const handleCustomAction = (action: string, notification: any) => {
  switch (action) {
    case 'retry-task':
      ElMessage.info('正在重新执行任务...')
      // 这里可以调用重新执行任务的API
      break
    default:
      console.log('Unknown action:', action)
  }
}

// 关闭抽屉
const handleClose = () => {
  visible.value = false
}

// 组件挂载时的操作
onMounted(() => {
  // 可以在这里获取通知数据
})
</script>

<style scoped>
.notification-drawer {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.notification-actions {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
  flex-shrink: 0;
}

.notification-filter {
  margin-bottom: 16px;
  flex-shrink: 0;
}

.notification-list {
  flex: 1;
  overflow-y: auto;
}

.notification-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  border-radius: 8px;
  border: 1px solid #e4e7ed;
  margin-bottom: 12px;
  cursor: pointer;
  transition: all 0.3s ease;
  position: relative;
}

.notification-item:hover {
  background-color: #f5f7fa;
  border-color: #c6e2ff;
}

.notification-item.unread {
  background-color: #ecf5ff;
  border-color: #b3d8ff;
}

.notification-item.unread::before {
  content: '';
  position: absolute;
  top: 16px;
  left: 8px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: #409eff;
}

.notification-icon {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
}

.notification-icon.icon-system {
  background-color: #e6f7ff;
  color: #1890ff;
}

.notification-icon.icon-task {
  background-color: #f6ffed;
  color: #52c41a;
}

.notification-icon.icon-success {
  background-color: #f6ffed;
  color: #52c41a;
}

.notification-icon.icon-warning {
  background-color: #fffbe6;
  color: #faad14;
}

.notification-icon.icon-error {
  background-color: #fff2f0;
  color: #ff4d4f;
}

.notification-icon.icon-info {
  background-color: #f0f0f0;
  color: #8c8c8c;
}

.notification-content {
  flex: 1;
  min-width: 0;
}

.notification-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 8px;
  gap: 8px;
}

.notification-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
  margin: 0;
  line-height: 1.4;
  flex: 1;
}

.notification-time {
  font-size: 12px;
  color: #909399;
  white-space: nowrap;
  flex-shrink: 0;
}

.notification-message {
  font-size: 13px;
  color: #606266;
  line-height: 1.5;
  margin: 0 0 8px 0;
}

.notification-actions-inline {
  margin-top: 8px;
}

.notification-delete {
  flex-shrink: 0;
}

/* 滚动条样式 */
.notification-list::-webkit-scrollbar {
  width: 6px;
}

.notification-list::-webkit-scrollbar-track {
  background: #f1f1f1;
  border-radius: 3px;
}

.notification-list::-webkit-scrollbar-thumb {
  background: #c1c1c1;
  border-radius: 3px;
}

.notification-list::-webkit-scrollbar-thumb:hover {
  background: #a8a8a8;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .notification-actions {
    flex-direction: column;
  }
  
  .notification-filter :deep(.el-radio-group) {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  
  .notification-filter :deep(.el-radio-button) {
    margin-right: 0;
  }
  
  .notification-item {
    padding: 12px;
  }
  
  .notification-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
  }
  
  .notification-time {
    align-self: flex-end;
  }
}
</style>
