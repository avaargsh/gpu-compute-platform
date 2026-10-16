'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { taskApi } from '@/services/taskApi';
import { queryKeys } from '@/lib/queryKeys';
import type { TaskCreateForm, TaskWithUser } from '@/types';

export function useTasksQuery(params?: Record<string, unknown>) {
  return useQuery({
    queryKey: queryKeys.tasks.list(params),
    queryFn: async () => {
      const res = await taskApi.getTasks(params as any);
      return res.items;
    },
  });
}

export function useTaskQuery(taskId: string) {
  return useQuery({
    queryKey: queryKeys.tasks.detail(taskId),
    queryFn: async () => taskApi.getTask(taskId),
    enabled: !!taskId,
  });
}

export function useCreateTaskMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: TaskCreateForm) => taskApi.createTask(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all });
    },
  });
}

export function useCancelTaskMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (taskId: string) => taskApi.cancelTask(taskId),
    onMutate: async (_taskId) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.tasks.all });
      const previous = queryClient.getQueryData<TaskWithUser[]>(queryKeys.tasks.list());
      queryClient.setQueryData<TaskWithUser[]>(queryKeys.tasks.list(), (old) =>
        old?.map((t) => (t.id === _taskId ? { ...t, status: 'cancelled' as const } : t)) ?? []
      );
      return { previous };
    },
    onError: (_err, _taskId, context) => {
      queryClient.setQueryData(queryKeys.tasks.list(), context?.previous);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all });
    },
  });
}

export function useRestartTaskMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (taskId: string) => taskApi.restartTask(taskId),
    onMutate: async (_taskId) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.tasks.all });
      const previous = queryClient.getQueryData<TaskWithUser[]>(queryKeys.tasks.list());
      queryClient.setQueryData<TaskWithUser[]>(queryKeys.tasks.list(), (old) =>
        old?.map((t) => (t.id === _taskId ? { ...t, status: 'running' as const } : t)) ?? []
      );
      return { previous };
    },
    onError: (_err, _taskId, context) => {
      queryClient.setQueryData(queryKeys.tasks.list(), context?.previous);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all });
    },
  });
}

export function useDeleteTaskMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (taskId: string) => taskApi.deleteTask(taskId),
    onMutate: async (_taskId) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.tasks.all });
      const previous = queryClient.getQueryData<TaskWithUser[]>(queryKeys.tasks.list());
      queryClient.setQueryData<TaskWithUser[]>(queryKeys.tasks.list(), (old) =>
        old?.filter((t) => t.id !== _taskId) ?? []
      );
      return { previous };
    },
    onError: (_err, _taskId, context) => {
      queryClient.setQueryData(queryKeys.tasks.list(), context?.previous);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all });
    },
  });
}
