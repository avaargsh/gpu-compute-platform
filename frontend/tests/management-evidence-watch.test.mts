import assert from 'node:assert/strict'
import test from 'node:test'
import {
  EVIDENCE_WATCH_INTERVAL_MS,
  EVIDENCE_WATCH_MAX_READS,
  evidenceWatchDecision,
} from '../src/management/evidence-watch.ts'
import type { EvidenceCheck } from '../src/management/evidence-check.ts'

const pending: EvidenceCheck = {
  readback: { status: 'desired-visible', detail: 'Desired recorded', desired: null },
  execution: null,
}

test('explicit read-only watch is bounded, never an unlimited background poll', () => {
  assert.equal(EVIDENCE_WATCH_INTERVAL_MS, 15_000)
  assert.equal(EVIDENCE_WATCH_MAX_READS, 12)
  for (const count of [1, 2, 11]) {
    assert.equal(evidenceWatchDecision(pending, count), 'continue')
  }
  assert.equal(evidenceWatchDecision(pending, 12), 'stop-budget')
  assert.equal(evidenceWatchDecision(pending, 13), 'stop-budget')
})

test('terminal Agent reports end read-only watching but do not imply cryptographic verification', () => {
  for (const code of ['SUCCEEDED', 'FAILED', 'IDENTITY_CONFLICT', 'OBSERVATION_CONFLICT'] as const) {
    const check: EvidenceCheck = {
      ...pending,
      execution: { code } as EvidenceCheck['execution'],
    }
    assert.equal(evidenceWatchDecision(check, 3), 'stop-terminal')
  }
})

test('advanced or contradictory Desired can no longer be attributed to the original operation', () => {
  for (const status of ['desired-advanced', 'inconsistent'] as const) {
    const check: EvidenceCheck = { ...pending, readback: { ...pending.readback, status } }
    assert.equal(evidenceWatchDecision(check, 1), 'stop-scope')
  }
})

test('absence of Observation and pending admission remain diagnostic, not verified results', () => {
  for (const code of ['NOT_OBSERVED', 'WAITING_QUOTA', 'WAITING_ADMISSION', 'WAITING_PODS', 'DELETING', 'STALE_OBSERVATION'] as const) {
    const check: EvidenceCheck = { ...pending, execution: { code } as EvidenceCheck['execution'] }
    assert.equal(evidenceWatchDecision(check, 2), 'continue')
  }
})
