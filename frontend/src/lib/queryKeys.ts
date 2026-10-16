const tasks = {
  all: ['tasks'] as const,
  list: (params?: Record<string, unknown>) => ['tasks', 'list', params] as const,
  detail: (taskId: string) => ['tasks', 'detail', taskId] as const,
  stats: (userId?: string) => ['tasks', 'stats', userId] as const,
}

const gpu = {
  all: ['gpu'] as const,
  metrics: ['gpu', 'metrics'] as const,
  history: (params?: Record<string, unknown>) => ['gpu', 'history', params] as const,
  info: ['gpu', 'info'] as const,
}

const cost = {
  all: ['cost'] as const,
  recommendations: (params?: Record<string, unknown>) => ['cost', 'recommendations', params] as const,
  analysis: (taskId?: string) => ['cost', 'analysis', taskId] as const,
  pricing: ['cost', 'pricing'] as const,
}

const system = {
  all: ['system'] as const,
  status: ['system', 'status'] as const,
  config: ['system', 'config'] as const,
  health: ['system', 'health'] as const,
}

export const queryKeys = {
  tasks,
  gpu,
  cost,
  system,
} as const
