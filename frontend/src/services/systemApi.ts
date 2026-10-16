import { apiClient } from '@/lib/axios';

export const systemApi = {
  getStatus: async (): Promise<any> => {
    const res = await apiClient.get<any>('/system/status');
    return res.data;
  },

  getConfig: async (): Promise<any> => {
    const res = await apiClient.get<any>('/system/config');
    return res.data;
  },

  healthCheck: async (): Promise<any> => {
    const res = await apiClient.get<any>('/health');
    return res.data;
  },
};
