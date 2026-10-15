import assert from 'node:assert/strict'
import test from 'node:test'
import { verifyEvidencePair } from '../src/management/evidence-check.ts'
import type { DesiredView, ExecutionView, Receipt } from '../src/management/api.ts'

const receipt: Receipt = {
  requestId: '0123456789abcdef0123456789abcdef', status: 'desired-committed',
  clusterId: 'ci-cluster', projectId: 'research', kind: 'Workload',
  resourceId: 'job-a', action: 'create', expectedGeneration: 0, resultingGeneration: 1,
  requestDigest: 'digest', committedAt: '2026-10-15T11:59:00Z',
}
const desired: DesiredView = {
  clusterId: 'ci-cluster', projectId: 'research', kind: 'Workload',
  id: 'job-a', generation: 1, deleting: false, spec: { projectID: 'research' },
}
const observation: ExecutionView = {
  clusterId: 'ci-cluster', projectId: 'research', kind: 'Workload',
  id: 'job-a', desiredGeneration: 1, deleting: false,
  observedGeneration: 1, observedAt: '2026-10-15T11:59:30Z',
  conditions: [
    { type: 'QuotaReserved', status: 'True' },
    { type: 'Admitted', status: 'True' },
    { type: 'Succeeded', status: 'True' },
  ],
}
const now = Date.parse('2026-10-15T12:00:00Z')

test('matching receipt + fresh Desired + independent observation may display observed success', () => {
  const result = verifyEvidencePair(receipt, desired, observation, now)
  assert.equal(result.readback.status, 'desired-visible')
  assert.equal(result.execution?.code, 'SUCCEEDED')
  assert.equal(result.execution?.newerThanReceipt, false)
})

test('a concurrent Desired update or delete mismatch never reuses an old success', () => {
  for (const changed of [
    { ...observation, desiredGeneration: 2 },
    { ...observation, desiredGeneration: 0 },
    { ...observation, deleting: true },
    { ...observation, projectId: 'another' },
  ]) assert.throws(() => verifyEvidencePair(receipt, desired, changed, now))

  const deleteReceipt: Receipt = { ...receipt, action: 'delete-request' }
  const deletion = { ...desired, deleting: true }
  const deletingObs = { ...observation, deleting: true }
  const result = verifyEvidencePair(deleteReceipt, deletion, deletingObs, now)
  assert.equal(result.readback.status, 'delete-request-visible')
  assert.equal(result.execution?.code, 'DELETING')
})

test('missing, advanced or contradictory Desired never claims Job success', () => {
  assert.deepEqual(verifyEvidencePair(receipt, null, null, now).execution, null)
  const advanced = verifyEvidencePair(receipt, { ...desired, generation: 2 }, null, now)
  assert.equal(advanced.readback.status, 'desired-advanced')
  assert.equal(advanced.execution, null)
  const inconsistent = verifyEvidencePair({ ...receipt, resultingGeneration: 2 }, desired, null, now)
  assert.equal(inconsistent.readback.status, 'inconsistent')
  assert.equal(inconsistent.execution, null)
  assert.throws(() => verifyEvidencePair(receipt, null, observation, now))
  assert.throws(() => verifyEvidencePair(receipt, { ...desired, generation: 2 }, observation, now))
  assert.throws(() => verifyEvidencePair(receipt, desired, null, now))
})

test('ComputePool receipt can be checked without Workload execution projection', () => {
  const poolReceipt: Receipt = { ...receipt, kind: 'ComputePool', resourceId: 'pool-a' }
  const poolDesired: DesiredView = { ...desired, kind: 'ComputePool', id: 'pool-a' }
  const result = verifyEvidencePair(poolReceipt, poolDesired, null, now)
  assert.equal(result.readback.status, 'desired-visible')
  assert.equal(result.execution, null)
  assert.throws(() => verifyEvidencePair(poolReceipt, poolDesired, observation, now))
})
