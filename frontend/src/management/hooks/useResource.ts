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
  readResource: (scope: Scope, token: string) => Promise<{ desired: DesiredView | null; message: string }>
  writeResource: (scope: Scope, token: string, rawJSON: string, operationId: string) => Promise<string>
  deleteResource: (scope: Scope, token: string, generation: number, operationId: string) => Promise<string>
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
      const value = result.value ?? null
      setDesired(value)
      const msg = '读取到授权项目内的 Desired 资源'
      setMessage(msg)
      return { desired: value, message: msg }
    } catch (e) {
      setDesired(null)
      const msg = e instanceof Error ? e.message : '读取资源失败'
      setError(msg)
      throw e instanceof Error ? e : new Error(msg)
    } finally {
      setLoading(false)
    }
  }, [])

  const writeResourceFn = useCallback(
    async (scope: Scope, token: string, rawJSON: string, operationId: string) => {
      try {
        // 提交确认框在 Management 页面层（镜像 Vue write()）；此处只负责请求与状态。
        JSON.parse(rawJSON)
        setLoading(true)
        const result = await writeResource(scope, token, rawJSON, operationId)
        setDesired(null)
        const msg =
          'Desired 事务已提交（HTTP ' + result.status + '）。操作 ID: ' + operationId + '；可查询回执，但尚无执行成功证明'
        setMessage(msg)
        return msg
      } catch (e) {
        const msg = e instanceof Error ? e.message : '提交失败'
        setError(msg)
        throw e instanceof Error ? e : new Error(msg)
      } finally {
        setLoading(false)
      }
    },
    []
  )

  const deleteResourceFn = useCallback(
    async (scope: Scope, token: string, generation: number, operationId: string) => {
      // 删除确认框在 Management 页面层（镜像 Vue remove()）；此处只负责请求与状态。
      setLoading(true)
      setError('')
      try {
        const result = await deleteResource(scope, token, generation, operationId)
        setDesired(null)
        const msg =
          '删除请求已提交（HTTP ' + result.status + '）。请查询操作回执及后续 Agent 观察'
        setMessage(msg)
        return msg
      } catch (e) {
        const msg = e instanceof Error ? e.message : '删除失败'
        setError(msg)
        throw e instanceof Error ? e : new Error(msg)
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
