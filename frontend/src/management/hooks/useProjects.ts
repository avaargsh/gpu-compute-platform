'use client'

import { useCallback, useState } from 'react'
import { listProjects } from '@/management/api'
import type { ProjectAccess } from '@/management/api'

export interface UseProjectsReturn {
  projects: ProjectAccess[]
  truncated: boolean
  loading: boolean
  message: string
  error: string
  loadProjects: (token: string) => Promise<void>
}

export function useProjects(): UseProjectsReturn {
  const [projects, setProjects] = useState<ProjectAccess[]>([])
  const [truncated, setTruncated] = useState(false)
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const loadProjects = useCallback(async (token: string) => {
    setLoading(true)
    setError('')
    try {
      const result = await listProjects(token)
      if (!result.value) throw new Error('服务端返回空项目列表')
      setProjects(result.value.projects ?? [])
      setTruncated(result.value.truncated ?? false)
      setMessage('服务端返回 ' + result.value.projects.length + ' 个当前授权项目')
    } catch (e) {
      setProjects([])
      setTruncated(false)
      setError(e instanceof Error ? e.message : '查询项目失败')
    } finally {
      setLoading(false)
    }
  }, [])

  return {
    projects,
    truncated,
    loading,
    message,
    error,
    loadProjects,
  }
}
