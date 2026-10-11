<template>
  <div class="portal-shell">
    <aside class="portal-sidebar">
      <div class="brand-mark"><span class="brand-icon">◈</span><div><strong>GPU Compute</strong><small>Control Portal · Local</small></div></div>
      <div class="sidebar-section">工作空间</div>
      <button type="button" class="nav-item" :class="{ active: view === 'overview' }" @click="view = 'overview'">◫ <span>总览</span></button>
      <button type="button" class="nav-item" :class="{ active: view === 'resources' }" @click="view = 'resources'">▤ <span>资源与工作负载</span></button>
      <button type="button" class="nav-item" :class="{ active: view === 'integrations' }" @click="view = 'integrations'">◇ <span>生态集成</span></button>
      <div class="sidebar-bottom"><span class="local-dot"></span>开发环境 · 只读模式</div>
    </aside>

    <main class="portal-main">
      <header class="portal-topbar">
        <div><span class="eyebrow">MANAGEMENT / {{ viewTitle }}</span><h1>{{ viewTitle }}</h1></div>
        <el-tag type="warning" effect="plain" size="large">LOCAL PREVIEW · READ ONLY</el-tag>
      </header>

      <el-alert title="此入口直接读取 Go Control Plane，尚未接入可信身份认证。仅可在本机开发环境使用，不支持公开部署和资源写入。" type="warning" :closable="false" show-icon class="safety-banner" />

      <section class="context-bar">
        <label class="context-label" for="cluster-id">Cluster ID</label>
        <el-input id="cluster-id" v-model.trim="clusterId" placeholder="输入已有集群 ID，例如 kind-golden" clearable class="cluster-input" @keyup.enter="refresh" />
        <el-button type="primary" :loading="loading" :disabled="!clusterId || loading" @click="refresh">读取实时状态</el-button>
        <span v-if="snapshot" class="last-sync">读取时间：{{ formatTime(snapshot.refreshedAt) }}</span>
      </section>
      <el-alert v-if="error" :title="error" type="error" :closable="true" @close="error = ''" show-icon class="error-banner" />

      <template v-if="!snapshot">
        <div class="welcome-box">
          <div class="welcome-icon">◈</div>
          <h2>连接你的计算集群</h2>
          <p>输入 Go Control Plane 中已注册的 Cluster ID，即可查看真实能力、计算池、工作负载与执行证据。这里不会生成演示数据。</p>
          <div class="welcome-hint"><code>GET /api/v1/clusters/{clusterID}/status</code> <span>·</span> <code>GET /api/v1/agent/desired</code></div>
        </div>
      </template>

      <template v-else>
        <div v-if="view === 'overview'" class="content-stack">
          <div class="metric-grid">
            <div class="metric-card"><div class="metric-label">集群状态</div><strong>{{ heartbeatLabel }}</strong><small>根据上次 Agent 心跳估算，非权威在线标记</small></div>
            <div class="metric-card"><div class="metric-label">ComputePools</div><strong>{{ poolCount }}</strong><small>当前集群已读取的期望资源</small></div>
            <div class="metric-card"><div class="metric-label">Workloads</div><strong>{{ workloadCount }}</strong><small>来自 Go Desired 状态</small></div>
            <div class="metric-card"><div class="metric-label">待观察 / 不一致</div><strong>{{ attentionCount }}</strong><small>仅限已读取状态的资源</small></div>
          </div>

          <div class="two-columns">
            <section class="panel">
              <div class="panel-heading"><h2>集群信息</h2><span>Agent registration</span></div>
              <div class="detail-grid">
                <div><label>Cluster ID</label><strong>{{ snapshot.agent.clusterId }}</strong></div>
                <div><label>Kubernetes</label><strong>{{ snapshot.agent.kubernetesVersion || '未知' }}</strong></div>
                <div><label>Agent 版本</label><strong>{{ snapshot.agent.agentVersion || '未知' }}</strong></div>
                <div><label>最近心跳</label><strong>{{ formatTime(snapshot.agent.lastHeartbeatAt) }}</strong></div>
              </div>
            </section>
            <section class="panel">
              <div class="panel-heading"><h2>资源能力发现</h2><span>Facts ≠ entitlement</span></div>
              <div class="capability-line"><span>Kueue</span><el-tag :type="snapshot.agent.capabilities.kueue ? 'success' : 'info'">{{ snapshot.agent.capabilities.kueue ? '已发现' : '未发现' }}</el-tag></div>
              <div class="capability-line"><span>DRA API</span><el-tag :type="snapshot.agent.capabilities.draApiAvailable ? 'warning' : 'info'">{{ snapshot.agent.capabilities.draApiAvailable ? (snapshot.agent.capabilities.draApiVersion || '已发现，仅 API') : '未发现' }}</el-tag></div>
              <div class="capability-line"><span>实际注册的 Provider</span><strong>{{ snapshot.agent.capabilities.providers?.join(', ') || '无记录' }}</strong></div>
              <div class="capability-line"><span>加速器类</span><strong>{{ snapshot.agent.capabilities.accelerators?.join(', ') || '未上报' }}</strong></div>
            </section>
          </div>
          <section class="panel">
            <div class="panel-heading"><h2>资源状态预览</h2><el-button text type="primary" @click="view = 'resources'">查看所有资源 →</el-button></div>
            <el-table :data="snapshot.rows.slice(0, 8)" empty-text="当前集群无 ComputePool / Workload" stripe @row-click="showDetails">
              <el-table-column prop="desired.kind" label="类别" width="150" />
              <el-table-column prop="desired.id" label="资源 ID" min-width="180" show-overflow-tooltip />
              <el-table-column label="Generation" width="130"><template #default="{ row }">{{ row.desired.generation }}</template></el-table-column>
              <el-table-column label="同步状态" width="160"><template #default="{ row }"><el-tag :type="statusColor(row)" effect="plain">{{ row.error ? '读取失败' : (row.state?.syncState || '未获取') }}</el-tag></template></el-table-column>
            </el-table>
          </section>
        </div>

        <section v-else-if="view === 'resources'" class="panel">
          <div class="panel-heading heading-with-controls"><div><h2>ComputePool 与 Workload</h2><p>Go Desired / Observed Generation · 点击记录查看原始条件和证据</p></div>
            <div class="filters"><el-select v-model="kindFilter" style="width: 160px"><el-option label="全部类型" value="all" /><el-option label="ComputePool" value="ComputePool" /><el-option label="Workload" value="Workload" /></el-select><el-input v-model="keyword" placeholder="搜索资源 ID" clearable style="width: 210px" /></div>
          </div>
          <el-alert v-if="snapshot.truncated" type="warning" :closable="false" :title="'集群共有 ' + snapshot.total + ' 条符合类型的资源，本地安全限制只读取前 120 条；当前计数不代表全集群。'" class="resource-alert" />
          <el-table :data="visibleRows" empty-text="没有匹配的实际资源" stripe @row-click="showDetails">
            <el-table-column label="类型" prop="desired.kind" width="145" />
            <el-table-column label="资源 ID" prop="desired.id" min-width="180" show-overflow-tooltip />
            <el-table-column label="Provider" min-width="120"><template #default="{ row }">{{ String(row.desired.spec.provider ?? '—') }}</template></el-table-column>
            <el-table-column label="期望 / 已观察" width="145"><template #default="{ row }">{{ row.desired.generation }} / {{ row.state?.observedGeneration ?? '—' }}</template></el-table-column>
            <el-table-column label="状态" width="150"><template #default="{ row }"><el-tag :type="statusColor(row)" effect="plain">{{ row.error ? '读取失败' : (row.state?.syncState || '未获取') }}</el-tag></template></el-table-column>
            <el-table-column label="删除中" width="95"><template #default="{ row }">{{ row.desired.deletionTimestamp ? '是' : '否' }}</template></el-table-column>
            <el-table-column label="证据条目" width="100"><template #default="{ row }">{{ row.state?.evidenceRefs?.length ?? '—' }}</template></el-table-column>
          </el-table>
          <p class="table-footnote">Synced 只表示观察 Generation 与期望 Generation 一致，不表示工作负载已 Ready、Kueue 已准入或 GPU 实际分配成功。</p>
        </section>

        <section v-else class="content-stack">
          <div class="two-columns">
            <section class="panel integration-panel"><span class="integration-symbol">K</span><h2>Kueue</h2><p>当前唯一生产级执行路径。能力是否已安装以 Agent 的实际发现为准。</p><el-tag :type="snapshot.agent.capabilities.providers?.includes('kueue') ? 'success' : 'info'">{{ snapshot.agent.capabilities.providers?.includes('kueue') ? '运行时已注册' : '未注册' }}</el-tag></section>
            <section class="panel integration-panel"><span class="integration-symbol">L</span><h2>llm-d / GAIE</h2><p>Serving 集成尚处于只读 InferencePool 前置验证阶段。Go Portal 不会凭 CRD 或历史 PR 推断 Serving Ready。</p><el-tag type="warning">独立 Draft PR · 未接入 Portal 数据源</el-tag></section>
          </div>
          <section class="panel">
            <div class="panel-heading"><h2>上游调度事实</h2><span>能力发现不会自动注册 Provider</span></div>
            <el-table :data="snapshot.agent.capabilities.schedulers || []" empty-text="未上报调度器" stripe>
              <el-table-column label="调度器" prop="name" />
              <el-table-column label="已观察版本" prop="version"><template #default="{ row }">{{ row.version || '版本未知' }}</template></el-table-column>
            </el-table>
          </section>
        </section>
      </template>
    </main>

    <el-drawer v-model="drawerOpen" title="资源详情 · 独立观察" size="min(560px, 100%)">
      <div v-if="selected" class="drawer-stack">
        <div class="drawer-title"><strong>{{ selected.desired.id }}</strong><el-tag>{{ selected.desired.kind }}</el-tag></div>
        <div class="detail-grid"><div><label>Desired Generation</label><strong>{{ selected.desired.generation }}</strong></div><div><label>Observed Generation</label><strong>{{ selected.state?.observedGeneration ?? '未获取' }}</strong></div><div><label>Sync State</label><strong>{{ selected.state?.syncState || '未知' }}</strong></div><div><label>Observation Lease Owner</label><strong>{{ selected.state?.observationLeaseOwner || '未提供' }}</strong></div></div>
        <el-alert v-if="selected.error" :title="selected.error" type="error" :closable="false" />
        <h3>Conditions</h3>
        <el-empty v-if="!selected.state?.conditions?.length" description="无独立观察条件" :image-size="65" />
        <div v-for="condition in selected.state?.conditions || []" :key="condition.type" class="evidence-item"><b>{{ condition.type }}</b><el-tag size="small" :type="condition.status === 'True' ? 'success' : 'info'">{{ condition.status }}</el-tag><small>{{ condition.reason || '—' }}</small></div>
        <h3>Evidence References</h3>
        <el-empty v-if="!selected.state?.evidenceRefs?.length" description="尚无可读取的证据引用" :image-size="65" />
        <code v-for="(ref, index) in selected.state?.evidenceRefs || []" :key="index" class="evidence-code">{{ ref }}</code>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { loadSnapshot, type PortalSnapshot, type ResourceRow } from './api'

type View = 'overview' | 'resources' | 'integrations'
const view = ref<View>('overview')
const clusterId = ref('')
const snapshot = ref<PortalSnapshot | null>(null)
const error = ref('')
const loading = ref(false)
const keyword = ref('')
const kindFilter = ref('all')
const selected = ref<ResourceRow | null>(null)
const drawerOpen = ref(false)

const viewTitle = computed(() => ({ overview: '计算平台总览', resources: '资源与工作负载', integrations: '生态集成' })[view.value])
const poolCount = computed(() => snapshot.value?.rows.filter(x => x.desired.kind === 'ComputePool').length ?? 0)
const workloadCount = computed(() => snapshot.value?.rows.filter(x => x.desired.kind === 'Workload').length ?? 0)
const attentionCount = computed(() => snapshot.value?.rows.filter(x => !x.state || x.error || x.state.syncState !== 'Synced').length ?? 0)
const visibleRows = computed(() => (snapshot.value?.rows ?? []).filter(x =>
  (kindFilter.value === 'all' || x.desired.kind === kindFilter.value) &&
  x.desired.id.toLowerCase().includes(keyword.value.toLowerCase()),
))
const heartbeatLabel = computed(() => {
  const heartbeat = snapshot.value?.agent.lastHeartbeatAt
  if (!heartbeat) return '心跳未知'
  const time = Date.parse(heartbeat)
  if (Number.isNaN(time)) return '时间无效'
  const delta = Date.now() - time
  if (delta < 0) return '时钟偏差'
  return delta <= 120_000 ? '近期有心跳' : '心跳超时'
})

function formatTime(value?: string | null): string {
  if (!value) return '尚无记录'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '时间无效' : date.toLocaleString('zh-CN')
}
function statusColor(row: ResourceRow): 'success' | 'warning' | 'danger' | 'info' {
  if (row.error) return 'danger'
  if (!row.state) return 'info'
  if (row.state.syncState === 'Inconsistent') return 'danger'
  if (row.state.syncState === 'Synced') return 'success'
  return 'warning'
}
function showDetails(row: ResourceRow): void {
  selected.value = row
  drawerOpen.value = true
}
async function refresh(): Promise<void> {
  if (loading.value || !clusterId.value) return
  loading.value = true
  error.value = ''
  snapshot.value = null
  drawerOpen.value = false
  try {
    snapshot.value = await loadSnapshot(clusterId.value)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Go Control Plane 读取失败'
  } finally {
    loading.value = false
  }
}
</script>
