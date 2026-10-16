import { apiClient } from '@/lib/axios';
import type {
  Task,
  TaskWithUser,
  TaskSubmitData,
  TaskCreateForm,
  PaginatedResponse,
} from '@/types';

export const taskApi = {
  getTasks: async (params?: {
    page?: number;
    per_page?: number;
    status?: string;
    provider?: string;
    search?: string;
    user_id?: string;
    sort_by?: 'created_at' | 'cost' | 'status';
    sort_order?: 'asc' | 'desc';
  }): Promise<PaginatedResponse<TaskWithUser>> => {
    const res = await apiClient.get<PaginatedResponse<TaskWithUser>>('/tasks', { params });
    return res.data;
  },

  getTask: async (taskId: string): Promise<TaskWithUser> => {
    const res = await apiClient.get<TaskWithUser>(`/tasks/${taskId}`);
    return res.data;
  },

  createTask: async (taskData: TaskCreateForm): Promise<TaskWithUser> => {
    const res = await apiClient.post<TaskWithUser>('/tasks', taskData);
    return res.data;
  },

  submitTask: async (taskData: TaskSubmitData): Promise<Task> => {
    const res = await apiClient.post<Task>('/tasks', taskData);
    return res.data;
  },

  cancelTask: async (taskId: string): Promise<void> => {
    await apiClient.post<void>(`/tasks/${taskId}/cancel`);
  },

  deleteTask: async (taskId: string): Promise<void> => {
    await apiClient.delete<void>(`/tasks/${taskId}`);
  },

  getTaskLogs: async (taskId: string): Promise<any> => {
    const res = await apiClient.get<any>(`/tasks/${taskId}/logs`);
    return res.data;
  },

  restartTask: async (taskId: string): Promise<TaskWithUser> => {
    const res = await apiClient.post<TaskWithUser>(`/tasks/${taskId}/restart`);
    return res.data;
  },

  getTaskStats: async (userId?: string): Promise<any> => {
    const params = userId ? { user_id: userId } : {};
    const res = await apiClient.get<any>('/tasks/stats', { params });
    return res.data;
  },
};
