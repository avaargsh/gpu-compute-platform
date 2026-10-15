# Apply a training / batch inference draft locally

The Portal's Train / Batch Inference composer now exports the existing Go `domain.Workload` contract. This developer CLI can optionally submit it to the local-only Go API and let the existing Cluster Agent and Kueue run the job.

Prerequisites: a local Go control-plane with matching registered Project and ComputePool bindings, an active Agent, Kueue, and the right provider/accelerator profile. In-memory state is not durable after restart.

```bash
# 1. Copy the Portal-generated JSON into a file, e.g. /tmp/workload.json
# 2. Validate locally without making any API call
python3 scripts/dev/apply_workload_draft.py /tmp/workload.json

# 3. Explicitly submit to the local development Go control-plane
python3 scripts/dev/apply_workload_draft.py /tmp/workload.json --apply

# 4. Inspect independent lifecycle/evidence, not just HTTP 204
curl --fail http://127.0.0.1:8080/api/v1/workloads/train-001
```

The script never accepts external hosts, does not use environment HTTP proxies, rejects redirects, checks duplicate JSON keys and schema, and requires explicit `--apply` before writing. **It is not an authenticated production API** and must never be routed through a public listener or browser credential. It cannot create a ComputePool or project binding by itself.

Check the local tool:

```bash
python3 -m unittest discover -s scripts/dev -p 'test_apply_workload_draft.py' -v
```

`204` means desired intent was stored, **not** that the Job is admitted, started, completed or that a model endpoint exists. Online Serving, persistent Workspace/artifacts and production OIDC/CAS writes remain separate vertical slices.
