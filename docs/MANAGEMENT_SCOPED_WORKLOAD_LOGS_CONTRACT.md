# Authenticated Workload Log Read — Contract Freeze (P0 design, NOT implemented)

Status: proposed, review required. Tracks #116 and security stop-ship #79.

## Product value and existing boundaries

An OIDC-authenticated Project operator can inspect a bounded excerpt of a training/batch-inference Workload's standard output without obtaining kubeconfig, an Agent token or Kubernetes admin privileges. Reuse the existing isolated Go Management ingress, live issuer/subject Project grants, and Kueue Job/Pod ownership model. This is **not** an arbitrary Kubernetes log proxy or a general cluster terminal. No public ingress, no automatic remediation, no second scheduler, no synthetic log data.

Current reality: `frontend/src/portal/execution-model.ts` offers only a **copy-only local kubectl command**; `internal/platform/management/execution.go` projects last-reported conditions via a DBA-owned fixed SQL function, not live pod/log content. `internal/agent/protocol.go` has no log-reading capability. No authorized log endpoint is implemented, and a new Management HTTP route alone would not supply secure Agent transport. Do not claim this design as an available product feature.

## Proposed HTTP contract (future, NOT routed)

    GET /api/v1/management/clusters/{clusterID}/projects/{projectID}/execution/workloads/{workloadID}/logs?tailLines=N

- Exact single `tailLines` query key, positive integer, default 100, maximum 200. Reject duplicates/unknown keys, empty paths and invalid IDs with 400. Do not accept namespace, pod, container, label selector, arbitrary URL or kubeconfig from browser.
- Request `Authorization: Bearer <OIDC Access Token>` validated on every request, including expiry, revocation and current Project role. For pilot, only current `operator`/`admin` may read raw logs; Viewer is forbidden until a separately reviewed visibility policy exists. Do not reuse Portal preview tokens or permit Agent credentials as browser identity.
- Return bounded JSON: `workloadId`, `clusterId`, `projectId`, `generation`, `observedAt`, `logReadAt`, `source` (Job UID, namespace, Pod UID, container identity from trusted Agent evidence, not caller input), `truncated`, `nextCursor: null`, `lines` (at most 200). Total serialized content must be capped (proposed <= 16 KiB including metadata); reject impossible/conflicting responses instead of silently implying completeness. Both timestamps are labeled, never presented as the same freshness fact.
- All responses: `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, restrictive CORS, no client or proxy caching. No log text, token, kubeconfig, provider credentials or raw upstream errors in application audit logs. Log reads are audited with request correlation, subject, project, resource, generation, byte/line count and outcome **without** retaining log contents.
- 401 for missing/invalid identity; 403 or privacy-preserving 404 for no current authorized Project/grant; 404 for no authorized Workload/log stream; 409 for Desired/Job UID/generation mismatch; 503 for unavailable Agent, untrusted log source, revocation/authorization dependency failure or missing secure transport. No fallbacks to legacy unauthenticated routes. A missing log is UNKNOWN, not an empty successful job.

## Mandatory trust chain and non-bypass rules

1. Management re-verifies current OIDC issuer/subject and Project role. Read current Project→Cluster binding and Workload `spec.projectID` in the **same DB snapshot** as resource/generation lookup, using fixed SQL with least-privilege grants. Existing Management LOGIN must never receive Kubernetes, Agent-admin or unrestricted observation-table rights.
2. Resolve one eligible Job from server/Agent-owned current reconciliation evidence: `(cluster, project, Workload ID, Desired Generation, Job UID, namespace, Pod UID, container)`. Do not accept a client-selected Pod/namespace. Require a coherent current generation and trusted Job-to-Workload ownership chain; a last-reported condition or human-supplied evidence ref is insufficient.
3. Fetch through a **separately authorized, cluster-bound Agent read capability** with mTLS and narrow workload identity. Agent independently checks cluster, generation, provider-owned Job UID and allowed namespace, applies bytes/lines and timeouts before reading the Kubernetes logs subresource, and rejects cross-tenant/cross-cluster aliases. Management must not be given a universal Kubernetes credential or connect to Kubernetes directly through the user's browser.
4. Cap output at Agent, transport and Management response boundaries; enforce cancellation, size limits, safe UTF-8 handling and bounded concurrency. A non-atomic read may contain stale logs. Never treat `Succeeded`, `Synced`, or a successful log GET as proof of training correctness, durable checkpoint or complete resource deletion.
5. Raw application logs are **potentially sensitive and untrusted text**: escape before displaying, never execute them, never feed them to an Agent as higher-priority instructions, and document limitations of regex-based redaction. If required redaction or retention policy cannot be enforced, return an unavailable/denied result rather than claim data is safe.

## Slice order (Adopt → Integrate → Contribute → Build)

1. **Freeze source identity.** Define a typed, read-only `WorkloadLogSource` backed by Kueue provider ownership (UID + generation + namespace + allowed container), with tests rejecting collisions, prior generations, cross-Project references and stale or missing evidence. Reuse client-go Kubernetes Pod log API; do not invent a scheduler or log service.
2. **Agent read adapter.** Add a bounded, mTLS cluster-scoped read primitive; fake-Kubernetes tests must inject huge output, truncated lines, Job/Pod replacement, timeout, network error and malicious pod identity. Separate its capability from existing mutating Agent control routes.
3. **Management guarded projection.** Authorize with fresh OIDC and SQL Project grant, then call only the narrow Agent capability. Integration tests with real PostgreSQL role and live revocation must cover Developer A, Viewer A, Developer B, revoked token, stale Project→Cluster rebind and Agent replay.
4. **Portal user journey.** On a selected Workload, explicit '读取最近日志' action, never background poll by default. Show authoritative source, read timestamps, truncation notice, per-read budget, and missing/untrusted source as unavailable. Do not render a kubectl command as equivalent production evidence.
5. **Exact-head acceptance.** Real Chromium + Keycloak + least-privilege PostgreSQL + authenticated Agent + kind/Kueue Job/Pod log stream on the SAME SHA, with denial matrix, rotated UID, retry/timeout, log-size and log-secret tests. No production promotion until #79 Agent identity/ingress and #38 required-check rules are independently accepted.

## Explicit non-goals and stop-ship

No central log aggregation, tail-follow/WebSocket, cross-job search, S3 archival, shell terminal, cost metering, workload command injection, secrets export or public read endpoint. This design cannot be called shipped until a real authenticated Agent read route, source ownership proof, bounded log projection, and integrated negative controls are implemented and reviewed. Any test fixture must remain labeled simulated/loopback; actual GPU training and checkpoint durability are independent proofs.
