'use client'

import { useCallback, useState } from 'react'
import {
  readResource,
  writeResource,
  deleteResource,
  type Scope,
  type DesiredView,
} from '@/management/api'

export interface UseResourceReturn {
  desired: DesiredView | null
  loading: boolean
  message: string
  error: string
  readResource: (scope: Scope, token: string) => Promise<void>
  writeResource: (scope: Scope, token: string, rawJSON: string, operationId: string) => Promise<void>
  deleteResource: (scope: Scope, token: string, generation: number, operationId: string) => Promise<void>
  clearDesired: () => void
}

export function useResource(): UseResourceReturn {
  const [desired, setDesired] = useState<DesiredView | null>(null)
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const clearDesired = useCallback(() => {
    setDesired(null)
    setError('')
    setMessage('')
  }, [])

  const readResourceFn = useCallback(async (scope: Scope, token: string) => {
    setLoading(true)
    setError('')
    try {
      const result = await readResource(scope, token)
      setDesired(result.value ?? null)
      setMessage('读取到授权项目内的 Desired 资源')
    } catch (e) {
      setDesired(null)
      setError(e instanceof Error ? e.message : '读取资源失败')
    } finally {
      setLoading(false)
    }
  }, [])

  const writeResourceFn = useCallback(
    async (scope: Scope, token: string, rawJSON: string, operationId: string) => {
      try {
        JSON.parse(rawJSON)
        if (!window.confirm(
          '确认向项目 ' + scope.project + ' 提交 ' + scope.kind + '/' + scope.resource + ' Desired 变更？'
        )) return
        setLoading(true)
        const result = await writeResource(scope, token, rawJSON, operationId)
        setDesired(null)
        setMessage(
          'Desired 事务已提交（HTTP ' + result.status + '）。操作 ID: ' + operationId + '；可查询回执，但尚无执行成功证明'
        )
      } catch (e) {
        setError(e instanceof Error ? e.message : '提交失败')
      } finally {
        setLoading(false)
      }
    },
    []
  )

  const deleteResourceFn = useCallback(
    async (scope: Scope, token: string, generation: number, operationId: string) => {
      if (!window.confirm(
        '确认发起删除 ' + scope.kind + '/' + scope.resource + '（Generation ' + generation + '）？此操作不会立即删除真实 GPU 资源。'
      )) return
      setLoading(true)
      setError('')
      try {
        const result = await deleteResource(scope, token, generation, operationId)
        setDesired(null)
        setMessage(
          '删除请求已提交（HTTP ' + result.status + '）。请查询操作回执及后续 Agent 观察'
        )
      } catch (e) {
        setError(e instanceof Error ? e.message : '删除失败')
      } finally {
        setLoading(false)
      }
    },
    []
  )

  return {
    desired,
    loading,
    message,
    error,
    readResource: readResourceFn,
    writeResource: writeResourceFn,
    deleteResource: deleteResourceFn,
    clearDesired,
  }
}
