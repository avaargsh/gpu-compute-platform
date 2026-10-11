<template>
  <div class="portal-shell">
    <aside class="portal-sidebar">
      <div class="brand-mark"><span class="brand-icon">◈</span><div><strong>GPU Compute</strong><small>Control Portal · Local</small></div></div>
      <div class="sidebar-section">工作空间</div>
      <button type="button" class="nav-item" :class="{ active: view === 'overview' }" @click="view = 'overview'">◫ <span>总览</span></button>
      <button type="button" class="nav-item" :class="{ active: view === 'projects' }" @click="view = 'projects'">▦ <span>项目视图</span></button>
      <button type="button" class="nav-item" :class="{ active: view === 'resources' }" @click="view = 'resources'">▤ <span>资源与工作负载</span></button>
      <button type="button" class="nav-item" :class="{ active: view === 'diagnostics' }" @click="view = 'diagnostics'">△ <span>异常待办</span><span v-if="findings.length" class="nav-count">{{ findings.length }}</span></button>
      <button type="button" class="nav-item" :class="{ active: view === 'integrations' }" @click="view = 'integrations'">◇ <span>生态集成</span></button>
      <div class="sidebar-bottom"><span class="local-dot"></span>开发环境 · 只读模式</div>
    </aside>

    <main class="portal-main">
      <header class="portal-topbar">
        <div><span class="eyebrow">MANAGEMENT / {{ viewTitle }}</span><h1>{{ viewTitle }}</h1></div>
        <el-tag type="warning" effect="plain" size="large">LOCAL PREVIEW · READ ONLY</el-tag>
      </header>

      <el-alert title="本机单操作员试用：服务端校验凭证及项目范围。尚未实现 OIDC / 多用户 RBAC，禁止公网部署和资源写入。凭证仅保留在本页内存，不持久化。"  type="warning" :closable="false" show-icon class="safety-banner" />

      <section class="context-bar">
        <label class="context-label" for="cluster-id">Cluster ID</label>
        <el-input id="cluster-id" v-model.trim="clusterId" placeholder="已授权集群 ID" clearable class="cluster-input" @keyup.enter="refresh" />
        <label class="context-label" for="operator-token">本机操作员凭证</label>
        <el-input id="operator-token" v-model="operatorToken" type="password" show-password autocomplete="off" placeholder="本机只读访问凭证（不存储）" class="cluster-input" @keyup.enter="refresh" />
        <el-button type="primary" :loading="loading" :disabled="!clusterId || operatorToken.length < 32 || loading" @click="refresh">授权读取</el-button>
        <span v-if="snapshot" class="last-sync">读取时间：{{ formatTime(snapshot.refreshedAt) }}</span>
      </section>
      <el-alert v-if="error" :title="error" type="error" :closable="true" @close="error = ''" show-icon class="error-banner" />

      <template v-if="!snapshot">
        <div class="welcome-box">
          <div class="welcome-icon">◈</div>
          <h2>连接你的计算集群</h2>
          <p>输入已授权 Cluster ID 和本机操作员凭证，读取服务端按项目过滤的计算池、工作负载与执行证据。没有模拟数据，也不允许在这里创建或删除资源。</p>
          <div class="welcome-hint"><code>GET /api/v1/operator/clusters/{clusterID}/snapshot</code><span>·</span><code>Read-only / scoped</code></div>
        </div>
      </template>

      <template v-else>
        <el-alert v-if="snapshot.truncated" type="warning" :closable="false" title="当前展示的是授权项目的一页结果，项目、异常和配额统计仅针对已加载页面。继续加载后才能查看更多资源。" class="resource-alert" />
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
          <section class="panel" v-if="findings.length">
            <div class="panel-heading"><h2>需要操作员关注</h2><el-button text type="primary" @click="view = 'diagnostics'">进入异常待办 →</el-button></div>
            <div v-for="finding in findings.slice(0, 4)" :key="finding.key" class="finding-row">
              <el-tag :type="finding.severity === 'critical' ? 'danger' : 'warning'" effect="plain">{{ finding.code }}</el-tag>
              <strong>{{ finding.resourceId }}</strong>
              <span>{{ finding.description }}</span>
              <el-button text type="primary" @click="showFinding(finding)">查看证据</el-button>
            </div>
          </section>
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
            <div class="filters">
              <el-select v-model="projectFilter" aria-label="按项目筛选" style="width: 165px">
                <el-option label="所有项目" value="all" />
                <el-option v-for="project in projects" :key="project.id" :label="project.id || '未标记项目'" :value="project.id" />
              </el-select>
              <el-select v-model="kindFilter" aria-label="按资源类型筛选" style="width: 155px"><el-option label="全部类型" value="all" /><el-option label="ComputePool" value="ComputePool" /><el-option label="Workload" value="Workload" /></el-select>
              <el-input v-model="keyword" placeholder="搜索资源 ID" clearable style="width: 190px" />
            </div>
          </div>
          <el-alert v-if="snapshot.truncated" type="warning" :closable="false" :title="'当前授权范围内共有 ' + snapshot.total + ' 条资源，已载入 ' + snapshot.rows.length + ' 条，可按页继续读取。'" class="resource-alert" />
          <el-table :data="visibleRows" empty-text="没有匹配的实际资源" stripe @row-click="showDetails">
            <el-table-column label="类型" prop="desired.kind" width="145" />
            <el-table-column label="资源 ID" prop="desired.id" min-width="180" show-overflow-tooltip />
            <el-table-column label="Provider" min-width="120"><template #default="{ row }">{{ String(row.desired.spec.provider ?? '—') }}</template></el-table-column>
            <el-table-column label="期望 / 已观察" width="145"><template #default="{ row }">{{ row.desired.generation }} / {{ row.state?.observedGeneration ?? '—' }}</template></el-table-column>
            <el-table-column label="状态" width="150"><template #default="{ row }"><el-tag :type="statusColor(row)" effect="plain">{{ row.error ? '读取失败' : (row.state?.syncState || '未获取') }}</el-tag></template></el-table-column>
            <el-table-column label="删除中" width="95"><template #default="{ row }">{{ row.desired.deletionTimestamp ? '是' : '否' }}</template></el-table-column>
            <el-table-column label="证据条目" width="100"><template #default="{ row }">{{ row.state?.evidenceRefs?.length ?? '—' }}</template></el-table-column>
          </el-table>
          <div class="paging-controls" v-if="snapshot.nextCursor"><el-button :loading="loadingMore" type="primary" plain @click="loadMore">继续加载下一页</el-button><span>已加载 {{ snapshot.rows.length }} / {{ snapshot.total }} 条</span></div>
          <p class="table-footnote">Synced 只表示观察 Generation 与期望 Generation 一致，不表示工作负载已 Ready、Kueue 已准入或 GPU 实际分配成功。</p>
        </section>

        <section v-else-if="view === 'projects'" class="panel">
          <div class="panel-heading"><div><h2>项目 / AI Initiatives</h2><p>按资源 spec.projectID 聚合；不是已鉴权的租户列表</p></div><el-tag effect="plain">{{ projects.length }} 个已观察项目分组</el-tag></div>
          <el-table :data="projects" stripe empty-text="当前集群没有可归组的资源">
            <el-table-column label="项目 ID" min-width="180">
              <template #default="{ row }"><strong>{{ row.id || '未标记项目（不可用于授权）' }}</strong></template>
            </el-table-column>
            <el-table-column label="ComputePool" prop="poolCount" width="135" />
            <el-table-column label="Workload" prop="workloadCount" width="135" />
            <el-table-column label="需关注" width="120">
              <template #default="{ row }"><el-tag :type="row.needsAttention ? 'warning' : 'info'" effect="plain">{{ row.needsAttention }}</el-tag></template>
            </el-table-column>
            <el-table-column label="声明 GPU 配额" width="160">
              <template #default="{ row }">{{ row.poolCount ? (row.declaredPoolGpuQuota ?? '未知') : '—' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="110">
              <template #default="{ row }"><el-button type="primary" link @click="openProject(row.id)">查看资源 →</el-button></template>
            </el-table-column>
          </el-table>
          <p class="table-footnote">声明 GPU 配额是 ComputePool.spec.accelerators 中的 quota 总和，仅表示产品意图；不是 GPU 已分配量、GPU 利用率、Kueue 准入，也不能据此计费。</p>
        </section>

        <section v-else-if="view === 'diagnostics'" class="panel">
          <div class="panel-heading"><div><h2>异常待办 / Operations Inbox</h2><p>从真实 Desired、Observation、Condition 派生，不调用 LLM 推断故障原因</p></div><el-tag :type="findings.length ? 'warning' : 'info'" effect="plain">{{ findings.length }} 条待检查</el-tag></div>
          <el-table :data="findings" stripe empty-text="在已读取的资源样本中，没有匹配的异常；不表示全平台健康">
            <el-table-column label="优先级" width="110">
              <template #default="{ row }"><el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'" effect="plain">{{ row.severity === 'critical' ? '高' : '待确认' }}</el-tag></template>
            </el-table-column>
            <el-table-column label="项目" min-width="120"><template #default="{ row }">{{ row.projectId || '未标记' }}</template></el-table-column>
            <el-table-column label="资源" min-width="155"><template #default="{ row }">{{ row.kind }} / {{ row.resourceId }}</template></el-table-column>
            <el-table-column label="异常类型" min-width="180" prop="code" />
            <el-table-column label="证据 / 原因" min-width="200" show-overflow-tooltip prop="description" />
            <el-table-column label="操作" width="112"><template #default="{ row }"><el-button type="primary" link @click="showFinding(row)">查看证据 →</el-button></template></el-table-column>
          </el-table>
          <p class="table-footnote">异常待办是只读观测分类，状态同步不等于推理就绪。当前不提供修复、重新调度、删除或手动重试按钮。</p>
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
import { findingsFor, projectIdOf, projectSummaries, triageFindings, type Finding } from './project-model'

type View = 'overview' | 'projects' | 'resources' | 'diagnostics' | 'integrations'
const view = ref<View>('overview')
const clusterId = ref('')
const operatorToken = ref('')
const loadingMore = ref(false)
const snapshot = ref<PortalSnapshot | null>(null)
const error = ref('')
const loading = ref(false)
const keyword = ref('')
const kindFilter = ref('all')
const projectFilter = ref('all')
const selected = ref<ResourceRow | null>(null)
const drawerOpen = ref(false)

const viewTitle = computed(() => ({
  overview: '计算平台总览', projects: '项目视图', resources: '资源与工作负载',
  diagnostics: '异常待办', integrations: '生态集成',
})[view.value])
const projects = computed(() => projectSummaries(snapshot.value?.rows ?? []))
const findings = computed(() => triageFindings(snapshot.value?.rows ?? []))
const poolCount = computed(() => snapshot.value?.rows.filter(x => x.desired.kind === 'ComputePool').length ?? 0)
const workloadCount = computed(() => snapshot.value?.rows.filter(x => x.desired.kind === 'Workload').length ?? 0)
const attentionCount = computed(() => snapshot.value?.rows.filter(x => findingsFor(x).length > 0).length ?? 0)
const visibleRows = computed(() => (snapshot.value?.rows ?? []).filter(x =>
  (projectFilter.value === 'all' || projectIdOf(x) === projectFilter.value) &&
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
function openProject(id: string): void {
  projectFilter.value = id
  kindFilter.value = 'all'
  keyword.value = ''
  view.value = 'resources'
}
function showFinding(finding: Finding): void {
  const row = snapshot.value?.rows.find(x => x.desired.kind === finding.kind && x.desired.id === finding.resourceId)
  if (row) showDetails(row)
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
  projectFilter.value = 'all'
  drawerOpen.value = false
  try {
    snapshot.value = await loadSnapshot(clusterId.value, operatorToken.value)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : 'Go Control Plane 读取失败'
  } finally {
    loading.value = false
  }
}
async function loadMore(): Promise<void> {
  if (loadingMore.value || !snapshot.value?.nextCursor || !clusterId.value) return
  loadingMore.value = true
  error.value = ''
  try {
    const old = snapshot.value
    const next = await loadSnapshot(clusterId.value, operatorToken.value, old.nextCursor)
    if (next.agent.clusterId !== old.agent.clusterId || next.total !== old.total) {
      throw new Error('读取期间资源集合发生变化，请重新读取以避免误判。')
    }
    const existing = new Set(old.rows.map(row => row.desired.kind + '/' + row.desired.id))
    if (next.rows.some(row => existing.has(row.desired.kind + '/' + row.desired.id))) {
      throw new Error('跨页资源身份重复，请重新读取。')
    }
    snapshot.value = { ...next, rows: [...old.rows, ...next.rows] }
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '加载资源失败'
  } finally {
    loadingMore.value = false
  }
}
</script>
