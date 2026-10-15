// Browser OAuth 2.0 Authorization Code + S256 PKCE for the LOCAL management
// workbench. The access token is never persisted; the transient verifier and
// state exist in per-tab sessionStorage only during the redirect.
export interface OAuthConfig {
  issuer: string
  clientId: string
  audience?: string
}

export interface Discovery {
  issuer: string
  authorization_endpoint: string
  token_endpoint: string
}

export interface AccessSession {
  accessToken: string
  expiresAt: number
}

interface PendingLogin {
  verifier: string
  state: string
  nonce: string
  redirectUri: string
  issuedAt: number
}

const KEY = 'gpu-management-pkce-v1'
const VERIFIER = /^[A-Za-z0-9._~-]{43,128}$/
const RANDOM = /^[a-f0-9]{64}$/

export function validateOAuthConfig(config: OAuthConfig): void {
  const url = new URL(config.issuer)
  if (url.protocol !== 'https:' || !url.hostname || url.username || url.password ||
      url.search || url.hash || url.pathname.endsWith('/')) {
    throw new Error('OIDC issuer 必须是无参数、无末尾斜杠的 HTTPS 地址')
  }
  if (!/^[a-zA-Z0-9._~-]{1,128}$/.test(config.clientId)) {
    throw new Error('必须配置有效的公开客户端 ID')
  }
}

export function secureRandom(): string {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}

export async function challenge(verifier: string): Promise<string> {
  if (!VERIFIER.test(verifier)) throw new Error('无效的 PKCE verifier')
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier)))
  const base64 = btoa(String.fromCharCode(...digest))
  return base64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function sameIssuerEndpoint(endpoint: string, issuer: string): boolean {
  try {
    const url = new URL(endpoint)
    const root = new URL(issuer)
    return url.protocol === 'https:' && url.origin === root.origin &&
      url.pathname.startsWith(root.pathname.replace(/\/$/, '') + '/') &&
      !url.username && !url.password && !url.hash
  } catch {
    return false
  }
}

export async function discover(config: OAuthConfig): Promise<Discovery> {
  validateOAuthConfig(config)
  const resp = await fetch(config.issuer + '/.well-known/openid-configuration', {
    method: 'GET', credentials: 'omit', cache: 'no-store', redirect: 'error',
    signal: AbortSignal.timeout(10000),
  })
  if (!resp.ok) throw new Error('OIDC Discovery 失败：HTTP ' + resp.status)
  const data = await resp.json() as Discovery
  if (data.issuer !== config.issuer ||
      !sameIssuerEndpoint(data.authorization_endpoint, config.issuer) ||
      !sameIssuerEndpoint(data.token_endpoint, config.issuer)) {
    throw new Error('OIDC Discovery 的 issuer 或 endpoints 不可信')
  }
  return data
}

export async function beginLogin(config: OAuthConfig, discovery: Discovery, redirectUri: string, storage: Storage, now = Date.now()): Promise<string> {
  validateOAuthConfig(config)
  if (discovery.issuer !== config.issuer ||
      !sameIssuerEndpoint(discovery.authorization_endpoint, config.issuer) ||
      !sameIssuerEndpoint(discovery.token_endpoint, config.issuer)) throw new Error('不可信 OIDC endpoints')
  const redirect = new URL(redirectUri)
  if (!['http:', 'https:'].includes(redirect.protocol) || redirect.pathname !== '/management.html' ||
      redirect.search || redirect.hash || redirect.username || redirect.password) throw new Error('回调地址必须是管理工作台页面')
  const verifier = secureRandom()
  const state = secureRandom()
  const nonce = secureRandom()
  const pending: PendingLogin = { verifier, state, nonce, redirectUri, issuedAt: now }
  // Intentionally no token or client secret in storage or URL.
  storage.setItem(KEY, JSON.stringify(pending))
  const authURL = new URL(discovery.authorization_endpoint)
  const params = authURL.searchParams
  params.set('response_type', 'code')
  params.set('client_id', config.clientId)
  params.set('redirect_uri', redirectUri)
  params.set('scope', 'openid profile')
  params.set('state', state)
  params.set('nonce', nonce)
  params.set('code_challenge_method', 'S256')
  params.set('code_challenge', await challenge(verifier))
  if (config.audience) params.set('audience', config.audience)
  return authURL.toString()
}

export async function finishLogin(config: OAuthConfig, discovery: Discovery, query: string, redirectUri: string, storage: Storage, now = Date.now()): Promise<AccessSession> {
  validateOAuthConfig(config)
  const raw = storage.getItem(KEY)
  // Consume one-use verifier even if callback was invalid.
  storage.removeItem(KEY)
  if (!raw) throw new Error('登录会话已过期或回调与当前浏览器标签不匹配')
  let pending: PendingLogin
  try { pending = JSON.parse(raw) as PendingLogin } catch { throw new Error('无效的登录会话') }
  const params = new URLSearchParams(query)
  if (params.has('error')) throw new Error('身份提供商拒绝登录')
  if (params.getAll('state').length !== 1 || params.getAll('code').length !== 1 ||
      !RANDOM.test(pending.state) || !RANDOM.test(pending.nonce) || !VERIFIER.test(pending.verifier) ||
      pending.state !== params.get('state') || !params.get('code') ||
      pending.redirectUri !== redirectUri || now - pending.issuedAt > 5 * 60_000 ||
      now < pending.issuedAt) throw new Error('OIDC 回调 state、时间或 redirectUri 校验失败')
  if (discovery.issuer !== config.issuer || !sameIssuerEndpoint(discovery.token_endpoint, config.issuer)) {
    throw new Error('OIDC Token Endpoint 不可信')
  }
  const payload = new URLSearchParams({
    grant_type: 'authorization_code',
    client_id: config.clientId,
    code: params.get('code')!,
    redirect_uri: redirectUri,
    code_verifier: pending.verifier,
  })
  const resp = await fetch(discovery.token_endpoint, {
    method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
    body: payload, credentials: 'omit', redirect: 'error', cache: 'no-store',
    signal: AbortSignal.timeout(15000),
  })
  if (!resp.ok) throw new Error('OIDC Token Exchange 失败：HTTP ' + resp.status)
  const session = await resp.json() as { access_token?: string; token_type?: string; expires_in?: number }
  if (session.token_type?.toLowerCase() !== 'bearer' || !session.access_token ||
      !Number.isFinite(session.expires_in) || (session.expires_in ?? 0) < 1) {
    throw new Error('身份提供商没有返回有效的 Access Token')
  }
  // We intentionally do not interpret or trust JWT/ID-token claims here.
  // The Go Management API MUST introspect every access token independently.
  return { accessToken: session.access_token, expiresAt: now + Math.min(session.expires_in!, 3600) * 1000 }
}

export function clearPendingLogin(storage: Storage): void {
  storage.removeItem(KEY)
}
