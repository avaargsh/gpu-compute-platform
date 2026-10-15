# Workspace PVC: checkpoint bytes survive the writer Job

A **real Kubernetes cluster and a trusted digest-pinned image** are required.
This tool is a storage acceptance experiment, not proof of GPU scheduling,
Kueue admission, model accuracy, checkpoint backup, or multi-tenant storage RBAC.

1. Export a Workload JSON with workspace.claimName from the Train/Batch Portal.
2. Provision and verify that PVC using workspace_pvc.py; verify ProjectBinding
   namespace and kubectl context out of band.
3. Preview the plan (no network access):

\`\`\`bash
python3 scripts/dev/workspace_durability.py workload.json \
  --namespace research --storage-class standard --context kind-gpu \
  --image registry.example/shell@sha256:<64hex>
\`\`\`

4. After approving the exact cluster/PVC, add \`--execute\`. The image
   MUST be digest-pinned and contain \`/bin/sh\`, \`mkdir\`, \`printf\`,
   \`sha256sum\` and a non-root-compatible userland.

The probe GETs a Bound, project-annotated PVC and records UID; creates a random
CPU-only writer Job mounting it; waits for completion; **deletes writer Job**;
rechecks PVC identity; creates a *different* reader Job; verifies reader's
sha256sum check and independently parses the reader's computed SHA-256; deletes
the reader Job; and finally checks that PVC UID did not change. The checkpoint
file is intentionally left on the PVC for follow-on inspection.

Kueue is NOT involved in this storage-only probe. Kubernetes pod security,
RWX/RWO policy, fsGroup, StorageClass binding behavior and container runtime
must permit the test. If any command fails, the probe exits nonzero and does
not claim success. Failed Jobs may remain for inspection; it will **never**
delete, patch or mutate the PVC. No confidential production data belongs in
this probe.

The success receipt proves only exact deterministic checkpoint bytes were read
after the writer Job was deleted, with unchanged PVC UID and a trusted image.
It does NOT test CSI outage, backup restore, cross-node mobility, RWO topology
failover, model training or formal operation ownership. Follow with real
storage disaster-recovery drills before production promotion.

No-cluster negative tests:
\`python3 -m unittest discover -s scripts/dev -p 'test_workspace_durability.py' -v\`.
