// Client-side projection freshness only; server OIDC/RBAC remains authoritative.
// A changed login/scope must not repaint a new user's UI. Clock-only expiry is
// distinct: the current user's formerly authorized projections must be cleared.
export type ReadContextStatus = 'current' | 'expired' | 'changed'

export function readContextStatus<S extends { expiresAt: number }>(
  startedSession: S | null,
  currentSession: S | null,
  startedAnchor: string,
  currentAnchor: string,
  now = Date.now(),
): ReadContextStatus {
  if (!startedSession || startedSession !== currentSession || startedAnchor !== currentAnchor) return 'changed'
  if (!Number.isFinite(startedSession.expiresAt) || startedSession.expiresAt <= now) return 'expired'
  return 'current'
}

export function isSameReadContext<S extends { expiresAt: number }>(
  startedSession: S | null,
  currentSession: S | null,
  startedAnchor: string,
  currentAnchor: string,
  now = Date.now(),
): boolean {
  return readContextStatus(startedSession, currentSession, startedAnchor, currentAnchor, now) === 'current'
}
