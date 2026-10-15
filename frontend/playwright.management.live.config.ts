import { defineConfig } from '@playwright/test'

// One test runs a real Chromium → Vite → Go Management → PostgreSQL stack.
// Only browser OAuth endpoints are intercepted (the IdP is a test double).
export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/management.live.e2e.spec.ts',
  timeout: 90_000,
  retries: 0,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:3002',
    browserName: 'chromium',
    headless: true,
    viewport: { width: 1280, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
})
