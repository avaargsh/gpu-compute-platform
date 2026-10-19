import type { EvidenceCheck } from './evidence-check.ts'

// Read-only browser classification. No output from this module is a signed
// authorization, Kubernetes outcome attestation, or permission to retry writes.
export type OperationVerdictCode =
  | 'OUTCOME_MISMATCH'
  | 'OWNERSHIP_UNPROVEN'
  | 'VERIFICATION_UNAVAILABLE'
  | 'OUTCOME_PENDING'

export interface OperationVerdict {
  code: OperationVerdictCode
  detail: string
  cryptographicallyVerified: false
}

function verdict(code: OperationVerdictCode, detail: string): OperationVerdict {
  return { code, detail, cryptographicallyVerified: false }
}

// The Management endpoints expose an authorized audit receipt, a
// generation-scoped Desired readback and a historical Agent observation.
// They do NOT expose a trusted signer, operation-bound Kubernetes UID, image
// digest or independently verified signed proof. Never mint VERIFIED here.
export function classifyOperationVerdict(check: EvidenceCheck): OperationVerdict {
  const status = check.readback.status
  if (status === 'inconsistent') {
    return verdict('OUTCOME_MISMATCH', '当前 Desired 与操作回执矛盾；必须调查服务端状态。')
  }
  if (status === 'desired-advanced' || status === 'deleting-after-commit') {
    return verdict('OWNERSHIP_UNPROVEN', '资源已经进入另一代或另一操作阶段；当前状态不能归属原始操作。')
  }
  if (status === 'delete-request-visible') {
    return verdict('OUTCOME_PENDING', '仅证明删除请求已提交；尚无目标 UID 消失和 Finalizer 完成的独立证明。')
  }
  if (status === 'not-visible' || !check.execution) {
    return verdict('VERIFICATION_UNAVAILABLE', '缺少可关联的执行观察；404、Desired 或回执均不构成实际资源删除证明。')
  }
  const execution = check.execution
  if (execution.newerThanReceipt) {
    return verdict('OWNERSHIP_UNPROVEN', '观察对应更新的 Generation，不能证明原操作的结果。')
  }
  if (execution.code === 'IDENTITY_CONFLICT' || execution.code === 'OBSERVATION_CONFLICT' ||
      execution.code === 'FAILED') {
    return verdict('OUTCOME_MISMATCH', '现有执行观察与期望成功状态不一致；不能颁发完成证明。')
  }
  if (execution.code === 'SUCCEEDED' || execution.code === 'RUNNING') {
    return verdict('OWNERSHIP_UNPROVEN', 'Agent 报告了运行或完成状态，但缺少操作归属、可信签名及独立 K8s 结果验证。')
  }
  if (['WAITING_QUOTA', 'WAITING_ADMISSION', 'WAITING_PODS', 'DELETING'].includes(execution.code)) {
    return verdict('OUTCOME_PENDING', '资源仍在调度或清理中；不能将中间状态判定为操作已验证成功。')
  }
  return verdict('VERIFICATION_UNAVAILABLE', '观察不存在、已过期或不能验证；请保留原操作 ID，禁止盲目重试。')
}
