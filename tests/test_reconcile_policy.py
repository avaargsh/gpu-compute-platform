from app.control_plane.reconcile_policy import needs_follow_up
from app.control_plane.status import Phase


def test_non_terminal_phases_require_follow_up():
    assert needs_follow_up(Phase.PENDING)
    assert needs_follow_up(Phase.PROGRESSING)
    assert needs_follow_up(Phase.DEGRADED)
    assert needs_follow_up(Phase.TERMINATING)


def test_terminal_phases_do_not_spin():
    assert not needs_follow_up(Phase.READY)
    assert not needs_follow_up(Phase.FAILED)
