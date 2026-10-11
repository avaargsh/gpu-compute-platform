import type { ResourceRow } from './api'

export type FindingSeverity = 'critical' | 'warning'

export interface Finding {
  key: string
  projectId: string
  resourceId: string
  kind: string
  severity: FindingSeverity
  code: string
  description: string
}

export interface ProjectSummary {
  id: string
  poolCount: number
  workloadCount: number
  needsAttention: number
  declaredPoolGpuQuota: number | null
  rows: ResourceRow[]
}

// Only report an explicitly present projectID. An unknown project is not a
// tenant identity and MUST NOT be used for authorization decisions.
export function projectIdOf(row: ResourceRow): string {
  const raw = row.desired.spec.projectID
  return typeof raw === 'string' && raw.trim().length > 0 ? raw : ''
}

// A resource generation being Synced is NOT inference/model readiness,
// admission, or ownership proof. Do not promote it to a generic Healthy label.
export function findingsFor(row: ResourceRow): Finding[] {
  const { desired, state } = row
  const projectId = projectIdOf(row)
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
    return [{ ...base, severity: 'warning', code: 'DELETION_PENDING', description: '已请求删除，等待 Provider 清理与 Finalizer' }]
  }
  if (!state) {
    return [{ ...base, severity: 'warning', code: 'OBSERVATION_MISSING', description: '缺少独立观察结果' }]
  }
  if (state.kind !== desired.kind || state.id !== desired.id ||
      state.desiredGeneration !== desired.generation) {
    return [{ ...base, severity: 'critical', code: 'IDENTITY_MISMATCH', description: '观察身份或期望 Generation 不匹配' }]
  }
  if (state.syncState === 'Inconsistent' || state.observedGeneration > desired.generation) {
    return [{ ...base, severity: 'critical', code: 'GENERATION_CONFLICT', description: '观察版本超前或不一致' }]
  }
  const conditions = state.conditions ?? []
  const blocked = conditions.filter(c => (
    c.status === 'False' && ['Ready', 'Admitted', 'QuotaReserved'].includes(c.type)
  ))
  if (blocked.length) {
    return blocked.map((condition, index) => ({
      ...base,
      key: base.key + ':condition:' + index,
      severity: 'warning' as const,
      code: condition.type.toUpperCase() + '_FALSE',
      description: condition.reason || condition.message || (condition.type + ' 为 False'),
    }))
  }
  if (state.syncState !== 'Synced' || state.observedGeneration !== desired.generation) {
    return [{ ...base, severity: 'warning', code: 'GENERATION_PENDING', description: '期望与观察 Generation 尚未收敛' }]
  }
  return []
}

function quotaOf(row: ResourceRow): number | null {
  if (row.desired.kind !== 'ComputePool') return null
  const accelerators = row.desired.spec.accelerators
  if (!Array.isArray(accelerators)) return null
  let result = 0
  for (const item of accelerators) {
    if (item === null || typeof item !== 'object') return null
    const quota = (item as Record<string, unknown>).quota
    if (typeof quota !== 'number' || !Number.isSafeInteger(quota) || quota < 0) return null
    result += quota
    if (!Number.isSafeInteger(result)) return null
  }
  return result
}

export function projectSummaries(rows: ResourceRow[]): ProjectSummary[] {
  const groups = new Map<string, ProjectSummary>()
  for (const row of rows) {
    const id = projectIdOf(row)
    let project = groups.get(id)
    if (!project) {
      project = { id, poolCount: 0, workloadCount: 0, needsAttention: 0, declaredPoolGpuQuota: 0, rows: [] }
      groups.set(id, project)
    }
    project.rows.push(row)
    if (row.desired.kind === 'ComputePool') {
      project.poolCount += 1
      const quota = quotaOf(row)
      if (project.declaredPoolGpuQuota !== null) {
        project.declaredPoolGpuQuota = quota === null ? null : project.declaredPoolGpuQuota + quota
      }
    } else if (row.desired.kind === 'Workload') {
      project.workloadCount += 1
    }
    if (findingsFor(row).length > 0) project.needsAttention += 1
  }
  return [...groups.values()].sort((a, b) => {
    if (!a.id) return 1
    if (!b.id) return -1
    return a.id.localeCompare(b.id)
  })
}

export function triageFindings(rows: ResourceRow[]): Finding[] {
  const findings = rows.flatMap(findingsFor)
  return findings.sort((a, b) => {
    if (a.severity !== b.severity) return a.severity === 'critical' ? -1 : 1
    return a.key.localeCompare(b.key)
  })
}
