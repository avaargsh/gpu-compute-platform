import assert from 'node:assert/strict'
import test from 'node:test'
import { findingsFor, projectSummaries, triageFindings } from '../src/portal/project-model.ts'

function row(id: string, kind = 'Workload', projectID = 'research') {
  return {
    desired: { kind, id, generation: 2, spec: { projectID } },
    state: {
      kind, id, desiredGeneration: 2, observedGeneration: 2,
      syncState: 'Synced', conditions: [], evidenceRefs: [],
    },
    error: null,
  }
}

test('synced generations alone are not counted as ready or GPU usage', () => {
  const record = row('w1')
  assert.deepEqual(findingsFor(record), [])
  assert.equal(record.state.syncState, 'Synced')
  // No Readiness verdict or utilization claim is generated from this state.
})

test('failed readiness condition remains actionable even when generations are synced', () => {
  const record = row('w2')
  record.state.conditions = [{ type: 'Admitted', status: 'False', reason: 'QueuePending' }]
  const findings = findingsFor(record)
  assert.equal(findings[0].code, 'ADMITTED_FALSE')
  assert.equal(findings[0].description, 'QueuePending')
})

test('ownership / generation identity mismatch fails closed', () => {
  const record = row('w3')
  record.state.id = 'different-resource'
  assert.equal(findingsFor(record)[0].code, 'IDENTITY_MISMATCH')
  record.state.id = 'w3'
  record.state.observedGeneration = 3
  assert.equal(findingsFor(record)[0].code, 'GENERATION_CONFLICT')
})

test('observation failure precedes deletion and missing observed state', () => {
  const record = row('w4')
  record.error = 'Go API returned 403'
  record.desired.deletionTimestamp = '2026-10-11T00:00:00Z'
  assert.equal(findingsFor(record)[0].code, 'OBSERVATION_READ_FAILED')
  record.error = null
  assert.equal(findingsFor(record)[0].code, 'DELETION_PENDING')
  record.desired.deletionTimestamp = null
  record.state = null
  assert.equal(findingsFor(record)[0].code, 'OBSERVATION_MISSING')
})

test('projects aggregate only declared requested pool quota, not used/allocated GPU', () => {
  const p = row('pool', 'ComputePool', 'team-a')
  p.desired.spec.accelerators = [{ class: 'h100-80g', quota: 4 }]
  const w = row('task', 'Workload', 'team-a')
  const missing = row('unknown', 'Workload', '')
  const projects = projectSummaries([missing, p, w])
  assert.equal(projects[0].id, 'team-a')
  assert.equal(projects[0].poolCount, 1)
  assert.equal(projects[0].workloadCount, 1)
  assert.equal(projects[0].declaredPoolGpuQuota, 4)
  assert.equal(projects[1].id, '')
  assert.equal(projects[1].workloadCount, 1)
})

test('malformed pool quota remains unknown rather than turning into zero', () => {
  const p = row('pool', 'ComputePool')
  p.desired.spec.accelerators = [{ class: 'x', quota: 'many' }]
  assert.equal(projectSummaries([p])[0].declaredPoolGpuQuota, null)
})

test('triage puts identity and read errors ahead of pending entries', () => {
  const pending = row('pending')
  pending.state.observedGeneration = 1
  const failed = row('failed')
  failed.error = 'HTTP 403'
  const results = triageFindings([pending, failed])
  assert.equal(results[0].code, 'OBSERVATION_READ_FAILED')
  assert.equal(results[1].code, 'GENERATION_PENDING')
})

test('failed Job reaches Operations Inbox even with Ready False and synced generation', () => {
  const w = row('run-failed')
  w.state.conditions = [
    { type: 'Ready', status: 'False', reason: 'JobFailed' },
    { type: 'Failed', status: 'True', reason: 'ContainerExitNonZero' },
  ]
  const found = findingsFor(w)
  assert.equal(found.length, 1)
  assert.equal(found[0].code, 'JOB_FAILED')
  assert.equal(found[0].severity, 'critical')
  assert.equal(found[0].description, 'ContainerExitNonZero')
})
test('conflicting job terminal observations are critical findings', () => {
  const w = row('run-conflict')
  w.state.conditions = [
    { type: 'Failed', status: 'True' },
    { type: 'Succeeded', status: 'True' },
  ]
  assert.equal(findingsFor(w)[0].code, 'JOB_OBSERVATION_CONFLICT')
})
