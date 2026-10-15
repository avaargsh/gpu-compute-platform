import type { DesiredView, ExecutionView, Receipt } from './api'
import { classifyDesiredReadback, type DesiredReadback } from './readback'
import { classifyScopedExecution, type ScopedExecution } from './execution'

// These endpoints are independent, sequentially authorized GETs — NOT a
// database snapshot transaction. Reject a mixed-generation pair rather than
// showing apparently current Kueue success from a different Desired version.
export interface EvidenceCheck {
  readback: DesiredReadback
  execution: ScopedExecution | null
}

export function verifyEvidencePair(
  receipt: Receipt,
  desired: DesiredView | null,
  observed: ExecutionView | null,
  now = Date.now(),
): EvidenceCheck {
  const readback = classifyDesiredReadback(receipt, desired)
  // No evidence of deletion, access or a current resource. A different
  // generation belongs to a NEW operation; do not ascribe its Job state to
  // the original receipt. A contradictory DB generation is fail-closed.
  if (!desired || readback.status === 'inconsistent' || readback.status === 'desired-advanced') {
    if (observed !== null) throw new Error('不允许将未知或不同代的执行观察归因于原始操作')
    return { readback, execution: null }
  }
  if (receipt.kind !== 'Workload') {
    if (observed !== null) throw new Error('ComputePool 不支持 Workload 执行观察')
    return { readback, execution: null }
  }
  if (!observed) throw new Error('服务端未提供有效的独立执行观察响应')
  if (observed.desiredGeneration !== desired.generation || observed.deleting !== desired.deleting) {
    throw new Error('Desired 与执行观察来自不同 Generation 或删除阶段；停止判定，请手动重新读取')
  }
  return { readback, execution: classifyScopedExecution(observed, receipt, now) }
}
