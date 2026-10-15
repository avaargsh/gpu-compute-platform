import assert from 'node:assert/strict'
import test from 'node:test'
import { buildWorkloadDraft, draftPools } from '../src/portal/workload-draft.ts'

function pool(id: string, projectID: string, quota: number, deleting = false) {
  return {
    desired: { kind: 'ComputePool', id, generation: 1, spec: {
      projectID, accelerators: [{ class: 'h100-80g', quota }],
    }, deletionTimestamp: deleting ? '2026-10-15T00:00:00Z' : null },
    state: null, error: null,
  }
}
const input = {
  id: 'train-001', poolId: 'pool-a', acceleratorClass: 'h100-80g', quota: 2,
  image: 'registry.example/pytorch@sha256:deadbeef', commandJson: '["python","train.py"]',
  mode: 'training' as const,
}
test('uses the actual pool project rather than a browser supplied project identity', () => {
  const pools = draftPools([pool('pool-a', 'research', 4)])
  const output = buildWorkloadDraft(input, pools)
  assert.deepEqual(output.errors, [])
  assert.equal(output.payload?.projectId, 'research')
  assert.equal(output.payload?.spec.accelerator.quota, 2)
  assert.deepEqual(output.payload?.spec.command, ['python', 'train.py'])
})
test('deleting, malformed and foreign/unlisted pool cannot generate a workload', () => {
  assert.equal(draftPools([pool('pool-a', 'research', 4, true)]).length, 0)
  assert.equal(draftPools([pool('pool-a', '', 4)]).length, 0)
  const result = buildWorkloadDraft(input, draftPools([pool('pool-b', 'research', 4)]))
  assert.equal(result.payload, null)
})
test('rejects oversized GPU request instead of claiming that quotas are available', () => {
  const result = buildWorkloadDraft({ ...input, quota: 5 }, draftPools([pool('pool-a', 'research', 4)]))
  assert.equal(result.payload, null)
  assert.match(result.errors.join(' '), /GPU/)
})
test('rejects invalid IDs, image and non-argv commands', () => {
  const result = buildWorkloadDraft({
    ...input, id: '../escape', image: '', commandJson: '{"shell":"rm -rf"}',
  }, draftPools([pool('pool-a', 'research', 4)]))
  assert.equal(result.payload, null)
  assert.ok(result.errors.length >= 3)
})
test('batch inference remains a Workload Job intent, not an online Serving endpoint', () => {
  const result = buildWorkloadDraft({
    ...input, mode: 'batch-inference', commandJson: '["python","batch_infer.py"]',
  }, draftPools([pool('pool-a', 'research', 4)]))
  assert.equal(result.payload?.spec.command[1], 'batch_infer.py')
  assert.equal(Object.hasOwn(result.payload ?? {}, 'serving'), false)
})
test('rejects duplicate or unrecognized accelerator class', () => {
  const result = buildWorkloadDraft({
    ...input, acceleratorClass: 'fake-h100',
  }, draftPools([pool('pool-a', 'research', 4)]))
  assert.equal(result.payload, null)
})

test('existing PVC name is included only when explicitly provided', () => {
  const pools = draftPools([pool('pool-a', 'research', 4)])
  const without = buildWorkloadDraft(input, pools)
  assert.equal(without.payload?.spec.workspace, undefined)
  const withPVC = buildWorkloadDraft({ ...input, workspaceClaim: 'train-data.v1' }, pools)
  assert.deepEqual(withPVC.errors, [])
  assert.deepEqual(withPVC.payload?.spec.workspace, { claimName: 'train-data.v1' })
})
test('invalid or cross-namespace PVC names fail closed', () => {
  const pools = draftPools([pool('pool-a', 'research', 4)])
  for (const name of ['../other', '/etc/passwd', 'OTHER_NS', 'bad..name', 'workspace/other', 'not valid']) {
    const got = buildWorkloadDraft({ ...input, workspaceClaim: name }, pools)
    assert.equal(got.payload, null, name)
    assert.match(got.errors.join(' '), /PVC/, name)
  }
})
