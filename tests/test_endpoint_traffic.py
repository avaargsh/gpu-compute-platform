import pytest
from pydantic import ValidationError

from app.control_plane.domain import EndpointSpec, TrafficTarget
from app.control_plane.endpoint_gateway import GatewayBinding, GatewayEndpointBuilder


def test_endpoint_requires_primary_weights_to_sum_to_100():
    with pytest.raises(ValidationError):
        EndpointSpec(
            name="chat",
            targets=[
                TrafficTarget(deployment="stable", weight=80),
                TrafficTarget(deployment="canary", weight=10),
            ],
        )


def test_gateway_route_separates_stable_endpoint_from_deployments():
    endpoint = EndpointSpec(
        name="chat",
        hostname="api.example.test",
        targets=[
            TrafficTarget(deployment="qwen-r1", weight=90),
            TrafficTarget(deployment="qwen-r2", weight=10),
            TrafficTarget(deployment="qwen-shadow", weight=0, shadow=True),
        ],
    )
    builder = GatewayEndpointBuilder(
        GatewayBinding(namespace="team-a", gateway_name="inference")
    )
    route = builder.build_route(endpoint)
    refs = route["spec"]["rules"][0]["backendRefs"]
    assert [(r["name"], r["weight"]) for r in refs] == [
        ("qwen-r1", 90),
        ("qwen-r2", 10),
    ]
    assert route["spec"]["hostnames"] == ["api.example.test"]
    assert builder.shadow_targets(endpoint) == ["qwen-shadow"]
