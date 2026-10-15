import type { ExecutionView, Receipt } from './api'
import type { ResourceRow } from '../portal/api'
import { executionOf, type ExecutionSummary } from '../portal/execution-model.ts'

export type ScopedExecution = ExecutionSummary & { observedAt: string | null; newerThanReceipt: boolean }

export function classifyScopedExecution(snapshot: ExecutionView, receipt: Receipt, now = Date.now()): ScopedExecution {
  if (receipt.kind !== 'Workload' || snapshot.kind !== 'Workload' ||
    receipt.clusterId !== snapshot.clusterId || receipt.projectId !== snapshot.projectId ||
    receipt.resourceId !== snapshot.id ||
    !Number.isSafeInteger(snapshot.desiredGeneration) || snapshot.desiredGeneration < receipt.resultingGeneration ||
    snapshot.desiredGeneration < 1) {
    throw new Error('执行观察与受保护操作回执身份/Generation 不一致')
  }
  const newerThanReceipt = snapshot.desiredGeneration > receipt.resultingGeneration
  const result = (code: ExecutionSummary['code'], detail: string): ScopedExecution => ({
    code, label: code === 'NOT_OBSERVED' ? '暂无独立观察' : '观察已过期/不可验证',
    detail, projectId: snapshot.projectId, workloadId: snapshot.id,
    generation: snapshot.desiredGeneration, conditions: [], evidenceRefs: [],
    workspaceClaim: null, observedAt: snapshot.observedAt, newerThanReceipt,
  })
  if (snapshot.deleting) return result('DELETING', 'Desired 正在删除；这不是 Kueue Job、DRA 或 Finalizer 清理完成证明')
  if (snapshot.observedGeneration === null) return result('NOT_OBSERVED', '尚无独立 Agent Observation；Desired 已提交不等于 Kueue 已准入')
  if (!Number.isSafeInteger(snapshot.observedGeneration) || snapshot.observedGeneration < 0 ||
    !snapshot.observedAt || !Array.isArray(snapshot.conditions)) {
    throw new Error('无效的 Agent 观察版本、时间或 Condition')
  }
  const observedTime = Date.parse(snapshot.observedAt)
  // A stale last-observed snapshot is never displayed as a current success.
  // It can still be inspected separately by the operator as historical data.
  if (!Number.isFinite(observedTime) || observedTime > now + 120_000 ||
    observedTime < now - 600_000) {
    return result('STALE_OBSERVATION', 'Agent 观察时间超过 10 分钟或时钟不可信；不能宣称当前任务已成功')
  }
  const row: ResourceRow = {
    desired: {
      kind: 'Workload', id: snapshot.id, generation: snapshot.desiredGeneration,
      spec: { projectID: snapshot.projectId },
    },
    state: {
      kind: 'Workload', id: snapshot.id,
      desiredGeneration: snapshot.desiredGeneration,
      observedGeneration: snapshot.observedGeneration,
      syncState: snapshot.observedGeneration === snapshot.desiredGeneration ? 'Synced' : 'Pending',
      conditions: snapshot.conditions, evidenceRefs: [],
    },
    error: null,
  }
  const summary = executionOf(row)
  return { ...summary, observedAt: snapshot.observedAt, newerThanReceipt }
}
