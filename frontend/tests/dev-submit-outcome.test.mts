import assert from 'node:assert/strict'
import test from 'node:test'
import { submitDevWorkload, WorkloadSubmissionUncertainError } from '../src/portal/dev-submit.ts'
import type { WorkloadDraftPayload } from '../src/portal/workload-draft.ts'

const reader = 'reader-test-strong-token-more-than-32-chars'
const writer = 'writer-test-distinct-token-more-than-32-chars'
const payload: WorkloadDraftPayload = {
  metadata: { id: 'train-a', generation: 1 }, projectId: 'research', poolId: 'pool-a',
  spec: { image: 'example.io/train@sha256:abc', command: ['python', 'train.py'],
    accelerator: { class: 'h100', quota: 1 } },
}

test('exact HTTP 204 is the only confirmed Desired acceptance', async () => {
  const original = globalThis.fetch
  let calls = 0
  globalThis.fetch = async () => {
    calls++
    return { status: 204 } as Response
  }
  try {
    await submitDevWorkload('cluster-a', reader, writer, payload)
    assert.equal(calls, 1)
  } finally {
    globalThis.fetch = original
  }
})

test('network failures, timeouts, 5xx and non-contract success do not imply rollback', async () => {
  const original = globalThis.fetch
  try {
    for (const status of [408, 500, 502, 503, 504, 200, 201, 202]) {
      let calls = 0
      globalThis.fetch = async () => {
        calls++
        return { status } as Response
      }
      await assert.rejects(submitDevWorkload('cluster-a', reader, writer, payload),
        (err: unknown) => {
          assert.ok(err instanceof WorkloadSubmissionUncertainError, 'HTTP ' + status)
          assert.match(err.message, /结果未知/)
          assert.doesNotMatch(err.message, new RegExp(reader + '|' + writer))
          return true
        })
      assert.equal(calls, 1, 'no automatic replay for HTTP ' + status)
    }
    for (const rejection of [new TypeError('failed to fetch'), new DOMException('timeout', 'TimeoutError')]) {
      let calls = 0
      globalThis.fetch = async () => {
        calls++
        throw rejection
      }
      await assert.rejects(submitDevWorkload('cluster-a', reader, writer, payload),
        WorkloadSubmissionUncertainError)
      assert.equal(calls, 1, 'no retry on transport uncertainty')
    }
  } finally {
    globalThis.fetch = original
  }
})

test('explicit 4xx denial is not misreported as confirmed success or uncertain commit', async () => {
  const original = globalThis.fetch
  try {
    for (const status of [400, 401, 403, 404, 409, 422, 429]) {
      globalThis.fetch = async () => ({ status }) as Response
      await assert.rejects(submitDevWorkload('cluster-a', reader, writer, payload),
        (err: unknown) => {
          assert.ok(err instanceof Error)
          assert.equal(err instanceof WorkloadSubmissionUncertainError, false)
          return true
        })
    }
  } finally {
    globalThis.fetch = original
  }
})

test('invalid scope or credentials fail before network, not as unknown outcome', async () => {
  const original = globalThis.fetch
  let calls = 0
  globalThis.fetch = async () => {
    calls++
    return { status: 204 } as Response
  }
  try {
    for (const [cluster, readToken, writeToken] of [
      ['../cluster-b', reader, writer],
      ['cluster-a', reader, reader],
      ['cluster-a', reader, 'short'],
    ]) {
      await assert.rejects(submitDevWorkload(cluster, readToken, writeToken, payload),
        (err: unknown) => {
          assert.ok(err instanceof Error)
          assert.equal(err instanceof WorkloadSubmissionUncertainError, false)
          return true
        })
    }
    assert.equal(calls, 0)
  } finally {
    globalThis.fetch = original
  }
})
