export interface InstanceRecommendation {
  id: string;
  name: string;
  gpu_type: string;
  gpu_count: number;
  vcpus: number;
  memory_gb: number;
  storage_gb: number;
  cost_per_hour: number;
  estimated_runtime_hours: number;
  total_cost: number;
  performance_score: number;
  availability: 'high' | 'medium' | 'low';
}

export interface CostAnalysis {
  current_cost: number;
  optimized_cost: number;
  savings: number;
  recommendations: InstanceRecommendation[];
}
