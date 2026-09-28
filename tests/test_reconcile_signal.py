from unittest.mock import patch

import pytest

from app.control_plane.reconcile_signal import enqueue_reconcile


def test_reconcile_signal_contains_only_resource_key():
    with patch("app.control_plane.reconcile_signal.celery_app.send_task") as send:
        enqueue_reconcile("workload/train-1", countdown=4)
    send.assert_called_once_with(
        "app.tasks.control_plane.reconcile_resource",
        args=["workload/train-1"],
        queue="control_plane",
        countdown=4,
    )
