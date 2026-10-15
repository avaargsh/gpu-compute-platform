import type { ResourceRow } from './api'

export type WorkloadMode = 'training' | 'batch-inference'

export interface DraftAccelerator {
  class: string
  quota: number
}

export interface DraftPool {
  id: string
  projectId: string
  accelerators: DraftAccelerator[]
}

export interface WorkloadDraftInput {
  id: string
  poolId: string
  acceleratorClass: string
  quota: number
  image: string
  commandJson: string
  mode: WorkloadMode
}

export interface WorkloadDraftPayload {
  metadata: { id: string; generation: number }
  projectId: string
  poolId: string
  spec: {
    image: string
    command: string[]
    accelerator: { class: string; quota: number }
  }
}

export interface WorkloadDraftResult {
  errors: string[]
  payload: WorkloadDraftPayload | null
}

const SAFE_ID = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/

// Read-only desired-state inventory is a source of candidate pools, not
// evidence of admitted GPU capacity. Ignore malformed / deleting pool rows.
export function draftPools(rows: ResourceRow[]): DraftPool[] {
  const pools: DraftPool[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    if (row.desired.kind !== 'ComputePool' || row.desired.deletionTimestamp) continue
    const id = row.desired.id
    const projectId = row.desired.spec.projectID
    if (!SAFE_ID.test(id) || typeof projectId !== 'string' || !SAFE_ID.test(projectId)) continue
    const raw = row.desired.spec.accelerators
    if (!Array.isArray(raw)) continue
    const accelerators: DraftAccelerator[] = []
    const classes = new Set<string>()
    for (const item of raw) {
      if (!item || typeof item !== 'object') continue
      const accelerator = item as Record<string, unknown>
      const cls = accelerator.class
      const quota = accelerator.quota
      if (typeof cls !== 'string' || !SAFE_ID.test(cls) ||
          typeof quota !== 'number' || !Number.isSafeInteger(quota) || quota < 1 || classes.has(cls)) continue
      classes.add(cls)
      accelerators.push({ class: cls, quota })
    }
    if (!accelerators.length || seen.has(id)) continue
    seen.add(id)
    pools.push({ id, projectId, accelerators })
  }
  return pools.sort((a, b) => a.projectId.localeCompare(b.projectId) || a.id.localeCompare(b.id))
}

// This generates the canonical v0.1 Workload intent only; submission must
// pass through a separately authenticated, CAS-protected management write API.
export function buildWorkloadDraft(input: WorkloadDraftInput, pools: DraftPool[]): WorkloadDraftResult {
  const errors: string[] = []
  if (!SAFE_ID.test(input.id)) errors.push('Workload ID 格式错误')
  const matches = pools.filter(pool => pool.id === input.poolId)
  const pool = matches.length === 1 ? matches[0] : undefined
  if (!pool) errors.push('请选择当前授权快照中的唯一 ComputePool')
  const profile = pool?.accelerators.find(a => a.class === input.acceleratorClass)
  if (!profile) errors.push('加速器类型与 ComputePool 不匹配')
  if (!Number.isSafeInteger(input.quota) || input.quota < 1 || !profile || input.quota > profile.quota) {
    errors.push('请求 GPU 数量必须为正整数，且不超过声明的池配额')
  }
  if (!input.image || input.image.length > 512 || /\s/.test(input.image)) {
    errors.push('必须提供合法的容器镜像引用（建议锁定 digest）')
  }
  if (input.mode !== 'training' && input.mode !== 'batch-inference') errors.push('未知任务类型')
  let command: string[] = []
  try {
    const value: unknown = JSON.parse(input.commandJson)
    if (!Array.isArray(value) || value.length === 0 || value.length > 64 ||
      !value.every(part => typeof part === 'string' && part.length > 0 && part.length <= 4096 && !part.includes('\0'))) {
      errors.push('Command 必须是非空 JSON 字符串数组（最多 64 项）')
    } else {
      command = value as string[]
    }
  } catch {
    errors.push('Command 不是合法 JSON 字符串数组')
  }
  if (errors.length || !pool) return { errors, payload: null }
  return {
    errors: [],
    payload: {
      metadata: { id: input.id, generation: 1 },
      projectId: pool.projectId,
      poolId: pool.id,
      spec: {
        image: input.image,
        command,
        accelerator: { class: input.acceleratorClass, quota: input.quota },
      },
    },
  }
}
