# Training and inference: first executable product slice

Status: Developer preview / v0.1 Workload intent composer. This slice is NOT an online serving deployment.

## Product path

Portal → verified operator-scoped **read-only** ComputePool snapshot → choose Project/Pool + accelerator class → configure training or batch inference → validate → export v0.1 Workload JSON.

The output has the exact existing `domain.Workload` fields: `metadata.id`, `metadata.generation=1`, `projectId`, `poolId`, `spec.image`, `spec.command` argv, `spec.accelerator.{class,quota}`. It does not introduce a parallel scheduler or change Kueue provider behavior.

Only project IDs derived from actual scoped pool rows are emitted; the browser cannot substitute its own project membership. Quota validation checks *declared* pool intent, not live GPU allocation, Kueue admission, throughput or SLA. No remote API mutation occurs, and the operator preview credential never becomes a write credential.

## Next vertical slices (parallel development backlog)

1. **Authenticated submission**: connect this form to one canonical Management API, OIDC access token, Project RBAC, idempotency key, generation CAS, audit and workload receipt. Keep Agent-only routes separated. Do not repurpose legacy `/api/v1/internal` writes.
2. **Training artifact**: Project Workspace + PVC / S3 / checkpoint pointers, Job logs and terminal output, evidence digest and failure/retry policy. Persist data independently of Job/Pod lifespan.
3. **Online inference**: separate Serving object + model URI/digest + image/runtime + stable endpoint + readiness/latency evidence + autoscale/rollout (choose existing serving stack based on POC); never claim a Kueue batch Job exposes a long-lived HTTP API.
4. **Templates + image provenance**: use OCI digest, cosign policy, ResourceClass/accelerator profiles, and named Workspace templates. Image install/credentials stay server-side.
5. **Evaluation**: tiny real Cluster Golden Path covering Project → Pool → Train Job / Batch Infer Job → Kueue admission → independent observation → cleanup; then true GPU hardware acceptance.

## Local smoke

```bash
cd frontend
npm ci
npm run portal:test
npm run portal:build
```

The source is `frontend/src/portal/workload-draft.ts` with tests in `frontend/tests/workload-draft.test.mts`. Browser UI is `frontend/src/portal/JobComposer.vue`; it is intentionally read-only until the separate authenticated write slice exists.
