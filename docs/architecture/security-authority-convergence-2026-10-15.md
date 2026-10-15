# Security authority convergence: one human policy, isolated Agent identity

**Status:** PROPOSED / documentation-only / NOT an authorization or production approval  
**Date:** 2026-10-15  
**Base:** `master@b3e571ce07fd1fb5f49e6f0c08ddf64072c6b8d2`  
**Decision owner:** maintainers of GPU Compute Platform; require an independent security review before adoption.

## 0. Why this document exists

Two current **Draft** delivery lines implement overlapping project authorization:

| Concern | Local management candidate #65 | Independent S0 read PoC #81 (stacked on #80 → #78) |
|---|---|---|
| Human serving boundary | Separate loopback `cmd/management-api`, `/api/v1/management/...` | HTTPS-only PoC human router, `/api/v1/projects/...` |
| User identity | OAuth access token, server-side RFC 7662 introspection | OAuth access token, server-side RFC 7662 introspection |
| Project grant source | PostgreSQL `management_project_grants` | PostgreSQL `platform_project_memberships` |
| Roles | `viewer/operator/admin` | `viewer/developer/owner` |
| Supported methods | Project GET / PUT / DELETE and scoped receipts | Project GET only |
| Agent identity | Not established by #65 | Separate cluster-bound Agent mTLS in #80 |
| Release status | Draft local pilot, not public | Draft isolated PoC, not production |

**Risk:** merging both directly introduces two different authorization stores, conflicting role vocabulary, duplicated human routers and potentially divergent revocation behavior. The two PoCs are evidence of feasibility, not independent production authorization authorities.

The current Stage B Kueue-only provider path, Cluster Agent lease/fencing, generation CAS, finalizer, evidence, and production startup prohibition remain frozen.

## 1. Proposed authority decision (requires review)

**Preferred integration target:** a **single, isolated Management API** that enforces verified human identity, canonical project membership and role policy for both reads and writes; a **different Agent-only mTLS router** bound to a verified cluster identity; and no browser-reachable internal/admin API.

Proposed role vocabulary for minimal project resource operations: **`viewer`, `operator`, `admin`** matching the more complete #65 CRUD/receipt path. Proposed translation of the #81 PoC vocabulary for analysis only: `viewer→viewer`, `developer→operator`, `owner→admin`. This translation is NOT an authorized migration or privilege escalation; any existing roles must be re-enrolled through a reviewed migration with explicit grant ownership, revoke semantics and rollback evidence.

- **Canonical grant store proposal:** `management_project_grants` (currently #65), keyed by verified `(issuer, subject, project_id)`; **no parallel production membership table**. If maintainers choose `platform_project_memberships` instead, migrate the complete read/write implementation to that one source before enabling either route. Record the choice here before implementation.
- **Authentication:** choose exactly one production access-token verification contract: server-side authenticated RFC 7662 HTTPS introspection with well-defined outage/rate-limit/revocation behavior **or** mature audited JWT access-token validation against pinned issuer/JWKS/audience. Neither decoded browser claims, ID tokens used as API bearer credentials, legacy Python auth nor forwarded identity headers count as authorization.
- **Project binding:** request path/projectId is a *selector*, not proof of permission. Resolve and check authoritative project→cluster binding under consistent DB fencing; resource scope is enforced before returning rows, counts, cursors, receipts or mutating Desired.
- **RBAC:** viewer read-only; operator creates/updates/deletes project resources with CAS and audit; admin includes operator but **does not gain permission to edit project grants, bindings, DB roles or cross-project internal resources** through ordinary resource routes. Grant administration is an explicitly separate privileged process.
- **Agent identity:** dedicated verified mTLS listener (trust domain + exact `spiffe://.../cluster/<id>/agent` URI SAN + EKU/chain checks) and enforced cluster binding for all Agent routes. Independent cert enrollment, rotation, expiry, revocation and actual TLS handshake tests remain required. Human bearer tokens never authorize Agent operations.
- **Internal/migration routes:** non-public privileged control plane, not registered on the human or Agent listener. Old unauthenticated `/api/v1/agent` and `/api/v1/internal` must not become externally reachable via Vite proxy, ingress or port-forward.
- **Receipt semantics:** Desired CAS commit, replay-safe mutation receipt and observed execution ownership proof are *distinct*. Retain exact `Idempotency-Key`, request digest binding, transaction-level append-only audit and fail-closed ambiguous commit handling; never call a management receipt 'Kueue/Agent execution succeeded'.

## 2. Concrete reuse vs replacement

| Existing slice | Disposition | Gate |
|---|---|---|
| #78 fail-closed bootstrap | Reuse its loopback and production startup guard | Exact-head checks + reviewer |
| #80 Agent mTLS | Integrate as isolated machine-facing boundary; do not merge its entire stack into #65 by default | Real handshake, negative cases, enrollment/revocation |
| #62/#64 Vue local Portal and protected operator preview | Keep a **local read-only pilot** until canonical Management GET lands. Treat the preview token as a temporary test-only credential | Local browser E2E, no public ingress |
| #65 authenticated management CRUD / CAS / receipt / DB privileges | Preferred **candidate** for canonical server-side read/write authority, but split the 253-commit / 74-file draft into reviewable contracts | Exact-head CI, independent security review and PostgreSQL/IdP E2E |
| #81 project-safe read projection and keyset pagination | **Port selected proven projections/tests** into canonical Management API; don't deploy a second membership store/router | Cross-project negative tests + indexed pagination |
| #76 OpenBao/CNPG foundation + #77 CNPG mTLS/PITR | Run isolated security/persistence POC; keep OpenBao delivery separate from first human auth release | mTLS, failover, WAL/PITR, rollback |
| Cedar Go | Optional shadow evaluator **after** baseline policy is frozen; never second active source of grant truth | All decision diffs explained, default-deny preserved |
| #61 llm-d / GAIE | Separate Stage C read-only discovery, never scheduler or admission promotion | GAIE, EPP, Gateway/request evidence |

## 3. Required acceptance matrix (fail closed)

| ID | Test / evidence | Acceptance |
|---|---|---|
| AUTH-C01 | Missing/expired/invalid, wrong issuer/audience, introspection error/redirect or duplicate Authorization | `401` or intentionally classified `503`; no data or token leakage |
| AUTH-C02 | Same human identity, revoked membership, role changes, cross-project request | Denial on next defined verification boundary; **no foreign row, count, detail, cursor or receipt** |
| AUTH-C03 | Viewer PUT/DELETE, operator tries project grants/binding/admin internal API | Reject; no Desired or audit state mutation |
| AUTH-C04 | Project→cluster rebind/revoke during concurrent GET/mutation | No stale project data/receipt access; no foreign write, generation/fence unchanged |
| AUTH-C05 | Identical lost-ACK retry vs changed-body/actor/project with same Idempotency-Key | One Desired commit + one audit receipt; mismatch conflicts; ambiguous commit is not fabricated as success |
| AUTH-C06 | Agent absent/wrong CA/expired cert/wrong URI SAN/wrong cluster/request body clusterId | Deny at TLS or router; no leases, reports, finalizations or data leakage |
| AUTH-C07 | Human token to Agent/internal route, Agent cert to human/internal route | Route absent/reject; no shared authentication bypass |
| AUTH-C08 | PostgreSQL runtime login gains inherited grant-table write, audit UPDATE, DDL, or SET-able escalation | Startup/mutation fails closed; tested with actual limited LOGIN, not only mocked DSN |
| AUTH-C09 | Stable keyset pagination across project/kind; concurrent changes; foreign-bound resource | No cross-scope leakage; explicit weak/snapshot pagination semantics; bounded indexed queries before production |
| AUTH-C10 | Negative allow/deny audit, actor/project/cluster/resource/operation correlation, redaction | Secrets never logged; append-only audit and failure consistency evidenced |
| AUTH-C11 | CI and end-to-end | Exact candidate SHA: Go suite/race, auth/DB tests, browser E2E, real kind/Kueue regression, supply chain |
| AUTH-C12 | Production profile startup/ingress | Remains blocked until real IdP, trusted TLS+NetworkPolicy, cert lifecycle, production DB least privilege and recovery are independently verified |

Negative response codes are to be pinned in one API contract; do not promise 401/403/404 choices inconsistently between the two PoCs. Browser E2E alone does not prove Cluster Agent identity or DB grant limits.

## 4. Sequencing and review checkpoints

1. **Freeze:** agree canonical role vocabulary, grant DB table, human route shape, trust model and revoke bound; document migration consequences and explicit API contracts. Do **not** merge both draft membership models.
2. **Local baseline:** validate #62/#64 scoped reads with real Go + Vue in a disposable environment; protect existing anonymous Agent route from ingress.
3. **Human identity/read:** select reusable #65 introspection and #81 projected field/negative-test semantics; prove token failure and tenant isolation against one grant store.
4. **Human writes:** promote only CAS/idempotent/audited ComputePool/Workload operations from #65 after least-privileged real LOGIN and real IdP review. Avoid a monolithic unreviewed branch merge.
5. **Machine authority:** apply #80 verified Agent mTLS to distinct listener, test actual handshake/enrollment/rotation and replay/lease regressions. Only then review the production profile guard.
6. **Durability:** test isolated CNPG PITR and recovery (#77), alongside credential/bootstrap boundary; do not make OpenBao a hard prerequisite for its own backing-store restoration.
7. **Production and serving:** after security sign-off, test the full login → project → create → Kueue admission → independent observation → audit/recovery → Portal path. #61 remains read-only Stage C until serving is separately accepted.

## 5. Non-goals and decision record

- No new scheduler, Agent runtime, gateway, homegrown credential protocol, generic identity platform, or billing.
- No production rollout, DB migration, operator role enrollment, token issuance, secret upload or merge in this documentation PR.
- This document is **proposal** until maintainers and security reviewers sign the canonical authorization and ingress contract. An accepted ADR should supersede the provisional mapping, not silently change grant semantics.
- Links: [#63](../issues/63) (human scoped reads), [#79](../issues/79) (production authN/Z), [#65](../pull/65), [#81](../pull/81), [#80](../pull/80), [#77](../issues/77). Canonical GitHub links: https://github.com/avaargsh/gpu-compute-platform/issues/79 .
