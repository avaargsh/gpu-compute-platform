export interface HTTPCommandInput {
  manifestValid: boolean
  context: string
  url: string
  host: string
}

const CONTEXT = /^[a-zA-Z0-9][a-zA-Z0-9_.:@/-]{0,127}$/
const URL_PATTERN = /^http:\/\/(?:127\.0\.0\.1|\[::1\]):([1-9][0-9]{0,4})(\/[A-Za-z0-9_./-]{0,255})$/
const HOST = /^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/

export function kserveHTTPAcceptanceCommand(input: HTTPCommandInput): string | null {
  if (!input.manifestValid || !CONTEXT.test(input.context)) return null
  const match = URL_PATTERN.exec(input.url)
  if (!match) return null
  const port = Number(match[1])
  if (port < 1 || port > 65535 || !Number.isSafeInteger(port) ||
      match[1] !== String(port)) return null
  const path = match[2]
  if (path.includes('//') || path.split('/').some(segment => segment === '..')) return null
  if (input.host && (
    input.host.length > 253 || !HOST.test(input.host) ||
    input.host.split('.').some(part => !part || part.length > 63 ||
      part.startsWith('-') || part.endsWith('-'))
  )) return null
  // The URL, context and optional HTTP Host are intentionally restricted to
  // shell-safe characters and numeric loopback, no arbitrary remote hosts,
  // URL credentials, queries, redirects, shell metacharacters or proxies.
  return 'python3 scripts/dev/kserve_http_acceptance.py serving.json' +
    ' --request request.json --expect expect.json --execute' +
    ' --context ' + input.context + ' --url ' + input.url +
    (input.host ? ' --host-header ' + input.host : '')
}
