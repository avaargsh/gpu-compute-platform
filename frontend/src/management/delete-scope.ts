import { desiredMatchesScope, type DesiredView, type Scope } from './api.ts'

// UI-side defense in depth: a Generation read from resource A cannot be used
// to send DELETE for resource B after the form's scope has been edited.
// The Management API still independently checks live OIDC RBAC + CAS; this
// helper does not authorize deletion or prove provider resource cleanup.
export function boundDeleteGeneration(desired: DesiredView | null, scope: Scope): number | null {
  if (!desired || !desiredMatchesScope(desired, scope) || desired.deleting ||
      !Number.isSafeInteger(desired.generation) || desired.generation < 1) {
    return null
  }
  return desired.generation
}
