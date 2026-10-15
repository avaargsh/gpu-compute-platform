<template>
  <div class="content-stack">
    <div class="panel-heading">
      <div>
        <h2>训练 / 批量推理执行监控</h2>
        <p>基于当前已授权资源快照的独立 Agent Observation；不请求模拟资源。</p>
      </div>
      <el-tag type="info" effect="plain">{{ workloadRows.length }} 条已读取 Workload</el-tag>
    </div>
    <el-alert v-if="truncated" type="warning" :closable="false"
      title="当前仅包含已读取页面的数据，列表不代表全项目运行任务。需要完整监控时先加载剩余资源页。" />
    <el-alert type="info" :closable="false"
      title="Synced 只表示 Generation 收敛。配额预留、Kueue Admission、Pod 就绪和 Job 终态必须由各自 Condition 独立确认；不代表模型性能或 Checkpoint 持久化。" />
    <el-table :data="workloadRows" stripe empty-text="此授权快照尚无 Workload 记录" style="width: 100%"
      @row-click="selectRun">
      <el-table-column label="项目 / 任务" min-width="200">
        <template #default="{ row }">
          <strong>{{ executionOf(row).workloadId }}</strong>
          <div><small>{{ executionOf(row).projectId || '项目未标记' }}</small></div>
        </template>
      </el-table-column>
      <el-table-column label="执行状态" min-width="170">
        <template #default="{ row }">
          <el-tag :type="statusColor(executionOf(row).code)" effect="plain">
            {{ executionOf(row).label }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Quota" width="91">
        <template #default="{ row }">{{ conditionFlag(row, 'QuotaReserved') }}</template>
      </el-table-column>
      <el-table-column label="Admitted" width="102">
        <template #default="{ row }">{{ conditionFlag(row, 'Admitted') }}</template>
      </el-table-column>
      <el-table-column label="PodsReady" width="110">
        <template #default="{ row }">{{ conditionFlag(row, 'PodsReady') }}</template>
      </el-table-column>
      <el-table-column label="原因" min-width="170" show-overflow-tooltip>
        <template #default="{ row }">{{ executionOf(row).detail }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }"><el-button type="primary" link @click.stop="selectRun(row)">查看详情</el-button></template>
      </el-table-column>
    </el-table>
    <div v-if="selected" class="drawer-stack">
      <div class="panel-heading">
        <div>
          <h3>任务详情：{{ selected.desired.id }}</h3>
          <small>独立观察快照 · Generation {{ selected.desired.generation }}</small>
        </div>
        <el-tag :type="statusColor(selectedSummary?.code ?? 'INDETERMINATE')">
          {{ selectedSummary?.label }}
        </el-tag>
      </div>
      <div class="detail-grid">
        <div><label>状态原因</label><strong>{{ selectedSummary?.detail }}</strong></div>
        <div><label>已观察 Generation</label><strong>{{ selected.state?.observedGeneration ?? '未知' }}</strong></div>
        <div><label>声明工作区 PVC</label><strong>{{ selectedSummary?.workspaceClaim || '未配置' }}</strong></div>
      </div>
      <h3>Kueue 与 Job Conditions</h3>
      <el-empty v-if="!selectedSummary?.conditions.length" description="尚未上报独立执行条件" :image-size="50" />
      <div v-for="(condition, index) in selectedSummary?.conditions ?? []" :key="condition.type + index" class="evidence-item">
        <b>{{ condition.type }}</b>
        <el-tag size="small" :type="condition.status === 'True' ? 'success' : condition.status === 'False' ? 'warning' : 'info'">
          {{ condition.status }}
        </el-tag>
        <small>{{ condition.reason || condition.message || '无原因' }}</small>
      </div>
      <h3>执行证据引用（仅文本）</h3>
      <el-empty v-if="!selectedSummary?.evidenceRefs.length" description="尚无执行证据引用" :image-size="50" />
      <code v-for="(ref, index) in selectedSummary?.evidenceRefs ?? []" :key="index" class="evidence-code">{{ ref }}</code>
      <h3>Pod 日志：本地只读命令</h3>
      <el-alert type="warning" :closable="false" title="日志可能包含敏感信息。Portal 不直接获取、缓存或展示 Pod 日志，必须由操作员在其具备 RBAC 权限的 kubeconfig 上自行执行。" />
      <el-form-item label="显式选择本地 Kubernetes context" style="margin-top: 12px">
        <el-input v-model.trim="kubeContext" placeholder="kind-gpu 或已验证的 kubeconfig context" />
      </el-form-item>
      <el-input type="textarea" :rows="2" readonly :model-value="logCommand || ''"
        placeholder="仅当观察身份及唯一 Job 证据均通过校验时才显示命令" />
      <el-button type="primary" plain :disabled="!logCommand" @click="copyLogCommand">复制只读日志命令</el-button>
      <small>{{ copyStatus }}</small>
      <p class="table-footnote">Checkpoint 持久性必须通过跨 Job 的真实文件摘要验收；任务结束或 PVC Bound 都不能替代该证据。当前页面不执行 kubectl，也不创建任何集群资源。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ResourceRow } from './api'
import { conditionFlag, executionOf, localLogCommand, type ExecutionCode } from './execution-model'

const props = defineProps<{ rows: ResourceRow[]; clusterId: string; truncated: boolean }>()
const selectedId = ref('')
const kubeContext = ref('')
const copyStatus = ref('')
const workloadRows = computed(() => props.rows.filter(x => x.desired.kind === 'Workload'))
const selected = computed(() => workloadRows.value.find(x => x.desired.id === selectedId.value) ?? null)
const selectedSummary = computed(() => selected.value ? executionOf(selected.value) : null)
const logCommand = computed(() =>
  selected.value ? localLogCommand(selected.value, props.clusterId, kubeContext.value) : null)

watch(() => props.clusterId, () => {
  selectedId.value = ''
  kubeContext.value = ''
  copyStatus.value = ''
})
function selectRun(row: ResourceRow): void {
  selectedId.value = row.desired.id
  copyStatus.value = ''
}
function statusColor(code: ExecutionCode): 'success' | 'danger' | 'warning' | 'info' {
  if (code === 'SUCCEEDED' || code === 'RUNNING') return 'success'
  if (code === 'READ_FAILED' || code === 'IDENTITY_CONFLICT' ||
      code === 'OBSERVATION_CONFLICT' || code === 'FAILED') return 'danger'
  if (code === 'WAITING_QUOTA' || code === 'WAITING_ADMISSION' ||
      code === 'WAITING_PODS' || code === 'STALE_OBSERVATION') return 'warning'
  return 'info'
}
async function copyLogCommand(): Promise<void> {
  if (!logCommand.value) return
  try {
    await navigator.clipboard.writeText(logCommand.value)
    copyStatus.value = '已复制本地只读命令；未执行，也未拉取任何日志。'
  } catch {
    copyStatus.value = '剪贴板访问被拒绝，可手动复制上方命令。'
  }
}
</script>
