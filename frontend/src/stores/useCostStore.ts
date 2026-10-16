import { create } from 'zustand';
import { devtools } from 'zustand/middleware';
import type { InstanceRecommendation, CostAnalysis } from '@/types';

interface CostState {
  recommendations: InstanceRecommendation[];
  analysis: CostAnalysis | null;
  setRecommendations: (recommendations: InstanceRecommendation[]) => void;
  setAnalysis: (analysis: CostAnalysis | null) => void;
}

export const useCostStore = create<CostState>()(
  devtools((set) => ({
    recommendations: [],
    analysis: null,
    setRecommendations: (recommendations) => set({ recommendations }),
    setAnalysis: (analysis) => set({ analysis }),
  }))
);
