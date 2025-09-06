<template>
  <div class="gpu-usage-chart">
    <el-card shadow="hover">
      <template #header>
        <div class="card-header">
          <span>GPU 资源监控</span>
          <div class="header-actions">
            <el-switch
              v-model="autoRefresh"
              active-text="自动刷新"
              @change="handleAutoRefreshChange"
            />
            <el-button size="small" @click="handleRefresh" :loading="loading">
              <el-icon><Refresh /></el-icon>
            </el-button>
          </div>
        </div>
      </template>

      <!-- 总体概览 -->
      <div class="overview-section">
        <el-row :gutter="16">
          <el-col :span="6">
            <div class="metric-card">
              <div class="metric-value">{{ totalGpuUsage.toFixed(1) }}%</div>
              <div class="metric-label">平均GPU使用率</div>
              <el-progress 
                :percentage="totalGpuUsage" 
                :show-text="false"
                :stroke-width="4"
                :color="getUsageColor(totalGpuUsage)"
              />
            </div>
          </el-col>
          <el-col :span="6">
            <div class="metric-card">
              <div class="metric-value">{{ totalMemoryUsage.toFixed(1) }}%</div>
              <div class="metric-label">平均内存使用率</div>
              <el-progress 
                :percentage="totalMemoryUsage" 
                :show-text="false"
                :stroke-width="4"
                :color="getUsageColor(totalMemoryUsage)"
              />
            </div>
          </el-col>
          <el-col :span="6">
            <div class="metric-card">
              <div class="metric-value">{{ averageTemperature.toFixed(0) }}°C</div>
              <div class="metric-label">平均温度</div>
              <el-progress 
                :percentage="(averageTemperature / 100) * 100" 
                :show-text="false"
                :stroke-width="4"
                :color="getTempColor(averageTemperature)"
              />
            </div>
          </el-col>
          <el-col :span="6">
            <div class="metric-card">
              <div class="metric-value">{{ totalPowerUsage.toFixed(0) }}W</div>
              <div class="metric-label">总功耗</div>
              <el-progress 
                :percentage="Math.min((totalPowerUsage / 1000) * 100, 100)" 
                :show-text="false"
                :stroke-width="4"
                color="#67C23A"
              />
            </div>
          </el-col>
        </el-row>
      </div>

      <!-- GPU列表 -->
      <div class="gpu-list-section">
        <h3>GPU详细信息</h3>
        <el-row :gutter="16">
          <el-col 
            v-for="gpu in gpus" 
            :key="gpu.id" 
            :span="12"
            style="margin-bottom: 16px"
          >
            <el-card class="gpu-card">
              <template #header>
                <div class="gpu-card-header">
                  <span>{{ gpu.name }}</span>
                  <el-tag 
                    :type="getHealthStatus(gpu).type"
                    size="small"
                  >
                    {{ getHealthStatus(gpu).text }}
                  </el-tag>
                </div>
              </template>
              
              <div class="gpu-metrics">
                <el-row :gutter="12">
                  <el-col :span="12">
                    <div class="gpu-metric">
                      <div class="metric-title">使用率</div>
                      <el-progress 
                        type="circle" 
                        :percentage="gpu.usage" 
                        :width="60"
                        :color="getUsageColor(gpu.usage)"
                      />
                    </div>
                  </el-col>
                  <el-col :span="12">
                    <div class="gpu-metric">
                      <div class="metric-title">内存</div>
                      <el-progress 
                        type="circle" 
                        :percentage="(gpu.memory_usage / gpu.memory_total) * 100" 
                        :width="60"
                        :color="getUsageColor((gpu.memory_usage / gpu.memory_total) * 100)"
                      />
                      <div class="memory-text">
                        {{ formatMemory(gpu.memory_usage) }} / {{ formatMemory(gpu.memory_total) }}
                      </div>
                    </div>
                  </el-col>
                </el-row>
                
                <div class="gpu-stats">
                  <el-descriptions :column="2" size="small">
                    <el-descriptions-item label="温度">{{ gpu.temperature }}°C</el-descriptions-item>
                    <el-descriptions-item label="功耗">{{ gpu.power_usage }}W</el-descriptions-item>
                  </el-descriptions>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>
      </div>

      <!-- 历史趋势图表 -->
      <div class="charts-section">
        <h3>使用率趋势</h3>
        <el-row :gutter="16">
          <el-col :span="12">
            <div ref="utilizationChartRef" class="chart-container"></div>
          </el-col>
          <el-col :span="12">
            <div ref="memoryChartRef" class="chart-container"></div>
          </el-col>
        </el-row>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import * as echarts from 'echarts'
import { useGpuStore } from '@/stores/gpuStore'
import { wsService } from '@/services/websocket'
import type { GPUInfo } from '@/types'

const gpuStore = useGpuStore()

// 响应式数据
const autoRefresh = ref(true)
const refreshInterval = ref<NodeJS.Timeout | null>(null)

// 图表引用
const utilizationChartRef = ref<HTMLElement>()
const memoryChartRef = ref<HTMLElement>()
let utilizationChart: echarts.ECharts | null = null
let memoryChart: echarts.ECharts | null = null

// 计算属性
const { 
  gpus, 
  loading, 
  totalGpuUsage, 
  totalMemoryUsage, 
  averageTemperature,
  totalPowerUsage,
  isConnected
} = gpuStore

// 格式化函数
const formatMemory = (bytes: number) => {
  const sizes = ['B', 'KB', 'MB', 'GB']
  if (bytes === 0) return '0 B'
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return Math.round(bytes / Math.pow(1024, i) * 100) / 100 + ' ' + sizes[i]
}

const getUsageColor = (percentage: number) => {
  if (percentage >= 90) return '#F56C6C'
  if (percentage >= 70) return '#E6A23C'
  return '#67C23A'
}

const getTempColor = (temp: number) => {
  if (temp >= 85) return '#F56C6C'
  if (temp >= 75) return '#E6A23C'
  return '#67C23A'
}

const getHealthStatus = (gpu: GPUInfo) => {
  const health = gpuStore.getGpuHealthStatus(gpu)
  const typeMap: Record<string, string> = {
    healthy: 'success',
    warning: 'warning',
    critical: 'danger'
  }
  
  return {
    type: typeMap[health.status] || 'info',
    text: health.message
  }
}

// 图表初始化
const initCharts = async () => {
  await nextTick()
  
  if (utilizationChartRef.value && !utilizationChart) {
    utilizationChart = echarts.init(utilizationChartRef.value)
    
    const utilizationOption = {
      title: {
        text: 'GPU使用率趋势',
        textStyle: { fontSize: 14 }
      },
      tooltip: {
        trigger: 'axis',
        formatter: '{b}: {c}%'
      },
      xAxis: {
        type: 'category',
        data: Array.from({ length: 50 }, (_, i) => i.toString()),
        show: false
      },
      yAxis: {
        type: 'value',
        max: 100,
        axisLabel: { formatter: '{value}%' }
      },
      series: gpus.value.map(gpu => ({
        name: gpu.name,
        type: 'line',
        smooth: true,
        data: gpu.utilization_history.slice(-50),
        itemStyle: { color: getRandomColor() }
      })),
      legend: {
        data: gpus.value.map(gpu => gpu.name)
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '3%',
        containLabel: true
      }
    }
    
    utilizationChart.setOption(utilizationOption)
  }
  
  if (memoryChartRef.value && !memoryChart) {
    memoryChart = echarts.init(memoryChartRef.value)
    
    const memoryOption = {
      title: {
        text: '内存使用趋势',
        textStyle: { fontSize: 14 }
      },
      tooltip: {
        trigger: 'axis',
        formatter: '{b}: {c}%'
      },
      xAxis: {
        type: 'category',
        data: Array.from({ length: 50 }, (_, i) => i.toString()),
        show: false
      },
      yAxis: {
        type: 'value',
        max: 100,
        axisLabel: { formatter: '{value}%' }
      },
      series: gpus.value.map(gpu => ({
        name: gpu.name,
        type: 'line',
        smooth: true,
        data: gpu.memory_history.slice(-50),
        itemStyle: { color: getRandomColor() }
      })),
      legend: {
        data: gpus.value.map(gpu => gpu.name)
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '3%',
        containLabel: true
      }
    }
    
    memoryChart.setOption(memoryOption)
  }
}

// 更新图表
const updateCharts = () => {
  if (utilizationChart) {
    utilizationChart.setOption({
      series: gpus.value.map(gpu => ({
        data: gpu.utilization_history.slice(-50)
      }))
    })
  }
  
  if (memoryChart) {
    memoryChart.setOption({
      series: gpus.value.map(gpu => ({
        data: gpu.memory_history.slice(-50)
      }))
    })
  }
}

// 获取随机颜色
const getRandomColor = () => {
  const colors = ['#5470C6', '#91CC75', '#FAC858', '#EE6666', '#73C0DE', '#3BA272']
  return colors[Math.floor(Math.random() * colors.length)]
}

// 事件处理
const handleRefresh = async () => {
  await gpuStore.fetchGpuMetrics()
}

const handleAutoRefreshChange = (value: boolean) => {
  if (value) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
}

const startAutoRefresh = () => {
  if (refreshInterval.value) return
  
  refreshInterval.value = setInterval(() => {
    gpuStore.fetchGpuMetrics()
  }, 5000) // 每5秒刷新一次
}

const stopAutoRefresh = () => {
  if (refreshInterval.value) {
    clearInterval(refreshInterval.value)
    refreshInterval.value = null
  }
}

// WebSocket 事件监听
const setupWebSocketListeners = () => {
  wsService.on('gpu_metrics', (message: any) => {
    gpuStore.updateMetrics(message.data)
    updateCharts()
  })
  
  wsService.on('connected', () => {
    wsService.subscribeToGpuMetrics()
  })
  
  wsService.on('disconnected', () => {
    gpuStore.setConnectionStatus(false)
  })
}

// 监听GPU数据变化
watch(gpus, () => {
  updateCharts()
}, { deep: true })

// 组件生命周期
onMounted(async () => {
  await gpuStore.fetchGpuMetrics()
  await initCharts()
  setupWebSocketListeners()
  
  if (autoRefresh.value) {
    startAutoRefresh()
  }
  
  // 监听窗口大小变化
  window.addEventListener('resize', () => {
    utilizationChart?.resize()
    memoryChart?.resize()
  })
})

onUnmounted(() => {
  stopAutoRefresh()
  wsService.off('gpu_metrics')
  wsService.off('connected')
  wsService.off('disconnected')
  wsService.unsubscribeFromGpuMetrics()
  
  utilizationChart?.dispose()
  memoryChart?.dispose()
  
  window.removeEventListener('resize', () => {
    utilizationChart?.resize()
    memoryChart?.resize()
  })
})
</script>

<style scoped>
.gpu-usage-chart {
  padding: 24px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.overview-section {
  margin-bottom: 24px;
}

.metric-card {
  text-align: center;
  padding: 16px;
  background: #f8f9fa;
  border-radius: 8px;
}

.metric-value {
  font-size: 24px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 4px;
}

.metric-label {
  font-size: 12px;
  color: #909399;
  margin-bottom: 8px;
}

.gpu-list-section h3,
.charts-section h3 {
  margin: 0 0 16px 0;
  color: #303133;
  font-size: 16px;
}

.gpu-card {
  height: 100%;
}

.gpu-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 500;
}

.gpu-metrics {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.gpu-metric {
  text-align: center;
}

.metric-title {
  font-size: 12px;
  color: #909399;
  margin-bottom: 8px;
}

.memory-text {
  font-size: 11px;
  color: #909399;
  margin-top: 4px;
}

.gpu-stats {
  padding-top: 12px;
  border-top: 1px solid #EBEEF5;
}

.charts-section {
  margin-top: 24px;
}

.chart-container {
  height: 300px;
  width: 100%;
}

:deep(.el-descriptions__label) {
  font-weight: 500;
}

:deep(.el-card__header) {
  padding: 12px 16px;
}

:deep(.el-card__body) {
  padding: 16px;
}
</style>
