import { expect, test } from '@playwright/test'

// No interception of /api/v1/management: this test must cross Vite, the real
// Go handler, provider introspection double and ephemeral PostgreSQL.
test('real browser creates Pool and Workload, reads receipts, enforces child guard and finalizer', async ({ page }) => {
  const base = 'http://127.0.0.1:3002'
  const issuer = 'https://idp.example.test/realms/gpu'
  const meta = {
    issuer,
    authorization_endpoint: issuer + '/protocol/openid-connect/auth',
    token_endpoint: issuer + '/protocol/openid-connect/token',
  }
  let browserToken = 'e2e-access-token-operator'
  const requests: { method: string; url: string }[] = []
  page.on('request', r => {
    if (r.url().includes('/api/v1/management/')) {
      requests.push({ method: r.method(), url: r.url() })
    }
  })
  await page.route('https://idp.example.test/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/.well-known/openid-configuration')) {
      await route.fulfill({
        status: 200, contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': base },
        body: JSON.stringify(meta),
      })
      return
    }
    if (url.pathname.endsWith('/protocol/openid-connect/auth')) {
      expect(url.searchParams.get('code_challenge_method')).toBe('S256')
      expect(url.searchParams.get('code_challenge')).toMatch(/^[A-Za-z0-9_-]{43}$/)
      expect(url.searchParams.get('redirect_uri')).toBe(base + '/management.html')
      await route.fulfill({
        status: 302, headers: {
          location: base + '/management.html?code=browser-pg-code&state=' + url.searchParams.get('state'),
        },
        body: '',
      })
      return
    }
    if (url.pathname.endsWith('/protocol/openid-connect/token')) {
      const body = new URLSearchParams(route.request().postData() ?? '')
      expect(body.get('code_verifier')).toMatch(/^[0-9a-f]{64}$/)
      expect(body.get('grant_type')).toBe('authorization_code')
      await route.fulfill({
        status: 200, contentType: 'application/json',
        headers: { 'Access-Control-Allow-Origin': base },
        body: JSON.stringify({
          access_token: browserToken, token_type: 'Bearer', expires_in: 300,
        }),
      })
      return
    }
    throw Error('unexpected OIDC test endpoint: ' + url.pathname)
  })
  page.on('dialog', dialog => dialog.accept())
  await page.goto('/management.html')
  await page.getByRole('button', { name: '通过 OIDC 登录' }).click()
  await expect(page.getByText(/已取得短期访问令牌/)).toBeVisible()
  await page.getByRole('button', { name: '查询当前账号的项目' }).click()
  await expect(page.getByRole('button', { name: '使用 research (browser-ci) · operator' })).toBeVisible()
  await expect(page.getByRole('button', { name: /viewer/ })).toHaveCount(0)
  await page.getByRole('button', { name: '使用 research (browser-ci) · operator' }).click()
  await expect(page.getByPlaceholder('cluster-a')).toHaveValue('browser-ci')
  await expect(page.getByPlaceholder('research')).toHaveValue('research')
  await page.getByPlaceholder('pool-a').fill('pool-a')

  // First write must reach the real Go handler and persist to PostgreSQL.
  await page.getByRole('button', { name: '确认并提交 PUT' }).click()
  await expect(page.getByRole('status')).toContainText('Desired 事务已提交')
  const poolRequestID = await page.getByPlaceholder('原始 Idempotency-Key').inputValue()
  expect(poolRequestID).toMatch(/^[0-9a-f]{32}$/)
  // Real browser reload discards the bearer, but the one-tab recovery
  // pointer survives. It must contain NO token, PKCE verifier or JSON spec.
  const persisted = await page.evaluate(() => {
    const entries = Object.fromEntries(Object.entries(sessionStorage))
    return entries
  })
  expect(JSON.stringify(persisted)).toContain(poolRequestID)
  expect(JSON.stringify(persisted)).not.toContain('e2e-access-token')
  expect(JSON.stringify(persisted)).not.toContain('code_verifier')
  expect(JSON.stringify(persisted)).not.toContain('acceleratorBindings')
  await page.reload()
  await expect(page.getByText('尚未登录')).toBeVisible()
  // Even scope and resource ID metadata remain hidden while signed out.
  await expect(page.getByRole('button', { name: '恢复操作范围与 ID' })).toHaveCount(0)
  // The receipt may only be queried after a NEW OIDC login; the pointer
  // itself never grants auth or restores a resource Generation.
  await expect(page.getByRole('button', { name: '查询已提交回执' })).toBeDisabled()
  await page.getByRole('button', { name: '通过 OIDC 登录' }).click()
  await expect(page.getByText(/已取得短期访问令牌/)).toBeVisible()
  await expect(page.getByRole('button', { name: '恢复操作范围与 ID' })).toBeVisible()
  await page.getByRole('button', { name: '恢复操作范围与 ID' }).click()
  await expect(page.getByPlaceholder('cluster-a')).toHaveValue('browser-ci')
  await expect(page.getByPlaceholder('research')).toHaveValue('research')
  await expect(page.getByPlaceholder('pool-a')).toHaveValue('pool-a')
  await expect(page.getByPlaceholder('原始 Idempotency-Key')).toHaveValue(poolRequestID)
  await expect(page.getByRole('button', { name: '确认并发起删除请求' })).toBeDisabled()
  await page.getByRole('button', { name: '查询已提交回执' }).click()
  await expect(page.getByRole('status')).toContainText('仅证明 Desired 写入和操作审计已提交')
  await expect(page.locator('pre').last()).toContainText(poolRequestID)
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('Generation:')).toContainText('1')

  // Workload belongs to the newly created pool. It is immutable.
  await page.locator('select').selectOption('Workload')
  await page.getByPlaceholder('pool-a').fill('job-a')
  await page.locator('textarea').fill(JSON.stringify({
    expectedGeneration: 0, poolId: 'pool-a',
    spec: { image: 'example.invalid/ci:1', accelerator: { class: 'h100', quota: 1 } },
  }, null, 2))
  await page.getByRole('button', { name: '确认并提交 PUT' }).click()
  await expect(page.getByRole('status')).toContainText('Desired 事务已提交')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('Generation:')).toContainText('1')

  // Parent deletion must reject existing dependent Workload.
  await page.locator('select').selectOption('ComputePool')
  await page.getByPlaceholder('pool-a').fill('pool-a')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await page.getByRole('button', { name: '确认并发起删除请求' }).click()
  await expect(page.getByRole('alert')).toContainText('资源版本已变化、资源仍被占用')

  // Workload deletion is a Finalizer request, not physical provider deletion.
  await page.locator('select').selectOption('Workload')
  await page.getByPlaceholder('pool-a').fill('job-a')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await page.getByRole('button', { name: '确认并发起删除请求' }).click()
  await expect(page.getByRole('status')).toContainText('删除请求已提交')
  const deletionID = await page.getByPlaceholder('原始 Idempotency-Key').inputValue()
  expect(deletionID).toMatch(/^[0-9a-f]{32}$/)
  await page.getByRole('button', { name: '查询已提交回执' }).click()
  await expect(page.locator('pre').last()).toContainText('delete-request')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('删除请求: 已发起')).toBeVisible()

  // New login, same signed issuer but downgraded operator identity: viewer
  // can GET the pool, but cannot mutate or read prior operator's receipt.
  await page.getByRole('button', { name: '清除本页令牌' }).click()
  browserToken = 'e2e-access-token-viewer'
  await page.getByRole('button', { name: '通过 OIDC 登录' }).click()
  await expect(page.getByText(/已取得短期访问令牌/)).toBeVisible()
  // OAuth code redirect reloads the tab. Scope is intentionally held only
  // in volatile component state, so the viewer must select it afresh.
  await expect(page.getByPlaceholder('cluster-a')).toHaveValue('')
  await expect(page.getByPlaceholder('research')).toHaveValue('')
  await page.getByRole('button', { name: '查询当前账号的项目' }).click()
  await expect(page.getByRole('button', { name: '使用 research (browser-ci) · viewer' })).toBeVisible()
  await expect(page.getByRole('button', { name: /operator/ })).toHaveCount(0)
  await page.getByRole('button', { name: '使用 research (browser-ci) · viewer' }).click()
  await page.locator('select').selectOption('ComputePool')
  await page.getByPlaceholder('pool-a').fill('pool-a')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByText('Generation:')).toContainText('1')
  await page.getByRole('button', { name: '确认并提交 PUT' }).click()
  await expect(page.getByRole('alert')).toContainText('当前项目没有操作权限')
  await page.getByPlaceholder('原始 Idempotency-Key').fill(poolRequestID)
  await page.getByRole('button', { name: '查询已提交回执' }).click()
  await expect(page.getByRole('alert')).toContainText('无可访问的资源或操作回执')

  expect(requests.filter(x => x.method === 'PUT').length).toBeGreaterThanOrEqual(3)
  expect(requests.some(x => x.method === 'DELETE')).toBe(true)
  expect(requests.every(x => x.url.startsWith(base + '/api/v1/management/'))).toBe(true)
})
