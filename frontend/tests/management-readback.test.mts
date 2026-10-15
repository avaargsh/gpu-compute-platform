import assert from 'node:assert/strict'
import test from 'node:test'
import { classifyDesiredReadback, receiptResourceScope } from '../src/management/readback.ts'
import type { DesiredView, Receipt } from '../src/management/api.ts'

const opId = '0123456789abcdef0123456789abcdef'
const receipt: Receipt = {
  requestId: opId, status: 'desired-committed',
  clusterId: 'browser-ci', projectId: 'research', kind: 'Workload',
  resourceId: 'train-a', action: 'create', expectedGeneration: 0,
  resultingGeneration: 1, committedAt: '2026-10-15T00:00:00Z',
  requestDigest: 'sha256-placeholder',
}
const desired: DesiredView = {
  clusterId: 'browser-ci', projectId: 'research', kind: 'Workload',
  id: 'train-a', generation: 1, deleting: false, spec: {},
}

test('receipt confines independent read-back to exact request ID and server-confirmed resource', () => {
  assert.deepEqual(receiptResourceScope(receipt, 'browser-ci', 'research', opId), {
    cluster: 'browser-ci', project: 'research', kind: 'Workload', resource: 'train-a',
  })
  const badReceipts: Receipt[] = [
    { ...receipt, requestId: 'different' },
    { ...receipt, projectId: 'other' },
    { ...receipt, clusterId: 'other' },
    { ...receipt, kind: 'Secret' as Receipt['kind'] },
    { ...receipt, resourceId: '../other' },
    { ...receipt, resultingGeneration: 0 },
    { ...receipt, status: 'executed' as Receipt['status'] },
    { ...receipt, action: 'other' },
  ]
  for (const bad of badReceipts) {
    assert.throws(() => receiptResourceScope(bad, 'browser-ci', 'research', opId))
  }
  assert.throws(() => receiptResourceScope(receipt, 'browser-ci', 'research', 'wrong'))
})

test('current same-generation Desired is a commit read-back only, never execution evidence', () => {
  const result = classifyDesiredReadback(receipt, desired)
  assert.equal(result.status, 'desired-visible')
  assert.match(result.detail, /不代表 Kueue/)
  assert.equal(classifyDesiredReadback(receipt, { ...desired, generation: 2 }).status, 'desired-advanced')
  assert.equal(classifyDesiredReadback(receipt, { ...desired, generation: 0 }).status, 'inconsistent')
  assert.equal(classifyDesiredReadback(receipt, { ...desired, deleting: true }).status, 'deleting-after-commit')
})

test('delete-request committed means cleanup is still unproven', () => {
  const deletion: Receipt = { ...receipt, action: 'delete-request', expectedGeneration: 1 }
  const result = classifyDesiredReadback(deletion, { ...desired, deleting: true })
  assert.equal(result.status, 'delete-request-visible')
  assert.match(result.detail, /仍须独立证明/)
  assert.equal(classifyDesiredReadback(deletion, desired).status, 'inconsistent')
})

test('404 is not proof of rollback or finalizer completion; cross-scope response rejects', () => {
  const result = classifyDesiredReadback(receipt, null)
  assert.equal(result.status, 'not-visible')
  assert.match(result.detail, /不是任务完成/)
  for (const x of [
    { ...desired, projectId: 'foreign' },
    { ...desired, clusterId: 'foreign' },
    { ...desired, id: 'other' },
    { ...desired, kind: 'ComputePool' as DesiredView['kind'] },
  ]) assert.throws(() => classifyDesiredReadback(receipt, x), /不匹配/)
})
