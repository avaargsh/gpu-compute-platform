import { create } from 'zustand';
import { devtools } from 'zustand/middleware';
import type { GPUMetrics } from '@/types';

interface GpuState {
  metrics: GPUMetrics | null;
  connected: boolean;
  setMetrics: (metrics: GPUMetrics | null) => void;
  setConnected: (connected: boolean) => void;
}

export const useGpuStore = create<GpuState>()(
  devtools((set) => ({
    metrics: null,
    connected: false,
    setMetrics: (metrics) => set({ metrics }),
    setConnected: (connected) => set({ connected }),
  }))
);
