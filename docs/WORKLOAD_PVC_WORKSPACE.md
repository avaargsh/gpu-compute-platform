# Training / batch inference: existing PVC workspace

Status: opt-in developer Workload feature, not a PVC provisioner, notebook
controller or guarantee of training-data durability.

```json
{
  "metadata": {"id": "train-pvc-001", "generation": 1},
  "projectId": "research",
  "poolId": "pool-h100",
  "spec": {
    "image": "registry.example/team/pytorch@sha256:<digest>",
    "command": ["python", "/workspace/train.py", "--output", "/workspace/checkpoints"],
    "accelerator": {"class": "h100-80g", "quota": 1},
    "workspace": {"claimName": "research-workspace"}
  }
}
```

ProjectBinding determines the **namespace**, never a browser-supplied field.
The PVC must already exist in that namespace, with valid access modes,
capacity, credentials and per-project policy. The Agent keeps its claim name
with the Desired generation and Kueue projects a Pod volume at **/workspace**
for both extended-resource and DRA Jobs.

When `workspace` is omitted the Kueue Job has no extra volume. The Workload
spec remains immutable; to change the claim, delete/finalize/recreate under
the existing generation and tombstone rules.

Deleting a Workload deletes the Job (and only the Job-owned accelerator DRA
ResourceClaim when applicable). **It never deletes the PVC.** The storage
layer owns PVC provisioning, snapshots, backup/restore, retention and quota.

Important: same-namespace PVC resolution and DNS-name validation **do not**
prove per-PVC entitlement. Production use requires the canonical Management
OIDC/Project RBAC and project storage authorization. Do not expose the
unauthenticated developer Go write routes outside loopback.

Tests verify API PUT/Desired/readback, Agent JSON decoding, Pod
PVC/VolumeMount for both DRA and extended-resource paths, invalid PVC name
rejection and unchanged legacy Job shape. Real storage-class mount durability
and backup/restore require a separate cluster test, not just unit tests.
