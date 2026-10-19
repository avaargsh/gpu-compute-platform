import assert from 'node:assert/strict'
import test from 'node:test'
import { classifyOperationVerdict, type OperationVerdictCode } from '../src/management/operation-verdict.ts'
import type { EvidenceCheck } from '../src/management/evidence-check.ts'
import type { ReadbackStatus } from '../src/management/readback.ts'
import type { ExecutionCode } from '../src/portal/execution-model.ts'

const inspect = (status: ReadbackStatus, code: ExecutionCode | null, newer = false) => {
  const check: EvidenceCheck = {
    readback: { status, detail: 'fixture only', desired: null },
    execution: code === null ? null : {
      code, label: 'fixture', detail: 'fixture only',
      projectId: 'research', workloadId: 'job-a', generation: 1,
      conditions: [], evidenceRefs: [], workspaceClaim: null,
      observedAt: '2026-10-15T11:59:30Z', newerThanReceipt: newer,
    },
  }
  return classifyOperationVerdict(check)
}

const cases: Array<[ReadbackStatus, ExecutionCode | null, OperationVerdictCode]> = [
  ['desired-visible', 'SUCCEEDED', 'OWNERSHIP_UNPROVEN'],
  ['desired-visible', 'RUNNING', 'OWNERSHIP_UNPROVEN'],
  ['desired-visible', 'FAILED', 'OUTCOME_MISMATCH'],
  ['desired-visible', 'OBSERVATION_CONFLICT', 'OUTCOME_MISMATCH'],
  ['inconsistent', null, 'OUTCOME_MISMATCH'],
  ['desired-advanced', null, 'OWNERSHIP_UNPROVEN'],
  ['deleting-after-commit', null, 'OWNERSHIP_UNPROVEN'],
  ['delete-request-visible', 'DELETING', 'OUTCOME_PENDING'],
  ['not-visible', null, 'VERIFICATION_UNAVAILABLE'],
  ['desired-visible', 'STALE_OBSERVATION', 'VERIFICATION_UNAVAILABLE'],
  ['desired-visible', 'WAITING_QUOTA', 'OUTCOME_PENDING'],
  ['desired-visible', null, 'VERIFICATION_UNAVAILABLE'],
]
for (const [status, code, expected] of cases) {
  test(`read-only ${status}/${code ?? 'none'} -> ${expected}`, () => {
    const result = inspect(status, code)
    assert.equal(result.code, expected)
    assert.equal(result.cryptographicallyVerified, false)
    assert.notEqual(result.code, 'AUTHORIZED_AND_VERIFIED')
  })
}
test('new-generation observation cannot be attributed to an old operation', () => {
  assert.equal(inspect('desired-visible', 'SUCCEEDED', true).code, 'OWNERSHIP_UNPROVEN')
})
