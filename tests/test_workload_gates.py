from app.control_plane.workload_gates import waiting_for_compute_pool


def test_waiting_for_compute_pool_is_observable_and_generation_scoped():
    state = waiting_for_compute_pool(
        pool_name="h100-training",
        pool_key="project/p1/computepool/h100-training",
        generation=7,
    )

    assert state.phase.value == "progressing"
    assert state.observed_generation == 7
    assert state.provider_ref is None
    assert state.provider_status["blocked_by"].endswith("/computepool/h100-training")

    conditions = {condition.type: condition for condition in state.conditions}
    assert conditions["Admitted"].status is False
    assert conditions["Admitted"].reason == "ComputePoolNotReady"
    assert conditions["Admitted"].observed_generation == 7
    assert conditions["Ready"].status is False
