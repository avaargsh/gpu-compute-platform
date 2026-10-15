import { defineConfig } from '@playwright/test'

// The only browser acceptance with a REAL HTTPS OIDC issuer, a real Go
// introspection call and an actual verify-full PostgreSQL runtime LOGIN.
export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/management.fullstack.e2e.spec.ts',
  timeout: 100_000,
  retries: 0,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:3002',
    browserName: 'chromium',
    ignoreHTTPSErrors: true, // disposable Keycloak CA only, never production
    headless: true,
    viewport: { width: 1280, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
})
