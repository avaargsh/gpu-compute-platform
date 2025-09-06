<template>
  <div class="task-list">
    <!-- 操作栏 -->
    <div class="task-actions">
      <el-row :gutter="16" align="middle">
        <el-col :span="6">
          <el-input
            v-model="searchText"
            placeholder="搜索任务..."
            prefix-icon="Search"
            clearable
            @input="handleSearch"
          />
        </el-col>
        <el-col :span="4">
          <el-select
            v-model="statusFilter"
            placeholder="状态筛选"
            clearable
            @change="handleFilter"
          >
            <el-option label="全部" value="" />
            <el-option label="等待中" value="pending" />
            <el-option label="运行中" value="running" />
            <el-option label="已完成" value="completed" />
            <el-option label="失败" value="failed" />
            <el-option label="已取消" value="cancelled" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-button type="primary" @click="handleRefresh" :loading="loading">
            <el-icon><Refresh /></el-icon>
            刷新
          </el-button>
          <el-button type="success" @click="handleCreateTask">
            <el-icon><Plus /></el-icon>
            创建任务
          </el-button>
        </el-col>
        <el-col :span="8" class="text-right">
          <el-tag v-if="runningTasks.length > 0" type="success">
            运行中: {{ runningTasks.length }}
          </el-tag>
          <el-tag v-if="failedTasks.length > 0" type="danger" style="margin-left: 8px">
            失败: {{ failedTasks.length }}
          </el-tag>
        </el-col>
      </el-row>
    </div>

    <!-- 任务表格 -->
    <el-table 
      v-loading="loading"
      :data="tasks"
      style="width: 100%"
      stripe
      @selection-change="handleSelectionChange"
      @row-click="handleRowClick"
    >
      <el-table-column type="selection" width="55" />
      
      <el-table-column prop="name" label="任务名称" min-width="150">
        <template #default="{ row }">
          <div class="task-name">
            <el-link type="primary" @click="handleViewTask(row.id)">
              {{ row.name }}
            </el-link>
            <el-tag 
              :type="getStatusTagType(row.status)"
              size="small"
              style="margin-left: 8px"
            >
              {{ getStatusText(row.status) }}
            </el-tag>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="进度" width="120">
        <template #default="{ row }">
          <el-progress 
            :percentage="row.progress"
            :status="row.status === 'failed' ? 'exception' : row.status === 'completed' ? 'success' : undefined"
            :stroke-width="6"
          />
        </template>
      </el-table-column>

      <el-table-column label="GPU使用率" width="100">
        <template #default="{ row }">
          <span v-if="row.status === 'running'">{{ row.gpu_usage }}%</span>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="内存使用" width="100">
        <template #default="{ row }">
          <span v-if="row.status === 'running'">{{ formatMemory(row.memory_usage) }}</span>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column prop="created_at" label="创建时间" width="160">
        <template #default="{ row }">
          {{ formatTime(row.created_at) }}
        </template>
      </el-table-column>

      <el-table-column label="运行时间" width="120">
        <template #default="{ row }">
          <span v-if="row.started_at">
            {{ getDuration(row.started_at, row.completed_at) }}
          </span>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <el-button
            size="small"
            @click.stop="handleViewTask(row.id)"
          >
            查看详情
          </el-button>
          <el-button
            v-if="row.status === 'running'"
            size="small"
            type="warning"
            @click.stop="handleCancelTask(row.id)"
          >
            取消
          </el-button>
          <el-dropdown
            v-else
            trigger="click"
            @click.stop
          >
            <el-button size="small">
              更多<el-icon><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item 
                  v-if="row.status === 'failed'"
                  @click="handleRestartTask(row.id)"
                >
                  重新运行
                </el-dropdown-item>
                <el-dropdown-item @click="handleDownloadLogs(row.id)">
                  下载日志
                </el-dropdown-item>
                <el-dropdown-item 
                  @click="handleDeleteTask(row.id)"
                  divided
                >
                  删除任务
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.per_page"
        :page-sizes="[10, 20, 50, 100]"
        :total="pagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handlePageSizeChange"
        @current-change="handleCurrentChange"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useTaskStore } from '@/stores/taskStore'
import { wsService } from '@/services/websocket'
import type { Task } from '@/types'

const router = useRouter()
const taskStore = useTaskStore()

// 响应式数据
const searchText = ref('')
const statusFilter = ref('')
const selectedTasks = ref<Task[]>([])

// 计算属性
const { 
  tasks, 
  loading, 
  pagination, 
  runningTasks, 
  failedTasks 
} = taskStore

// 格式化函数
const formatTime = (timeStr: string) => {
  return new Date(timeStr).toLocaleString('zh-CN')
}

const formatMemory = (bytes: number) => {
  const sizes = ['B', 'KB', 'MB', 'GB']
  if (bytes === 0) return '0 B'
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return Math.round(bytes / Math.pow(1024, i) * 100) / 100 + ' ' + sizes[i]
}

const getDuration = (startTime: string, endTime?: string) => {
  const start = new Date(startTime).getTime()
  const end = endTime ? new Date(endTime).getTime() : Date.now()
  const diff = Math.floor((end - start) / 1000)
  
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ${diff % 60}s`
  return `${Math.floor(diff / 3600)}h ${Math.floor((diff % 3600) / 60)}m`
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

// 事件处理
const handleSearch = () => {
  fetchTasks()
}

const handleFilter = () => {
  fetchTasks()
}

const handleRefresh = () => {
  fetchTasks()
}

const handleSelectionChange = (selection: Task[]) => {
  selectedTasks.value = selection
}

const handleRowClick = (row: Task) => {
  handleViewTask(row.id)
}

const handleViewTask = (taskId: string) => {
  router.push(`/tasks/${taskId}`)
}

const handleCreateTask = () => {
  router.push('/tasks/create')
}

const handleCancelTask = async (taskId: string) => {
  try {
    await ElMessageBox.confirm(
      '确认要取消这个任务吗？',
      '确认取消',
      {
        confirmButtonText: '确认',
        cancelButtonText: '取消',
        type: 'warning',
      }
    )
    
    await taskStore.cancelTask(taskId)
    ElMessage.success('任务已取消')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('取消任务失败')
    }
  }
}

const handleRestartTask = async (taskId: string) => {
  try {
    // TODO: 实现重启任务功能
    ElMessage.success('任务重启成功')
  } catch (error) {
    ElMessage.error('重启任务失败')
  }
}

const handleDownloadLogs = (taskId: string) => {
  // TODO: 实现下载日志功能
  ElMessage.info('正在下载日志...')
}

const handleDeleteTask = async (taskId: string) => {
  try {
    await ElMessageBox.confirm(
      '确认要删除这个任务吗？删除后无法恢复。',
      '确认删除',
      {
        confirmButtonText: '确认',
        cancelButtonText: '取消',
        type: 'warning',
      }
    )
    
    // TODO: 实现删除任务功能
    ElMessage.success('任务已删除')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除任务失败')
    }
  }
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.per_page = pageSize
  fetchTasks()
}

const handleCurrentChange = (currentPage: number) => {
  pagination.page = currentPage
  fetchTasks()
}

// 获取任务列表
const fetchTasks = () => {
  taskStore.fetchTasks(pagination.page, pagination.per_page)
}

// WebSocket 事件监听
const setupWebSocketListeners = () => {
  wsService.on('task_update', (message: any) => {
    const { task_id, ...updates } = message.data
    taskStore.updateTaskStatus(task_id, updates)
  })
}

// 组件生命周期
onMounted(async () => {
  await fetchTasks()
  setupWebSocketListeners()
})

onUnmounted(() => {
  wsService.off('task_update')
})
</script>

<style scoped>
.task-list {
  padding: 24px;
}

.task-actions {
  margin-bottom: 24px;
}

.task-name {
  display: flex;
  align-items: center;
}

.text-muted {
  color: #909399;
}

.text-right {
  text-align: right;
}

.pagination-wrapper {
  margin-top: 24px;
  text-align: center;
}

:deep(.el-table__row) {
  cursor: pointer;
}

:deep(.el-table__row:hover) {
  background-color: #f5f7fa;
}
</style>
