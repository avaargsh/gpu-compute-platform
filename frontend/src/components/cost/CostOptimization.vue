<template>
  <div class="cost-optimization">
    <el-card shadow="hover">
      <template #header>
        <div class="card-header">
          <span>成本优化与实例推荐</span>
          <el-button size="small" @click="handleRefresh" :loading="loading">
            <el-icon><Refresh /></el-icon>
            刷新
          </el-button>
        </div>
      </template>

      <!-- 成本分析概览 -->
      <div v-if="costAnalysis" class="cost-overview">
        <el-row :gutter="16">
          <el-col :span="8">
            <div class="cost-metric">
              <div class="metric-value">${{ costAnalysis.current_cost.toFixed(2) }}</div>
              <div class="metric-label">当前成本</div>
            </div>
          </el-col>
          <el-col :span="8">
            <div class="cost-metric">
              <div class="metric-value">${{ costAnalysis.optimized_cost.toFixed(2) }}</div>
              <div class="metric-label">优化后成本</div>
            </div>
          </el-col>
          <el-col :span="8">
            <div class="cost-metric savings">
              <div class="metric-value">
                ${{ costAnalysis.savings.toFixed(2) }}
                <span class="percentage">({{ savingsPercentage.toFixed(1) }}%)</span>
              </div>
              <div class="metric-label">节省成本</div>
            </div>
          </el-col>
        </el-row>
      </div>

      <!-- 筛选选项 -->
      <div class="filter-section">
        <el-row :gutter="16" align="middle">
          <el-col :span="4">
            <el-select v-model="filters.priority" placeholder="优先级" clearable>
              <el-option label="成本优先" value="cost" />
              <el-option label="性能优先" value="performance" />
              <el-option label="平衡" value="balanced" />
            </el-select>
          </el-col>
          <el-col :span="4">
            <el-select v-model="filters.gpuType" placeholder="GPU类型" clearable>
              <el-option 
                v-for="gpuType in gpuTypeOptions" 
                :key="gpuType" 
                :label="gpuType" 
                :value="gpuType" 
              />
            </el-select>
          </el-col>
          <el-col :span="4">
            <el-input-number
              v-model="filters.maxCost"
              placeholder="最大成本"
              :min="0"
              :precision="2"
              size="default"
            />
          </el-col>
          <el-col :span="4">
            <el-select v-model="filters.availability" placeholder="可用性" clearable>
              <el-option label="高可用" value="high" />
              <el-option label="中等可用" value="medium" />
              <el-option label="低可用" value="low" />
            </el-select>
          </el-col>
          <el-col :span="4">
            <el-button type="primary" @click="applyFilters">
              <el-icon><Search /></el-icon>
              筛选
            </el-button>
          </el-col>
        </el-row>
      </div>

      <!-- 推荐视图切换 -->
      <div class="view-tabs">
        <el-radio-group v-model="currentView">
          <el-radio-button label="table">表格视图</el-radio-button>
          <el-radio-button label="chart">图表分析</el-radio-button>
          <el-radio-button label="comparison">对比分析</el-radio-button>
        </el-radio-group>
      </div>

      <!-- 表格视图 -->
      <div v-if="currentView === 'table'" class="table-view">
        <el-table 
          :data="filteredRecommendations" 
          style="width: 100%"
          @selection-change="handleSelectionChange"
        >
          <el-table-column type="selection" width="55" />
          <el-table-column prop="name" label="实例名称" width="150">
            <template #default="{ row }">
              <div class="instance-name">
                <strong>{{ row.name }}</strong>
                <el-tag 
                  :type="getAvailabilityTagType(row.availability)" 
                  size="small"
                  style="margin-left: 8px"
                >
                  {{ getAvailabilityText(row.availability) }}
                </el-tag>
              </div>
            </template>
          </el-table-column>
          
          <el-table-column label="配置" width="200">
            <template #default="{ row }">
              <div class="instance-config">
                <div>{{ row.gpu_type }} × {{ row.gpu_count }}</div>
                <div class="config-details">{{ row.vcpus }}核 / {{ row.memory_gb }}GB / {{ row.storage_gb }}GB</div>
              </div>
            </template>
          </el-table-column>

          <el-table-column prop="cost_per_hour" label="单价" width="100">
            <template #default="{ row }">
              ${{ row.cost_per_hour.toFixed(3) }}/h
            </template>
          </el-table-column>

          <el-table-column prop="estimated_runtime_hours" label="预估时长" width="100">
            <template #default="{ row }">
              {{ row.estimated_runtime_hours }}h
            </template>
          </el-table-column>

          <el-table-column prop="total_cost" label="总成本" width="100">
            <template #default="{ row }">
              ${{ row.total_cost.toFixed(2) }}
            </template>
          </el-table-column>

          <el-table-column label="性能评分" width="120">
            <template #default="{ row }">
              <el-progress 
                :percentage="row.performance_score" 
                :stroke-width="6"
                :show-text="false"
              />
              <span style="margin-left: 8px">{{ row.performance_score }}</span>
            </template>
          </el-table-column>

          <el-table-column label="性价比" width="100">
            <template #default="{ row }">
              {{ getValueScore(row).toFixed(1) }}
            </template>
          </el-table-column>

          <el-table-column label="操作" width="120" fixed="right">
            <template #default="{ row }">
              <el-button size="small" type="primary" @click="selectInstance(row)">
                选择
              </el-button>
              <el-button size="small" @click="viewDetails(row)">
                详情
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 图表分析视图 -->
      <div v-if="currentView === 'chart'" class="chart-view">
        <el-row :gutter="16">
          <el-col :span="12">
            <div ref="costChartRef" class="chart-container"></div>
          </el-col>
          <el-col :span="12">
            <div ref="performanceChartRef" class="chart-container"></div>
          </el-col>
        </el-row>
        <div style="margin-top: 16px">
          <div ref="scatterChartRef" class="chart-container"></div>
        </div>
      </div>

      <!-- 对比分析视图 -->
      <div v-if="currentView === 'comparison'" class="comparison-view">
        <div v-if="selectedInstances.length === 0" class="no-selection">
          <el-empty description="请在表格视图中选择要对比的实例" />
        </div>
        <div v-else>
          <h3>实例对比分析</h3>
          <el-table :data="selectedInstances" style="width: 100%">
            <el-table-column prop="name" label="实例名称" width="150" />
            <el-table-column label="配置">
              <template #default="{ row }">
                {{ row.gpu_type }} × {{ row.gpu_count }}, {{ row.vcpus }}核, {{ row.memory_gb }}GB
              </template>
            </el-table-column>
            <el-table-column prop="total_cost" label="总成本" width="100">
              <template #default="{ row }">
                ${{ row.total_cost.toFixed(2) }}
              </template>
            </el-table-column>
            <el-table-column prop="performance_score" label="性能评分" width="100" />
            <el-table-column label="性价比" width="100">
              <template #default="{ row }">
                {{ getValueScore(row).toFixed(1) }}
              </template>
            </el-table-column>
            <el-table-column label="推荐程度" width="120">
              <template #default="{ row }">
                <el-rate 
                  :model-value="getRecommendationRating(row)"
                  disabled
                  :max="5"
                  size="small"
                />
              </template>
            </el-table-column>
          </el-table>
          
          <!-- 对比图表 -->
          <div style="margin-top: 24px">
            <div ref="comparisonChartRef" class="chart-container"></div>
          </div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import * as echarts from 'echarts'
import { ElMessage } from 'element-plus'
import { useCostStore } from '@/stores/costStore'
import type { InstanceRecommendation } from '@/types'

const costStore = useCostStore()

// 响应式数据
const currentView = ref('table')
const selectedInstances = ref<InstanceRecommendation[]>([])
const filters = ref({
  priority: '',
  gpuType: '',
  maxCost: null as number | null,
  availability: ''
})

// 图表引用
const costChartRef = ref<HTMLElement>()
const performanceChartRef = ref<HTMLElement>()
const scatterChartRef = ref<HTMLElement>()
const comparisonChartRef = ref<HTMLElement>()

// 图表实例
let costChart: echarts.ECharts | null = null
let performanceChart: echarts.ECharts | null = null
let scatterChart: echarts.ECharts | null = null
let comparisonChart: echarts.ECharts | null = null

// 计算属性
const { 
  recommendations,
  costAnalysis,
  loading,
  gpuTypeOptions,
  savingsPercentage
} = costStore

const filteredRecommendations = computed(() => {
  if (!filters.value.gpuType && !filters.value.maxCost && !filters.value.availability) {
    return recommendations.value
  }
  
  return costStore.filterRecommendations({
    maxCost: filters.value.maxCost || undefined,
    gpuTypes: filters.value.gpuType ? [filters.value.gpuType] : undefined,
    availability: filters.value.availability ? [filters.value.availability as any] : undefined
  })
})

// 方法
const getAvailabilityText = (availability: string) => {
  const map: Record<string, string> = {
    high: '高',
    medium: '中',
    low: '低'
  }
  return map[availability] || availability
}

const getAvailabilityTagType = (availability: string) => {
  const map: Record<string, string> = {
    high: 'success',
    medium: 'warning',
    low: 'danger'
  }
  return map[availability] || 'info'
}

const getValueScore = (instance: InstanceRecommendation) => {
  return costStore.getValueScore(instance)
}

const getRecommendationRating = (instance: InstanceRecommendation) => {
  // 基于性价比和性能评分计算推荐程度
  const valueScore = getValueScore(instance)
  const normalizedValue = Math.min(valueScore / 20, 1) // 假设20为满分性价比
  const normalizedPerformance = instance.performance_score / 100
  return Math.round((normalizedValue + normalizedPerformance) * 2.5)
}

const handleRefresh = async () => {
  await Promise.all([
    costStore.fetchRecommendations(),
    costStore.fetchCostAnalysis()
  ])
}

const applyFilters = () => {
  // 筛选逻辑已在计算属性中处理
  ElMessage.success('筛选已应用')
}

const handleSelectionChange = (selection: InstanceRecommendation[]) => {
  selectedInstances.value = selection
}

const selectInstance = (instance: InstanceRecommendation) => {
  ElMessage.success(`已选择实例: ${instance.name}`)
  // TODO: 实现选择实例的逻辑
}

const viewDetails = (instance: InstanceRecommendation) => {
  ElMessage.info(`查看实例详情: ${instance.name}`)
  // TODO: 实现查看详情的逻辑
}

// 图表初始化和更新
const initCharts = async () => {
  await nextTick()
  
  // 成本分布图
  if (costChartRef.value && !costChart) {
    costChart = echarts.init(costChartRef.value)
    updateCostChart()
  }
  
  // 性能分布图
  if (performanceChartRef.value && !performanceChart) {
    performanceChart = echarts.init(performanceChartRef.value)
    updatePerformanceChart()
  }
  
  // 散点图
  if (scatterChartRef.value && !scatterChart) {
    scatterChart = echarts.init(scatterChartRef.value)
    updateScatterChart()
  }
  
  // 对比图表
  if (comparisonChartRef.value && !comparisonChart) {
    comparisonChart = echarts.init(comparisonChartRef.value)
    updateComparisonChart()
  }
}

const updateCostChart = () => {
  if (!costChart || !recommendations.value.length) return
  
  const option = {
    title: {
      text: '成本分布',
      left: 'center'
    },
    tooltip: {
      trigger: 'item',
      formatter: '{a} <br/>{b} : ${c} ({d}%)'
    },
    series: [{
      name: '成本分布',
      type: 'pie',
      radius: '50%',
      data: recommendations.value.map(item => ({
        value: item.total_cost,
        name: item.name
      }))
    }]
  }
  
  costChart.setOption(option)
}

const updatePerformanceChart = () => {
  if (!performanceChart || !recommendations.value.length) return
  
  const option = {
    title: {
      text: '性能评分分布',
      left: 'center'
    },
    tooltip: {
      trigger: 'axis'
    },
    xAxis: {
      type: 'category',
      data: recommendations.value.map(item => item.name),
      axisLabel: {
        rotate: 45
      }
    },
    yAxis: {
      type: 'value',
      name: '性能评分'
    },
    series: [{
      data: recommendations.value.map(item => item.performance_score),
      type: 'bar',
      itemStyle: {
        color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
          { offset: 0, color: '#83bff6' },
          { offset: 0.5, color: '#188df0' },
          { offset: 1, color: '#188df0' }
        ])
      }
    }]
  }
  
  performanceChart.setOption(option)
}

const updateScatterChart = () => {
  if (!scatterChart || !recommendations.value.length) return
  
  const option = {
    title: {
      text: '成本 vs 性能散点图',
      left: 'center'
    },
    tooltip: {
      trigger: 'item',
      formatter: (params: any) => {
        const item = recommendations.value[params.dataIndex]
        return `${item.name}<br/>成本: $${item.total_cost.toFixed(2)}<br/>性能: ${item.performance_score}`
      }
    },
    xAxis: {
      type: 'value',
      name: '总成本 ($)',
      scale: true
    },
    yAxis: {
      type: 'value',
      name: '性能评分',
      scale: true
    },
    series: [{
      symbolSize: 20,
      data: recommendations.value.map(item => [item.total_cost, item.performance_score]),
      type: 'scatter'
    }]
  }
  
  scatterChart.setOption(option)
}

const updateComparisonChart = () => {
  if (!comparisonChart || !selectedInstances.value.length) return
  
  const option = {
    title: {
      text: '实例对比',
      left: 'center'
    },
    tooltip: {
      trigger: 'axis'
    },
    legend: {
      data: ['成本', '性能评分', '性价比'],
      top: 30
    },
    radar: {
      indicator: selectedInstances.value.map(item => ({
        name: item.name,
        max: 100
      }))
    },
    series: [{
      type: 'radar',
      data: [
        {
          value: selectedInstances.value.map(item => (item.total_cost / Math.max(...selectedInstances.value.map(i => i.total_cost))) * 100),
          name: '成本（相对）'
        },
        {
          value: selectedInstances.value.map(item => item.performance_score),
          name: '性能评分'
        },
        {
          value: selectedInstances.value.map(item => Math.min(getValueScore(item) * 5, 100)),
          name: '性价比'
        }
      ]
    }]
  }
  
  comparisonChart.setOption(option)
}

// 监听视图变化
watch(currentView, async (newView) => {
  if (newView === 'chart') {
    await initCharts()
  }
})

// 监听选中实例变化
watch(selectedInstances, () => {
  if (currentView.value === 'comparison') {
    updateComparisonChart()
  }
}, { deep: true })

// 组件生命周期
onMounted(async () => {
  await handleRefresh()
  if (currentView.value === 'chart') {
    await initCharts()
  }
  
  // 监听窗口大小变化
  window.addEventListener('resize', () => {
    costChart?.resize()
    performanceChart?.resize()
    scatterChart?.resize()
    comparisonChart?.resize()
  })
})
</script>

<style scoped>
.cost-optimization {
  padding: 24px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.cost-overview {
  margin-bottom: 24px;
  padding: 16px;
  background: #f8f9fa;
  border-radius: 8px;
}

.cost-metric {
  text-align: center;
}

.cost-metric.savings {
  color: #67C23A;
}

.metric-value {
  font-size: 24px;
  font-weight: 600;
  margin-bottom: 4px;
}

.percentage {
  font-size: 14px;
  margin-left: 8px;
}

.metric-label {
  font-size: 12px;
  color: #909399;
}

.filter-section {
  margin-bottom: 24px;
  padding: 16px;
  background: #fff;
  border: 1px solid #EBEEF5;
  border-radius: 8px;
}

.view-tabs {
  margin-bottom: 24px;
}

.instance-name {
  display: flex;
  align-items: center;
}

.instance-config .config-details {
  font-size: 12px;
  color: #909399;
}

.chart-container {
  height: 400px;
  width: 100%;
}

.comparison-view h3 {
  margin: 0 0 16px 0;
  color: #303133;
}

.no-selection {
  text-align: center;
  padding: 40px;
}

:deep(.el-table th) {
  background-color: #fafafa;
}

:deep(.el-empty) {
  padding: 40px;
}
</style>
