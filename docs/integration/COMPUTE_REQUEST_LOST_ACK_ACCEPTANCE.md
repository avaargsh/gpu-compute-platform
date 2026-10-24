# Management Compute lost-ACK acceptance

Status: **opt-in integration acceptance; requires a disposable, real environment**.

This test closes one failure-injection gap between the existing PostgreSQL idempotency tests and the GPU/Kueue Golden Path: it drops the client-visible HTTP response **after the authenticated Management API has returned HTTP 204**, then SIGKILLs the caller process while the response is held. The expected result is one durable Desired Workload + one operation receipt, recoverable by the original 128-bit request ID.

It does not claim exactly-once provider side effects. Provider reconciliation remains at-least-once and must converge via the existing deterministic resource identity, generation/lease fencing and independent observation contract.

## Prerequisites

Run only against a dedicated disposable cluster/database/project/pool. The harness refuses non-loopback HTTP endpoints and requires an immutable image digest. Never point it at production.

Required local dependencies:

- Python 3.11+ (standard library only for the harness)
- kubectl configured for the dedicated kind/Kubernetes cluster
- the Go Control Plane + Cluster Agent running on 127.0.0.1:8080
- the separate authenticated Management API running on 127.0.0.1:8081
- PostgreSQL-backed Management API with the Workload Project grant and Project/Pool/Cluster/Kueue bindings already provisioned
- Kueue and the accelerator profile supported by the target pool
- a short-lived OIDC access token authorized as an operator for the disposable Project

Export these values in the shell that will run the test:

~~~bash
export OIDC_ACCESS_TOKEN='<short-lived access token>'
export CLUSTER_ID='cluster-golden'
export PROJECT_ID='project-golden'
export POOL_ID='pool-h100'
export NAMESPACE='golden-go'
export ACCELERATOR_CLASS='h100-80g'
export IMAGE='registry.example.test/acceptance/worker@sha256:<64-lowercase-hex-digest>'

# Optional overrides:
export MANAGEMENT_BASE_URL='http://127.0.0.1:8081'
export CONTROL_PLANE_BASE_URL='http://127.0.0.1:8080'
export ACCEPTANCE_TIMEOUT_SECONDS=240
# export KUBECTL_CONTEXT='kind-golden'
# export WORKLOAD_ID='ack-loss-test-unique'
# export ACCELERATOR_QUOTA=1
~~~

The example values are placeholders; replace them with the real IDs, registry and digest from the disposable environment. The image must run the configured command. The command defaults to a bounded sleep 120 process, which is useful for testing a live allocation before deletion.

## Run

~~~bash
python3 -m py_compile scripts/e2e/management-compute-lost-ack.py
make management-compute-lost-ack-acceptance
~~~

The test creates a fresh 32-hex requestId before the first side effect. The same value is sent as Idempotency-Key and reused for recovery. The harness:

1. Starts an ephemeral loopback HTTP proxy in front of the authenticated Management API.
2. Sends a Workload create through a child caller process. The proxy waits for the real Management API's 204 response (the PostgreSQL Desired state and operation receipt have committed), withholds it, then the parent sends SIGKILL to the child and closes the client connection without forwarding the ACK.
3. Reads the owner-scoped Management operation receipt with the original request ID and verifies its server-returned generation and request digest. It re-reads authorized Desired state and compares the Workload identity, pool, image and generation.
4. Replays the exact same PUT with the same idempotency key and checks that the receipt and generation remain unchanged.
5. Waits for independently reported current-generation Kueue/provider conditions (Synced, QuotaReserved, Admitted, and Ready or Succeeded).
6. Deletes the Workload with a fresh operation ID and the exact observed Generation, reads the deletion receipt, waits for both Management/Control Plane state to disappear, and independently checks that matching Kubernetes Jobs and Pods are absent.

The generated Workload ID is unique by default. Cleanup re-reads the resource and checks Project, Cluster, pool and image identity before requesting deletion. On uncertainty, the test fails and preserves the relevant resource/tombstone for reconciliation rather than claiming successful cleanup.

## Evidence and limits

A pass is valid only for the exact code SHA, disposable environment, test command and preserved redacted output. Record those values with the test report; do not put bearer tokens or secret values in logs.

This test specifically covers **Management create ACK loss after durable commit**. It does not by itself prove:

- a real cluster Agent/controller process crash between Kubernetes CREATE and observation/finalization;
- provider operation ownership when a same-name/label foreign Job or Pod exists;
- non-GPU Worker/Sandbox runtime integration;
- production Keycloak/IdP, multi-tenant revocation races or production ingress;
- image signature, build provenance or complete supply-chain policy acceptance.

Continue to run make acceptance-contract and make e2e-golden on the same candidate SHA for the existing PostgreSQL lifecycle/tombstone/lease-fencing and kind/Kueue provider takeover contracts. Ownership-proof negative tests remain tracked in [gpu-compute-platform #141](https://github.com/avaargsh/gpu-compute-platform/issues/141); the authenticated Portal → receipt → Kueue end-to-end slice remains tracked in [#116](https://github.com/avaargsh/gpu-compute-platform/issues/116). Cross-repository resource Schema/lifecycle remains a review-gated draft in [agent-control-plane PR #229](https://github.com/avaargsh/agent-control-plane/pull/229).

A successful run is one acceptance result, **not** permission to open production ingress or mark all P0/P1 requirements complete.
