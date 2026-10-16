import { create } from 'zustand';
import { devtools } from 'zustand/middleware';
import type { Task, TaskStatus } from '@/types';

interface TaskState {
  tasks: Task[];
  currentTask: Task | null;
  setTasks: (tasks: Task[]) => void;
  setCurrentTask: (task: Task | null) => void;
  updateTask: (id: string, updates: Partial<Task>) => void;
  setTaskStatus: (id: string, status: TaskStatus) => void;
}

export const useTaskStore = create<TaskState>()(
  devtools((set) => ({
    tasks: [],
    currentTask: null,
    setTasks: (tasks) => set({ tasks }),
    setCurrentTask: (task) => set({ currentTask: task }),
    updateTask: (id, updates) =>
      set((state) => ({
        tasks: state.tasks.map((t) => (t.id === id ? { ...t, ...updates } : t)),
        currentTask:
          state.currentTask?.id === id ? { ...state.currentTask, ...updates } : state.currentTask,
      })),
    setTaskStatus: (id, status) =>
      set((state) => ({
        tasks: state.tasks.map((t) => (t.id === id ? { ...t, status } : t)),
        currentTask:
          state.currentTask?.id === id ? { ...state.currentTask, status } : state.currentTask,
      })),
  }))
);
