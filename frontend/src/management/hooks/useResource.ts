'use client'

import { useCallback, useRef, useState } from 'react'
import {
  readResource,
  writeResource,
  deleteResource,
  type Scope,
  type DesiredView,
} from '@/management/api'

// Any older GET that completes after a newer GET, scope/session invalidation
// or mutation dispatch is no longer authorized to repaint the current view.
export class StaleResourceReadError extends Error {
  constructor() {
    super('资源读取已失效')
    this.name = 'StaleResourceReadError'
  }
}

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
  const requestEpoch = useRef(0)

  const clearDesired = useCallback(() => {
    requestEpoch.current += 1
    setDesired(null)
    setError('')
    setMessage('')
    setLoading(false)
  }, [])

  const readResourceFn = useCallback(async (scope: Scope, token: string) => {
    const epoch = ++requestEpoch.current
    // A new GET must not leave a previous scope's Generation actionable.
    setDesired(null)
    setLoading(true)
    setError('')
    setMessage('')
    try {
      const result = await readResource(scope, token)
      if (epoch !== requestEpoch.current) throw new StaleResourceReadError()
      const value = result.value ?? null
      setDesired(value)
      const msg = '读取到授权项目内的 Desired 资源'
      setMessage(msg)
      return { desired: value, message: msg }
    } catch (e) {
      if (epoch !== requestEpoch.current) throw new StaleResourceReadError()
      setDesired(null)
      const msg = e instanceof Error ? e.message : '读取资源失败'
      setError(msg)
      throw e instanceof Error ? e : new Error(msg)
    } finally {
      if (epoch === requestEpoch.current) setLoading(false)
    }
  }, [])

  const writeResourceFn = useCallback(
    async (scope: Scope, token: string, rawJSON: string, operationId: string) => {
      // An in-flight read predating this mutation must not republish an old
      // Generation after the server has processed the write.
      JSON.parse(rawJSON)
      const epoch = ++requestEpoch.current
      setDesired(null)
      setLoading(true)
      setError('')
      setMessage('')
      try {
        const result = await writeResource(scope, token, rawJSON, operationId)
        const msg =
          'Desired 事务已提交（HTTP ' + result.status + '）。操作 ID: ' + operationId + '；可查询回执，但尚无执行成功证明'
        if (epoch === requestEpoch.current) setMessage(msg)
        return msg
      } catch (e) {
        const msg = e instanceof Error ? e.message : '提交失败'
        if (epoch === requestEpoch.current) setError(msg)
        throw e instanceof Error ? e : new Error(msg)
      } finally {
        if (epoch === requestEpoch.current) setLoading(false)
      }
    },
    []
  )

  const deleteResourceFn = useCallback(
    async (scope: Scope, token: string, generation: number, operationId: string) => {
      // The server still enforces live OIDC/RBAC and Generation CAS.
      const epoch = ++requestEpoch.current
      setDesired(null)
      setLoading(true)
      setError('')
      setMessage('')
      try {
        const result = await deleteResource(scope, token, generation, operationId)
        const msg =
          '删除请求已提交（HTTP ' + result.status + '）。请查询操作回执及后续 Agent 观察'
        if (epoch === requestEpoch.current) setMessage(msg)
        return msg
      } catch (e) {
        const msg = e instanceof Error ? e.message : '删除失败'
        if (epoch === requestEpoch.current) setError(msg)
        throw e instanceof Error ? e : new Error(msg)
      } finally {
        if (epoch === requestEpoch.current) setLoading(false)
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
