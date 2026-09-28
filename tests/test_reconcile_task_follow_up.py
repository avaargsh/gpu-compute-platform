from unittest.mock import patch

from app.control_plane.status import Phase
from app.tasks.control_plane import _enqueue_follow_up


def test_task_follow_up_enqueues_non_terminal_state():
    with patch("app.tasks.control_plane.enqueue_reconcile") as enqueue:
        _enqueue_follow_up("project/p/workload/w", Phase.PENDING, True)
    enqueue.assert_called_once_with("project/p/workload/w", countdown=2)


def test_task_follow_up_does_not_spin_terminal_state():
    with patch("app.tasks.control_plane.enqueue_reconcile") as enqueue:
        _enqueue_follow_up("project/p/workload/w", Phase.READY, True)
    enqueue.assert_not_called()


def test_task_follow_up_requires_observed_write_to_win():
    with patch("app.tasks.control_plane.enqueue_reconcile") as enqueue:
        _enqueue_follow_up("project/p/workload/w", Phase.PENDING, False)
    enqueue.assert_not_called()
