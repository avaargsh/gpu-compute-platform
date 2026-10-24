import type { EvidenceCheck } from './evidence-check'

// A user-initiated diagnostic watch, not an automated controller or signed
// Kubernetes outcome. 12 immediate/interval reads cover <3 minutes.
export const EVIDENCE_WATCH_INTERVAL_MS = 15_000
export const EVIDENCE_WATCH_MAX_READS = 12

export type EvidenceWatchDecision = 'continue' | 'stop-terminal' | 'stop-scope' | 'stop-budget'

export function evidenceWatchDecision(check: EvidenceCheck, completedReads: number): EvidenceWatchDecision {
  if (!check || check.readback.status === 'inconsistent' ||
      check.readback.status === 'desired-advanced') return 'stop-scope'
  if (check.execution && ['SUCCEEDED', 'FAILED', 'IDENTITY_CONFLICT', 'OBSERVATION_CONFLICT'].includes(check.execution.code)) {
    return 'stop-terminal'
  }
  if (!Number.isSafeInteger(completedReads) || completedReads < 1 ||
      completedReads >= EVIDENCE_WATCH_MAX_READS) return 'stop-budget'
  return 'continue'
}
