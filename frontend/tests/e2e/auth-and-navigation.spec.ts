import { test, expect } from '@playwright/test'

const LOGIN_RESPONSE = JSON.stringify({
  success: true,
  data: {
    id: 'user-1',
    username: 'admin',
    email: 'admin@example.com',
    nickname: 'Admin',
    role: 'admin',
    token: 'mock-access-token',
    is_active: true,
    created_at: '2024-01-01T00:00:00Z',
  },
})

const TASKS_RESPONSE = JSON.stringify({
  items: [],
  total: 0,
  page: 1,
  per_page: 20,
  pages: 0,
})

const GPU_METRICS_RESPONSE = JSON.stringify({
  timestamp: '2024-01-01T00:00:00Z',
  gpus: [],
  total_usage: 0,
  total_memory_usage: 0,
})

test.beforeEach(async ({ page }) => {
  await page.route('**/api/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: LOGIN_RESPONSE,
    })
  })
  await page.route('**/api/tasks', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: TASKS_RESPONSE,
    })
  })
  await page.route('**/api/gpu/metrics', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: GPU_METRICS_RESPONSE,
    })
  })
})

test('login page renders and shows form fields', async ({ page }) => {
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('input[id="username"]', { timeout: 15000 })
  await expect(page.locator('input[id="username"]')).toBeVisible()
  await expect(page.locator('input[id="password"]')).toBeVisible()
  await expect(page.locator('button:has-text("登录")')).toBeVisible()
})

async function loginAndWait(page) {
  // 通过 localStorage 直接设置认证状态，模拟已登录用户
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  await page.evaluate(() => {
    localStorage.setItem(
      'auth-storage',
      JSON.stringify({
        state: {
          user: {
            id: 'user-1',
            username: 'admin',
            email: 'admin@example.com',
            nickname: 'Admin',
            role: 'admin',
            token: 'mock-token',
            is_active: true,
            created_at: '2024-01-01',
          },
          token: 'mock-token',
          isLoggedIn: true,
        },
        version: 0,
      })
    )
  })
  await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('h1:has-text("GPU 计算平台")')).toBeVisible()
}

test('dashboard page renders after login', async ({ page }) => {
  await loginAndWait(page)
})

test('task list page renders', async ({ page }) => {
  await loginAndWait(page)
  await page.goto('/tasks', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1000)
  await expect(page).toHaveURL('/tasks', { timeout: 10000 })
  await expect(page.locator('h3')).toBeVisible()
})

test('gpu monitor page renders', async ({ page }) => {
  await loginAndWait(page)
  await page.goto('/gpu', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2000)
  await expect(page).toHaveURL('/gpu', { timeout: 10000 })
  const bodyText = await page.locator('body').textContent()
  expect(bodyText?.trim().length).toBeGreaterThan(0)
})
