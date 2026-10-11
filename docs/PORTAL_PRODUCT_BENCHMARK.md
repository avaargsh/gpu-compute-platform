# GPU Compute Portal — product parity and UI decision record

Status: product direction and staged acceptance mapping, **not a production
feature checklist**. Reviewed 2026-10-11.

## Product boundary

A private-cloud AI compute **resource management portal**, not a replacement
for Rancher, Run:ai scheduler, Kueue, OpenShift AI, KServe or llm-d.

The interface models the user journey as:

```
Cluster → Project → ComputePool → Workload → Independent Observation / Evidence
                                 \→ Future Serving via upstream KServe / llm-d
```

The current Go control plane is authoritative for desired/observed resources.
The old Python UI API, legacy task/cost widgets and stored browser token do not
make the Go server authenticated or production-ready.

## Reference matrix

| Mature platform | Feature / UX to adopt | Platform boundary | Current status |
| --- | --- | --- | --- |
| SUSE Rancher Prime / Rancher Manager | fleet -> cluster -> project contextual navigation; detailed resource tables; scoped user access | do not implement another general Kubernetes Manager | single-cluster local preview, not authenticated |
| NVIDIA Run:ai | project-scoped resource views, quota vs allocation, workload lifecycle and exception triage | Kueue remains the production admission path; no competing GPU scheduler | **declared** pool quota and exception view only, no actual GPU utilization |
| Red Hat OpenShift AI | project workspace entry, progression from workloads to model Serving | do not clone training/pipeline/controller ecosystem | project-to-workload drilldown only |
| SUSE Rancher AI Assistant (Liz) | contextual human help and workflow guidance | do not ship an ungrounded AI chat widget | deferred until reliable read-only tools + evidence |
| llm-d + KServe | native InferencePool / serving deployment, EPP/router + P/D | upstream owns routing, model Serving & KV | separate Draft PR #61, no serving readiness claim |

Official sources (design reference, not source for implementation copies):

- https://ranchermanager.docs.rancher.com/getting-started/overview
- https://documentation.suse.com/en-us/cloudnative/rancher-ai/latest/en/quick-start.html
- https://run-ai-docs.nvidia.com/saas/platform-management/aiinitiatives/organization/projects
- https://run-ai-docs.nvidia.com/self-hosted/workloads-in-nvidia-run-ai/workloads
- https://docs.redhat.com/en/documentation/red_hat_openshift_ai_self-managed/3.5/html/working_on_projects/using-project-workbenches_projects
- https://llm-d.ai/

## UI information architecture

**Local MVP, existing PR #62**

- Overview: cluster registration/heartbeat facts; pool, workload and
  operator-attention counts from **observed sample**, not global capacity.
- Projects: cluster-scoped grouping by `DesiredResource.spec.projectID`,
  declared pool accelerator quotas, workload and attention counts.
- Resources: project/type/text filters; desired vs observed generation,
  provider, deletion flag, independent evidence references.
- Operations Inbox: falsifiable failure classification from read errors,
  identity mismatch, generation conflict, incomplete deletion, missing
  observation, explicit negative Ready/Admitted/QuotaReserved conditions.
- Ecosystem: runtime provider registry distinct from discovered schedulers;
  llm-d preflight remains non-promoted.

Each view uses consistent filters, neutral table backgrounds, small
status labels and an explicit detail drilldown. **No fake GPU meter, no
invented billing totals, no mock utilization or inferred Serving Ready.**
No sensitive credentials are displayed or stored in the Portal.

### Important semantics

`Synced` means generation agreement only. It does not mean:
- Kueue QuotaReserved or Admitted,
- Pod Ready, running GPU utilization, or absence of errors,
- llm-d Model Server / Gateway / EPP health,
- ownership, successful deployment, or a completed deletion.

Declared GPU quota from ComputePool accelerator entries means a requested
**quota intent**, not allocatable/used/paid-for GPU count. If the data cannot
be interpreted safely, display **unknown**, never infer zero from malformed
fields. No tenant isolation is implied by client-side project filtering.

Current internal desired endpoint may expose many records, so the local
preview caps processed resources at 120 and 6 concurrent detail GETs, with
a prominent sample-only notice. Future product API must provide pagination
and server-side tenant enforcement.

## Promotion matrix (in order)

| Milestone | Required product proof | Gate |
| --- | --- | --- |
| P0: Portal local workbench | actual Go API; no mock metrics; project grouping, resource details, triage; deterministic unit and Vue build tests | current Draft PR #62 |
| P0: Authenticated management read API | trusted OIDC identity, server-side tenant/project authorization, opaque paginated list, GET denied for other tenants | Issue #63; BLOCKED |
| P0: Operator actions | trusted user-scoped create/delete + generation CAS, idempotency, audit, dangerous action confirmation; no cross-project mutation | dependent on preceding API gate |
| P1: Monitoring | measured Prometheus/DCGM-style metrics with source, freshness and device/vendor compatibility, correct missing-data UX | not started |
| P1: Serving | upstream llm-d/KServe install pinned by immutable digest + signatures, InferencePool/EPP/Gateway/model request, independent observation | Draft #61 plus real cluster proof |
| P2: Chargeback / billing | attributed metering, reconciliation and policy for refunds/retry, backed by durable records | not started |
| P2: AI assistant | evidence-limited, read-only troubleshooting skill with user approval for actions | not started |

Do not expand into every Rancher or Run:ai menu. Prioritize the resource
management happy path and the first-class failure path; preserve the frozen
Stage B / Kueue execution contract. Production exposure is forbidden until
server authentication, RBAC and audited mutations are verified.
