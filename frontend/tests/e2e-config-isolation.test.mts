import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

// Regression guard: management browser suites require OIDC + PostgreSQL on
// port 3002, while the Portal job runs only the Go operator snapshot on 3001.
// A broad *.spec.ts glob caused false failures after adding Management E2E.
test('Portal and Management Playwright configurations own disjoint suites', () => {
  const portal = readFileSync(new URL('../playwright.portal.config.ts', import.meta.url), 'utf8')
  const management = readFileSync(new URL('../playwright.management.live.config.ts', import.meta.url), 'utf8')
  assert.match(portal, /testMatch:\s*['"]\*\*\/portal\.e2e\.spec\.ts['"]/)
  assert.match(management, /testMatch:\s*['"]\*\*\/management\.live\.e2e\.spec\.ts['"]/)
  assert.doesNotMatch(portal, /testMatch:\s*['"]\*\*\/\*\.spec\.ts['"]/)
  assert.match(portal, /baseURL:\s*['"]http:\/\/127\.0\.0\.1:3001['"]/)
  assert.match(management, /baseURL:\s*['"]http:\/\/127\.0\.0\.1:3002['"]/)
})
