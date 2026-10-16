import assert from 'node:assert/strict'
import test from 'node:test'
import { isSameReadContext, readContextStatus } from '../src/management/read-context.ts'

test('management GET result cannot cross logout or a fresh login by the same user', () => {
  const first = { expiresAt: 2000, subject: 'alice' }
  const fresh = { expiresAt: 2000, subject: 'alice' }
  assert.equal(isSameReadContext(first, first, 'cluster/project/id', 'cluster/project/id', 1999), true)
  assert.equal(readContextStatus(first, null, 'a', 'a', 1999), 'changed')
  assert.equal(readContextStatus(first, fresh, 'a', 'a', 1999), 'changed')
  assert.equal(readContextStatus(null, first, 'a', 'a', 1999), 'changed')
})

test('scope edits are distinct from expiry, so a new view is never wiped by a stale response', () => {
  const current = { expiresAt: 2000 }
  assert.equal(readContextStatus(current, current, 'project-a', 'project-b', 1999), 'changed')
  assert.equal(readContextStatus(current, current, 'old', 'new', 2001), 'changed')
})

test('clock-only expiry is explicit and fail-closed even for an unchanged session', () => {
  const current = { expiresAt: 2000 }
  const invalid = { expiresAt: Number.NaN }
  const infinite = { expiresAt: Number.POSITIVE_INFINITY }
  assert.equal(readContextStatus(current, current, 'same', 'same', 1999), 'current')
  assert.equal(readContextStatus(current, current, 'same', 'same', 2000), 'expired')
  assert.equal(readContextStatus(current, current, 'same', 'same', 2001), 'expired')
  assert.equal(readContextStatus(invalid, invalid, 'same', 'same', 0), 'expired')
  assert.equal(readContextStatus(infinite, infinite, 'same', 'same', 0), 'expired')
  assert.equal(isSameReadContext(current, current, 'same', 'same', 2000), false)
})
