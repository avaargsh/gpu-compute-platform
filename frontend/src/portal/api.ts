// Go v0.1 control-plane contracts only. Do not use the legacy Python /api endpoints.
export interface ClusterCapabilities {
  kueue: boolean
  serving: boolean
  schedulers?: Array<{ name: string; version?: string }>
  providers?: string[]
  draApiAvailable: boolean
  draApiVersion?: string
  accelerators?: string[]
}
export interface AgentStatus {
  clusterId: string
  agentVersion: string
  kubernetesVersion: string
  capabilities: ClusterCapabilities
  registeredAt: string
  lastHeartbeatAt?: string
}
export interface DesiredResource {
  kind: string
  id: string
  generation: number
  spec: Record<string, unknown>
  deletionTimestamp?: string | null
  finalizers?: string[]
}
export interface Condition {
  type: string
  status: string
  reason?: string
  message?: string
  lastTransitionTime?: string
}
export interface ResourceState {
  kind: string
  id: string
  desiredGeneration: number
  observedGeneration: number
  syncState: string
  conditions?: Condition[]
  evidenceRefs?: string[]
  observationLeaseOwner?: string
}
export interface ResourceRow {
  desired: DesiredResource
  state: ResourceState | null
  error: string | null
}
export interface PortalSnapshot {
  agent: AgentStatus
  rows: ResourceRow[]
  total: number
  truncated: boolean
  refreshedAt: string
}

const MAX_ROWS = 120
const MAX_CONCURRENT = 6
const SEGMENT = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/

export function isSafeSegment(value: string): boolean {
  return SEGMENT.test(value)
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    method: 'GET',
    credentials: 'omit',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
  if (!response.ok) throw new Error('Go Control Plane returned HTTP ' + response.status)
  return response.json() as Promise<T>
}

export async function loadSnapshot(clusterId: string): Promise<PortalSnapshot> {
  if (!isSafeSegment(clusterId)) {
    throw new Error('Cluster ID 只能使用字母、数字、点、下划线和连字符')
  }
  const cluster = encodeURIComponent(clusterId)
  const [agent, resources] = await Promise.all([
    getJSON<AgentStatus>('/api/v1/clusters/' + cluster + '/status'),
    getJSON<DesiredResource[]>('/api/v1/agent/desired?clusterId=' + cluster),
  ])
  if (!Array.isArray(resources)) throw new Error('Go API 返回的 Desired 列表格式错误')
  const supported = resources.filter(item => item.kind === 'ComputePool' || item.kind === 'Workload')
  const toLoad = supported.slice(0, MAX_ROWS)
  const rows: ResourceRow[] = toLoad.map(desired => ({ desired, state: null, error: null }))
  let cursor = 0

  async function worker() {
    while (cursor < rows.length) {
      const index = cursor++
      const row = rows[index]
      const { kind, id } = row.desired
      if (!isSafeSegment(id)) {
        row.error = '资源 ID 不能安全地用于 HTTP 路径'
        continue
      }
      try {
        row.state = await getJSON<ResourceState>(
          '/api/v1/internal/clusters/' + cluster +
          '/state/' + encodeURIComponent(kind) + '/' + encodeURIComponent(id),
        )
      } catch (error) {
        row.error = error instanceof Error ? error.message : '读取状态失败'
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(MAX_CONCURRENT, rows.length) }, () => worker()))
  return {
    agent, rows, total: supported.length, truncated: supported.length > MAX_ROWS,
    refreshedAt: new Date().toISOString(),
  }
}
