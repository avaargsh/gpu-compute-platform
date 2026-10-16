import { describe, it, expect, beforeEach } from 'vitest'
import { useTaskStore } from '@/stores/useTaskStore'
import type { Task } from '@/types'

const mockTask: Task = {
  id: 'task-1',
  name: 'Test Task',
  status: 'pending',
  progress: 0,
  gpu_usage: 0,
  memory_usage: 0,
  created_at: '2024-01-01T00:00:00Z',
  logs: [],
}

describe('useTaskStore', () => {
  beforeEach(() => {
    useTaskStore.setState({ tasks: [], currentTask: null })
  })

  it('should set tasks', () => {
    useTaskStore.getState().setTasks([mockTask])
    expect(useTaskStore.getState().tasks).toHaveLength(1)
    expect(useTaskStore.getState().tasks[0].id).toBe('task-1')
  })

  it('should set current task', () => {
    useTaskStore.getState().setCurrentTask(mockTask)
    expect(useTaskStore.getState().currentTask?.id).toBe('task-1')
  })

  it('should update task status', () => {
    useTaskStore.getState().setTasks([mockTask])
    useTaskStore.getState().setCurrentTask(mockTask)
    useTaskStore.getState().setTaskStatus('task-1', 'running')

    const state = useTaskStore.getState()
    expect(state.tasks[0].status).toBe('running')
    expect(state.currentTask?.status).toBe('running')
  })
})
