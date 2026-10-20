# One-shot Observer process — REVIEWED TEMPLATE ONLY

The binary `cmd/verifiedops-observer` is a bounded, read-only **single
invocation** entrypoint for the signed work-ticket and Kubernetes Observer
code introduced by PR #157.

It has no HTTP listener, PostgreSQL access, long-running reconcile loop,
Kubernetes write client or built-in private key generation. Execution:
1. Read a signed dispatcher work-ticket from a local file (≤1 MiB).
2. Read a separately pinned dispatcher Ed25519 public key (base64 raw
   32-byte public key) and verify the short-lived ticket.
3. Load an externally provisioned Ed25519 private key in **PKCS#8 PEM**
   format from a non-symlink, regular file readable only by its owner
   (mode 0400 or 0600; no group/other bits).
4. Use **only** `rest.InClusterConfig` to create a fresh Kubernetes client.
   No kubeconfig or admin x509 fallback exists. Authenticate itself with
   `SelfSubjectReview`, requiring exactly the dedicated Observer
   ServiceAccount, before any Job/Pod/Node reads.
5. Read/validate a real Job/Pod/Node snapshot, emit **one** DSSE JSON
   envelope to stdout, and exit. No keys, tokens, tickets or observations
   are written into application logs.

Required env variables are:
- `VERIFIEDOPS_TICKET_FILE`
- `VERIFIEDOPS_DISPATCHER_PUBKEY_FILE`
- `VERIFIEDOPS_DISPATCHER_KEY_ID`
- `VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE`
- `VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID`

The `observer-job.example.yaml` is deliberately **suspended**, uses an
unpullable placeholder image, runs non-root without privilege escalation or
hostPath, drops Linux capabilities and mounts a projected **600-second**
ServiceAccount token (not an auto-mounted long-lived credential). The
signed ticket and pinned root are injected via separately controlled
objects; the private key mount requires an external CSI SecretProviderClass.
No key, credential, signed work-ticket or cluster-admin manifest is
checked in. A real deployment MUST review the CSI driver's file ownership
and mode to supply a 0400/0600 owner-only signer file readable by UID 65532.

## Current limitations / no deployment claim

The example cannot run out of the box: a reviewed, signed image digest,
actual dispatcher service and ticket, public trust root, external key
provider, NetworkPolicy, node admission policy and project-scoped
RoleBinding must be provisioned and approved separately. The file key
adapter exposes decrypted private key bytes to the signed process; it is
**not equivalent to KMS/HSM signing**. A production signer should replace
it with a bounded remote `crypto.Signer` or identity-attested KMS.

There is not yet an authenticated ticket delivery endpoint, signer key
rotation/revocation, or a real GPU Ready observation under a hardened Pod.
The CLI stdout is **observational evidence only**, not a Control Plane
verdict. It does NOT set `AUTHORIZED_AND_VERIFIED`.
