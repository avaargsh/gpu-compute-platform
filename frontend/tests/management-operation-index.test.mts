import assert from 'node:assert/strict'
import test from 'node:test'
import { listRecentOperations, ManagementHTTPError } from '../src/management/api.ts'

test('recent own-audit discovery uses scoped same-origin bearer GET only', async () => {
  const old = globalThis.fetch
  const calls: Array<{ url: unknown; opts: RequestInit }> = []
  try {
    globalThis.fetch = async (url, opts) => {
      calls.push({ url, opts })
      return Response.json({
        items: [{ requestId: '0123456789abcdef0123456789abcdef', kind: 'Workload',
          resourceId: 'train-1', action: 'create', committedAt: '2026-10-16T01:00:00Z' }],
        truncated: true,
      })
    }
    const result = await listRecentOperations('cluster-a', 'project-a', 'valid-access-token')
    assert.equal(result.status, 200)
    assert.equal(result.value?.items[0]?.resourceId, 'train-1')
    assert.equal(result.value?.truncated, true)
    assert.equal(calls.length, 1)
    assert.equal(calls[0]?.url, '/api/v1/management/clusters/cluster-a/projects/project-a/operations')
    assert.equal(calls[0]?.opts.method, 'GET')
    assert.equal((calls[0]?.opts.headers as Record<string, string>).Authorization, 'Bearer valid-access-token')
    assert.equal(calls[0]?.opts.credentials, 'omit')
    assert.equal(calls[0]?.opts.redirect, 'error')
    assert.equal(calls[0]?.opts.cache, 'no-store')
    assert.equal('body' in (calls[0]?.opts ?? {}), true)
    assert.equal((calls[0]?.opts.headers as Record<string, string>)['Idempotency-Key'], undefined)
    await assert.rejects(listRecentOperations('../foreign', 'project-a', 'valid-access-token'), /无效/)
    await assert.rejects(listRecentOperations('cluster-a', 'project/a', 'valid-access-token'), /无效/)
    assert.equal(calls.length, 1)
    globalThis.fetch = async () => new Response('', { status: 403 })
    await assert.rejects(listRecentOperations('cluster-a', 'project-a', 'valid-access-token'),
      e => e instanceof ManagementHTTPError && e.status === 403)
  } finally { globalThis.fetch = old }
})
