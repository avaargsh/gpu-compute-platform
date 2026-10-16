export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';

export interface TaskLog {
  timestamp: string;
  level: 'info' | 'warning' | 'error';
  message: string;
}

export interface Task {
  id: string;
  name: string;
  status: TaskStatus;
  progress: number;
  gpu_usage: number;
  memory_usage: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
  logs: TaskLog[];
}

export interface TaskSubmitData {
  name: string;
  script_path: string;
  requirements: string[];
  gpu_type?: string;
  max_duration?: number;
}

export interface TaskWithUser extends Task {
  user_id: string;
  user_name: string;
  provider: 'alibaba' | 'tencent' | 'runpod';
  gpu_model: string;
  image: string;
  script: string;
  dataset_path?: string;
  scheduling_strategy: 'cost' | 'performance' | 'availability';
  estimated_cost: number;
  actual_cost?: number;
  submitted_at: string;
}

export interface TaskCreateForm {
  name: string;
  provider: 'alibaba' | 'tencent' | 'runpod';
  gpu_model: string;
  image: string;
  script: string;
  dataset_path?: string;
  scheduling_strategy: 'cost' | 'performance' | 'availability';
  max_duration?: number | null;
  budget_limit?: number | null;
  environment_vars?: Record<string, string>;
  requirements?: string[];
}
