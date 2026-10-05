# Legacy GPU provider architecture

Status: **retired / reference only**.

This document previously described the Python-era cloud job provider layer
(Alibaba Cloud, Tencent Cloud, RunPod and related application APIs). That layer
is not the current GPU Compute Platform product boundary.

The canonical implementation is now the Go control plane:

```text
ClusterBinding
  -> provider adapter identity
  -> Cluster Agent
  -> provider.Adapter
  -> Kubernetes / scheduler / accelerator stack
```

Current supported adapter:

```text
kueue
```

Do not add new functionality to `app/gpu/*`, Python cloud-provider factories,
Celery workers, or the retired submit-job abstraction.

For current provider work, use:

- [PROVIDER_ADAPTER_SPI.md](PROVIDER_ADAPTER_SPI.md)
- [PROVIDER_RECOVERY_CONTRACT.md](PROVIDER_RECOVERY_CONTRACT.md)
- [STAGE_B_PROVIDER_CONFORMANCE.md](STAGE_B_PROVIDER_CONFORMANCE.md)
- [CAPABILITY_RELEASE_MATRIX.md](CAPABILITY_RELEASE_MATRIX.md)

The historical Python source remains in the repository temporarily to preserve
context while the Go mainline stabilizes. It is excluded from the canonical
Docker/Compose/CI path.
