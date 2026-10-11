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
  nextCursor?: string
  refreshedAt: string
}

// The production legacy Agent and Python endpoints are never called by this
// operator preview. Credential is provided by the operator and kept only in
// the current component memory; no cookies/localStorage or shared session.
const MAX_ROWS = 120
const SEGMENT = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/

export function isSafeSegment(value: string): boolean {
  return SEGMENT.test(value)
}

export async function loadSnapshot(clusterId: string, operatorToken: string, cursor = ''): Promise<PortalSnapshot> {
  if (!isSafeSegment(clusterId)) {
    throw new Error('Cluster ID 只能使用字母、数字、点、下划线和连字符')
  }
  if (operatorToken.length < 32) {
    throw new Error('必须提供至少 32 字符的本机操作员凭证')
  }
  const params = new URLSearchParams({ limit: String(MAX_ROWS) })
  if (cursor) params.set('cursor', cursor)
  const url = '/api/v1/operator/clusters/' + encodeURIComponent(clusterId) + '/snapshot?' + params.toString()
  const response = await fetch(url, {
    method: 'GET',
    credentials: 'omit',
    headers: {
      Accept: 'application/json',
      Authorization: 'Bearer ' + operatorToken,
    },
    cache: 'no-store',
    redirect: 'error',
  })
  if (!response.ok) {
    if (response.status === 401) throw new Error('凭证无效或服务端未启用操作员预览')
    if (response.status === 404) throw new Error('当前凭证不能访问该集群，或该集群尚未注册')
    throw new Error('受保护管理 API 返回 HTTP ' + response.status)
  }
  const snapshot = await response.json() as PortalSnapshot
  if (!snapshot || !snapshot.agent || !Array.isArray(snapshot.rows) || typeof snapshot.total !== 'number') {
    throw new Error('受保护管理 API 返回了无效数据')
  }
  return snapshot
}
