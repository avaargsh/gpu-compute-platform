import type { DesiredView, Receipt, Scope } from './api'

// A committed audit receipt and current Desired are independent reads.
// Neither proves Kubernetes admission, reconciliation, deletion completion,
// training success, or durable output artifacts.
export type ReadbackStatus =
  | 'desired-visible'
  | 'desired-advanced'
  | 'delete-request-visible'
  | 'deleting-after-commit'
  | 'not-visible'
  | 'inconsistent'

export interface DesiredReadback {
  status: ReadbackStatus
  detail: string
  desired: DesiredView | null
}

const KEY = /^[0-9a-f]{32}$/
const ID = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/

export function receiptResourceScope(receipt: Receipt, cluster: string, project: string, operationId: string): Scope {
  if (!KEY.test(operationId) || receipt.requestId !== operationId ||
      receipt.clusterId !== cluster || receipt.projectId !== project ||
      receipt.status !== 'desired-committed' ||
      !ID.test(cluster) || !ID.test(project) || !ID.test(receipt.resourceId) ||
      (receipt.kind !== 'ComputePool' && receipt.kind !== 'Workload') ||
      !Number.isSafeInteger(receipt.resultingGeneration) || receipt.resultingGeneration < 1 ||
      !Number.isSafeInteger(receipt.expectedGeneration) || receipt.expectedGeneration < 0 ||
      !['create', 'update', 'delete-request'].includes(receipt.action)) {
    throw new Error('回执与原始操作 ID 或授权范围不一致，拒绝跨资源回读')
  }
  return { cluster, project, kind: receipt.kind, resource: receipt.resourceId }
}

export function classifyDesiredReadback(receipt: Receipt, desired: DesiredView | null): DesiredReadback {
  if (desired === null) {
    return {
      status: 'not-visible',
      detail: '操作回执已提交，但该 Desired 当前不可读取（可能已终结、权限已变化或资源不可见）。这不是任务完成、已删除或写入回滚的证明。',
      desired: null,
    }
  }
  if (desired.clusterId !== receipt.clusterId || desired.projectId !== receipt.projectId ||
      desired.kind !== receipt.kind || desired.id !== receipt.resourceId ||
      !Number.isSafeInteger(desired.generation) || desired.generation < 1) {
    throw new Error('回读的 Desired 与审计回执资源范围不匹配')
  }
  if (desired.generation < receipt.resultingGeneration) {
    return {
      status: 'inconsistent',
      detail: '当前 Desired 的 Generation 低于已提交回执，读写证据不一致；停止操作并调查数据来源。',
      desired,
    }
  }
  if (desired.generation > receipt.resultingGeneration) {
    return {
      status: 'desired-advanced',
      detail: '操作回执已提交，但 Desired 已进入更新的 Generation；不能把当前状态归因于原始操作。',
      desired,
    }
  }
  if (receipt.action === 'delete-request') {
    return desired.deleting
      ? { status: 'delete-request-visible', detail: '原始删除请求已提交，当前 Desired 显示 deleting；仍须独立证明 Finalizer 和实际资源清理完成。', desired }
      : { status: 'inconsistent', detail: '原始删除回执已提交，但当前同代 Desired 尚未标记 deleting；需要调查，不可再次盲删。', desired }
  }
  if (desired.deleting) {
    return { status: 'deleting-after-commit', detail: '原始创建/更新已提交，但资源现在正在删除；不能据此宣称原操作运行成功。', desired }
  }
  return {
    status: 'desired-visible',
    detail: '回执确认 Desired 事务已提交，授权回读看到了相同 Generation；不代表 Kueue 已准入、Pod Ready 或训练/推理成功。',
    desired,
  }
}
