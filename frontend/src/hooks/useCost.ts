'use client';

import { useQuery } from '@tanstack/react-query';
import { costApi } from '@/services/costApi';
import { queryKeys } from '@/lib/queryKeys';

export function useCostRecommendationsQuery(params?: Record<string, unknown>) {
  return useQuery({
    queryKey: queryKeys.cost.recommendations(params),
    queryFn: async () => costApi.getRecommendations(params as any),
  });
}

export function useCostAnalysisQuery(taskId?: string) {
  return useQuery({
    queryKey: queryKeys.cost.analysis(taskId),
    queryFn: async () => costApi.getCostAnalysis(taskId),
    enabled: !!taskId,
  });
}

export function useCostPricingQuery() {
  return useQuery({
    queryKey: queryKeys.cost.pricing,
    queryFn: async () => costApi.getPricing(),
  });
}
