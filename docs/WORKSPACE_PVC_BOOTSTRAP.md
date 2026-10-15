# Workspace PVC bootstrap (developer workflow)

The existing Portal Train/Batch Workload JSON is the source of truth for the
PVC claim name and Project ID. This CLI provisions an **existing cluster's**
standard Kubernetes PVC. It does not deploy a custom storage controller.

```bash
# Preview a manifest: no Kubernetes connection or mutation.
python3 scripts/dev/workspace_pvc.py workload.json --namespace research-ns --storage-class standard --size 20Gi

# Explicit create-only call: does NOT replace/patch a preexisting PVC.
python3 scripts/dev/workspace_pvc.py workload.json --namespace research-ns --storage-class standard --size 20Gi --context kind-gpu-dev --create

# Read identity, project annotation, class, access mode and Bound phase.
python3 scripts/dev/workspace_pvc.py workload.json --namespace research-ns --storage-class standard --size 20Gi --context kind-gpu-dev --status

# Submit to the existing Go dev control plane; Kueue alone schedules the Job.
python3 scripts/dev/apply_workload_draft.py workload.json --apply
```

Preconditions: an operator verifies that `--namespace` equals the **actual
server-side ProjectBinding namespace**, the StorageClass exists and PVC
provisioning permissions are project-scoped. Client annotations are metadata,
not RBAC. Existing claims are not overwritten. This script never issues a
delete command. The Kubernetes PVC is separate from the Workload and its DRA
resource claim, and Workload cleanup must not delete it.

A Bound phase only confirms PVC binding; it does **not** prove a successful
mount, checkpoint bytes, data retention, snapshots, backup or restore. On a
real CSI driver, the next acceptance experiment is a two-Job test: create
writer Job, write checkpoint+digest to /workspace, delete Job, create reader
Job mounting the same PVC, compare exact bytes and unchanged PVC UID. Real
cluster testing is still required.

Tests: `python3 -m unittest discover -s scripts/dev -p 'test_*.py' -v`.
