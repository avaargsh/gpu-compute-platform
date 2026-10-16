'use client'

import { useCallback, useState } from 'react'
import {
  readReceipt,
  readResource,
  readExecution,
  type Receipt,
  type Scope,
} from '@/management/api'
import {
  classifyDesiredReadback,
  type DesiredReadback,
} from '@/management/readback'
import {
  classifyScopedExecution,
} from '@/management/execution'
import { verifyEvidencePair, type EvidenceCheck } from '@/management/evidence-check'

export interface UseEvidenceReturn {
  check: EvidenceCheck | null
  loading: boolean
  message: string
  error: string
  readBackDesired: (receipt: Receipt, cluster: string, project: string, operationId: string, token: string) => Promise<void>
  readExecutionObservation: (audit: Receipt, readback: DesiredReadback, cluster: string, project: string, operationId: string, token: string) => Promise<void>
  refreshEvidenceChain: (audit: Receipt, cluster: string, project: string, requestId: string, token: string) => Promise<void>
}

export function useEvidence(): UseEvidenceReturn {
  const [check, setCheck] = useState<EvidenceCheck | null>(null)
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const readBackDesiredFn = useCallback(
    async (receipt: Receipt, cluster: string, project: string, _operationId: string, token: string) => {
      setLoading(true)
      setError('')
      try {
        const target = {
          cluster,
          project,
          kind: receipt.kind,
          resource: receipt.resourceId,
        } as Scope
        const access = token
        let current
        try {
          const result = await readResource(target, access)
          current = result.value
        } catch (e) {
          if ((e as { status?: number }).status !== 404) throw e
          current = null
        }
        const readback = classifyDesiredReadback(receipt, current)
        setCheck({ readback, execution: null })
        setMessage('操作回执与当前 Desired 已分开核对；没有 Kubernetes 执行或清理完成证明')
      } catch (e) {
        setError(e instanceof Error ? e.message : '回读 Desired 失败')
      } finally {
        setLoading(false)
      }
    },
    []
  )

  const readExecutionObservationFn = useCallback(
    async (audit: Receipt, readback: DesiredReadback, cluster: string, project: string, _operationId: string, token: string) => {
      if (!audit || audit.kind !== 'Workload' || !readback?.desired) return
      setLoading(true)
      setError('')
      try {
        const target = {
          cluster,
          project,
          kind: audit.kind,
          resource: audit.resourceId,
        } as Scope
        const result = await readExecution(target, token)
        if (!result.value) throw new Error('执行观察没有有效数据，请重新查询')
        const classified = classifyScopedExecution(result.value, audit)
        setCheck((prev) => prev ? { ...prev, execution: classified } : { readback: {} as any, execution: classified })
        setMessage('已读取授权范围内的独立 Agent/Kueue 观察；仅供诊断，并非实时 Kubernetes 状态证明')
      } catch (e) {
        setError(e instanceof Error ? e.message : '读取执行观察失败')
      } finally {
        setLoading(false)
      }
    },
    []
  )

  const refreshEvidenceChainFn = useCallback(
    async (_audit: Receipt, cluster: string, project: string, requestId: string, token: string) => {
      setLoading(true)
      setError('')
      try {
        const access = token
        const receiptResult = await readReceipt(cluster, project, requestId, access)
        if (!receiptResult.value) throw new Error('服务端未返回原始审计回执')
        const audit2 = receiptResult.value
        const target = {
          cluster,
          project,
          kind: audit2.kind,
          resource: audit2.resourceId,
        } as Scope
        let current
        try {
          current = (await readResource(target, access)).value ?? null
        } catch (e) {
          if ((e as { status?: number }).status !== 404) throw e
          current = null
        }
        const currentReadback = classifyDesiredReadback(audit2, current)
        let observed
        if (
          target.kind === 'Workload' &&
          current &&
          !['inconsistent', 'desired-advanced'].includes(currentReadback.status)
        ) {
          observed = (await readExecution(target, access)).value ?? null
        }
        const evidence = verifyEvidencePair(audit2, current, observed ?? null)
        setCheck(evidence)
        setMessage('原始操作回执、当前 Desired 和独立观察已完成分层核对；未发起写入或自动重试')
      } catch (e) {
        setError(e instanceof Error ? e.message : '证据链核对失败')
      } finally {
        setLoading(false)
      }
    },
    []
  )

  return {
    check,
    loading,
    message,
    error,
    readBackDesired: readBackDesiredFn,
    readExecutionObservation: readExecutionObservationFn,
    refreshEvidenceChain: refreshEvidenceChainFn,
  }
}
