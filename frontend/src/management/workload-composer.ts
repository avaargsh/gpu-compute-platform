import type { DesiredView, Scope } from './api'

// Pure, browser-side draft validation only. The trusted Management API
// rechecks the user's live OIDC grant, Project/Pool binding, Generation,
// Workload immutability and Kueue provider within one serializable write.
const SAFE_ID = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/

export interface AuthorizedPool {
  cluster: string
  project: string
  id: string
  generation: number
  accelerators: Array<{ class: string; quota: number }>
}

export interface WorkloadForm {
  id: string
  image: string
  acceleratorClass: string
  quota: number
  commandJSON: string
}

export function parseAuthorizedPool(cluster: string, project: string, desired: DesiredView): AuthorizedPool {
  if (!SAFE_ID.test(cluster) || !SAFE_ID.test(project) ||
      desired.clusterId !== cluster || desired.projectId !== project ||
      desired.kind !== 'ComputePool' || !SAFE_ID.test(desired.id) ||
      desired.deleting || !Number.isSafeInteger(desired.generation) || desired.generation < 1 ||
      desired.spec.projectID !== project || desired.spec.provider !== 'kueue') {
    throw new Error('授权读取的 ComputePool 与当前项目、集群或 Kueue Provider 不一致')
  }
  const raw = desired.spec.accelerators
  const bindings = desired.spec.acceleratorBindings
  if (!Array.isArray(raw) || !Array.isArray(bindings) || raw.length === 0) {
    throw new Error('ComputePool 缺少有效的加速器与绑定信息')
  }
  const boundClasses = new Set<string>()
  for (const binding of bindings) {
    if (typeof binding !== 'object' || binding === null) continue
    const candidate = binding as Record<string, unknown>
    if (typeof candidate.class === 'string' && SAFE_ID.test(candidate.class) &&
        typeof candidate.resourceName === 'string' && candidate.resourceName.length > 0 &&
        typeof candidate.flavor === 'string' && candidate.flavor.length > 0) {
      boundClasses.add(candidate.class)
    }
  }
  const accelerators: AuthorizedPool['accelerators'] = []
  const classes = new Set<string>()
  for (const item of raw) {
    if (typeof item !== 'object' || item === null) continue
    const a = item as Record<string, unknown>
    if (typeof a.class === 'string' && SAFE_ID.test(a.class) &&
        typeof a.quota === 'number' && Number.isSafeInteger(a.quota) && a.quota > 0 &&
        boundClasses.has(a.class) && !classes.has(a.class)) {
      classes.add(a.class)
      accelerators.push({ class: a.class, quota: a.quota })
    }
  }
  if (!accelerators.length) throw new Error('ComputePool 中没有可用的加速器类绑定')
  return { cluster, project, id: desired.id, generation: desired.generation, accelerators }
}

export function buildAuthorizedWorkloadRequest(
  pool: AuthorizedPool, form: WorkloadForm, selectedCluster: string, selectedProject: string,
): { target: Scope; requestJSON: string } {
  if (pool.cluster !== selectedCluster || pool.project !== selectedProject) {
    throw new Error('项目或集群已切换，请重新读取授权 ComputePool')
  }
  if (!SAFE_ID.test(form.id)) throw new Error('Workload ID 格式无效')
  const accelerator = pool.accelerators.find(item => item.class === form.acceleratorClass)
  if (!accelerator || !Number.isSafeInteger(form.quota) ||
      form.quota < 1 || form.quota > accelerator.quota) {
    throw new Error('请求的加速器类型或数量超出所选 ComputePool 声明配额')
  }
  if (!form.image || form.image.length > 512 || /\s/.test(form.image)) {
    throw new Error('容器镜像引用无效；建议固定 sha256 digest')
  }
  let command: unknown
  try { command = JSON.parse(form.commandJSON) } catch {
    throw new Error('Command 不是合法 JSON argv 数组')
  }
  if (!Array.isArray(command) || command.length === 0 || command.length > 64 ||
      !command.every(arg => typeof arg === 'string' && arg.length > 0 &&
        arg.length <= 4096 && !arg.includes('\0'))) {
    throw new Error('Command 必须是 1–64 项非空 argv 数组')
  }

  // This is the current management API contract, NOT the old dev-operator
  // DTO: the pool ID belongs at top level, and expectedGeneration is zero.
  // No workspace/PVC promise: the current Management backend does not
  // yet persist that field for this Workload type.
  const target: Scope = {
    cluster: pool.cluster, project: pool.project, kind: 'Workload', resource: form.id,
  }
  return {
    target,
    requestJSON: JSON.stringify({
      expectedGeneration: 0, poolId: pool.id,
      spec: { image: form.image, command, accelerator: { class: accelerator.class, quota: form.quota } },
    }, null, 2),
  }
}
