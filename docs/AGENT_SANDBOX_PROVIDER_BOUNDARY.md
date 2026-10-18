# Agent Workspace / On-Demand Sandbox Provider Boundary

**Status:** experimental developer-only planning contract, not production VM
management; does not change Stage B Kueue, Cluster Agent, OIDC, RBAC, or the
existing workload lifecycle.

## Goal

An external persistent Agent Harness has long-lived logical user Workspaces
but should not allocate one always-on microVM per user. The compute platform
may supply a *replaceable execution provider* (Daytona, ephemeral container,
Firecracker in a later stage), not the Harness's conversation/goal/Temporal
ownership, notification policy, or approval store.

```text
Agent Harness / Temporal durable Goal
           |       |
           |       +-- ACP exact authorization / evidence (separate authority)
           v
Sandbox capability request [canonical user/project scope + approved operation]
           |
           v
Compute Platform sandbox provider SPI
  +-- WorkspaceBinding (persistent identity + storage reference)
  +-- provider observation / lease and generation guard
  +-- plan_transition: CREATE / PAUSE / RESUME / NOOP / OBSERVE_ONLY / CONFLICT
           |
           v
Optional execution: Daytona/E2B/container/Firecracker adapter (NOT implemented)
           |
           v
Provider observation + operation evidence returned to ACP/Harness
```

The current reference lives in `scripts/dev/agent_sandbox_contract.py`. It is
**pure planning**: no credential acquisition, no provider API calls, no VM
creation, no PVC mutation, and no claim of successful execution.

## Lifecycle invariants

1. `WorkspaceBinding` persists across runs: owner scope, workspace ID,
   storage reference, provider, generation, authority digest and runtime spec
   digest are bound together. The VM itself is ephemeral.
2. Verified `ABSENT + RUNNING` -> `CREATE` (requires *fresh external approval*);
   `ABSENT + PAUSED` -> `NOOP`. A sleeping user need not reserve compute.
3. Matching `RUNNING + PAUSED` -> `PAUSE`; matching `PAUSED + RUNNING` ->
   `RESUME`. A pause never implicitly deletes persistent storage.
4. `UNKNOWN` or unverified observation -> `OBSERVE_ONLY`: do not create,
   resume, delete, or adopt a potentially existing instance.
5. Any mismatch in owner, workspace, provider, storage, generation, authority
   or spec digest -> `CONFLICT`. Cross-tenant adoption is forbidden.
6. The planned action is **not a capability**: runtime operations require
   authenticated caller identity, project authorization, per-operation ACP
   authority checks, fenced leases, idempotency keys and verified observations.
7. Provider status and VM snapshot existence do not prove a remote side effect
   or a resumed browser login. Browser sessions may need separate recovery.
8. Sharing a storage volume does not imply sharing user secrets or browser
   Profiles. Per-Bot execution identity and profiles remain isolated.

## Developer verification

```bash
python3 -m unittest discover -s scripts/dev \
    -p 'test_agent_sandbox_contract.py' -v
```

Includes on-demand allocation, pause/resume, UNKNOWN and unverified observation
fail-closed, immutable identity drift, bad input shape and repeated planning.

## Important gaps before a real provider

- Real project entitlement enforcement, authenticated service identity,
  capability grants and independent operation proof.
- Durable task-to-sandbox execution lease with fencing and CAS; authority
  cannot be inferred from a `requires_authorization` flag.
- Provider lookup by deterministic physical ID, create-or-adopt and lost ACK
  reconciliation; the reference intentionally never performs CREATE itself.
- Browser Profile isolation, screenshot/live-control authorization, connection
  disruption handling after snapshot/restore.
- Workspace volume/PVC access checks, secret isolation, encryption, backup,
  capacity accounting and idle timeout quotas.
- Production acceptance: restart/replay, stale owner and crash matrix, cross
  tenant negative tests, and real provider E2E under independent review.

**Decision:** keep this optional SPI experiment outside Kueue core. Do not
replace the existing ComputeProvider abstraction or elevate this reference
to an active execution route without review.
