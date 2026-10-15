import { defineConfig } from '@playwright/test'

// Acceptance-only: disposable self-signed localhost gateway and Keycloak.
// NEVER copy ignoreHTTPSErrors to a deployed production browser configuration.
export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/management.static-tls.e2e.spec.ts',
  timeout: 100_000,
  retries: 0,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: 'https://127.0.0.1:3443',
    browserName: 'chromium',
    ignoreHTTPSErrors: true,
    headless: true,
    viewport: { width: 1280, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
})
