'use client'

import { useCallback, useRef, useState } from 'react'
import { readReceipt, type Receipt } from '@/management/api'

// A stale promise must never re-publish a receipt from a previous request,
// scope, or OIDC session after the caller invalidates that read.
export class StaleReceiptReadError extends Error {
  constructor() {
    super('回执查询已失效')
    this.name = 'StaleReceiptReadError'
  }
}

export interface UseReceiptReturn {
  receipt: Receipt | null
  loading: boolean
  message: string
  error: string
  invalidate: () => void
  readReceipt: (
    cluster: string,
    project: string,
    operationId: string,
    token: string
  ) => Promise<{ receipt: Receipt; message: string }>
}

export function useReceipt(): UseReceiptReturn {
  const [receipt, setReceipt] = useState<Receipt | null>(null)
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const requestEpoch = useRef(0)

  const invalidate = useCallback(() => {
    requestEpoch.current += 1
    setReceipt(null)
    setMessage('')
    setError('')
    setLoading(false)
  }, [])

  const readReceiptFn = useCallback(
    async (
      cluster: string,
      project: string,
      operationId: string,
      token: string
    ): Promise<{ receipt: Receipt; message: string }> => {
      const epoch = ++requestEpoch.current
      // Do not show an old project's receipt while the new read is pending.
      setReceipt(null)
      setMessage('')
      setError('')
      setLoading(true)
      try {
        const result = await readReceipt(cluster, project, operationId, token)
        if (epoch !== requestEpoch.current) throw new StaleReceiptReadError()
        if (!result.value) throw new Error('服务端未返回有效的操作回执')
        setReceipt(result.value)
        const msg = '仅证明 Desired 写入和操作审计已提交，非 Workload 执行证明'
        setMessage(msg)
        return { receipt: result.value, message: msg }
      } catch (e) {
        const msg = e instanceof Error ? e.message : '查询回执失败'
        if (epoch === requestEpoch.current) {
          setReceipt(null)
          setMessage('')
          setError(msg)
        }
        throw e instanceof Error ? e : new Error(msg)
      } finally {
        if (epoch === requestEpoch.current) setLoading(false)
      }
    },
    []
  )

  return {
    receipt,
    loading,
    message,
    error,
    invalidate,
    readReceipt: readReceiptFn,
  }
}
