import assert from 'node:assert/strict'
import test from 'node:test'
import { buildKServeDraft, servingProjects } from '../src/portal/kserve-draft.ts'
import type { ResourceRow } from '../src/portal/api.ts'

const row = (projectID: unknown, error: string | null = null): ResourceRow => ({
  desired: { kind: 'ComputePool', id: 'pool-a', generation: 1, spec: { projectID } },
  state: null, error,
})
const projects = [{ id: 'research' }, { id: 'inference' }]
const input = {
  id: 'qwen-serving', projectId: 'research', namespace: 'research',
  image: 'registry.example/vllm@sha256:' + 'a'.repeat(64),
  argsJson: '["--model","/models/qwen"]', wholeGpuCount: 2,
}
test('generates KServe custom predictor v1beta1, no extra controller or second queue', () => {
  const result = buildKServeDraft(input, projects)
  assert.deepEqual(result.errors, [])
  const manifest = result.manifest
  assert.ok(manifest)
  assert.equal(manifest.apiVersion, 'serving.kserve.io/v1beta1')
  assert.equal(manifest.kind, 'InferenceService')
  assert.equal(manifest.metadata.annotations['ai.compute/project-id'], 'research')
  const predictor = manifest.spec.predictor
  assert.equal(predictor.minReplicas, 1)
  assert.equal(predictor.maxReplicas, 1)
  assert.equal(predictor.containers[0].name, 'kserve-container')
  assert.deepEqual(predictor.containers[0].resources.requests, predictor.containers[0].resources.limits)
  assert.equal(predictor.containers[0].resources.limits['nvidia.com/gpu'], 2)
  assert.equal(JSON.stringify(manifest).includes('kueue'), false)
})
test('refuses image without digest, foreign project, unverified namespace and fractional GPU', () => {
  for (const change of [
    { projectId: 'foreign' },
    { namespace: '../other' },
    { namespace: 'BadNamespace' },
    { image: 'registry.example/vllm:latest' },
    { image: 'registry.example/vllm@sha256:deadbeef' },
    { wholeGpuCount: 0 },
    { wholeGpuCount: 1.5 },
    { argsJson: '["--model",null]' },
    { argsJson: '["a\\u0000b"]' },
  ]) {
    const result = buildKServeDraft({ ...input, ...change }, projects)
    assert.equal(result.manifest, null, JSON.stringify(change))
    assert.ok(result.errors.length > 0)
  }
})
test('only actual scoped nondeleting projects appear as choices', () => {
  const a = row('research')
  const b = row('inference')
  const x = row('foreign', 'read forbidden')
  const pending = row('other')
  pending.desired.deletionTimestamp = '2026-10-15T00:00:00Z'
  assert.deepEqual(servingProjects([a, b, a, x, pending]), projects.reverse())
})
