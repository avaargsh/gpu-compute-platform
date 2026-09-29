"""Runtime providers translate ServingConfig into container runtime configuration."""

from __future__ import annotations

from typing import Any

from app.control_plane.domain import DeploymentSpec, RuntimeKind
from app.control_plane.providers import RuntimeProvider


class OpenAICompatibleRuntimeProvider(RuntimeProvider):
    """Build vLLM/SGLang OpenAI-compatible runtime containers."""

    def build_runtime(self, deployment: DeploymentSpec) -> dict[str, Any]:
        serving = deployment.serving
        model_ref = f"{deployment.model_revision.model}:{deployment.model_revision.revision}"

        if serving.runtime == RuntimeKind.VLLM:
            command = ["vllm", "serve", model_ref]
            command += ["--tensor-parallel-size", str(serving.tensor_parallelism)]
            if serving.pipeline_parallelism > 1:
                command += ["--pipeline-parallel-size", str(serving.pipeline_parallelism)]
        elif serving.runtime == RuntimeKind.SGLANG:
            command = ["python", "-m", "sglang.launch_server", "--model-path", model_ref]
            command += ["--tp-size", str(serving.tensor_parallelism)]
        else:
            raise ValueError(f"unsupported runtime: {serving.runtime}")

        for key, value in sorted(serving.runtime_args.items()):
            flag = "--" + key.replace("_", "-")
            if isinstance(value, bool):
                if value:
                    command.append(flag)
            else:
                command += [flag, str(value)]

        return {
            "command": command,
            "port": 8000,
            "protocol": "http",
            "api": "openai",
        }
