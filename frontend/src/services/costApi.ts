import { apiClient } from '@/lib/axios';
import type { InstanceRecommendation, CostAnalysis } from '@/types';

export const costApi = {
  getRecommendations: async (params?: {
    task_type?: string;
    budget_limit?: number;
    duration_estimate?: number;
    performance_priority?: 'cost' | 'performance' | 'balanced';
  }): Promise<InstanceRecommendation[]> => {
    const res = await apiClient.get<InstanceRecommendation[]>('/cost/recommendations', { params });
    return res.data;
  },

  getCostAnalysis: async (taskId?: string): Promise<CostAnalysis> => {
    const url = taskId ? `/cost/analysis/${taskId}` : '/cost/analysis';
    const res = await apiClient.get<CostAnalysis>(url);
    return res.data;
  },

  getPricing: async (): Promise<any> => {
    const res = await apiClient.get<any>('/cost/pricing');
    return res.data;
  },

  compareInstances: async (instanceIds: string[]): Promise<any> => {
    const res = await apiClient.post<any>('/cost/compare', { instance_ids: instanceIds });
    return res.data;
  },
};
