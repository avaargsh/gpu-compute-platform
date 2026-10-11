import assert from 'node:assert/strict'
import test from 'node:test'
import { readResource, writeResource, deleteResource, readReceipt, ManagementHTTPError } from '../src/management/api.ts'

const scope = { cluster: 'gpu-cluster', project: 'research', kind: 'ComputePool', resource: 'pool-a' }
const token = 'valid-test-oidc-access-token'
const key = '0123456789abcdef0123456789abcdef'
const resourcePath = '/api/v1/management/clusters/gpu-cluster/projects/research/resources/ComputePool/pool-a'
const receiptPath = '/api/v1/management/clusters/gpu-cluster/projects/research/operations/' + key

test('browser CRUD sends only scoped same-origin routes, bearer, CAS and idempotency key', async () => {
  const original = globalThis.fetch
  const calls = []
  try {
    globalThis.fetch = async (url, opts) => {
      calls.push({ url, opts })
      if (opts.method === 'GET' && String(url).includes('operations')) {
        return Response.json({ requestId: key, status: 'desired-committed', resultingGeneration: 1 })
      }
      if (opts.method === 'GET') return Response.json({ clusterId: scope.cluster, generation: 1, spec: {} })
      return new Response(null, { status: opts.method === 'PUT' ? 204 : 202 })
    }
    const read = await readResource(scope, token)
    assert.equal(read.value.generation, 1)
    await writeResource(scope, token, '{"expectedGeneration":0,"spec":{}}', key)
    await deleteResource(scope, token, 1, key)
    const receipt = await readReceipt(scope.cluster, scope.project, key, token)
    assert.equal(receipt.value.status, 'desired-committed')
    assert.equal(calls.length, 4)
    assert.deepEqual(calls.map(x => x.url), [resourcePath, resourcePath, resourcePath, receiptPath])
    assert.deepEqual(calls.map(x => x.opts.method), ['GET', 'PUT', 'DELETE', 'GET'])
    for (const call of calls) {
      assert.equal(call.opts.credentials, 'omit')
      assert.equal(call.opts.redirect, 'error')
      assert.equal(call.opts.cache, 'no-store')
      assert.equal(call.opts.headers.Authorization, 'Bearer ' + token)
      assert.equal(String(call.url).includes(token), false)
      assert.equal(String(call.url).includes('/agent/'), false)
      assert.equal(String(call.url).includes('/internal/'), false)
      assert.ok(String(call.url).startsWith('/api/v1/management/'))
    }
    assert.equal(calls[1].opts.headers['Idempotency-Key'], key)
    assert.equal(calls[1].opts.headers['Content-Type'], 'application/json')
    assert.equal(calls[2].opts.headers['Idempotency-Key'], key)
    assert.equal(calls[2].opts.headers['If-Match'], '"1"')
  } finally { globalThis.fetch = original }
})

test('unsafe resource IDs, invalid Generation and invalid receipt key never reach the network', async () => {
  const original = globalThis.fetch
  let count = 0
  try {
    globalThis.fetch = async () => { count++; return Response.json({}) }
    await assert.rejects(readResource({ ...scope, project: '../foreign' }, token), /无效/)
    await assert.rejects(readResource({ ...scope, resource: 'pool/a' }, token), /无效/)
    await assert.rejects(writeResource(scope, token, '{}', 'short'), /Idempotency-Key/)
    await assert.rejects(deleteResource(scope, token, 0, key), /Generation/)
    await assert.rejects(readReceipt(scope.cluster, scope.project, 'wrong', token), /原始/)
    assert.equal(count, 0)
  } finally { globalThis.fetch = original }
})

test('negative responses fail closed and preserve exact HTTP class for operator handling', async () => {
  const original = globalThis.fetch
  try {
    for (const status of [401, 403, 404, 409, 428, 503]) {
      globalThis.fetch = async () => new Response('', { status })
      await assert.rejects(
        () => writeResource(scope, token, '{"expectedGeneration":0,"spec":{}}', key),
        e => e instanceof ManagementHTTPError && e.status === status,
      )
    }
  } finally { globalThis.fetch = original }
})
