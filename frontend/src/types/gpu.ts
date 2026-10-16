export interface GPUInfo {
  id: string;
  name: string;
  usage: number;
  memory_usage: number;
  memory_total: number;
  temperature: number;
  power_usage: number;
  utilization_history: number[];
  memory_history: number[];
}

export interface GPUMetrics {
  timestamp: string;
  gpus: GPUInfo[];
  total_usage: number;
  total_memory_usage: number;
}
