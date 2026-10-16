import assert from 'node:assert/strict'
import test from 'node:test'
import { isSameReadContext } from '../src/management/read-context.ts'

test('management GET results cannot cross logout or a fresh login by the same user', () => {
  const first = { expiresAt: 2000, subject: 'alice' }
  const fresh = { expiresAt: 2000, subject: 'alice' }
  assert.equal(isSameReadContext(first, first, 'cluster/project/id', 'cluster/project/id', 1999), true)
  assert.equal(isSameReadContext(first, null, 'cluster/project/id', 'cluster/project/id', 1999), false)
  assert.equal(isSameReadContext(first, fresh, 'cluster/project/id', 'cluster/project/id', 1999), false)
  assert.equal(isSameReadContext(null, first, 'cluster/project/id', 'cluster/project/id', 1999), false)
})

test('GET result cannot reappear after selector edits or a clock-only expiry', () => {
  const current = { expiresAt: 2000 }
  assert.equal(isSameReadContext(current, current, 'project-a', 'project-b', 1999), false)
  assert.equal(isSameReadContext(current, current, 'same', 'same', 2000), false)
  assert.equal(isSameReadContext(current, current, 'same', 'same', 2001), false)
  const invalid = { expiresAt: Number.NaN }
  const infinite = { expiresAt: Number.POSITIVE_INFINITY }
  assert.equal(isSameReadContext(invalid, invalid, 'a', 'a', 0), false)
  assert.equal(isSameReadContext(infinite, infinite, 'a', 'a', 0), false)
  assert.equal(isSameReadContext(current, current, 'same', 'same', 1999), true)
})
