import type { Kind, Scope } from './api'

// A single tab-local recovery pointer. NEVER persist bearer tokens, PKCE
// verifier, request bodies, response bodies, or provider-specific secrets.
// A receipt is authoritative only after a fresh server-side OIDC+RBAC check.
const KEY = 'gpu-management-operation-pointer-v1'
const TTL_MS = 24 * 60 * 60 * 1000
const ID = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/
const OPERATION_ID = /^[0-9a-f]{32}$/

export interface OperationPointer {
  operationId: string
  cluster: string
  project: string
  kind: Kind
  resource: string
  method: 'PUT' | 'DELETE'
  issuedAt: number
}

function valid(pointer: OperationPointer, now: number): boolean {
  return typeof pointer === 'object' && pointer !== null &&
    OPERATION_ID.test(pointer.operationId) &&
    [pointer.cluster, pointer.project, pointer.resource].every(x => typeof x === 'string' && ID.test(x)) &&
    (pointer.kind === 'ComputePool' || pointer.kind === 'Workload') &&
    (pointer.method === 'PUT' || pointer.method === 'DELETE') &&
    Number.isSafeInteger(pointer.issuedAt) && pointer.issuedAt <= now &&
    pointer.issuedAt > now - TTL_MS
}

export function rememberOperation(storage: Storage, scope: Scope, method: 'PUT' | 'DELETE', operationId: string, now = Date.now()): OperationPointer {
  const pointer: OperationPointer = {
    operationId, cluster: scope.cluster, project: scope.project,
    kind: scope.kind, resource: scope.resource, method, issuedAt: now,
  }
  if (!valid(pointer, now)) throw new Error('无效的操作记录，拒绝发送写请求')
  // If storage is disabled or full, fail closed BEFORE network dispatch:
  // there would be no reliable way to recover after an ambiguous response.
  storage.setItem(KEY, JSON.stringify(pointer))
  if (storage.getItem(KEY) !== JSON.stringify(pointer)) {
    throw new Error('无法保存本页操作标识，拒绝发送写请求')
  }
  return pointer
}

export function recoverOperation(storage: Storage, now = Date.now()): OperationPointer | null {
  let raw: string | null
  try { raw = storage.getItem(KEY) } catch { return null }
  if (!raw) return null
  if (raw.length > 1024) {
    try { storage.removeItem(KEY) } catch { /* no storage access */ }
    return null
  }
  let parsed: OperationPointer
  try { parsed = JSON.parse(raw) as OperationPointer } catch { parsed = null as unknown as OperationPointer }
  if (!valid(parsed, now)) {
    try { storage.removeItem(KEY) } catch { /* no storage access */ }
    return null
  }
  // Copy only the allowlisted fields. Ignore any injected extra properties.
  return {
    operationId: parsed.operationId, cluster: parsed.cluster,
    project: parsed.project, kind: parsed.kind, resource: parsed.resource,
    method: parsed.method, issuedAt: parsed.issuedAt,
  }
}

export function forgetOperation(storage: Storage): void {
  storage.removeItem(KEY)
}
