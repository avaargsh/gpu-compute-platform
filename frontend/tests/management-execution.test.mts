import assert from 'node:assert/strict'
import test from 'node:test'
import type { Receipt, ExecutionView } from '../src/management/api.ts'
import { classifyScopedExecution } from '../src/management/execution.ts'

const now = Date.parse('2026-10-15T12:00:00Z')
const receipt: Receipt = {
  requestId: '0123456789abcdef0123456789abcdef', status: 'desired-committed',
  clusterId: 'browser-ci', projectId: 'research', kind: 'Workload',
  resourceId: 'job-a', action: 'create', resultingGeneration: 1, expectedGeneration: 0,
  requestDigest: 'digest', committedAt: '2026-10-15T11:59:00Z',
}
const base: ExecutionView = {
  clusterId: 'browser-ci', projectId: 'research', kind: 'Workload', id: 'job-a',
  desiredGeneration: 1, deleting: false, observedGeneration: null,
  observedAt: null, conditions: [],
}
const observed: ExecutionView = {
  ...base, observedGeneration: 1, observedAt: '2026-10-15T11:59:30Z',
  conditions: [
    { type: 'QuotaReserved', status: 'True' },
    { type: 'Admitted', status: 'True' },
    { type: 'PodsReady', status: 'True' },
  ],
}

test('missing or stale Observation cannot be mislabelled as GPU execution success', () => {
  assert.equal(classifyScopedExecution(base, receipt, now).code, 'NOT_OBSERVED')
  assert.equal(classifyScopedExecution({ ...observed, observedGeneration: 0 }, receipt, now).code, 'STALE_OBSERVATION')
  assert.equal(classifyScopedExecution({ ...observed, observedAt: '2026-10-15T11:40:00Z' }, receipt, now).code, 'STALE_OBSERVATION')
  assert.equal(classifyScopedExecution({ ...observed, observedAt: '2026-10-15T12:05:00Z' }, receipt, now).code, 'STALE_OBSERVATION')
})

test('Kueue conditions carry independent readiness and terminal failure evidence', () => {
  assert.equal(classifyScopedExecution(observed, receipt, now).code, 'RUNNING')
  assert.equal(classifyScopedExecution({
    ...observed, conditions: [...observed.conditions, { type: 'Failed', status: 'True', reason: 'JobFailed' }],
  }, receipt, now).code, 'FAILED')
  assert.equal(classifyScopedExecution({
    ...observed, conditions: [...observed.conditions, { type: 'Succeeded', status: 'True' }],
  }, receipt, now).code, 'SUCCEEDED')
  assert.equal(classifyScopedExecution({
    ...observed, conditions: [
      { type: 'QuotaReserved', status: 'False' }, { type: 'Admitted', status: 'False' },
    ],
  }, receipt, now).code, 'WAITING_QUOTA')
  assert.equal(classifyScopedExecution({ ...observed, deleting: true }, receipt, now).code, 'DELETING')
})

test('project or resource mismatch is rejected; advanced Generation is not original operation outcome', () => {
  for (const x of [
    { ...observed, projectId: 'other' },
    { ...observed, clusterId: 'other' },
    { ...observed, id: 'other' },
    { ...observed, desiredGeneration: 0 },
  ]) assert.throws(() => classifyScopedExecution(x, receipt, now))
  const newer = classifyScopedExecution({ ...observed, desiredGeneration: 2 }, receipt, now)
  assert.equal(newer.newerThanReceipt, true)
  assert.equal(newer.code, 'STALE_OBSERVATION')
})
