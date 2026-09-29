from app.control_plane.kv_transfer import (
    KVTransferBinding, KVTransferProvider, KVTransportKind,
)


def test_kv_transport_is_provider_binding_not_serving_domain():
    binding = KVTransferBinding(
        transport=KVTransportKind.NIXL,
        config={"endpoint": "kv://transfer-plane"},
    )
    overrides = KVTransferProvider().runtime_overrides("prefill", binding)
    assert overrides["transport"] == "nixl"
    assert overrides["role"] == "prefill"


def test_kv_transfer_rejects_unified_role():
    import pytest
    with pytest.raises(ValueError):
        KVTransferProvider().runtime_overrides(
            "unified",
            KVTransferBinding(transport=KVTransportKind.MOONCAKE, config={}),
        )
