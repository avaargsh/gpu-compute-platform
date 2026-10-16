// Client-side presentation fence for asynchronous OIDC-protected reads.
// This is NOT an authorization decision; Management Go API still validates
// bearer identity and Project RBAC for every request. Object identity ensures
// a new login by the same user cannot reuse the old read result.
export function isSameReadContext<S extends { expiresAt: number }>(
  startedSession: S | null,
  currentSession: S | null,
  startedAnchor: string,
  currentAnchor: string,
  now = Date.now(),
): boolean {
  return startedSession !== null &&
    startedSession === currentSession &&
    Number.isFinite(startedSession.expiresAt) &&
    startedSession.expiresAt > now &&
    startedAnchor === currentAnchor
}
