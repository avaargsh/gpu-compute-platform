import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useTaskStore } from './taskStore'
import { taskApi } from '@/services/api'
import type { Task, TaskSubmitData, PaginatedResponse, TaskLog } from '@/types'

// Mock the API
vi.mock('@/services/api', () => ({
  taskApi: {
    getTasks: vi.fn(),
    getTask: vi.fn(),
    submitTask: vi.fn(),
    cancelTask: vi.fn(),
  }
}))

const mockTask: Task = {
  id: 'task-1',
  name: 'Test Task',
  status: 'running',
  progress: 50,
  gpu_usage: 75,
  memory_usage: 2048,
  created_at: '2024-01-01T00:00:00Z',
  started_at: '2024-01-01T00:01:00Z',
  logs: [
    {
      timestamp: '2024-01-01T00:01:00Z',
      level: 'info',
      message: 'Task started'
    }
  ]
}

const mockTasksResponse: PaginatedResponse<Task> = {
  items: [mockTask],
  total: 1,
  page: 1,
  per_page: 20,
  pages: 1
}

describe('TaskStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  describe('initial state', () => {
    it('should have correct initial state', () => {
      const taskStore = useTaskStore()
      
      expect(taskStore.tasks).toEqual([])
      expect(taskStore.currentTask).toBeNull()
      expect(taskStore.loading).toBe(false)
      expect(taskStore.error).toBeNull()
      expect(taskStore.pagination).toEqual({
        page: 1,
        per_page: 20,
        total: 0,
        pages: 0
      })
    })
  })

  describe('computed properties', () => {
    it('should filter running tasks correctly', () => {
      const taskStore = useTaskStore()
      taskStore.tasks = [
        { ...mockTask, id: '1', status: 'running' },
        { ...mockTask, id: '2', status: 'completed' },
        { ...mockTask, id: '3', status: 'running' }
      ]

      expect(taskStore.runningTasks).toHaveLength(2)
      expect(taskStore.runningTasks.every(task => task.status === 'running')).toBe(true)
    })

    it('should filter completed tasks correctly', () => {
      const taskStore = useTaskStore()
      taskStore.tasks = [
        { ...mockTask, id: '1', status: 'running' },
        { ...mockTask, id: '2', status: 'completed' },
        { ...mockTask, id: '3', status: 'completed' }
      ]

      expect(taskStore.completedTasks).toHaveLength(2)
      expect(taskStore.completedTasks.every(task => task.status === 'completed')).toBe(true)
    })

    it('should filter failed tasks correctly', () => {
      const taskStore = useTaskStore()
      taskStore.tasks = [
        { ...mockTask, id: '1', status: 'running' },
        { ...mockTask, id: '2', status: 'failed' },
        { ...mockTask, id: '3', status: 'failed' }
      ]

      expect(taskStore.failedTasks).toHaveLength(2)
      expect(taskStore.failedTasks.every(task => task.status === 'failed')).toBe(true)
    })

    it('should calculate total GPU usage correctly', () => {
      const taskStore = useTaskStore()
      taskStore.tasks = [
        { ...mockTask, id: '1', status: 'running', gpu_usage: 80 },
        { ...mockTask, id: '2', status: 'running', gpu_usage: 60 },
        { ...mockTask, id: '3', status: 'completed', gpu_usage: 40 }
      ]

      expect(taskStore.totalGpuUsage).toBe(70) // (80 + 60) / 2
    })

    it('should return 0 for total GPU usage when no running tasks', () => {
      const taskStore = useTaskStore()
      taskStore.tasks = [
        { ...mockTask, id: '1', status: 'completed', gpu_usage: 80 },
        { ...mockTask, id: '2', status: 'failed', gpu_usage: 60 }
      ]

      expect(taskStore.totalGpuUsage).toBe(0)
    })
  })

  describe('fetchTasks', () => {
    it('should fetch tasks successfully', async () => {
      const mockResponse = { data: mockTasksResponse }
      vi.mocked(taskApi.getTasks).mockResolvedValue(mockResponse)

      const taskStore = useTaskStore()
      await taskStore.fetchTasks(1, 10)

      expect(taskApi.getTasks).toHaveBeenCalledWith({ page: 1, per_page: 10 })
      expect(taskStore.tasks).toEqual([mockTask])
      expect(taskStore.pagination).toEqual(mockTasksResponse)
      expect(taskStore.loading).toBe(false)
      expect(taskStore.error).toBeNull()
    })

    it('should handle fetch tasks error', async () => {
      const errorMessage = 'Network error'
      vi.mocked(taskApi.getTasks).mockRejectedValue(new Error(errorMessage))

      const taskStore = useTaskStore()
      await taskStore.fetchTasks()

      expect(taskStore.error).toBe(errorMessage)
      expect(taskStore.loading).toBe(false)
      expect(taskStore.tasks).toEqual([])
    })
  })

  describe('fetchTask', () => {
    it('should fetch single task successfully', async () => {
      const mockResponse = { data: mockTask }
      vi.mocked(taskApi.getTask).mockResolvedValue(mockResponse)

      const taskStore = useTaskStore()
      taskStore.tasks = [{ ...mockTask, progress: 30 }] // existing task with different data
      
      await taskStore.fetchTask('task-1')

      expect(taskApi.getTask).toHaveBeenCalledWith('task-1')
      expect(taskStore.currentTask).toEqual(mockTask)
      expect(taskStore.tasks[0]).toEqual(mockTask) // should update existing task
    })

    it('should handle fetch task error', async () => {
      const errorMessage = 'Task not found'
      vi.mocked(taskApi.getTask).mockRejectedValue(new Error(errorMessage))

      const taskStore = useTaskStore()
      await taskStore.fetchTask('task-1')

      expect(taskStore.error).toBe(errorMessage)
      expect(taskStore.currentTask).toBeNull()
    })
  })

  describe('submitTask', () => {
    it('should submit task successfully', async () => {
      const taskData: TaskSubmitData = {
        name: 'New Task',
        script_path: '/path/to/script.py',
        requirements: ['torch', 'numpy']
      }
      const newTask = { ...mockTask, id: 'new-task', name: 'New Task' }
      const mockResponse = { data: newTask }
      vi.mocked(taskApi.submitTask).mockResolvedValue(mockResponse)

      const taskStore = useTaskStore()
      const result = await taskStore.submitTask(taskData)

      expect(taskApi.submitTask).toHaveBeenCalledWith(taskData)
      expect(result).toEqual(newTask)
      expect(taskStore.tasks[0]).toEqual(newTask)
      expect(taskStore.loading).toBe(false)
    })

    it('should handle submit task error', async () => {
      const taskData: TaskSubmitData = {
        name: 'New Task',
        script_path: '/path/to/script.py',
        requirements: []
      }
      const errorMessage = 'Submission failed'
      vi.mocked(taskApi.submitTask).mockRejectedValue(new Error(errorMessage))

      const taskStore = useTaskStore()
      
      await expect(taskStore.submitTask(taskData)).rejects.toThrow(errorMessage)
      expect(taskStore.error).toBe(errorMessage)
    })
  })

  describe('cancelTask', () => {
    it('should cancel task successfully', async () => {
      vi.mocked(taskApi.cancelTask).mockResolvedValue({ success: true })

      const taskStore = useTaskStore()
      taskStore.tasks = [{ ...mockTask, status: 'running' }]
      taskStore.currentTask = { ...mockTask, status: 'running' }

      await taskStore.cancelTask('task-1')

      expect(taskApi.cancelTask).toHaveBeenCalledWith('task-1')
      expect(taskStore.tasks[0].status).toBe('cancelled')
      expect(taskStore.currentTask?.status).toBe('cancelled')
    })

    it('should handle cancel task error', async () => {
      const errorMessage = 'Cancel failed'
      vi.mocked(taskApi.cancelTask).mockRejectedValue(new Error(errorMessage))

      const taskStore = useTaskStore()
      
      await expect(taskStore.cancelTask('task-1')).rejects.toThrow(errorMessage)
      expect(taskStore.error).toBe(errorMessage)
    })
  })

  describe('updateTaskStatus', () => {
    it('should update task status in tasks array', () => {
      const taskStore = useTaskStore()
      taskStore.tasks = [{ ...mockTask, status: 'running', progress: 30 }]

      taskStore.updateTaskStatus('task-1', { status: 'completed', progress: 100 })

      expect(taskStore.tasks[0].status).toBe('completed')
      expect(taskStore.tasks[0].progress).toBe(100)
    })

    it('should update currentTask if it matches', () => {
      const taskStore = useTaskStore()
      taskStore.currentTask = { ...mockTask, status: 'running', progress: 30 }

      taskStore.updateTaskStatus('task-1', { status: 'completed', progress: 100 })

      expect(taskStore.currentTask?.status).toBe('completed')
      expect(taskStore.currentTask?.progress).toBe(100)
    })
  })

  describe('addTaskLog', () => {
    it('should add log to task in tasks array', () => {
      const taskStore = useTaskStore()
      const initialLogs = [mockTask.logs[0]]
      taskStore.tasks = [{ ...mockTask, logs: [...initialLogs] }]

      const newLog: TaskLog = {
        timestamp: '2024-01-01T00:02:00Z',
        level: 'info',
        message: 'Progress update'
      }

      taskStore.addTaskLog('task-1', newLog)

      expect(taskStore.tasks[0].logs).toHaveLength(2)
      expect(taskStore.tasks[0].logs[1]).toEqual(newLog)
    })

    it('should add log to currentTask if it matches', () => {
      const taskStore = useTaskStore()
      const initialLogs = [mockTask.logs[0]]
      taskStore.currentTask = { ...mockTask, logs: [...initialLogs] }

      const newLog: TaskLog = {
        timestamp: '2024-01-01T00:02:00Z',
        level: 'warning',
        message: 'Warning message'
      }

      taskStore.addTaskLog('task-1', newLog)

      expect(taskStore.currentTask?.logs).toHaveLength(2)
      expect(taskStore.currentTask?.logs[1]).toEqual(newLog)
    })
  })

  describe('clearError', () => {
    it('should clear error state', () => {
      const taskStore = useTaskStore()
      taskStore.error = 'Some error'

      taskStore.clearError()

      expect(taskStore.error).toBeNull()
    })
  })
})
