import assert from 'node:assert/strict'
import test from 'node:test'
import { rememberOperation, recoverOperation, forgetOperation } from '../src/management/recovery.ts'

const scope = { cluster: 'browser-ci', project: 'research', kind: 'ComputePool', resource: 'pool-a' }
const operationId = '0123456789abcdef0123456789abcdef'
const now = 1_750_000_000_000

function memoryStorage() {
  const entries = new Map()
  return {
    getItem: k => entries.get(k) ?? null,
    setItem: (k, value) => entries.set(k, value),
    removeItem: k => entries.delete(k),
    clear: () => entries.clear(),
    dump: () => Object.fromEntries(entries),
  }
}

test('one-tab recovery records only scope + ID before an ambiguous write', () => {
  const storage = memoryStorage()
  const pointer = rememberOperation(storage, scope, 'PUT', operationId, now)
  assert.deepEqual(recoverOperation(storage, now + 1000), pointer)
  const stored = JSON.stringify(storage.dump())
  assert.equal(stored.includes('accessToken'), false)
  assert.equal(stored.includes('code_verifier'), false)
  assert.equal(stored.includes('password'), false)
  assert.equal(stored.includes('spec'), false)
  assert.equal(stored.includes('token'), false)
  assert.deepEqual(JSON.parse(JSON.stringify(pointer)), {
    operationId, cluster: 'browser-ci', project: 'research',
    kind: 'ComputePool', resource: 'pool-a', method: 'PUT', issuedAt: now,
  })
  forgetOperation(storage)
  assert.equal(recoverOperation(storage, now + 2000), null)
})

test('invalid, injected, malformed and expired pointers are fail closed', () => {
  const storage = memoryStorage()
  const valid = rememberOperation(storage, scope, 'DELETE', operationId, now)
  assert.equal(recoverOperation(storage, now + 24 * 60 * 60 * 1000), null)
  for (const broken of [
    '{',
    JSON.stringify({ ...valid, operationId: 'wrong' }),
    JSON.stringify({ ...valid, kind: 'Secret' }),
    JSON.stringify({ ...valid, method: 'POST' }),
    JSON.stringify({ ...valid, project: '../foreign' }),
    'x'.repeat(1025),
  ]) {
    storage.setItem('gpu-management-operation-pointer-v1', broken)
    assert.equal(recoverOperation(storage, now + 500), null)
  }
  storage.setItem('gpu-management-operation-pointer-v1', JSON.stringify({ ...valid, accessToken: 'do-not-restore' }))
  const restored = recoverOperation(storage, now + 500)
  assert.equal('accessToken' in restored, false)
  assert.equal(restored.operationId, operationId)
})

test('storage failure blocks write before operation dispatch', () => {
  const storage = {
    getItem() { return null },
    setItem() { throw Error('storage disabled') },
    removeItem() {},
  }
  assert.throws(
    () => rememberOperation(storage, scope, 'PUT', operationId, now),
    /storage disabled/,
  )
  assert.throws(
    () => rememberOperation(memoryStorage(), { ...scope, project: '/invalid' }, 'DELETE', operationId, now),
    /无效的操作记录/,
  )
})
