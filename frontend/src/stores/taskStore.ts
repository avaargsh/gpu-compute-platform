import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Task, TaskLog, TaskSubmitData, PaginatedResponse } from '@/types'
import { taskApi } from '@/services/api'

export const useTaskStore = defineStore('tasks', () => {
  // 状态
  const tasks = ref<Task[]>([])
  const currentTask = ref<Task | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const pagination = ref({
    page: 1,
    per_page: 20,
    total: 0,
    pages: 0
  })

  // 计算属性
  const runningTasks = computed(() => 
    tasks.value.filter(task => task.status === 'running')
  )

  const completedTasks = computed(() =>
    tasks.value.filter(task => task.status === 'completed')
  )

  const failedTasks = computed(() =>
    tasks.value.filter(task => task.status === 'failed')
  )

  const totalGpuUsage = computed(() => {
    const runningTasksUsage = runningTasks.value.reduce(
      (sum, task) => sum + task.gpu_usage, 0
    )
    return runningTasksUsage / runningTasks.value.length || 0
  })

  // 方法
  const fetchTasks = async (page = 1, per_page = 20) => {
    loading.value = true
    error.value = null
    try {
      const response = await taskApi.getTasks({ page, per_page })
      tasks.value = response.data.items
      pagination.value = {
        page: response.data.page,
        per_page: response.data.per_page,
        total: response.data.total,
        pages: response.data.pages
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : '获取任务列表失败'
      console.error('Error fetching tasks:', err)
    } finally {
      loading.value = false
    }
  }

  const fetchTask = async (taskId: string) => {
    loading.value = true
    error.value = null
    try {
      const response = await taskApi.getTask(taskId)
      currentTask.value = response.data
      
      // 更新任务列表中的对应任务
      const index = tasks.value.findIndex(task => task.id === taskId)
      if (index !== -1) {
        tasks.value[index] = response.data
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : '获取任务详情失败'
      console.error('Error fetching task:', err)
    } finally {
      loading.value = false
    }
  }

  const submitTask = async (taskData: TaskSubmitData) => {
    loading.value = true
    error.value = null
    try {
      const response = await taskApi.submitTask(taskData)
      const newTask = response.data
      tasks.value.unshift(newTask)
      return newTask
    } catch (err) {
      error.value = err instanceof Error ? err.message : '提交任务失败'
      console.error('Error submitting task:', err)
      throw err
    } finally {
      loading.value = false
    }
  }

  const cancelTask = async (taskId: string) => {
    loading.value = true
    error.value = null
    try {
      await taskApi.cancelTask(taskId)
      const task = tasks.value.find(t => t.id === taskId)
      if (task) {
        task.status = 'cancelled'
      }
      if (currentTask.value?.id === taskId) {
        currentTask.value.status = 'cancelled'
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : '取消任务失败'
      console.error('Error cancelling task:', err)
      throw err
    } finally {
      loading.value = false
    }
  }

  const updateTaskStatus = (taskId: string, updates: Partial<Task>) => {
    const task = tasks.value.find(t => t.id === taskId)
    if (task) {
      Object.assign(task, updates)
    }
    if (currentTask.value?.id === taskId) {
      Object.assign(currentTask.value, updates)
    }
  }

  const addTaskLog = (taskId: string, log: TaskLog) => {
    const task = tasks.value.find(t => t.id === taskId)
    if (task) {
      task.logs.push(log)
    }
    if (currentTask.value?.id === taskId) {
      currentTask.value.logs.push(log)
    }
  }

  const clearError = () => {
    error.value = null
  }

  return {
    // 状态
    tasks,
    currentTask,
    loading,
    error,
    pagination,
    
    // 计算属性
    runningTasks,
    completedTasks,
    failedTasks,
    totalGpuUsage,
    
    // 方法
    fetchTasks,
    fetchTask,
    submitTask,
    cancelTask,
    updateTaskStatus,
    addTaskLog,
    clearError
  }
})
