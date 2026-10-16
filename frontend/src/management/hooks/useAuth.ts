'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  discover,
  beginLogin,
  finishLogin,
  clearPendingLogin,
  type OAuthConfig,
  type AccessSession,
  type Discovery,
} from '@/management/auth'
import { isSessionActive } from '@/management/workload-composer'

export interface UseAuthReturn {
  issuer: string
  clientId: string
  configured: boolean
  session: AccessSession | null
  authenticated: boolean
  busy: boolean
  message: string
  error: string
  signIn: () => Promise<void>
  signOut: () => void
  handleOAuthCallback: () => Promise<void>
  token: () => string
}

export function useAuth(): UseAuthReturn {
  const [issuer, setIssuer] = useState(
    import.meta.env.VITE_MANAGEMENT_OIDC_ISSUER || ''
  )
  const [clientId, setClientId] = useState(
    import.meta.env.VITE_MANAGEMENT_OIDC_CLIENT_ID || ''
  )
  const [session, setSession] = useState<AccessSession | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const configured = Boolean(issuer && clientId)
  const authenticated = useMemo(
    () => !!session && isSessionActive(session),
    [session]
  )

  const config: OAuthConfig = { issuer, clientId }

  const token = useCallback((): string => {
    if (!isSessionActive(session)) throw new Error('需要重新登录')
    return session!.accessToken
  }, [session])

  const signIn = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      const discovery = await discover(config)
      const redirectUri =
        window.location.origin + '/management.html'
      const url = await beginLogin(
        config,
        discovery,
        redirectUri,
        sessionStorage
      )
      window.location.assign(url)
    } catch (e) {
      setError(e instanceof Error ? e.message : '登录失败')
      setBusy(false)
    }
  }, [config])

  const signOut = useCallback(() => {
    setSession(null)
    clearPendingLogin(sessionStorage)
    setMessage('本页访问令牌已清除；身份提供商 SSO 会话未注销')
    setError('')
  }, [])

  const handleOAuthCallback = useCallback(async () => {
    if (!configured) {
      setError('回调失败：未配置身份提供商')
      return
    }
    const params = new URLSearchParams(window.location.search)
    if (!params.has('code') && !params.has('error')) return
    window.history.replaceState({}, '', window.location.pathname)
    setBusy(true)
    try {
      const discovery = await discover(config)
      const redirectUri = window.location.origin + '/management.html'
      const newSession = await finishLogin(
        config,
        discovery,
        window.location.search,
        redirectUri,
        sessionStorage
      )
      setSession(newSession)
      setMessage(
        '身份提供商返回 Access Token；管理 API 仍将执行令牌与项目权限验证'
      )
    } catch (e) {
      clearPendingLogin(sessionStorage)
      setError(e instanceof Error ? e.message : 'OIDC 登录失败')
    } finally {
      setBusy(false)
    }
  }, [config, configured])

  useEffect(() => {
    handleOAuthCallback()
  }, [handleOAuthCallback])

  return {
    issuer,
    setIssuer,
    clientId,
    setClientId,
    configured,
    session,
    authenticated,
    busy,
    message,
    setMessage,
    error,
    setError,
    signIn,
    signOut,
    handleOAuthCallback,
    token,
  }
}
