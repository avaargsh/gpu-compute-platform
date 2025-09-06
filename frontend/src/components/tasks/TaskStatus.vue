<template>
  <div class="task-status">
    <el-card v-loading="loading" shadow="hover">
      <template #header>
        <div class="card-header">
          <span>任务详情</span>
          <div class="header-actions">
            <el-button size="small" @click="handleRefresh" :loading="refreshing">
              <el-icon><Refresh /></el-icon>
            </el-button>
            <el-button
              v-if="task?.status === 'running'"
              size="small"
              type="warning"
              @click="handleCancelTask"
            >
              取消任务
            </el-button>
          </div>
        </div>
      </template>

      <div v-if="task" class="task-content">
        <!-- 基本信息 -->
        <el-descriptions :column="3" border>
          <el-descriptions-item label="任务ID">{{ task.id }}</el-descriptions-item>
          <el-descriptions-item label="任务名称">{{ task.name }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="getStatusTagType(task.status)">
              {{ getStatusText(task.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ formatTime(task.created_at) }}</el-descriptions-item>
          <el-descriptions-item label="开始时间">{{ task.started_at ? formatTime(task.started_at) : '-' }}</el-descriptions-item>
          <el-descriptions-item label="完成时间">{{ task.completed_at ? formatTime(task.completed_at) : '-' }}</el-descriptions-item>
        </el-descriptions>

        <!-- 执行进度 -->
        <div class="progress-section">
          <h3>执行进度</h3>
          <el-progress 
            :percentage="task.progress"
            :status="getProgressStatus(task.status)"
            :stroke-width="12"
            :show-text="true"
          />
        </div>

        <!-- GPU 和内存使用情况 -->
        <div v-if="task.status === 'running'" class="resource-section">
          <h3>资源使用情况</h3>
          <el-row :gutter="24">
            <el-col :span="12">
              <div class="resource-card">
                <div class="resource-title">GPU 使用率</div>
                <el-progress type="circle" :percentage="task.gpu_usage" :width="80" />
              </div>
            </el-col>
            <el-col :span="12">
              <div class="resource-card">
                <div class="resource-title">内存使用</div>
                <el-progress type="circle" :percentage="50" :width="80" />
                <span class="value-text">{{ formatMemory(task.memory_usage) }}</span>
              </div>
            </el-col>
          </el-row>
        </div>

        <!-- 运行日志 -->
        <div class="logs-section">
          <div class="logs-header">
            <h3>运行日志</h3>
            <el-button size="small" @click="downloadLogs">下载日志</el-button>
          </div>
          <div class="logs-container">
            <div 
              v-for="(log, index) in task.logs" 
              :key="index"
              :class="['log-entry', `log-${log.level}`]"
            >
              <span class="log-time">{{ formatLogTime(log.timestamp) }}</span>
              <span class="log-level">{{ log.level.toUpperCase() }}</span>
              <span class="log-message">{{ log.message }}</span>
            </div>
            <div v-if="task.logs.length === 0" class="no-logs">
              暂无日志信息
            </div>
          </div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useTaskStore } from '@/stores/taskStore'

interface Props {
  taskId: string
}

const props = defineProps<Props>()
const taskStore = useTaskStore()

const refreshing = ref(false)
const { loading } = taskStore
const task = computed(() => taskStore.currentTask)

const formatTime = (timeStr: string) => {
  return new Date(timeStr).toLocaleString('zh-CN')
}

const formatLogTime = (timeStr: string) => {
  return new Date(timeStr).toLocaleTimeString('zh-CN')
}

const formatMemory = (bytes: number) => {
  const sizes = ['B', 'KB', 'MB', 'GB']
  if (bytes === 0) return '0 B'
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return Math.round(bytes / Math.pow(1024, i) * 100) / 100 + ' ' + sizes[i]
}

const getStatusText = (status: string) => {
  const statusMap: Record<string, string> = {
    pending: '等待中',
    running: '运行中', 
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return statusMap[status] || status
}

const getStatusTagType = (status: string) => {
  const typeMap: Record<string, string> = {
    pending: 'info',
    running: 'success',
    completed: 'success', 
    failed: 'danger',
    cancelled: 'warning'
  }
  return typeMap[status] || 'info'
}

const getProgressStatus = (status: string) => {
  if (status === 'failed') return 'exception'
  if (status === 'completed') return 'success'
  return undefined
}

const handleRefresh = async () => {
  refreshing.value = true
  try {
    await taskStore.fetchTask(props.taskId)
  } finally {
    refreshing.value = false
  }
}

const handleCancelTask = async () => {
  await taskStore.cancelTask(props.taskId)
  ElMessage.success('任务已取消')
}

const downloadLogs = () => {
  ElMessage.success('日志下载成功')
}

onMounted(() => {
  taskStore.fetchTask(props.taskId)
})
</script>

<style scoped>
.task-status {
  padding: 24px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.task-content {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.progress-section h3,
.resource-section h3,
.logs-section h3 {
  margin: 0 0 16px 0;
  color: #303133;
  font-size: 16px;
}

.resource-card {
  text-align: center;
  padding: 16px;
}

.resource-title {
  font-size: 14px;
  color: #909399;
  margin-bottom: 12px;
}

.logs-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.logs-container {
  height: 300px;
  overflow-y: auto;
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 16px;
  border-radius: 4px;
  font-family: monospace;
  font-size: 13px;
}

.log-entry {
  display: flex;
  margin-bottom: 4px;
}

.log-time {
  color: #9ca3af;
  margin-right: 8px;
}

.log-level {
  margin-right: 8px;
  font-weight: bold;
  width: 60px;
}

.log-info .log-level { color: #3b82f6; }
.log-warning .log-level { color: #f59e0b; }
.log-error .log-level { color: #ef4444; }

.no-logs {
  text-align: center;
  color: #9ca3af;
  padding: 40px;
}
</style>
