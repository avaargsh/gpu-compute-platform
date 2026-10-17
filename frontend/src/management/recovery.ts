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
  // A previous request may have committed even when its HTTP ACK was lost.
  // A single tab has only one durable recovery pointer: never overwrite a
  // live different operation without an explicit operator decision.
  // Read before writing, so disabled storage cannot silently hide a pointer.
  const stored = storage.getItem(KEY)
  if (stored !== null) {
    const previous = recoverOperation(storage, now)
    if (!previous) {
      // An expired or malformed record may still identify a committed write.
      // Never replace or erase its raw bytes without operator intervention.
      throw new Error('存在已过期或损坏的操作记录；已阻止新写入。请先调查历史回执，并手动确认清除此记录。')
    }
    if (previous) {
      if (previous.operationId === pointer.operationId &&
          previous.cluster === pointer.cluster &&
          previous.project === pointer.project &&
          previous.kind === pointer.kind &&
          previous.resource === pointer.resource &&
          previous.method === pointer.method) {
        // Idempotent access must not renew the original 24h recovery TTL.
        return previous
      }
      throw new Error('已有尚未核对的操作 ID；请先恢复该操作、查询授权回执并保留记录。确认处理后手动清除此记录，才能发起新写入。')
    }
  }
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
  // Never silently delete unknown bytes: they could be the only remaining
  // evidence of a timed-out mutation whose HTTP ACK was lost.
  if (raw.length > 1024) return null
  let parsed: OperationPointer
  try { parsed = JSON.parse(raw) as OperationPointer } catch { parsed = null as unknown as OperationPointer }
  if (!valid(parsed, now)) return null
  // Copy only the allowlisted fields. Ignore any injected extra properties.
  return {
    operationId: parsed.operationId, cluster: parsed.cluster,
    project: parsed.project, kind: parsed.kind, resource: parsed.resource,
    method: parsed.method, issuedAt: parsed.issuedAt,
  }
}

// Successful authorized receipt verification may retire only its own
// tab-local pointer. A concurrent/newer write or different project must
// never be erased by a late response from a previous request.

// An invalid/expired record is not reusable, but its existence must remain
// visible so the operator can explicitly investigate and discard it.
export function hasUnresolvedRecoveryRecord(storage: Storage, now = Date.now()): boolean {
  try {
    const raw = storage.getItem(KEY)
    return raw !== null && recoverOperation(storage, now) === null
  } catch {
    // Disabled storage cannot grant write authority.
    return true
  }
}

export function retireOperation(
  storage: Storage, scope: Scope, method: 'PUT' | 'DELETE', operationId: string,
  now = Date.now(),
): boolean {
  const previous = recoverOperation(storage, now)
  if (!previous || previous.operationId !== operationId ||
      previous.method !== method || previous.cluster !== scope.cluster ||
      previous.project !== scope.project || previous.kind !== scope.kind ||
      previous.resource !== scope.resource) return false
  // A browser may revoke storage writes between dispatch and receipt
  // verification. Cleanup failure must not turn a verified server receipt
  // into a UI error; the still-present pointer continues to block new writes.
  try {
    storage.removeItem(KEY)
    return storage.getItem(KEY) === null
  } catch {
    return false
  }
}

export function forgetOperation(storage: Storage): void {
  storage.removeItem(KEY)
}
