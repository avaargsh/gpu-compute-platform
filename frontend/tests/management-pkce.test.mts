import assert from 'node:assert/strict'
import test from 'node:test'
import { beginLogin, finishLogin, challenge, discover, secureRandom } from '../src/management/auth.ts'

const config = { issuer: 'https://idp.example.test/realms/gpu', clientId: 'gpu-management-public' }
const metadata = {
  issuer: config.issuer,
  authorization_endpoint: config.issuer + '/protocol/openid-connect/auth',
  token_endpoint: config.issuer + '/protocol/openid-connect/token',
}
const redirect = 'http://127.0.0.1:3002/management.html'

function memoryStorage() {
  const values = new Map()
  return {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value) },
    removeItem: key => { values.delete(key) },
    clear: () => values.clear(),
  }
}

test('PKCE S256 challenge is deterministic and never equals verifier', async () => {
  const verifier = secureRandom()
  assert.match(verifier, /^[0-9a-f]{64}$/)
  const result = await challenge(verifier)
  assert.match(result, /^[A-Za-z0-9_-]{43}$/)
  assert.notEqual(result, verifier)
  assert.equal(await challenge(verifier), result)
  await assert.rejects(challenge('too-short'), /verifier/)
})

test('OAuth authorization URL binds state, nonce, S256 challenge and exact redirect, without tokens', async () => {
  const storage = memoryStorage()
  const url = new URL(await beginLogin(config, metadata, redirect, storage, 1700000000000))
  assert.equal(url.origin, 'https://idp.example.test')
  assert.equal(url.searchParams.get('response_type'), 'code')
  assert.equal(url.searchParams.get('client_id'), config.clientId)
  assert.equal(url.searchParams.get('redirect_uri'), redirect)
  assert.equal(url.searchParams.get('code_challenge_method'), 'S256')
  assert.match(url.searchParams.get('code_challenge'), /^[A-Za-z0-9_-]{43}$/)
  assert.match(url.searchParams.get('state'), /^[0-9a-f]{64}$/)
  assert.match(url.searchParams.get('nonce'), /^[0-9a-f]{64}$/)
  assert.equal(url.toString().includes('access_token'), false)
  assert.equal(storage.getItem('gpu-management-pkce-v1').includes('accessToken'), false)
})

test('OAuth callback is one-use: consumes state/verifier, rejects replay, invalid state and expired callback', async () => {
  const initialFetch = globalThis.fetch
  const storage = memoryStorage()
  try {
    let requests = []
    globalThis.fetch = async (path, opts) => {
      requests.push({ path, opts })
      return Response.json({ access_token: 'local-test-bearer-token', token_type: 'Bearer', expires_in: 300 })
    }
    const loginURL = new URL(await beginLogin(config, metadata, redirect, storage, 1700000000000))
    const state = loginURL.searchParams.get('state')
    const callback = '?code=single-use-authorization-code&state=' + state
    const result = await finishLogin(config, metadata, callback, redirect, storage, 1700000001000)
    assert.equal(result.accessToken, 'local-test-bearer-token')
    assert.equal(result.expiresAt, 1700000301000)
    assert.equal(requests.length, 1)
    assert.equal(requests[0].path, metadata.token_endpoint)
    assert.equal(requests[0].opts.method, 'POST')
    assert.equal(requests[0].opts.credentials, 'omit')
    assert.equal(requests[0].opts.redirect, 'error')
    assert.equal(requests[0].opts.body.get('grant_type'), 'authorization_code')
    assert.equal(requests[0].opts.body.get('redirect_uri'), redirect)
    assert.equal(requests[0].opts.body.get('client_id'), config.clientId)
    assert.match(requests[0].opts.body.get('code_verifier'), /^[0-9a-f]{64}$/)
    assert.equal(storage.getItem('gpu-management-pkce-v1'), null)
    await assert.rejects(finishLogin(config, metadata, callback, redirect, storage, 1700000001000), /会话已过期/)
    assert.equal(requests.length, 1)
    const otherLogin = new URL(await beginLogin(config, metadata, redirect, storage, 1700000000000))
    await assert.rejects(finishLogin(config, metadata, '?code=forged&state=attacker', redirect, storage, 1700000001000), /state/)
    assert.equal(storage.getItem('gpu-management-pkce-v1'), null)
    const last = await beginLogin(config, metadata, redirect, storage, 1700000000000)
    await assert.rejects(finishLogin(config, metadata, '?code=x&state=' + new URL(last).searchParams.get('state'), redirect, storage, 1700000400000), /时间/)
    assert.equal(requests.length, 1)
    assert.ok(otherLogin.searchParams.get('state'))
  } finally { globalThis.fetch = initialFetch }
})

test('OIDC discovery fails closed for issuer mismatch or endpoint redirect/host injection', async () => {
  const initialFetch = globalThis.fetch
  try {
    for (const result of [
      { ...metadata, issuer: 'https://attacker.example' },
      { ...metadata, token_endpoint: 'https://attacker.example/steal' },
      { ...metadata, authorization_endpoint: 'http://idp.example.test/login' },
      { ...metadata, token_endpoint: config.issuer + '-evil/token' },
    ]) {
      globalThis.fetch = async () => Response.json(result)
      await assert.rejects(discover(config), /不可信/)
    }
    globalThis.fetch = async (url, opts) => {
      assert.equal(url, config.issuer + '/.well-known/openid-configuration')
      assert.equal(opts.redirect, 'error')
      return Response.json(metadata)
    }
    assert.deepEqual(await discover(config), metadata)
  } finally { globalThis.fetch = initialFetch }
})
