import assert from 'node:assert/strict'
import test from 'node:test'
import { buildAuthorizedWorkloadRequest, parseAuthorizedPool } from '../src/management/workload-composer.ts'
import type { DesiredView } from '../src/management/api.ts'

const project = 'research'
const cluster = 'browser-ci'
const pool: DesiredView = {
  clusterId: cluster, projectId: project, kind: 'ComputePool',
  id: 'pool-a', generation: 1, deleting: false,
  spec: { projectID: project, provider: 'kueue', namespace: project,
    accelerators: [{ class: 'h100', quota: 2 }, { class: 'cpu', quota: 8 }],
    acceleratorBindings: [
      { class: 'h100', resourceName: 'nvidia.com/gpu', flavor: 'h100' },
      { class: 'cpu', resourceName: 'cpu', flavor: 'cpu' },
    ],
  },
}
const form = {
  id: 'train-a', image: 'registry.example/train@sha256:' + 'a'.repeat(64),
  acceleratorClass: 'h100', quota: 1, commandJSON: '["python","train.py"]',
}

test('OIDC-authorized pool produces current canonical management PUT contract (no network or credentials)', () => {
  const verified = parseAuthorizedPool(cluster, project, pool)
  assert.deepEqual(verified.accelerators[0], { class: 'h100', quota: 2 })
  const { target, requestJSON } = buildAuthorizedWorkloadRequest(verified, form, cluster, project)
  assert.deepEqual(target, { cluster, project, kind: 'Workload', resource: 'train-a' })
  assert.deepEqual(JSON.parse(requestJSON), {
    expectedGeneration: 0, poolId: 'pool-a',
    spec: {
      image: form.image,
      command: ['python', 'train.py'],
      accelerator: { class: 'h100', quota: 1 },
    },
  })
  assert.equal(requestJSON.includes('accessToken'), false)
  assert.equal(requestJSON.includes('namespace'), false)
  assert.equal(requestJSON.includes('role'), false)
})

test('pool selection fails closed for foreign, deleting or non-Kueue resources', () => {
  const invalid: DesiredView[] = [
    { ...pool, clusterId: 'foreign-cluster' },
    { ...pool, projectId: 'foreign-project' },
    { ...pool, kind: 'Workload' },
    { ...pool, deleting: true },
    { ...pool, generation: 0 },
    { ...pool, spec: { ...pool.spec, projectID: 'foreign-project' } },
    { ...pool, spec: { ...pool.spec, provider: 'volcano' } },
    { ...pool, spec: { ...pool.spec, acceleratorBindings: [] } },
    { ...pool, spec: { ...pool.spec, accelerators: [] } },
  ]
  for (const candidate of invalid) {
    assert.throws(() => parseAuthorizedPool(cluster, project, candidate))
  }
})

test('invalid Workload identity, quota, image, command and stale project prevent draft construction', () => {
  const verified = parseAuthorizedPool(cluster, project, pool)
  const invalid = [
    { ...form, id: '../foreign' },
    { ...form, quota: 0 },
    { ...form, quota: 3 },
    { ...form, acceleratorClass: 'nonexistent' },
    { ...form, image: 'image with spaces' },
    { ...form, commandJSON: '{"shell":"rm -rf /"}' },
    { ...form, commandJSON: '[]' },
    { ...form, commandJSON: '[""]' },
    { ...form, commandJSON: '["hello\\u0000world"]' },
  ]
  for (const candidate of invalid) {
    assert.throws(() => buildAuthorizedWorkloadRequest(verified, candidate, cluster, project))
  }
  assert.throws(() => buildAuthorizedWorkloadRequest(verified, form, cluster, 'foreign'))
  assert.throws(() => buildAuthorizedWorkloadRequest(verified, form, 'foreign', project))
})
