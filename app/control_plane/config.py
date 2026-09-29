"""Runtime configuration for control-plane provider bindings."""

from pydantic_settings import BaseSettings


class ControlPlaneSettings(BaseSettings):
    scheduler_provider: str = "disabled"
    kueue_namespace: str = "default"
    kueue_local_queue: str = "default"
    accelerator_resource: str = "nvidia.com/gpu"
    kubeconfig: str | None = None

    model_config = {
        "env_prefix": "CONTROL_PLANE_",
        "case_sensitive": False,
    }


control_plane_settings = ControlPlaneSettings()
