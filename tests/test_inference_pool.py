from app.control_plane.inference_pool import (
    InferencePoolBinding, InferencePoolBuilder, inference_pool_backend_ref,
)


def test_inference_pool_uses_stable_v1_api():
    manifest = InferencePoolBuilder(
        InferencePoolBinding(
            namespace="team-a",
            endpoint_picker_service="qwen-epp",
            endpoint_picker_port=9002,
        )
    ).build("qwen-pool", {"compute.platform/deployment": "qwen-prod"})

    assert manifest["apiVersion"] == "inference.networking.k8s.io/v1"
    assert manifest["kind"] == "InferencePool"
    assert manifest["spec"]["targetPorts"] == [{"number": 8000}]
    assert manifest["spec"]["endpointPickerRef"]["name"] == "qwen-epp"
    assert manifest["spec"]["endpointPickerRef"]["failureMode"] == "FailOpen"


def test_endpoint_picker_can_be_implementation_managed():
    manifest = InferencePoolBuilder(
        InferencePoolBinding(namespace="team-a")
    ).build("qwen-pool", {"app": "qwen"})
    assert "endpointPickerRef" not in manifest["spec"]


def test_http_route_can_target_inference_pool_backend():
    ref = inference_pool_backend_ref("qwen-pool", weight=90)
    assert ref == {
        "group": "inference.networking.k8s.io",
        "kind": "InferencePool",
        "name": "qwen-pool",
        "weight": 90,
    }
