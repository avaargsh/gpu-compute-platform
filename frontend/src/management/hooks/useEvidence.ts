'use client'

import { useCallback, useRef, useState } from 'react'
import { readReceipt, readResource, readExecution, type Receipt, type Scope } from '@/management/api'
import { classifyDesiredReadback, receiptResourceScope, type DesiredReadback } from '@/management/readback'
import { classifyScopedExecution } from '@/management/execution'
import { verifyEvidencePair, type EvidenceCheck } from '@/management/evidence-check'

// Revocation, project/operation switching and a newer read all supersede old
// network results. No late response or error may republish an old evidence chain.
export class StaleEvidenceReadError extends Error {
  constructor() {
    super('证据链查询已失效')
    this.name = 'StaleEvidenceReadError'
  }
}

export interface UseEvidenceReturn {
  check: EvidenceCheck | null
  loading: boolean
  message: string
  error: string
  invalidate: () => void
  readBackDesired: (
    receipt: Receipt, cluster: string, project: string, operationId: string, token: string
  ) => Promise<{ check: EvidenceCheck; message: string }>
  readExecutionObservation: (
    audit: Receipt, readback: DesiredReadback, cluster: string,
    project: string, operationId: string, token: string
  ) => Promise<{ check: EvidenceCheck; message: string }>
  refreshEvidenceChain: (
    cluster: string, project: string, requestId: string, token: string
  ) => Promise<{ check: EvidenceCheck; message: string; receipt: Receipt }>
}

export function useEvidence(): UseEvidenceReturn {
  const [check, setCheck] = useState<EvidenceCheck | null>(null)
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const epochRef = useRef(0)

  const invalidate = useCallback(() => {
    epochRef.current += 1
    setCheck(null)
    setLoading(false)
    setMessage('')
    setError('')
  }, [])

  // A single request epoch owns all three async evidence operations, including
  // intermediate reads in a multi-step chain. Check immediately after each
  // network await, before another scoped request is dispatched.
  const perform = useCallback(async (
    work: (assertCurrent: () => void) => Promise<EvidenceCheck>,
    successMessage: string
  ): Promise<{ check: EvidenceCheck; message: string }> => {
    const epoch = ++epochRef.current
    const assertCurrent = () => {
      if (epoch !== epochRef.current) throw new StaleEvidenceReadError()
    }
    setCheck(null)
    setLoading(true)
    setMessage('')
    setError('')
    try {
      const evidence = await work(assertCurrent)
      assertCurrent()
      setCheck(evidence)
      setMessage(successMessage)
      return { check: evidence, message: successMessage }
    } catch (e) {
      const msg = e instanceof Error ? e.message : '证据链查询失败'
      if (epoch === epochRef.current) {
        setCheck(null)
        setMessage('')
        setError(msg)
      }
      throw e instanceof Error ? e : new Error(msg)
    } finally {
      if (epoch === epochRef.current) setLoading(false)
    }
  }, [])

  const readBackDesiredFn = useCallback(async (
    receipt: Receipt, cluster: string, project: string, _operationId: string, token: string
  ) => perform(async assertCurrent => {
    const target: Scope = {
      cluster, project, kind: receipt.kind, resource: receipt.resourceId,
    }
    let current
    try {
      const result = await readResource(target, token)
      assertCurrent()
      current = result.value ?? null
    } catch (e) {
      assertCurrent()
      if ((e as { status?: number }).status !== 404) throw e
      current = null
    }
    const readback = classifyDesiredReadback(receipt, current)
    return { readback, execution: null }
  }, '操作回执与当前 Desired 已分开核对；没有 Kubernetes 执行或清理完成证明'), [perform])

  const readExecutionObservationFn = useCallback(async (
    audit: Receipt, readback: DesiredReadback,
    cluster: string, project: string, _operationId: string, token: string
  ) => {
    if (!audit || audit.kind !== 'Workload' || !readback?.desired) {
      // No previous project's cached check may become a fallback for this call.
      return perform(async () => ({ readback, execution: null }), '')
    }
    return perform(async assertCurrent => {
      const target: Scope = { cluster, project, kind: audit.kind, resource: audit.resourceId }
      const result = await readExecution(target, token)
      assertCurrent()
      if (!result.value) throw new Error('执行观察没有有效数据，请重新查询')
      return { readback, execution: classifyScopedExecution(result.value, audit) }
    }, '已读取授权范围内的独立 Agent/Kueue 观察；仅供诊断，并非实时 Kubernetes 状态证明')
  }, [perform])

  const refreshEvidenceChainFn = useCallback(async (
    cluster: string, project: string, requestId: string, token: string
  ) => {
    let verifiedReceipt: Receipt | null = null
    const result = await perform(async assertCurrent => {
    const receiptResult = await readReceipt(cluster, project, requestId, token)
    assertCurrent()
    if (!receiptResult.value) throw new Error('服务端未返回原始审计回执')
    const audit = receiptResult.value
    const target = receiptResourceScope(audit, cluster, project, requestId)
    verifiedReceipt = audit
    let current
    try {
      const result = await readResource(target, token)
      assertCurrent()
      current = result.value ?? null
    } catch (e) {
      assertCurrent()
      if ((e as { status?: number }).status !== 404) throw e
      current = null
    }
    const readback = classifyDesiredReadback(audit, current)
    let observed = null
    if (target.kind === 'Workload' && current &&
        !['inconsistent', 'desired-advanced'].includes(readback.status)) {
      const result = await readExecution(target, token)
      assertCurrent()
      observed = result.value ?? null
    }
    return verifyEvidencePair(audit, current, observed)
    }, '原始操作回执、当前 Desired 和独立观察已完成分层核对；未发起写入或自动重试')
    if (!verifiedReceipt) throw new Error('未取得可信的原始操作回执')
    return { ...result, receipt: verifiedReceipt }
  }, [perform])

  return {
    check, loading, message, error, invalidate,
    readBackDesired: readBackDesiredFn,
    readExecutionObservation: readExecutionObservationFn,
    refreshEvidenceChain: refreshEvidenceChainFn,
  }
}
