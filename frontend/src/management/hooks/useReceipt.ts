'use client'

import { useCallback, useState } from 'react'
import { readReceipt, type Receipt } from '@/management/api'

export interface UseReceiptReturn {
  receipt: Receipt | null
  loading: boolean
  message: string
  error: string
  readReceipt: (cluster: string, project: string, operationId: string, token: string) => Promise<void>
}

export function useReceipt(): UseReceiptReturn {
  const [receipt, setReceipt] = useState<Receipt | null>(null)
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const readReceiptFn = useCallback(
    async (cluster: string, project: string, operationId: string, token: string) => {
      setLoading(true)
      setError('')
      try {
        const result = await readReceipt(cluster, project, operationId, token)
        if (!result.value) throw new Error('服务端未返回有效的操作回执')
        setReceipt(result.value)
        setMessage('仅证明 Desired 写入和操作审计已提交，非 Workload 执行证明')
      } catch (e) {
        setError(e instanceof Error ? e.message : '查询回执失败')
      } finally {
        setLoading(false)
      }
    },
    []
  )

  return {
    receipt,
    loading,
    message,
    error,
    readReceipt: readReceiptFn,
  }
}
