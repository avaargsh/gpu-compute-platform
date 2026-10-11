import { expect, test } from '@playwright/test'

const issuer = 'https://idp.example.test/realms/gpu'
const base = 'http://127.0.0.1:3002'
const metadata = {
  issuer,
  authorization_endpoint: issuer + '/protocol/openid-connect/auth',
  token_endpoint: issuer + '/protocol/openid-connect/token',
}

test('browser completes one-use S256 PKCE and sends audited CRUD without legacy Agent paths', async ({ page }) => {
  const idpCalls: string[] = []
  const mgmtCalls: Array<{ method: string; url: string; auth: string; operation: string | undefined; match: string | undefined }> = []
  let deletionCommitted = false
  await page.route('https://idp.example.test/**', async route => {
    const req = route.request()
    const url = new URL(req.url())
    idpCalls.push(url.pathname)
    if (url.pathname.endsWith('/.well-known/openid-configuration')) {
      await route.fulfill({ status: 200, contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': base },
        body: JSON.stringify(metadata) })
      return
    }
    if (url.pathname.endsWith('/protocol/openid-connect/auth')) {
      expect(url.searchParams.get('code_challenge_method')).toBe('S256')
      expect(url.searchParams.get('code_challenge')).toMatch(/^[A-Za-z0-9_-]{43}$/)
      expect(url.searchParams.get('state')).toMatch(/^[0-9a-f]{64}$/)
      expect(url.searchParams.get('redirect_uri')).toBe(base + '/management.html')
      expect(url.searchParams.get('client_id')).toBe('gpu-management-public')
      await route.fulfill({ status: 302, headers: {
        location: base + '/management.html?code=e2e-once&state=' + url.searchParams.get('state'),
      }, body: '' })
      return
    }
    if (url.pathname.endsWith('/protocol/openid-connect/token')) {
      expect(req.method()).toBe('POST')
      const data = new URLSearchParams(req.postData() || '')
      expect(data.get('grant_type')).toBe('authorization_code')
      expect(data.get('code')).toBe('e2e-once')
      expect(data.get('code_verifier')).toMatch(/^[0-9a-f]{64}$/)
      expect(data.get('redirect_uri')).toBe(base + '/management.html')
      await route.fulfill({ status: 200, contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': base },
        body: JSON.stringify({ access_token: 'e2e-test-access-token', token_type: 'Bearer', expires_in: 300 }) })
      return
    }
    throw Error('unexpected IdP path: ' + url.pathname)
  })
  await page.route('**/api/v1/management/**', async route => {
    const req = route.request()
    const method = req.method(), url = req.url()
    const auth = req.headers()['authorization']
    const operation = req.headers()['idempotency-key']
    const match = req.headers()['if-match']
    mgmtCalls.push({ method, url, auth, operation, match })
    expect(url).toContain('/api/v1/management/clusters/gpu-test/projects/research/')
    expect(auth).toBe('Bearer e2e-test-access-token')
    expect(url.includes('/agent/')).toBe(false)
    expect(url.includes('/internal/')).toBe(false)
    if (url.endsWith('/resources/ComputePool/pool-a') && method === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json',
        body: JSON.stringify({ clusterId: 'gpu-test', projectId: 'research',
          kind: 'ComputePool', id: 'pool-a', generation: 2, deleting: false, spec: { projectID: 'research' } }) })
      return
    }
    if (url.endsWith('/resources/ComputePool/pool-a') && method === 'PUT') {
      expect(operation).toMatch(/^[0-9a-f]{32}$/)
      await route.fulfill({ status: 204, body: '' })
      return
    }
    if (url.endsWith('/resources/ComputePool/pool-a') && method === 'DELETE') {
      expect(match).toBe('"2"')
      expect(operation).toMatch(/^[0-9a-f]{32}$/)
      deletionCommitted = true
      await route.fulfill({ status: 202, body: '' })
      return
    }
    if (url.includes('/operations/') && method === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json',
        body: JSON.stringify({ requestId: url.split('/').pop(), status: 'desired-committed', clusterId: 'gpu-test',
          projectId: 'research', kind: 'ComputePool', resourceId: 'pool-a',
          action: 'create', expectedGeneration: 0, resultingGeneration: 1,
          requestDigest: 'a'.repeat(64), committedAt: new Date().toISOString() }) })
      return
    }
    throw Error('unexpected management operation: ' + method + ' ' + url)
  })

  await page.goto('/management.html')
  await expect(page.getByText('尚未登录')).toBeVisible()
  await page.getByRole('button', { name: '通过 OIDC 登录' }).click()
  await expect(page.getByText(/已取得短期访问令牌/)).toBeVisible()
  expect(page.url()).toBe(base + '/management.html')
  expect(idpCalls.filter(path => path.endsWith('/protocol/openid-connect/token'))).toHaveLength(1)

  await page.getByPlaceholder('cluster-a').fill('gpu-test')
  await page.getByPlaceholder('research').fill('research')
  await page.getByPlaceholder('pool-a').fill('pool-a')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('Generation:', { exact: false })).toBeVisible()

  page.on('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: '确认并提交 PUT' }).click()
  await expect(page.getByText(/Desired 事务已提交/)).toBeVisible()
  const firstID = await page.getByPlaceholder('原始 Idempotency-Key').inputValue()
  expect(firstID).toMatch(/^[0-9a-f]{32}$/)
  await page.getByRole('button', { name: '查询已提交回执' }).click()
  await expect(page.getByText(/仅证明 Desired 写入和操作审计已提交/)).toBeVisible()

  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('Generation:', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: '确认并发起删除请求' }).click()
  await expect(page.getByText(/删除请求已提交/)).toBeVisible()
  expect(deletionCommitted).toBe(true)
  expect(mgmtCalls.filter(c => c.method === 'PUT')).toHaveLength(1)
  expect(mgmtCalls.filter(c => c.method === 'DELETE')).toHaveLength(1)
  expect(mgmtCalls.every(c => c.auth === 'Bearer e2e-test-access-token')).toBe(true)
})

test('authenticated browser cannot delete a different resource using stale read Generation', async ({ page }) => {
  let deleteCalls = 0
  await page.route('https://idp.example.test/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/.well-known/openid-configuration')) {
      await route.fulfill({ status: 200, contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': base }, body: JSON.stringify(metadata) })
    } else if (url.pathname.endsWith('/protocol/openid-connect/auth')) {
      await route.fulfill({ status: 302, headers: {
        location: base + '/management.html?code=one-use&state=' + url.searchParams.get('state'),
      }, body: '' })
    } else if (url.pathname.endsWith('/protocol/openid-connect/token')) {
      await route.fulfill({ status: 200, contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': base },
        body: JSON.stringify({ access_token: 'e2e-second-token', token_type: 'Bearer', expires_in: 300 }) })
    } else {
      throw Error('unexpected IdP request')
    }
  })
  await page.route('**/api/v1/management/**', async route => {
    if (route.request().method() === 'DELETE') {
      deleteCalls++
      await route.fulfill({ status: 202, body: '' })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json',
      body: JSON.stringify({ clusterId: 'gpu-test', projectId: 'research',
        kind: 'ComputePool', id: 'pool-a', generation: 7, deleting: false, spec: {} }) })
  })
  await page.goto('/management.html')
  await page.getByRole('button', { name: '通过 OIDC 登录' }).click()
  await expect(page.getByText(/已取得短期访问令牌/)).toBeVisible()
  await page.getByPlaceholder('cluster-a').fill('gpu-test')
  await page.getByPlaceholder('research').fill('research')
  await page.getByPlaceholder('pool-a').fill('pool-a')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('Generation:', { exact: false })).toBeVisible()

  await page.getByPlaceholder('pool-a').fill('another-resource')
  await page.getByRole('button', { name: '确认并发起删除请求' }).click()
  await expect(page.getByRole('alert')).toContainText('资源范围已切换')
  expect(deleteCalls).toBe(0)

  await page.getByPlaceholder('pool-a').fill('pool-a')
  await page.getByPlaceholder('research').fill('other-project')
  await page.getByRole('button', { name: '确认并发起删除请求' }).click()
  await expect(page.getByRole('alert')).toContainText('资源范围已切换')
  expect(deleteCalls).toBe(0)
})
