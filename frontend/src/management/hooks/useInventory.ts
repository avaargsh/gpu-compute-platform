'use client'

import { useCallback, useState } from 'react'
import {
  listWorkloads,
  listComputePools,
  type WorkloadInventoryPage,
} from '@/management/api'

export interface UseInventoryReturn {
  items: WorkloadInventoryPage['items']
  nextCursor: string | null
  hasMore: boolean
  loading: boolean
  loadingMore: boolean
  message: string
  error: string
  listWorkloads: (cluster: string, project: string, token: string, after?: string) => Promise<void>
  listComputePools: (cluster: string, project: string, token: string, after?: string) => Promise<void>
}

export function useInventory(): UseInventoryReturn {
  const [items, setItems] = useState<WorkloadInventoryPage['items']>([])
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const listWorkloadsFn = useCallback(
    async (cluster: string, project: string, token: string, after?: string) => {
      setLoading(!after)
      setLoadingMore(!!after)
      setError('')
      try {
        const result = await listWorkloads(cluster, project, token, after)
        if (!result.value) throw new Error('服务端返回空列表')
        const page = result.value
        const previous = after ? items : []
        const ids = new Set(previous.map((i) => i.id))
        for (const item of page.items) {
          if (ids.has(item.id)) throw new Error('分页出现重复资源，拒绝合并')
          ids.add(item.id)
        }
        setItems([...previous, ...page.items])
        setNextCursor(page.nextCursor ?? null)
        setHasMore(page.hasMore)
        setMessage('已读取项目资源列表；只读、分页且不包含执行完成证明')
      } catch (e) {
        setError(e instanceof Error ? e.message : '查询资源失败')
      } finally {
        setLoading(false)
        setLoadingMore(false)
      }
    },
    [items]
  )

  const listComputePoolsFn = useCallback(
    async (cluster: string, project: string, token: string, after?: string) => {
      setLoading(!after)
      setLoadingMore(!!after)
      setError('')
      try {
        const result = await listComputePools(cluster, project, token, after)
        if (!result.value) throw new Error('服务端返回空列表')
        const page = result.value
        const previous = after ? items : []
        const ids = new Set(previous.map((i) => i.id))
        for (const item of page.items) {
          if (ids.has(item.id)) throw new Error('分页出现重复资源，拒绝合并')
          ids.add(item.id)
        }
        setItems([...previous, ...page.items])
        setNextCursor(page.nextCursor ?? null)
        setHasMore(page.hasMore)
        setMessage('已读取项目 ComputePool 列表；请再单独验证选中的 Pool 绑定')
      } catch (e) {
        setError(e instanceof Error ? e.message : '查询 ComputePool 失败')
      } finally {
        setLoading(false)
        setLoadingMore(false)
      }
    },
    [items]
  )

  return {
    items,
    nextCursor,
    hasMore,
    loading,
    loadingMore,
    message,
    error,
    listWorkloads: listWorkloadsFn,
    listComputePools: listComputePoolsFn,
  }
}
