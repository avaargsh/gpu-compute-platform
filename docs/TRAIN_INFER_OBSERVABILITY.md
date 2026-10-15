# Training and batch-inference execution monitor (Portal)

User flow: **Portal → 训推任务 → 已授权快照 → 执行监控 → 任务详情 / 条件 / 证据 / 只读日志指令**.

This view consumes ONLY the existing operator-scoped, read-only Go
snapshot. No new unauthenticated API, agent RPC, log proxy or simulation
fixture is required. The existing Portal refresh still controls staleness.
The list represents only rows fetched on the current page; with pagination
truncation it cannot claim coverage of every task.

The status decision is intentionally conservative:
- absent, stale, mismatched or conflicting Observation is *unknown/error*, never Running.
- `Synced` by itself proves **only** observed-vs-desired generation convergence;
  cannot prove Kueue quota or admission, Pod readiness, Job success or GPU usage.
- `QuotaReserved`, `Admitted`, `PodsReady`, `Succeeded`, `Failed`
  Conditions are evaluated individually, and contradictory terminal
  conditions fail closed.
- `spec.workspace.claimName` is a desired reference only, not evidence
  that a PVC was mounted or a checkpoint was retained.
- Evidence refs are displayed as **text**, not clickable external URLs.

For logs, operator types an explicit **local kubeconfig context**. A
copy-only `kubectl logs job/...` command is offered ONLY if a single
Kubernetes Job evidence ref has a valid name, namespace and current scoped
cluster identity, and the observation generation is coherent.
Browser never executes kubectl or fetches raw logs. The command is a
convenience, not a proof of authorization; Kubernetes RBAC decides access.
Do not paste sensitive logs into the portal.

Negative tests in `frontend/tests/execution-model.test.mts` cover stale /
tampered Observation, wait states, contradictions, missing evidence, shell
injection, cross-cluster Job refs and unproven PVC durability.

```bash
cd frontend
npm ci
npm run portal:test
npm run portal:build
```

Next slice is a properly authenticated tenant-scoped logs streaming/read
API with bounded logs and server-side per-job authorization, followed by
Workspace checkpoint evidence collection. Do not proxy raw Kubernetes
credentials to browser or silently inherit the old operator preview token
for write access.
