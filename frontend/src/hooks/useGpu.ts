'use client';

import { useQuery } from '@tanstack/react-query';
import { gpuApi } from '@/services/gpuApi';
import { queryKeys } from '@/lib/queryKeys';

export function useGpuMetricsQuery() {
  return useQuery({
    queryKey: queryKeys.gpu.metrics,
    queryFn: async () => gpuApi.getMetrics(),
    refetchInterval: 5000,
    refetchIntervalInBackground: true,
  });
}

export function useGpuHistoryQuery(params?: Record<string, unknown>) {
  return useQuery({
    queryKey: queryKeys.gpu.history(params),
    queryFn: async () => gpuApi.getHistoryData(params as any),
    enabled: !!params,
  });
}

export function useGpuInfoQuery() {
  return useQuery({
    queryKey: queryKeys.gpu.info,
    queryFn: async () => gpuApi.getGpuInfo(),
  });
}
