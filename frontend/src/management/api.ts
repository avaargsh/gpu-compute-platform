// Browser-facing client for the *isolated* Go Management API.
// Relative same-origin paths only; no legacy Agent route, no user-supplied URL.
export type Kind = 'ComputePool' | 'Workload'
export type Method = 'GET' | 'PUT' | 'DELETE'

const SEGMENT = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/
const KEY = /^[0-9a-f]{32}$/

export interface Scope {
  cluster: string
  project: string
  kind: Kind
  resource: string
}

export interface DesiredView {
  clusterId: string
  projectId: string
  kind: Kind
  id: string
  generation: number
  deleting: boolean
  spec: Record<string, unknown>
}

export interface Receipt {
  requestId: string
  status: 'desired-committed'
  clusterId: string
  projectId: string
  kind: Kind
  resourceId: string
  action: string
  expectedGeneration: number
  resultingGeneration: number
  committedAt: string
  requestDigest: string
}

// Never apply an If-Match from a previously selected resource after the
// operator changes cluster, project, kind or resource fields.
export function desiredMatchesScope(desired: DesiredView, scope: Scope): boolean {
  return desired.clusterId === scope.cluster &&
    desired.projectId === scope.project &&
    desired.kind === scope.kind &&
    desired.id === scope.resource
}

export class ManagementHTTPError extends Error {
  readonly status: number
  constructor(status: number, detail: string) {
    super(detail)
    this.status = status
  }
}

function safe(segment: string): void {
  if (!SEGMENT.test(segment)) throw new Error('无效的集群、项目或资源标识符')
}

function resourcePath(scope: Scope): string {
  safe(scope.cluster); safe(scope.project); safe(scope.resource)
  if (scope.kind !== 'ComputePool' && scope.kind !== 'Workload') throw new Error('只支持 ComputePool / Workload')
  return '/api/v1/management/clusters/' + scope.cluster + '/projects/' + scope.project +
    '/resources/' + scope.kind + '/' + scope.resource
}

function receiptPath(cluster: string, project: string, operationId: string): string {
  safe(cluster); safe(project)
  if (!KEY.test(operationId)) throw new Error('必须使用原始的 32 位十六进制操作 ID')
  return '/api/v1/management/clusters/' + cluster + '/projects/' + project + '/operations/' + operationId
}

function credential(token: string): void {
  if (token.length < 8 || /\s/.test(token)) throw new Error('缺少有效的 OIDC Access Token')
}

async function request<T>(path: string, method: Method, token: string, payload?: string, key?: string, generation?: number): Promise<{ status: number; value: T | null }> {
  credential(token)
  const headers: Record<string, string> = {
    Authorization: 'Bearer ' + token,
    Accept: 'application/json',
  }
  if (method === 'PUT' || method === 'DELETE') {
    if (!key || !KEY.test(key)) throw new Error('写操作必须携带原始 Idempotency-Key')
    headers['Idempotency-Key'] = key
  }
  if (method === 'PUT') {
    if (!payload || !JSON.parse(payload)) throw new Error('必须提供 JSON 请求体')
    headers['Content-Type'] = 'application/json'
  }
  if (method === 'DELETE') {
    if (!Number.isSafeInteger(generation) || (generation ?? 0) < 1) throw new Error('删除需要准确的 Generation')
    headers['If-Match'] = '"' + generation + '"'
  }
  const resp = await fetch(path, {
    method, headers, body: payload, credentials: 'omit', redirect: 'error', cache: 'no-store',
    signal: AbortSignal.timeout(15000),
  })
  if (!resp.ok) {
    const reasons: Record<number, string> = {
      400: '请求参数无效',
      401: '身份过期或被撤销，请重新登录',
      403: '当前项目没有操作权限',
      404: '无可访问的资源或操作回执',
      409: '资源版本已变化、资源仍被占用或操作 ID 冲突',
      428: '缺少 Generation 或幂等操作 ID',
      503: '身份服务或数据库暂时不可用，先查询原操作 ID 再重试',
    }
    throw new ManagementHTTPError(resp.status, reasons[resp.status] ?? 'Management API HTTP ' + resp.status)
  }
  const value = resp.status === 204 || resp.status === 202 ? null : await resp.json() as T
  return { status: resp.status, value }
}

export async function readResource(scope: Scope, token: string): Promise<{ status: number; value: DesiredView | null }> {
  return request<DesiredView>(resourcePath(scope), 'GET', token)
}

export async function writeResource(scope: Scope, token: string, rawJSON: string, operationId: string): Promise<{ status: number; value: null }> {
  return request<null>(resourcePath(scope), 'PUT', token, rawJSON, operationId)
}

export async function deleteResource(scope: Scope, token: string, generation: number, operationId: string): Promise<{ status: number; value: null }> {
  return request<null>(resourcePath(scope), 'DELETE', token, undefined, operationId, generation)
}

export async function readReceipt(cluster: string, project: string, operationId: string, token: string): Promise<{ status: number; value: Receipt | null }> {
  return request<Receipt>(receiptPath(cluster, project, operationId), 'GET', token)
}
