import assert from 'node:assert/strict'
import test from 'node:test'
import { kserveHTTPAcceptanceCommand } from '../src/portal/kserve-http-command.ts'

const base = {
  manifestValid: true, context: 'kind-gpu',
  url: 'http://127.0.0.1:18080/v1/chat/completions', host: '',
}
test('emits explicit read-only KServe HTTP acceptance from allowed loopback URL', () => {
  assert.equal(kserveHTTPAcceptanceCommand(base),
    'python3 scripts/dev/kserve_http_acceptance.py serving.json' +
    ' --request request.json --expect expect.json --execute' +
    ' --context kind-gpu --url http://127.0.0.1:18080/v1/chat/completions')
  const withHost = kserveHTTPAcceptanceCommand({ ...base, host: 'model-a.research.example.local' })
  assert.ok(withHost?.endsWith('--host-header model-a.research.example.local'))
})
test('no command before manifest valid or local kubectl context entered', () => {
  assert.equal(kserveHTTPAcceptanceCommand({ ...base, manifestValid: false }), null)
  assert.equal(kserveHTTPAcceptanceCommand({ ...base, context: '' }), null)
  assert.equal(kserveHTTPAcceptanceCommand({ ...base, context: '--delete' }), null)
})
test('shell metacharacters, cross-origin, URLs with ambiguity are never commands', () => {
  for (const url of [
    'http://localhost:18080/v1/models', 'https://127.0.0.1:18080/v1/models',
    'http://10.0.0.1:18080/v1/models', 'http://127.0.0.1:18080/v1/models;rm -rf /',
    'http://127.0.0.1:18080/v1/models?redirect=https://other.example',
    'http://127.0.0.1:18080/../admin', 'http://127.0.0.1:18080//api',
    'http://127.0.0.1:08080/v1/models', 'http://127.0.0.1:70000/v1/models',
    'http://127.0.0.1:18080@evil.example/v1/models',
  ]) {
    assert.equal(kserveHTTPAcceptanceCommand({ ...base, url }), null, url)
  }
  for (const host of ['a;evil', 'evil.example\nAuthorization:token', 'A.EXAMPLE', '-unsafe.example']) {
    assert.equal(kserveHTTPAcceptanceCommand({ ...base, host }), null, host)
  }
})
