'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Input,
  Label,
  Badge,
  Alert,
  AlertTitle,
  AlertDescription,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui'
import { loadSnapshot, type PortalSnapshot, type ResourceRow } from './api'
import { projectSummaries, triageFindings, type Finding } from './project-model'
import { isSafeSegment } from './api'

type View =
  | 'overview'
  | 'projects'
  | 'resources'
  | 'jobs'
  | 'serving'
  | 'diagnostics'
  | 'integrations'

const VIEW_TITLES: Record<View, string> = {
  overview: '计算平台总览',
  projects: '项目视图',
  resources: '资源与工作负载',
  jobs: '训推任务',
  serving: '在线推理',
  diagnostics: '异常待办',
  integrations: '生态集成',
}

const NAV_ITEMS: Array<{ path: View; label: string }> = [
  { path: 'overview', label: '总览' },
  { path: 'projects', label: '项目视图' },
  { path: 'resources', label: '资源与工作负载' },
  { path: 'jobs', label: '训推任务' },
  { path: 'serving', label: '在线推理' },
  { path: 'diagnostics', label: '异常待办' },
  { path: 'integrations', label: '生态集成' },
]

export function PortalApp() {
  const [view, setView] = useState<View>('overview')
  const [clusterId, setClusterId] = useState('')
  const [operatorToken, setOperatorToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [snapshot, setSnapshot] = useState<PortalSnapshot | null>(null)
  const [error, setError] = useState('')
  const [keyword, setKeyword] = useState('')
  const [kindFilter, setKindFilter] = useState('all')
  const [projectFilter, setProjectFilter] = useState('all')
  const [selected, setSelected] = useState<ResourceRow | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const sessionEpoch = useRef(0)

  const projects = useMemo(() => projectSummaries(snapshot?.rows ?? []), [snapshot])
  const findings = useMemo(() => triageFindings(snapshot?.rows ?? []), [snapshot])

  const poolCount = useMemo(
    () => snapshot?.rows.filter((x) => x.desired.kind === 'ComputePool').length ?? 0,
    [snapshot]
  )
  const workloadCount = useMemo(
    () => snapshot?.rows.filter((x) => x.desired.kind === 'Workload').length ?? 0,
    [snapshot]
  )
  const attentionCount = useMemo(
    () =>
      snapshot?.rows.filter((x) => findingsForRow(x).length > 0).length ?? 0,
    [snapshot]
  )

  const visibleRows = useMemo(
    () =>
      (snapshot?.rows ?? []).filter(
        (x) =>
          (projectFilter === 'all' || projectIdOf(x) === projectFilter) &&
          (kindFilter === 'all' || x.desired.kind === kindFilter) &&
          x.desired.id.toLowerCase().includes(keyword.toLowerCase()),
      ),
    [snapshot, projectFilter, kindFilter, keyword]
  )

  const heartbeatLabel = useMemo(() => {
    const heartbeat = snapshot?.agent.lastHeartbeatAt
    if (!heartbeat) return '心跳未知'
    const time = Date.parse(heartbeat)
    if (Number.isNaN(time)) return '时间无效'
    const delta = Date.now() - time
    if (delta < 0) return '时钟偏差'
    return delta <= 120_000 ? '近期有心跳' : '心跳超时'
  }, [snapshot])

  const viewTitle = VIEW_TITLES[view]

  const handleRefresh = useCallback(async () => {
    if (loading || !clusterId) return
    const epoch = ++sessionEpoch.current
    setLoading(true)
    setError('')
    setSnapshot(null)
    setProjectFilter('all')
    setDrawerOpen(false)
    try {
      const next = await loadSnapshot(clusterId, operatorToken)
      if (epoch === sessionEpoch.current) setSnapshot(next)
    } catch (cause) {
      if (epoch === sessionEpoch.current) {
        setError(cause instanceof Error ? cause.message : 'Go Control Plane 读取失败')
      }
    } finally {
      if (epoch === sessionEpoch.current) setLoading(false)
    }
  }, [clusterId, operatorToken, loading])

  const handleLoadMore = useCallback(async () => {
    if (loadingMore || !snapshot?.nextCursor || !clusterId) return
    const epoch = ++sessionEpoch.current
    setLoadingMore(true)
    setError('')
    try {
      const old = snapshot
      const next = await loadSnapshot(clusterId, operatorToken, old.nextCursor)
      if (epoch !== sessionEpoch.current) return
      if (
        next.agent.clusterId !== old.agent.clusterId ||
        next.total !== old.total
      ) {
        throw new Error('读取期间资源集合发生变化，请重新读取以避免误判。')
      }
      const existing = new Set(old.rows.map((row) => row.desired.kind + '/' + row.desired.id))
      if (next.rows.some((row) => existing.has(row.desired.kind + '/' + row.desired.id))) {
        throw new Error('跨页资源身份重复，请重新读取。')
      }
      setSnapshot({ ...next, rows: [...old.rows, ...next.rows] })
    } catch (cause) {
      if (epoch === sessionEpoch.current) {
        setError(cause instanceof Error ? cause.message : '加载资源失败')
      }
    } finally {
      if (epoch === sessionEpoch.current) setLoadingMore(false)
    }
  }, [clusterId, operatorToken, loadingMore, snapshot])

  const handleLock = useCallback(() => {
    setOperatorToken('')
    sessionEpoch.current++
    setSnapshot(null)
    setSelected(null)
    setDrawerOpen(false)
  }, [])

  const handleOpenProject = (id: string) => {
    setProjectFilter(id)
    setKindFilter('all')
    setKeyword('')
    setView('resources')
  }

  const handleShowFinding = (finding: Finding) => {
    const row = snapshot?.rows.find(
      (x) => x.desired.kind === finding.kind && x.desired.id === finding.resourceId
    )
    if (row) showDetails(row)
  }

  const showDetails = (row: ResourceRow) => {
    setSelected(row)
    setDrawerOpen(true)
  }

  const handleSelectView = (nextView: View) => {
    setView(nextView)
  }

  const localFormatTime = useCallback((value?: string | null): string => {
    if (!value) return '尚无记录'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '时间无效' : date.toLocaleString('zh-CN')
  }, [])

  return (
    <div className="portal-shell flex h-screen bg-background">
      {/* Sidebar */}
      <aside className="portal-sidebar fixed left-0 top-0 bottom-0 w-64 border-r border-border bg-card">
        <div className="brand-mark flex items-center gap-2 p-6">
          <span className="brand-icon">◈</span>
          <div>
            <strong>GPU Compute</strong>
            <div className="text-xs text-muted-foreground">Control Portal · Local</div>
          </div>
        </div>
        <div className="sidebar-section px-6 py-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
          工作空间
        </div>
        <nav className="space-y-1">
          {NAV_ITEMS.map((item) => (
            <Button
              key={item.path}
              variant={view === item.path ? 'default' : 'ghost'}
              size="sm"
              className="w-full flex items-center gap-3 text-sm rounded-none border-none bg-transparent hover:bg-accent"
              onClick={() => {
                setView(item.path)
              }}
            >
              <span className="w-4">{item.label}</span>
            </Button>
          ))}
        </nav>
        <div className="sidebar-bottom absolute bottom-0 left-0 right-0 p-6 border-t border-border">
          <span className="local-dot inline-block w-2 h-2 rounded-full bg-green-500 mr-1"></span>
          开发环境 · 默认只读
        </div>
      </aside>

      <main className="flex-1 flex flex-col overflow-hidden">
        <header className="portal-topbar flex items-center justify-between px-6 py-3 border-b border-border bg-background">
          <div className="flex items-center gap-2">
            <span className="eyebrow text-xs text-muted-foreground uppercase tracking-wider">
              MANAGEMENT / {viewTitle}
            </span>
            <h1 className="text-lg font-semibold">{viewTitle}</h1>
          </div>
          <Alert variant="destructive" className="text-xs px-3 py-1.5">
            <AlertTitle>LOCAL PREVIEW · DEV WRITE OPT-IN</AlertTitle>
          </Alert>
        </header>

        <Alert
          title="本机单操作员预览：默认只读。仅 development 配置独立写入凭证后，可创建指定项目 Workload Desired；尚未实现生产 OIDC / RBAC，严禁公网部署。凭证仅保留在页面内存。"
          type="warning"
          closable={false}
          showIcon
          className="safety-banner text-xs px-6 py-2"
        />

        <section className="context-bar px-6 py-3 border-b border-border bg-background">
          <div className="grid grid-cols-1 md:grid-cols-12 gap-4 items-end">
            <div className="md:col-span-4">
              <Label htmlFor="cluster-id">Cluster ID</Label>
              <Input
                id="cluster-id"
                value={clusterId}
                onChange={(e) => setClusterId(e.target.value)}
                placeholder="已授权集群 ID"
                className="cluster-input"
                onKeyDown={(e) => e.key === 'Enter' && handleRefresh()}
              />
            </div>
            <div className="md:col-span-5">
              <Label htmlFor="operator-token">本机操作员凭证</Label>
              <Input
                id="operator-token"
                type="password"
                value={operatorToken}
                onChange={(e) => setOperatorToken(e.target.value)}
                showPassword
                placeholder="本机只读访问凭证（不存储）"
                className="cluster-input"
                onKeyDown={(e) => e.key === 'Enter' && handleRefresh()}
              />
            </div>
            <div className="md:col-span-3 flex gap-2">
              <Button
                type="button"
                variant="default"
                loading={loading}
                disabled={!clusterId || operatorToken.length < 32 || loading}
                onClick={handleRefresh}
                className="w-full"
              >
                授权读取
              </Button>
              <Button
                type="button"
                variant="destructive"
                size="sm"
                disabled={!operatorToken && !snapshot}
                onClick={handleLock}
              >
                锁定
              </Button>
            </div>
            {snapshot && (
              <div className="md:col-span-12 text-xs text-muted-foreground mt-2">
                读取时间：{localFormatTime(snapshot.refreshedAt)}
              </div>
            )}
          </div>
        </section>

        {error && (
          <Alert variant="destructive" className="error-banner mx-6 mt-3">
            <AlertTitle>{error}</AlertTitle>
          </Alert>
        )}

        <div className="flex-1 overflow-y-auto p-6">
          {!snapshot ? (
            <div className="welcome-box flex flex-col items-center justify-center py-20 text-center">
              <div className="welcome-icon text-4xl text-muted-foreground mb-4">◈</div>
              <h2 className="text-xl font-semibold mb-2">连接你的计算集群</h2>
              <p className="text-muted-foreground text-sm max-w-md">
                输入已授权 Cluster ID 和本机操作员凭证，读取服务端按项目过滤的计算池、工作负载与执行证据。
                没有模拟数据，也不允许在这里创建或删除资源。
              </p>
              <div className="welcome-hint mt-4 text-xs text-muted-foreground flex flex-col gap-1">
                <code>GET /api/v1/operator/clusters/{{clusterID}}/snapshot</code>
                <span>· Read-only / scoped</span>
              </div>
            </div>
          ) : (
            <div className="content-stack space-y-6">
              {view === 'overview' && (
                <OverviewView
                  poolCount={poolCount}
                  workloadCount={workloadCount}
                  attentionCount={attentionCount}
                  heartbeatLabel={heartbeatLabel}
                  snapshot={snapshot}
                  findings={findings}
                  formatTime={localFormatTime}
                />
              )}
              {view === 'projects' && <ProjectsView projects={projects} findings={findings} />}
              {view === 'resources' && <ResourcesView visibleRows={visibleRows} snapshot={snapshot} />}
              {view === 'jobs' && <JobsView visibleRows={visibleRows} snapshot={snapshot} />}
              {view === 'serving' && <ServingView snapshot={snapshot} />}
              {view === 'diagnostics' && <DiagnosticsView findings={findings} onShowFinding={handleShowFinding} />}
              {view === 'integrations' && <IntegrationsView snapshot={snapshot} />}
            </div>
          )}
        </div>

        <Dialog open={drawerOpen} onOpenChange={setDrawerOpen}>
          <DialogTrigger asChild>
            <Button variant="outline" hidden />
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>
                {selected?.desired.id} <Badge>{selected?.desired.kind}</Badge>
              </DialogTitle>
            </DialogHeader>
            <DialogDescription>
              <div className="grid gap-2 text-sm">
                <div>
                  <Label>Desired Generation</Label>
                  <strong>{selected.desired.generation}</strong>
                </div>
                <div>
                  <Label>Observed Generation</Label>
                  <strong>{selected.state?.observedGeneration ?? '未获取'}</strong>
                </div>
                <div>
                  <Label>Sync State</Label>
                  <strong>{selected.state?.syncState || '未知'}</strong>
                </div>
                <div>
                  <Label>Observation Lease Owner</Label>
                  <strong>{selected.state?.observationLeaseOwner || '未提供'}</strong>
                </div>
              </div>
              {selected.error && (
                <Alert variant="destructive" className="mt-2">
                  <AlertTitle>{selected.error}</AlertTitle>
                </Alert>
              )}
              <h3 className="mt-4 text-sm font-semibold">Conditions</h3>
              {selected.state?.conditions?.length ? (
                selected.state.conditions.map((c) => (
                  <div key={c.type} className="evidence-item flex items-center gap-2 text-sm">
                    <b>{c.type}</b>
                    <Badge variant={c.status === 'True' ? 'default' : 'secondary'}>
                      {c.status}
                    </Badge>
                    <small className="text-muted-foreground">{c.reason || '—'}</small>
                  </div>
                ))
              ) : (
                <div className="text-muted-foreground text-sm">无独立观察条件</div>
              )}
              <h3 className="mt-4 text-sm font-semibold">Evidence References</h3>
              {selected.state?.evidenceRefs?.length ? (
                selected.state.evidenceRefs.map((ref, i) => (
                  <code key={i} className="evidence-code block text-xs text-muted-foreground break-all">
                    {ref}
                  </code>
                ))
              ) : (
                <div className="text-muted-foreground text-sm">尚无可读取的证据引用</div>
              )}
            </DialogDescription>
          </DialogContent>
        </Dialog>
      </main>
    </div>
  )
}

function findingsForRow(row: ResourceRow): Finding[] {
  const { desired, state } = row
  const projectId =
    typeof desired.spec.projectID === 'string' && desired.spec.projectID.trim().length > 0
      ? desired.spec.projectID
      : ''
  const base = {
    key: desired.kind + ':' + desired.id,
    resourceId: desired.id,
    kind: desired.kind,
    projectId,
  }
  if (row.error) {
    return [{ ...base, severity: 'critical', code: 'OBSERVATION_READ_FAILED', description: row.error }]
  }
  if (desired.deletionTimestamp) {
    return [
      {
        ...base,
        severity: 'warning',
        code: 'DELETION_PENDING',
        description: '已请求删除，等待 Provider 清理与 Finalizer',
      },
    ]
  }
  if (!state) {
    return [{ ...base, severity: 'warning', code: 'OBSERVATION_MISSING', description: '缺少独立观察结果' }]
  }
  if (
    state.kind !== desired.kind ||
    state.id !== desired.id ||
    state.desiredGeneration !== desired.generation
  ) {
    return [
      {
        ...base,
        severity: 'critical',
        code: 'IDENTITY_MISMATCH',
        description: '观察身份或期望 Generation 不匹配',
      },
    ]
  }
  if (state.syncState === 'Inconsistent' || state.observedGeneration > desired.generation) {
    return [
      {
        ...base,
        severity: 'critical',
        code: 'GENERATION_CONFLICT',
        description: '观察版本超前或不一致',
      },
    ]
  }
  const conditions = state.conditions ?? []
  const blocked = conditions.filter((c) =>
    c.status === 'False' && ['Ready', 'Admitted', 'QuotaReserved'].includes(c.type)
  )
  if (blocked.length) {
    return blocked.map((condition, index) => ({
      ...base,
      key: base.key + ':condition:' + index,
      severity: 'warning' as const,
      code: condition.type.toUpperCase() + '_FALSE',
      description: condition.reason || condition.message || condition.type + ' 为 False',
    }))
  }
  if (state.syncState !== 'Synced' || state.observedGeneration !== desired.generation) {
    return [
      {
        ...base,
        severity: 'warning',
        code: 'GENERATION_PENDING',
        description: '期望与观察 Generation 尚未收敛',
      },
    ]
  }
  return []
}

function projectIdOf(row: ResourceRow): string {
  const raw = row.desired.spec.projectID
  return typeof raw === 'string' && raw.trim().length > 0 ? raw : ''
}

function OverviewView({
  poolCount,
  workloadCount,
  attentionCount,
  heartbeatLabel,
  snapshot,
  findings,
  formatTime,
}: {
  poolCount: number
  workloadCount: number
  attentionCount: number
  heartbeatLabel: string
  snapshot: PortalSnapshot | null
  findings: Finding[]
  formatTime: (value?: string | null) => string
}) {
  const statCards = [
    { title: '集群状态', value: heartbeatLabel, color: 'text-blue-600' },
    { title: 'ComputePools', value: String(poolCount), color: 'text-green-600' },
    { title: 'Workloads', value: String(workloadCount), color: 'text-purple-600' },
    { title: '待观察 / 不一致', value: String(attentionCount), color: 'text-amber-600' },
  ]

  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {statCards.map((card) => (
          <Card key={card.title}>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">{card.title}</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-xl font-bold {card.color}">{card.value}</div>
              <div className="text-xs text-muted-foreground mt-1">
                {card.title === '集群状态'
                  ? '根据上次 Agent 心跳估算，非权威在线标记'
                  : card.title === 'ComputePools'
                    ? '当前集群已读取的期望资源'
                    : card.title === 'Workloads'
                      ? '来自 Go Desired 状态'
                      : '仅限已读取状态的资源'}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle>集群信息</CardTitle>
            <span className="text-xs text-muted-foreground">Agent registration</span>
          </CardHeader>
          <CardContent className="space-y-2">
            {snapshot?.agent && (
              <>
                <div className="flex justify-between">
                  <Label>Cluster ID</Label>
                  <strong>{snapshot.agent.clusterId}</strong>
                </div>
                <div className="flex justify-between">
                  <Label>Kubernetes</Label>
                  <strong>{snapshot.agent.kubernetesVersion || '未知'}</strong>
                </div>
                <div className="flex justify-between">
                  <Label>Agent 版本</Label>
                  <strong>{snapshot.agent.agentVersion || '未知'}</strong>
                </div>
                <div className="flex justify-between">
                  <Label>最近心跳</Label>
                  <strong>{formatTime(snapshot.agent.lastHeartbeatAt)}</strong>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle>资源能力发现</CardTitle>
            <span className="text-xs text-muted-foreground">Facts ≠ entitlement</span>
          </CardHeader>
          <CardContent className="space-y-2">
            {snapshot?.agent && (
              <>
                <div className="capability-line flex items-center justify-between">
                  <span>Kueue</span>
                  <Badge variant={snapshot.agent.capabilities.kueue ? 'default' : 'secondary'}>
                    {snapshot.agent.capabilities.kueue ? '已发现' : '未发现'}
                  </Badge>
                </div>
                <div className="capability-line flex items-center justify-between">
                  <span>DRA API</span>
                  <Badge variant={snapshot.agent.capabilities.draApiAvailable ? 'warning' : 'secondary'}>
                    {snapshot.agent.capabilities.draApiAvailable
                      ? snapshot.agent.capabilities.draApiVersion || '已发现，仅 API'
                      : '未发现'}
                  </Badge>
                </div>
                <div className="flex justify-between">
                  <Label>实际注册的 Provider</Label>
                  <strong>{snapshot.agent.capabilities.providers?.join(', ') || '无记录'}</strong>
                </div>
                <div className="flex justify-between">
                  <Label>加速器类</Label>
                  <strong>{snapshot.agent.capabilities.accelerators?.join(', ') || '未上报'}</strong>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      {findings.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle>需要操作员关注</CardTitle>
            <Button variant="outline" size="sm" onClick={() => {}}>
              进入异常待办 →
            </Button>
          </CardHeader>
          <CardContent className="space-y-2">
            {findings.slice(0, 4).map((finding) => (
              <div key={finding.key} className="finding-row flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Badge
                    variant={
                      finding.severity === 'critical' ? 'destructive' : 'secondary'
                    }
                  >
                    {finding.code}
                  </Badge>
                  <strong>{finding.resourceId}</strong>
                  <span className="text-sm text-muted-foreground">{finding.description}</span>
                </div>
                <Button variant="outline" size="sm" onClick={() => {}}>
                  查看证据
                </Button>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle>资源状态预览</CardTitle>
          <Button variant="outline" size="sm" onClick={() => {}}>
            查看所有资源 →
          </Button>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>类别</TableHead>
                <TableHead>资源 ID</TableHead>
                <TableHead>Generation</TableHead>
                <TableHead>同步状态</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {snapshot?.rows.slice(0, 8).map((row) => (
                <TableRow key={row.desired.kind + '/' + row.desired.id}>
                  <TableCell>{row.desired.kind}</TableCell>
                  <TableCell>{row.desired.id}</TableCell>
                  <TableCell>{row.desired.generation}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        row.error
                          ? 'destructive'
                          : row.state?.syncState === 'Synced'
                            ? 'default'
                            : 'secondary'
                      }
                    >
                      {row.error ? '读取失败' : row.state?.syncState || '未获取'}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
              {snapshot.rows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    当前集群无 ComputePool / Workload
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}

function ProjectsView({ projects, findings }: { projects: any[]; findings: Finding[] }) {
  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle>项目 / AI Initiatives</CardTitle>
          <Badge>{projects.length} 个已观察项目分组</Badge>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>项目 ID</TableHead>
                <TableHead>ComputePool</TableHead>
                <TableHead>Workload</TableHead>
                <TableHead>需关注</TableHead>
                <TableHead>声明 GPU 配额</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {projects.map((project) => (
                <TableRow key={project.id}>
                  <TableCell>
                    <strong>{project.id || '未标记项目（不可用于授权）'}</strong>
                  </TableCell>
                  <TableCell>{project.poolCount}</TableCell>
                  <TableCell>{project.workloadCount}</TableCell>
                  <TableCell>
                    <Badge variant={project.needsAttention > 0 ? 'destructive' : 'secondary'}>
                      {project.needsAttention}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {project.poolCount ? (project.declaredPoolGpuQuota ?? '未知') : '—'}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="outline" size="sm" onClick={() => {}}>
                      查看资源 →
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {projects.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground">
                    当前集群没有可归组的资源
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <p className="text-xs text-muted-foreground">
        声明 GPU 配额是 ComputePool.spec.accelerators 中的 quota 总和，仅表示产品意图；不是
        GPU 已分配量、GPU 利用率、Kueue 准入，也不能据此计费。
      </p>
    </div>
  )
}

function ResourcesView({ visibleRows, snapshot }: { visibleRows: ResourceRow[]; snapshot: PortalSnapshot | null }) {
  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <div>
            <CardTitle>ComputePool 与 Workload</CardTitle>
            <p className="text-xs text-muted-foreground">
              Go Desired / Observed Generation · 点击记录查看原始条件和证据
            </p>
          </div>
          <div className="flex gap-2">
            <Select defaultValue="all" onValueChange={(v) => {}}>
              <SelectTrigger className="w-[165px]">
                <SelectValue placeholder="按项目筛选" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">所有项目</SelectItem>
              </SelectContent>
            </Select>
            <Select defaultValue="all" onValueChange={(v) => {}}>
              <SelectTrigger className="w-[155px]">
                <SelectValue placeholder="全部类型" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部类型</SelectItem>
                <SelectItem value="ComputePool">ComputePool</SelectItem>
                <SelectItem value="Workload">Workload</SelectItem>
              </SelectContent>
            </Select>
            <Input placeholder="搜索资源 ID" className="w-[190px]" />
          </div>
        </CardHeader>
        {snapshot?.truncated && (
          <Alert variant="warning" className="mb-4">
            当前授权范围内共有 {snapshot.total} 条资源，已载入 {snapshot.rows.length} 条，可按页继续读取。
          </Alert>
        )}
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[145px]">类型</TableHead>
                <TableHead className="min-w-[180px]">资源 ID</TableHead>
                <TableHead className="w-[120px]">Provider</TableHead>
                <TableHead className="w-[145px]">期望 / 已观察</TableHead>
                <TableHead className="w-[150px]">状态</TableHead>
                <TableHead className="w-[95px]">删除中</TableHead>
                <TableHead className="w-[100px]">证据条目</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleRows.map((row) => (
                <TableRow key={row.desired.kind + '/' + row.desired.id}>
                  <TableCell>{row.desired.kind}</TableCell>
                  <TableCell>{row.desired.id}</TableCell>
                  <TableCell>
                    {String(row.desired.spec.provider ?? '—')}
                  </TableCell>
                  <TableCell>
                    {row.desired.generation} / {row.state?.observedGeneration ?? '—'}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        row.error
                          ? 'destructive'
                          : row.state?.syncState === 'Synced'
                            ? 'default'
                            : 'secondary'
                      }
                    >
                      {row.error ? '读取失败' : row.state?.syncState || '未获取'}
                    </Badge>
                  </TableCell>
                  <TableCell>{row.desired.deletionTimestamp ? '是' : '否'}</TableCell>
                  <TableCell>
                    {row.state?.evidenceRefs?.length ?? '—'}
                  </TableCell>
                </TableRow>
              ))}
              {visibleRows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="text-center text-muted-foreground">
                    没有匹配的实际资源
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
        {snapshot?.nextCursor && (
          <div className="paging-controls flex items-center gap-2 pt-4">
            <Button variant="outline" size="sm" onClick={() => {}}>
              继续加载下一页
            </Button>
            <span className="text-sm text-muted-foreground">
              已加载 {snapshot.rows.length} / {snapshot.total} 条
            </span>
          </div>
        )}
        <p className="text-xs text-muted-foreground">
          Synced 只表示观察 Generation 与期望 Generation 一致，不表示工作负载已 Ready、Kueue 已准入或
          GPU 实际分配成功。
        </p>
      </Card>
    </div>
  )
}

function DiagnosticsView({ findings, onShowFinding }: { findings: Finding[]; onShowFinding: (f: Finding) => void }) {
  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <div>
            <CardTitle>异常待办 / Operations Inbox</CardTitle>
            <p className="text-xs text-muted-foreground">
              从真实 Desired、Observation、Condition 派生，不调用 LLM 推断故障原因
            </p>
          </div>
          <Badge variant={findings.length ? 'warning' : 'secondary'}>
            {findings.length} 条待检查
          </Badge>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[110px]">优先级</TableHead>
                <TableHead className="min-w-[120px]">项目</TableHead>
                <TableHead className="min-w-[155px]">资源</TableHead>
                <TableHead className="w-[180px]">异常类型</TableHead>
                <TableHead className="w-[200px]">证据 / 原因</TableHead>
                <TableHead className="w-[112px] text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {findings.map((finding) => (
                <TableRow key={finding.key}>
                  <TableCell>
                    <Badge
                      variant={
                        finding.severity === 'critical' ? 'destructive' : 'secondary'
                      }
                    >
                      {finding.severity === 'critical' ? '高' : '待确认'}
                    </Badge>
                  </TableCell>
                  <TableCell>{finding.projectId || '未标记'}</TableCell>
                  <TableCell>
                    {finding.kind} / {finding.resourceId}
                  </TableCell>
                  <TableCell>{finding.code}</TableCell>
                  <TableCell>{finding.description}</TableCell>
                  <TableCell className="text-right">
                    <Button variant="outline" size="sm" onClick={() => onShowFinding(finding)}>
                      查看证据 →
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {findings.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground">
                    在已读取的资源样本中，没有匹配的异常；不表示全平台健康
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <p className="text-xs text-muted-foreground">
        异常待办是只读观测分类，状态同步不等于推理就绪。当前不提供修复、重新调度、删除或手动重试
        按钮。
      </p>
    </div>
  )
}

function IntegrationsView({ snapshot }: { snapshot: PortalSnapshot | null }) {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="integration-panel">
          <div className="flex items-start gap-3">
            <span className="integration-symbol">K</span>
            <div>
              <h2 className="text-lg font-semibold">Kueue</h2>
              <p className="text-sm text-muted-foreground">
                当前唯一生产级执行路径。能力是否已安装以 Agent 的实际发现为准。
              </p>
              <Badge
                variant={
                  snapshot?.agent.capabilities.providers?.includes('kueue')
                    ? 'default'
                    : 'secondary'
                }
              >
                {snapshot?.agent.capabilities.providers?.includes('kueue')
                  ? '运行时已注册'
                  : '未注册'}
              </Badge>
            </div>
          </div>
        </Card>
        <Card className="integration-panel">
          <div className="flex items-start gap-3">
            <span className="integration-symbol">L</span>
            <div>
              <h2 className="text-lg font-semibold">llm-d / GAIE</h2>
              <p className="text-sm text-muted-foreground">
                Serving 当前已提供 GAIE InferencePool 只读预检查；尚无模型部署、持续健康检查或
                Serving API 创建闭环。
              </p>
              <Badge variant="warning">只读探测已合并 · 未接入 Portal 数据源</Badge>
            </div>
          </div>
        </Card>
      </div>

    </div>
  )
}

function JobsView({ visibleRows, snapshot }: { visibleRows: ResourceRow[]; snapshot: PortalSnapshot | null }) {
  const workloadRows = visibleRows.filter((row) => row.desired.kind === 'Workload')

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <div>
            <CardTitle>训推任务 / Workloads</CardTitle>
            <p className="text-xs text-muted-foreground">
              从已授权项目的 ComputePool 生成 v0.1 Workload 意图；当前为只读预览。
            </p>
          </div>
          <Badge variant={workloadRows.length ? 'default' : 'secondary'}>
            {workloadRows.length} 条 Workload
          </Badge>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[145px]">项目</TableHead>
                <TableHead className="min-w-[180px]">Workload ID</TableHead>
                <TableHead className="w-[120px]">GPU 类</TableHead>
                <TableHead className="w-[100px]">请求数量</TableHead>
                <TableHead className="w-[145px]">期望 / 已观察</TableHead>
                <TableHead className="w-[150px]">状态</TableHead>
                <TableHead className="w-[100px]">删除中</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {workloadRows.map((row) => {
                const spec = row.desired.spec as Record<string, unknown>
                const accelerator = spec.accelerator as { class?: string; quota?: number } | undefined
                return (
                  <TableRow key={row.desired.kind + '/' + row.desired.id}>
                    <TableCell>{row.desired.spec.projectID ?? '—'}</TableCell>
                    <TableCell>{row.desired.id}</TableCell>
                    <TableCell>{accelerator?.class ?? '—'}</TableCell>
                    <TableCell>{accelerator?.quota ?? '—'}</TableCell>
                    <TableCell>
                      {row.desired.generation} / {row.state?.observedGeneration ?? '—'}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          row.error
                            ? 'destructive'
                            : row.state?.syncState === 'Synced'
                              ? 'default'
                              : 'secondary'
                        }
                      >
                        {row.error ? '读取失败' : row.state?.syncState || '未获取'}
                      </Badge>
                    </TableCell>
                    <TableCell>{row.desired.deletionTimestamp ? '是' : '否'}</TableCell>
                  </TableRow>
                )
              })}
              {workloadRows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="text-center text-muted-foreground">
                    暂无 Workload 资源
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <p className="text-xs text-muted-foreground">
        Workload 创建需通过管理工作台（Management）的 OIDC 项目授权流程完成；Portal 仅提供只读观测。
      </p>
    </div>
  )
}

function ServingView({ snapshot }: { snapshot: PortalSnapshot | null }) {
  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle>在线推理 / Serving</CardTitle>
          <Badge variant="secondary">只读预检查</Badge>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-start gap-3">
            <span className="integration-symbol">L</span>
            <div>
              <h2 className="text-lg font-semibold">llm-d / GAIE</h2>
              <p className="text-sm text-muted-foreground">
                Serving 当前已提供 GAIE InferencePool 只读预检查；尚无模型部署、持续健康检查或
                Serving API 创建闭环。
              </p>
              <Badge variant="warning">只读探测已合并 · 未接入 Portal 数据源</Badge>
            </div>
          </div>

          <div className="bg-muted/50 rounded-lg p-4 text-xs text-muted-foreground">
            <p className="mb-2 font-medium">能力说明</p>
            <ul className="list-disc list-inside space-y-1">
              <li>InferencePool 预检查仅基于 Agent 能力发现，不代表模型已就绪</li>
              <li>持续健康检查、模型版本管理、灰度发布功能尚未实现</li>
              <li>Serving 路由与负载均衡属于后续控制面范围</li>
            </ul>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle>上游调度事实</CardTitle>
          <span className="text-xs text-muted-foreground">能力发现不会自动注册 Provider</span>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>调度器</TableHead>
                <TableHead>已观察版本</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {snapshot?.agent.capabilities.schedulers?.map((s, i) => (
                <TableRow key={i}>
                  <TableCell>{s.name}</TableCell>
                  <TableCell>{s.version || '版本未知'}</TableCell>
                </TableRow>
              ))}
              {snapshot?.agent.capabilities.schedulers?.length === 0 && (
                <TableRow>
                  <TableCell colSpan={2} className="text-center text-muted-foreground">
                    未上报调度器
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
