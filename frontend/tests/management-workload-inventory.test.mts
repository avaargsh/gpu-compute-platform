import assert from 'node:assert/strict'
import test from 'node:test'
import { listWorkloads, ManagementHTTPError } from '../src/management/api.ts'

const token = 'authorized-oidc-test-token'

test('project Workload inventory only sends OIDC GET to fixed scoped same-origin path', async () => {
  const original = globalThis.fetch
  const calls: Array<{ path: string; opts: RequestInit }> = []
  try {
    globalThis.fetch = async (path, opts) => {
      calls.push({ path: String(path), opts })
      return Response.json({
        items: [{ id: 'job-a', generation: 1, deleting: false }],
        nextCursor: null, hasMore: false,
      })
    }
    const first = await listWorkloads('gpu-cluster', 'research', token)
    const next = await listWorkloads('gpu-cluster', 'research', token, 'job-25')
    assert.equal(first.value?.items[0]?.id, 'job-a')
    assert.equal(next.value?.nextCursor, null)
    assert.deepEqual(calls.map(x => x.path), [
      '/api/v1/management/clusters/gpu-cluster/projects/research/workloads',
      '/api/v1/management/clusters/gpu-cluster/projects/research/workloads?after=job-25',
    ])
    for (const x of calls) {
      assert.equal(x.opts.method, 'GET')
      assert.equal((x.opts.headers as Record<string, string>).Authorization, 'Bearer ' + token)
      assert.equal(x.opts.credentials, 'omit')
      assert.equal(x.opts.redirect, 'error')
      assert.equal(x.opts.cache, 'no-store')
      assert.equal(x.path.includes(token), false)
      assert.equal(x.path.includes('/agent/'), false)
    }
    await assert.rejects(listWorkloads('../foreign', 'research', token), /无效/)
    await assert.rejects(listWorkloads('gpu-cluster', 'research', token, '../invalid'), /无效/)
    assert.equal(calls.length, 2)
    globalThis.fetch = async () => new Response(null, { status: 403 })
    await assert.rejects(listWorkloads('gpu-cluster', 'research', token),
      e => e instanceof ManagementHTTPError && e.status === 403)
  } finally {
    globalThis.fetch = original
  }
})
