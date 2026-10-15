import assert from 'node:assert/strict'
import test from 'node:test'
import { submitDevWorkload } from '../src/portal/dev-submit.ts'
import type { WorkloadDraftPayload } from '../src/portal/workload-draft.ts'

const read = 'read-token-long-enough-to-meet-32-characters'
const write = 'separate-write-token-long-enough-to-meet-32'
const draft: WorkloadDraftPayload = {
  metadata: { id: 'train-test', generation: 1 }, projectId: 'research', poolId: 'pool-h100',
  spec: {
    image: 'registry.example/train:latest', command: ['python', 'train.py'],
    accelerator: { class: 'h100-80g', quota: 1 },
  },
}
test('developer submit uses only scoped operator URL and two distinct credentials', async () => {
  const calls: Array<[string, RequestInit]> = []
  const original = globalThis.fetch
  globalThis.fetch = async (url, options) => {
    calls.push([String(url), options ?? {}])
    return { status: 204 } as Response
  }
  try {
    await submitDevWorkload('cluster-a', read, write, draft)
    assert.equal(calls.length, 1)
    assert.equal(calls[0][0], '/api/v1/operator/clusters/cluster-a/projects/research/workloads/train-test')
    assert.equal(calls[0][1].method, 'PUT')
    assert.deepEqual(JSON.parse(String(calls[0][1].body)), draft)
    const headers = calls[0][1].headers as Record<string, string>
    assert.equal(headers.Authorization, 'Bearer ' + read)
    assert.equal(headers['X-Operator-Write-Token'], write)
    assert.equal(calls[0][1].credentials, 'omit')
    assert.equal(calls[0][1].redirect, 'error')
  } finally {
    globalThis.fetch = original
  }
})
test('refuses identity injection or same/missing write token without network', async () => {
  const original = globalThis.fetch
  let calls = 0
  globalThis.fetch = async () => {
    calls++
    return { status: 204 } as Response
  }
  try {
    for (const [cluster, reader, writer, input] of [
      ['../foreign', read, write, draft],
      ['cluster-a', read, read, draft],
      ['cluster-a', read, 'short', draft],
      ['cluster-a', read, write, { ...draft, projectId: '../foreign' }],
    ] as const) {
      await assert.rejects(submitDevWorkload(cluster, reader, writer, input))
    }
    assert.equal(calls, 0)
  } finally {
    globalThis.fetch = original
  }
})
test('never claims accepted on 401/403/404/409/500', async () => {
  const original = globalThis.fetch
  try {
    for (const status of [401, 403, 404, 409, 500]) {
      globalThis.fetch = async () => ({ status }) as Response
      await assert.rejects(submitDevWorkload('cluster-a', read, write, draft))
    }
  } finally {
    globalThis.fetch = original
  }
})
