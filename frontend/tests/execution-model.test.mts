import assert from 'node:assert/strict'
import test from 'node:test'
import { conditionFlag, executionOf, localLogCommand, verifiedJobEvidence } from '../src/portal/execution-model.ts'
import type { ResourceRow, Condition } from '../src/portal/api.ts'

function workload(conditions: Condition[] = []): ResourceRow {
  return {
    desired: { kind: 'Workload', id: 'train-a', generation: 2,
      spec: { projectID: 'research', workspace: { claimName: 'data-pvc' } } },
    state: {
      kind: 'Workload', id: 'train-a', desiredGeneration: 2, observedGeneration: 2,
      syncState: 'Synced', conditions,
      evidenceRefs: ['k8s://cluster-a/namespaces/research/jobs/job-train-a',
        'kueue://cluster-a/namespaces/research/workloads/kueue-train-a'],
    },
    error: null,
  }
}
const c = (type: string, status: string, reason = '') => ({ type, status, reason })
test('generation Synced alone never means admitted or successful', () => {
  assert.equal(executionOf(workload()).code, 'INDETERMINATE')
  assert.equal(conditionFlag(workload(), 'Admitted'), 'Unknown')
})
test('independently observed Kueue stages', () => {
  const waiting = workload([c('QuotaReserved', 'False', 'InsufficientQuota')])
  assert.equal(executionOf(waiting).code, 'WAITING_QUOTA')
  assert.equal(executionOf(waiting).detail, 'InsufficientQuota')
  assert.equal(executionOf(workload([c('QuotaReserved', 'True'), c('Admitted', 'False')])).code,
    'WAITING_ADMISSION')
  assert.equal(executionOf(workload([
    c('QuotaReserved', 'True'), c('Admitted', 'True'), c('PodsReady', 'False'),
  ])).code, 'WAITING_PODS')
  assert.equal(executionOf(workload([
    c('QuotaReserved', 'True'), c('Admitted', 'True'), c('PodsReady', 'True'),
  ])).code, 'RUNNING')
})
test('observed job terminal outcomes do not claim checkpoint persisted', () => {
  assert.equal(executionOf(workload([c('Succeeded', 'True', 'JobSucceeded')])).code, 'SUCCEEDED')
  assert.equal(executionOf(workload([c('Failed', 'True', 'JobFailed')])).code, 'FAILED')
  assert.equal(executionOf(workload([c('Failed', 'True'), c('Succeeded', 'True')])).code,
    'OBSERVATION_CONFLICT')
})
test('rejects stale and mismatched observation even if it says success', () => {
  const w = workload([c('Succeeded', 'True')])
  if (!w.state) throw new Error('test fixture')
  w.state.observedGeneration = 1
  assert.equal(executionOf(w).code, 'STALE_OBSERVATION')
  w.state.observedGeneration = 3
  assert.equal(executionOf(w).code, 'OBSERVATION_CONFLICT')
  w.state.observedGeneration = 2
  w.state.id = 'other'
  assert.equal(executionOf(w).code, 'IDENTITY_CONFLICT')
})
test('read error, delete, missing observation and duplicate conditions fail closed', () => {
  const w = workload([c('Admitted', 'True'), c('Admitted', 'False')])
  assert.equal(executionOf(w).code, 'OBSERVATION_CONFLICT')
  w.state = null
  assert.equal(executionOf(w).code, 'NOT_OBSERVED')
  w.error = 'private backend details'
  assert.equal(executionOf(w).code, 'READ_FAILED')
  assert.notEqual(executionOf(w).detail, w.error)
  w.desired.deletionTimestamp = '2026-10-15T00:00:00Z'
  assert.equal(executionOf(w).code, 'READ_FAILED')
})
test('evidence allows only one exact scoped Kubernetes Job and explicit local context', () => {
  const w = workload([c('QuotaReserved', 'True'), c('Admitted', 'True')])
  assert.deepEqual(verifiedJobEvidence(w, 'cluster-a'),
    { namespace: 'research', job: 'job-train-a' })
  assert.equal(localLogCommand(w, 'cluster-a', 'kind-gpu'),
    'kubectl --context kind-gpu --namespace research logs job/job-train-a --all-containers=true --tail=200')
  assert.equal(localLogCommand(w, 'cluster-a', ';rm -rf /'), null)
  assert.equal(localLogCommand(w, 'cluster-b', 'kind-gpu'), null)
})
test('injected namespace, ambiguous Job references and mismatched evidence are rejected', () => {
  const w = workload([c('QuotaReserved', 'True'), c('Admitted', 'True')])
  if (!w.state) throw new Error('test fixture')
  w.state.evidenceRefs = ['k8s://cluster-a/namespaces/research;bad/jobs/job-a']
  assert.equal(localLogCommand(w, 'cluster-a', 'kind-gpu'), null)
  w.state.evidenceRefs = [
    'k8s://cluster-a/namespaces/research/jobs/job-a',
    'k8s://cluster-a/namespaces/research/jobs/job-b',
  ]
  assert.equal(localLogCommand(w, 'cluster-a', 'kind-gpu'), null)
  w.state.evidenceRefs = ['k8s://cluster-b/namespaces/research/jobs/job-a']
  assert.equal(localLogCommand(w, 'cluster-a', 'kind-gpu'), null)
})
test('workspace claim shows only desired declaration, never persisted checkpoint proof', () => {
  const value = executionOf(workload())
  assert.equal(value.workspaceClaim, 'data-pvc')
  assert.notEqual(value.code, 'SUCCEEDED')
})
