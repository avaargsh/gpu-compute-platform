import { execFileSync } from 'node:child_process'
import { expect, test } from '@playwright/test'

const issuer = 'https://127.0.0.1:8443/realms/gpu-management-e2e'
const subject = '08d71ae7-c789-4c56-b0b4-6a987efdaa12'

function dba(sql: string): string {
  const container = process.env.GPU_MANAGEMENT_E2E_PG_CONTAINER
  if (!container || !/^gpu-management-browser-pg-[0-9]+$/.test(container)) {
    throw new Error('refuse non-disposable PostgreSQL target')
  }
  return execFileSync('docker', [
    'exec', container, 'psql', '-X', '-qAt',
    '-v', 'ON_ERROR_STOP=1', '-U', 'gpu',
    '-d', 'gpu_management_fullstack_ci', '-c', sql,
  ], { encoding: 'utf8', timeout: 12000 })
}

test('real Keycloak S256 authorization code -> verified-TLS Go Management -> PG audit -> live grant downgrade and revocation', async ({ page }) => {
  const ui = 'http://127.0.0.1:3002'
  const browserIdPRequests: string[] = []
  const managementRequests: string[] = []
  page.on('request', r => {
    if (r.url().startsWith(issuer)) browserIdPRequests.push(r.url())
    if (r.url().includes('/api/v1/management/')) managementRequests.push(r.url())
  })
  // No browser route stubs, no intercepted management endpoints and no
  // mock introspection; real self-signed HTTPS is trusted only for CI.
  page.on('dialog', d => d.accept())
  await page.goto('/management.html')
  await expect(page.getByText('尚未登录')).toBeVisible()
  await page.getByRole('button', { name: '通过 OIDC 登录' }).click()
  await expect(page).toHaveURL(new RegExp('^' + issuer.replaceAll('.', '\\.') + '/protocol/openid-connect/auth'))
  await page.locator('#username').fill('ci-operator')
  await page.locator('#password').fill('ci-disposable-password-not-for-production')
  await page.locator('#kc-login').click()
  await expect(page).toHaveURL(ui + '/management.html', { timeout: 25000 })
  await expect(page.getByText(/已取得短期访问令牌/)).toBeVisible()

  await page.getByRole('button', { name: '查询当前账号的项目' }).click()
  await expect(page.getByRole('button', {
    name: '使用 research (browser-ci) · operator',
  })).toBeVisible()
  await page.getByRole('button', {
    name: '使用 research (browser-ci) · operator',
  }).click()
  await expect(page.getByPlaceholder('cluster-a')).toHaveValue('browser-ci')
  await page.getByPlaceholder('pool-a').fill('pool-browser-ci')
  await page.getByRole('button', { name: '确认并提交 PUT' }).click()
  await expect(page.getByRole('status')).toContainText('Desired 事务已提交')
  const operationID = await page.getByPlaceholder('原始 Idempotency-Key').inputValue()
  expect(operationID).toMatch(/^[0-9a-f]{32}$/)

  await page.getByRole('button', { name: '查询已提交回执' }).click()
  await expect(page.locator('pre').last()).toContainText('desired-committed')
  await expect(page.locator('pre').last()).toContainText(operationID)
  const dbAudit = dba(
    "SELECT count(*) FROM management_operation_audit WHERE request_id='" + operationID +
    "' AND subject='" + subject + "' AND action='create' AND project_id='research'",
  )
  expect(dbAudit.trim()).toBe('1')

  // No Keycloak token is changed here: the SAME still-active access token
  // must instantly lose mutation rights when database authority changes.
  dba("UPDATE management_project_grants SET role='viewer' WHERE issuer='" +
      issuer + "' AND subject='" + subject + "' AND project_id='research'")
  await page.getByRole('button', { name: '查询当前账号的项目' }).click()
  await expect(page.getByRole('button', {
    name: '使用 research (browser-ci) · viewer',
  })).toBeVisible()
  await page.getByRole('button', { name: '查询已提交回执' }).click()
  await expect(page.getByRole('alert')).toContainText('无可访问的资源或操作回执')
  await page.getByRole('button', { name: '确认并提交 PUT' }).click()
  await expect(page.getByRole('alert')).toContainText('当前项目没有操作权限')

  // Full membership revocation must remove project discovery as well.
  dba("DELETE FROM management_project_grants WHERE issuer='" + issuer +
      "' AND subject='" + subject + "' AND project_id='research'")
  await page.getByRole('button', { name: '查询当前账号的项目' }).click()
  await expect(page.getByRole('status')).toContainText('服务端返回 0 个当前授权项目')
  await page.getByRole('button', { name: '从 PostgreSQL 读取' }).click()
  await expect(page.getByRole('alert')).toContainText('当前项目没有操作权限')
  expect(managementRequests.length).toBeGreaterThan(4)
  expect(managementRequests.every(u => u.startsWith(ui + '/api/v1/management/'))).toBe(true)
  expect(browserIdPRequests.some(u => u.includes('/protocol/openid-connect/auth'))).toBe(true)
  expect(browserIdPRequests.some(u => u.includes('/protocol/openid-connect/token'))).toBe(true)
  expect(browserIdPRequests.every(u => u.startsWith(issuer))).toBe(true)
  // No raw bearer content or IdP client secret is emitted by this test.
})
