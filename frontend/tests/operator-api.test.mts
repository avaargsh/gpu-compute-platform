import assert from 'node:assert/strict'
import test from 'node:test'
import { loadSnapshot, isSafeSegment } from '../src/portal/api.ts'

const clusterID = 'cluster-a'
const token = 'test-operator-token-with-more-than-thirty-two-characters'
const page = {
  agent: { clusterId: clusterID, agentVersion: 'dev', kubernetesVersion: 'v1.34', capabilities: { kueue: true, serving: false, draApiAvailable: false } },
  rows: [
    { desired: { kind: 'Workload', id: 'work-a', generation: 3, spec: { projectID: 'research' } }, state: null, error: null },
  ],
  total: 2,
  truncated: true,
  nextCursor: 'signed-server-cursor',
  refreshedAt: '2026-10-11T00:00:00Z',
}

test('operator reads only authenticated read-only snapshot with memory credential', async () => {
  const original = globalThis.fetch
  const requests = []
  try {
    globalThis.fetch = async (url, opts) => {
      requests.push({ url, opts })
      return Response.json(page)
    }
    const result = await loadSnapshot(clusterID, token)
    assert.equal(result.agent.clusterId, clusterID)
    assert.equal(result.nextCursor, 'signed-server-cursor')
    assert.equal(result.rows[0].desired.id, 'work-a')
    assert.equal(requests.length, 1)
    const { url, opts } = requests[0]
    assert.equal(url, '/api/v1/operator/clusters/cluster-a/snapshot?limit=120')
    assert.equal(opts.method, 'GET')
    assert.equal(opts.credentials, 'omit')
    assert.equal(opts.redirect, 'error')
    assert.equal(opts.cache, 'no-store')
    assert.equal(opts.headers.Authorization, 'Bearer ' + token)
    assert.equal(typeof opts.signal, 'object')
    assert.equal(String(url).includes(token), false)
    assert.equal(String(url).includes('/agent/'), false)
    assert.equal(String(url).includes('/internal/'), false)
  } finally {
    globalThis.fetch = original
  }
})

test('pagination uses opaque signed cursor, not resource IDs or client project claims', async () => {
  const original = globalThis.fetch
  try {
    let url = ''
    globalThis.fetch = async path => {
      url = String(path)
      return Response.json({ ...page, rows: [], truncated: false, nextCursor: '' })
    }
    await loadSnapshot(clusterID, token, 'opaque+token=/signature')
    const parsed = new URL(url, 'http://localhost')
    assert.equal(parsed.searchParams.get('cursor'), 'opaque+token=/signature')
    assert.equal(parsed.searchParams.get('limit'), '120')
    assert.equal(parsed.searchParams.has('projectID'), false)
    assert.equal(parsed.searchParams.has('token'), false)
  } finally {
    globalThis.fetch = original
  }
})

test('no unauthorized request for missing token or unsafe cluster path', async () => {
  const original = globalThis.fetch
  let calls = 0
  try {
    globalThis.fetch = async () => { calls++; return Response.json(page) }
    await assert.rejects(loadSnapshot('cluster-a', 'short'), /32/)
    await assert.rejects(loadSnapshot('../cluster-foreign', token), /Cluster ID/)
    assert.equal(calls, 0)
    assert.equal(isSafeSegment('cluster-a'), true)
    assert.equal(isSafeSegment('bad/name'), false)
  } finally {
    globalThis.fetch = original
  }
})

test('401/404/500 and malformed snapshots are rejected, never treated as empty successful inventory', async () => {
  const original = globalThis.fetch
  try {
    for (const status of [401, 404, 500]) {
      globalThis.fetch = async () => new Response('', { status })
      await assert.rejects(loadSnapshot(clusterID, token), status === 500 ? /HTTP 500/ : /凭证|不能访问/)
    }
    globalThis.fetch = async () => Response.json({ agent: page.agent, rows: 'not-an-array', total: 1 })
    await assert.rejects(loadSnapshot(clusterID, token), /无效数据/)
  } finally {
    globalThis.fetch = original
  }
})
