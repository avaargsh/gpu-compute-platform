# Local development Portal workload submission

The training/batch-inference composer can now optionally submit v0.1 Workload
Desired directly to the existing Go API without requiring copy/paste to a CLI.
This is a **development-only** product slice, not a production management API.

For an explicitly local development Control Plane (loopback only) configure:

```bash
export CONTROL_PLANE_PROFILE=development
export PORTAL_OPERATOR_TOKEN='<strong-independent-read-token>'
export PORTAL_OPERATOR_WRITE_TOKEN='<different-strong-write-token>'
export PORTAL_OPERATOR_CLUSTER_ID=cluster-a
export PORTAL_OPERATOR_PROJECT_IDS=research
```

Start the existing Go Control Plane with the above profile. In the Portal,
authorize reading with the read token; choose an actual project ComputePool,
image, accelerator and optional existing PVC; in the developer submit section
enter the separate write token, then create Workload Desired.

The backend checks **both tokens**, project scope, ProjectBinding, PoolBinding,
the ComputePool Desired `projectID` identity and cluster, then passes the
canonical typed request to the **existing** atomic Go Workload creation path.
This does not register a new provider, expose public APIs, or bypass Kueue.

Security boundary:
- The read-only operator token alone can never write.
- `PORTAL_OPERATOR_WRITE_TOKEN` is not stored by the browser, in localStorage,
  in cookies, or in the URL; it remains in page memory.
- Server forbids untrusted browser Origin and non-JSON content type.
- Missing / misconfigured write token means no new operator write route.
- Production and PoC bootstrap profiles cannot enable this dev mutation.
- Go startup already rejects public/untrusted control-plane bind addresses.
- This is **not** multi-tenant OIDC/RBAC, production audit, a credential vault,
  GPU reservation proof, execution success proof or persisted checkpoint proof.

### Submission outcome and safe recovery

- Only the canonical HTTP **204** response confirms that the local Go API accepted
  the Desired write. The UI labels this **Desired accepted**, never Job completed,
  GPU assigned, Kueue admitted or checkpoint persisted.
- A timeout, network disconnect, redirect rejection, server **5xx**, HTTP 408,
  or unexpected 2xx is an **unknown outcome**: the request may already have
  committed. The UI does **not** automatically retry, and disables submitting
  the same draft again until the operator deliberately changes the intent.
- After a 204 or unknown outcome, the form deliberately keeps the result
  visible instead of automatically refreshing (which would unmount it). The
  operator can explicitly click **重新读取授权快照** to read back the same
  Project / Workload ID / Generation and inspect independent Agent Observation.
  A paginated or stale snapshot that does not show the resource is **not proof
  of rollback**.
- Explicit 401/403/404/409 denials remain separate actionable errors. Do not
  elevate a local read token into a write credential or persist either token.
- The exact request status is not an operation receipt or durable idempotency
  guarantee. The production replacement must use Management OIDC + project
  grants + generation CAS + operation ID and audited receipt.

Next milestone: migrate to the canonical audited Management API with OIDC,
project grants and CAS; continue using the existing Kueue executor and
independent Agent Observations. Avoid persisting dev write tokens.

Run: `cd frontend && npm ci && npm run portal:test && npm run portal:build`.
Negative HTTP/transport cases: `frontend/tests/dev-submit-outcome.test.mts`.
