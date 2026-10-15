import type { Condition, ResourceRow } from './api'

export type ExecutionCode =
  | 'READ_FAILED' | 'DELETING' | 'NOT_OBSERVED' | 'IDENTITY_CONFLICT'
  | 'STALE_OBSERVATION' | 'OBSERVATION_CONFLICT' | 'FAILED'
  | 'SUCCEEDED' | 'RUNNING' | 'WAITING_QUOTA' | 'WAITING_ADMISSION'
  | 'WAITING_PODS' | 'INDETERMINATE'

export interface ExecutionSummary {
  code: ExecutionCode
  label: string
  detail: string
  projectId: string
  workloadId: string
  generation: number
  conditions: Condition[]
  evidenceRefs: string[]
  workspaceClaim: string | null
}

const TITLES: Record<ExecutionCode, string> = {
  READ_FAILED: '观察读取失败',
  DELETING: '删除中',
  NOT_OBSERVED: '暂无独立观察',
  IDENTITY_CONFLICT: '身份不一致',
  STALE_OBSERVATION: '观察尚未追上',
  OBSERVATION_CONFLICT: '观察结果冲突',
  FAILED: 'Job 失败',
  SUCCEEDED: 'Job 已完成',
  RUNNING: 'Pod 已就绪',
  WAITING_QUOTA: '等待配额',
  WAITING_ADMISSION: '等待 Kueue 准入',
  WAITING_PODS: '等待 Pod 就绪',
  INDETERMINATE: '尚不能判断执行状态',
}

function conditionMap(values: Condition[]): Map<string, Condition> | null {
  const result = new Map<string, Condition>()
  for (const value of values) {
    if (!value || typeof value.type !== 'string' ||
        !['True', 'False', 'Unknown'].includes(value.status) ||
        result.has(value.type)) return null
    result.set(value.type, value)
  }
  return result
}

function detailOf(c?: Condition): string {
  return c?.reason || c?.message || '尚无明确原因'
}

function result(row: ResourceRow, code: ExecutionCode, detail: string): ExecutionSummary {
  const raw = row.desired.spec.projectID
  const workspace = row.desired.spec.workspace
  let workspaceClaim: string | null = null
  if (workspace && typeof workspace === 'object' && !Array.isArray(workspace)) {
    const claim = (workspace as Record<string, unknown>).claimName
    if (typeof claim === 'string') workspaceClaim = claim
  }
  return {
    code, detail, label: TITLES[code],
    projectId: typeof raw === 'string' ? raw : '',
    workloadId: row.desired.id,
    generation: row.desired.generation,
    conditions: row.state?.conditions ?? [],
    evidenceRefs: row.state?.evidenceRefs ?? [],
    workspaceClaim,
  }
}

// Synced means generation convergence only, NOT admission, readiness, GPU
// execution, model correctness or checkpoint persistence. Future-generation
// and malformed Conditions are failed closed before interpreting success.
export function executionOf(row: ResourceRow): ExecutionSummary {
  if (row.desired.kind !== 'Workload') return result(row, 'INDETERMINATE', '非 Workload 资源')
  if (row.error) return result(row, 'READ_FAILED', '独立观察读取失败')
  if (row.desired.deletionTimestamp) return result(row, 'DELETING', '等待 Job / DRA 清理')
  const state = row.state
  if (!state) return result(row, 'NOT_OBSERVED', '尚无 Agent Observation')
  if (state.kind !== 'Workload' || state.id !== row.desired.id ||
      state.desiredGeneration !== row.desired.generation) {
    return result(row, 'IDENTITY_CONFLICT', '观察身份与 Desired 不符')
  }
  if (state.observedGeneration > row.desired.generation || state.syncState === 'Inconsistent') {
    return result(row, 'OBSERVATION_CONFLICT', '观察版本超前')
  }
  if (state.observedGeneration !== row.desired.generation || state.syncState !== 'Synced') {
    return result(row, 'STALE_OBSERVATION', '观察 Generation 尚未收敛')
  }
  const conditions = conditionMap(state.conditions ?? [])
  if (!conditions) return result(row, 'OBSERVATION_CONFLICT', '重复或格式错误的 Condition')
  const success = conditions.get('Succeeded')
  const failed = conditions.get('Failed')
  const ready = conditions.get('Ready')
  const pods = conditions.get('PodsReady')
  const admitted = conditions.get('Admitted')
  const reserved = conditions.get('QuotaReserved')
  if (success?.status === 'True' && failed?.status === 'True') {
    return result(row, 'OBSERVATION_CONFLICT', '同时报告成功与失败')
  }
  if (failed?.status === 'True') return result(row, 'FAILED', detailOf(failed))
  if (success?.status === 'True') {
    if (admitted?.status === 'False' || reserved?.status === 'False') {
      return result(row, 'OBSERVATION_CONFLICT', '成功与未准入/未预留配额矛盾')
    }
    return result(row, 'SUCCEEDED', detailOf(success))
  }
  if (reserved?.status === 'False') return result(row, 'WAITING_QUOTA', detailOf(reserved))
  if (admitted?.status === 'False') return result(row, 'WAITING_ADMISSION', detailOf(admitted))
  if (pods?.status === 'True' || ready?.status === 'True') {
    if (admitted?.status !== 'True' || reserved?.status !== 'True') {
      return result(row, 'OBSERVATION_CONFLICT', 'Pod 就绪但缺少已准入与配额证据')
    }
    return result(row, 'RUNNING', detailOf(pods ?? ready))
  }
  if (admitted?.status === 'True' && reserved?.status === 'True') {
    return result(row, 'WAITING_PODS', detailOf(pods))
  }
  return result(row, 'INDETERMINATE', '没有可验证的 Kueue 执行条件')
}

const SAFE_PART = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/
const SAFE_DNS = /^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$/
const SAFE_CONTEXT = /^[a-zA-Z0-9][a-zA-Z0-9_.:@/-]{0,127}$/

export function verifiedJobEvidence(row: ResourceRow, clusterId: string): {
  namespace: string; job: string
} | null {
  if (!SAFE_PART.test(clusterId) || row.desired.kind !== 'Workload') return null
  const summary = executionOf(row)
  if (['READ_FAILED', 'DELETING', 'NOT_OBSERVED', 'IDENTITY_CONFLICT',
       'STALE_OBSERVATION', 'OBSERVATION_CONFLICT'].includes(summary.code)) return null
  const matches: { namespace: string; job: string }[] = []
  for (const ref of summary.evidenceRefs) {
    const match = /^k8s:\/\/([^/]+)\/namespaces\/([^/]+)\/jobs\/([^/]+)$/.exec(ref)
    if (!match) continue
    const [, cluster, namespace, job] = match
    if (cluster !== clusterId || !SAFE_DNS.test(namespace) || !SAFE_DNS.test(job) ||
        namespace.length > 63 || job.length > 63) return null
    matches.push({ namespace, job })
  }
  if (matches.length !== 1) return null
  return matches[0]
}

// This is a copy-only command. Browser never executes kubectl; operator
// supplies a separate verified kubeconfig context on their local machine.
export function localLogCommand(row: ResourceRow, clusterId: string, context: string): string | null {
  if (!SAFE_CONTEXT.test(context) || context.startsWith('-')) return null
  const ref = verifiedJobEvidence(row, clusterId)
  if (!ref) return null
  return 'kubectl --context ' + context + ' --namespace ' + ref.namespace +
    ' logs job/' + ref.job + ' --all-containers=true --tail=200'
}

export function conditionFlag(row: ResourceRow, type: string): 'True' | 'False' | 'Unknown' {
  const summary = executionOf(row)
  if (['READ_FAILED', 'DELETING', 'NOT_OBSERVED', 'IDENTITY_CONFLICT',
       'STALE_OBSERVATION', 'OBSERVATION_CONFLICT'].includes(summary.code)) return 'Unknown'
  const found = summary.conditions.find(c => c.type === type)
  return found?.status === 'True' || found?.status === 'False' ? found.status : 'Unknown'
}
