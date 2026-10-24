import assert from 'node:assert/strict'
import test from 'node:test'
import { boundDeleteGeneration } from '../src/management/delete-scope.ts'
import type { DesiredView, Scope } from '../src/management/api.ts'

const scope: Scope = {
  cluster: 'cluster-a', project: 'project-a', kind: 'Workload', resource: 'train-1',
}
const desired: DesiredView = {
  clusterId: 'cluster-a', projectId: 'project-a', kind: 'Workload', id: 'train-1',
  generation: 3, deleting: false, spec: {},
}

test('authorized DELETE preflight binds the exact read Desired identity and Generation', () => {
  assert.equal(boundDeleteGeneration(desired, scope), 3)
})

test('same Generation from another resource cannot authorize the selected DELETE target', () => {
  const foreignSelections: Scope[] = [
    { ...scope, cluster: 'cluster-b' },
    { ...scope, project: 'project-b' },
    { ...scope, kind: 'ComputePool' },
    { ...scope, resource: 'train-2' },
  ]
  for (const selected of foreignSelections) {
    assert.equal(boundDeleteGeneration(desired, selected), null)
  }
  assert.equal(boundDeleteGeneration({ ...desired, clusterId: 'cluster-b' }, scope), null)
  assert.equal(boundDeleteGeneration({ ...desired, projectId: 'project-b' }, scope), null)
  assert.equal(boundDeleteGeneration({ ...desired, kind: 'ComputePool' }, scope), null)
  assert.equal(boundDeleteGeneration({ ...desired, id: 'train-2' }, scope), null)
})

test('missing, deleting or invalid Generation cannot authorize DELETE from cached UI state', () => {
  assert.equal(boundDeleteGeneration(null, scope), null)
  assert.equal(boundDeleteGeneration({ ...desired, deleting: true }, scope), null)
  for (const generation of [0, -1, Number.NaN, Number.POSITIVE_INFINITY, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    assert.equal(boundDeleteGeneration({ ...desired, generation }, scope), null)
  }
})
