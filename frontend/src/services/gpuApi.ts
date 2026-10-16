import { apiClient } from '@/lib/axios';
import type { GPUMetrics } from '@/types';

export const gpuApi = {
  getMetrics: async (): Promise<GPUMetrics> => {
    const res = await apiClient.get<GPUMetrics>('/gpu/metrics');
    return res.data;
  },

  getHistoryData: async (params?: {
    gpu_id?: string;
    start_time?: string;
    end_time?: string;
    interval?: string;
  }): Promise<any> => {
    const res = await apiClient.get<any>('/gpu/history', { params });
    return res.data;
  },

  getGpuInfo: async (): Promise<any> => {
    const res = await apiClient.get<any>('/gpu/info');
    return res.data;
  },
};
