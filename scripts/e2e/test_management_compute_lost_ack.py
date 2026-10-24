"""Regression tests for Management lost-ACK cleanup authorization."""

from __future__ import annotations

import copy
import importlib.util
from pathlib import Path
import sys
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib import request as urllib_request
from urllib.error import URLError
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("management-compute-lost-ack.py")
SPEC = importlib.util.spec_from_file_location("management_compute_lost_ack", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)

IMAGE = "registry.example.test/worker@sha256:" + ("a" * 64)
COMMAND = ["/bin/sh", "-c", "sleep 3600"]
ACCELERATOR_CLASS = "h100-80g"
QUOTA = 1


def desired(generation: int = 1) -> dict:
    return {
        "id": "ack-loss-test",
        "clusterId": "cluster-a",
        "projectId": "project-a",
        "generation": generation,
        "deleting": False,
        "spec": {
            "image": IMAGE,
            "poolID": "pool-a",
            "command": list(COMMAND),
            "accelerator": {"class": ACCELERATOR_CLASS, "quota": QUOTA},
        },
    }


def match_desired(value: dict, generation: int = 1) -> bool:
    return MODULE.desired_matches_create(
        value,
        cluster="cluster-a",
        project="project-a",
        workload="ack-loss-test",
        generation=generation,
        image=IMAGE,
        pool="pool-a",
        command=COMMAND,
        accelerator_class=ACCELERATOR_CLASS,
        quota=QUOTA,
    )


def valid_receipt(digest: str = "a" * 64) -> dict:
    return {
        "requestId": "f" * 32,
        "status": "desired-committed",
        "clusterId": "cluster-a",
        "projectId": "project-a",
        "kind": "Workload",
        "resourceId": "ack-loss-test",
        "action": "create",
        "expectedGeneration": 0,
        "resultingGeneration": 1,
        "requestDigest": digest,
        "committedAt": "2026-10-24T00:00:00Z",
    }


class OperationReceiptValidationTests(unittest.TestCase):
    def test_accepts_nonzero_lowercase_sha256_digest(self) -> None:
        self.assertEqual(
            MODULE.verify_receipt(
                valid_receipt(),
                request_id="f" * 32,
                cluster="cluster-a",
                project="project-a",
                workload="ack-loss-test",
                action="create",
                expected_generation=0,
            ),
            1,
        )

    def test_rejects_malformed_or_zero_request_digest(self) -> None:
        for digest in ("", "sha256:" + "a" * 64, "A" * 64, "0" * 64, "a" * 63, "a" * 65):
            with self.subTest(digest=digest):
                with self.assertRaisesRegex(RuntimeError, "requestDigest"):
                    MODULE.verify_receipt(
                        valid_receipt(digest),
                        request_id="f" * 32,
                        cluster="cluster-a",
                        project="project-a",
                        workload="ack-loss-test",
                        action="create",
                        expected_generation=0,
                    )


class ReceiptSnapshotTests(unittest.TestCase):
    def test_identical_replay_preserves_every_receipt_field(self) -> None:
        receipt = valid_receipt()
        self.assertTrue(MODULE.receipt_snapshot_matches(receipt, copy.deepcopy(receipt), expected_generation=1))

    def test_snapshot_rejects_changed_digest(self) -> None:
        before = valid_receipt()
        after = copy.deepcopy(before)
        after["requestDigest"] = "b" * 64
        self.assertFalse(MODULE.receipt_snapshot_matches(before, after, expected_generation=1))

    def test_snapshot_rejects_changed_generation(self) -> None:
        before = valid_receipt()
        after = copy.deepcopy(before)
        after["resultingGeneration"] = 2
        self.assertFalse(MODULE.receipt_snapshot_matches(before, after, expected_generation=1))

    def test_snapshot_rejects_changed_commit_time(self) -> None:
        before = valid_receipt()
        after = copy.deepcopy(before)
        after["committedAt"] = "2026-10-24T00:00:01Z"
        self.assertFalse(MODULE.receipt_snapshot_matches(before, after, expected_generation=1))


class DesiredCreateIdentityTests(unittest.TestCase):
    def test_exact_receipt_generation_and_requested_spec_authorize_cleanup(self) -> None:
        self.assertTrue(match_desired(desired()))

    def test_generation_drift_revokes_cleanup_authorization(self) -> None:
        self.assertFalse(match_desired(desired(generation=2)))

    def test_command_drift_revokes_cleanup_authorization(self) -> None:
        value = desired()
        value["spec"]["command"] = ["/bin/sh", "-c", "echo foreign"]
        self.assertFalse(match_desired(value))

    def test_accelerator_drift_revokes_cleanup_authorization(self) -> None:
        value = desired()
        value["spec"]["accelerator"]["quota"] = 2
        self.assertFalse(match_desired(value))

    def test_pool_or_image_drift_revokes_cleanup_authorization(self) -> None:
        for field, replacement in (("poolID", "pool-b"), ("image", "registry.example.test/other@sha256:" + ("b" * 64))):
            with self.subTest(field=field):
                value = desired()
                value["spec"][field] = replacement
                self.assertFalse(match_desired(value))


class CleanupGenerationFenceTests(unittest.TestCase):
    def run_cleanup(self, value: dict) -> None:
        with (
            patch.object(MODULE, "json_call", return_value=(200, value)),
            patch.object(MODULE, "http_call") as delete,
        ):
            with self.assertRaisesRegex(RuntimeError, "generation|spec|identity"):
                MODULE.cleanup_workload(
                    "http://127.0.0.1:8081",
                    "http://127.0.0.1:8080",
                    "token-not-used",
                    "cluster-a",
                    "project-a",
                    "ack-loss-test",
                    "golden-go",
                    None,
                    30,
                    IMAGE,
                    "pool-a",
                    expected_generation=1,
                    expected_command=COMMAND,
                    expected_accelerator_class=ACCELERATOR_CLASS,
                    expected_quota=QUOTA,
                )
            delete.assert_not_called()

    def test_cleanup_refuses_same_name_and_image_after_generation_replacement(self) -> None:
        self.run_cleanup(desired(generation=2))

    def test_cleanup_refuses_changed_command_even_when_generation_matches(self) -> None:
        value = desired()
        value["spec"]["command"] = ["/bin/sh", "-c", "echo foreign"]
        self.run_cleanup(value)

    def test_cleanup_refuses_changed_accelerator_even_when_generation_matches(self) -> None:
        value = desired()
        value["spec"]["accelerator"]["quota"] = 2
        self.run_cleanup(value)

    def test_cleanup_uses_receipt_generation_as_delete_precondition(self) -> None:
        receipt = {
            "requestId": "f" * 32,
            "status": "desired-committed",
            "clusterId": "cluster-a",
            "projectId": "project-a",
            "kind": "Workload",
            "resourceId": "ack-loss-test",
            "action": "delete-request",
            "expectedGeneration": 1,
            "resultingGeneration": 2,
            "requestDigest": "c" * 64,
        }
        with (
            patch.object(MODULE, "json_call", side_effect=[
                (200, desired()),
                (200, receipt),
                (200, receipt),
                (404, None),
                (404, None),
            ]),
            patch.object(MODULE, "run_faulted_caller") as drop_ack,
            patch.object(MODULE, "http_call", return_value=(204, b"")) as replay,
            patch.object(MODULE, "wait_for_provider_absent") as wait_absent,
            patch.object(MODULE.secrets, "token_hex", return_value="f" * 32),
        ):
            MODULE.cleanup_workload(
                "http://127.0.0.1:8081",
                "http://127.0.0.1:8080",
                "token-not-used",
                "cluster-a",
                "project-a",
                "ack-loss-test",
                "golden-go",
                None,
                30,
                IMAGE,
                "pool-a",
                expected_generation=1,
                expected_command=COMMAND,
                expected_accelerator_class=ACCELERATOR_CLASS,
                expected_quota=QUOTA,
            )
        drop_ack.assert_called_once_with(
            "http://127.0.0.1:8081",
            "/api/v1/management/clusters/cluster-a/projects/project-a/resources/Workload/ack-loss-test",
            "f" * 32,
            b"",
            30,
            method="DELETE",
            if_match='"1"',
            accepted_statuses=(202, 204),
        )
        self.assertEqual(replay.call_args.args[1], "DELETE")
        self.assertEqual(replay.call_args.kwargs["if_match"], '"1"')
        wait_absent.assert_called_once()
        self.assertEqual(
            wait_absent.call_args.args[:3],
            ("golden-go", "ack-loss-test", None),
        )
        self.assertGreater(wait_absent.call_args.args[3], 0.0)
        self.assertLessEqual(wait_absent.call_args.args[3], 30.0)


if __name__ == "__main__":
    unittest.main()


class DeleteAckDropProxyTests(unittest.TestCase):
    def test_delete_202_is_withheld_until_caller_connection_is_dropped(self) -> None:
        upstream_requests: list[tuple[str, str, str]] = []

        class UpstreamHandler(BaseHTTPRequestHandler):
            def log_message(self, _format: str, *_args: object) -> None:
                return

            def do_DELETE(self) -> None:  # noqa: N802
                upstream_requests.append((
                    self.command,
                    self.headers.get("Idempotency-Key", ""),
                    self.headers.get("If-Match", ""),
                ))
                self.send_response(202)
                self.send_header("Content-Length", "0")
                self.end_headers()

        upstream = ThreadingHTTPServer(("127.0.0.1", 0), UpstreamHandler)
        upstream_thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        upstream_thread.start()
        state = MODULE.FaultState()
        path = "/api/v1/management/clusters/cluster-a/projects/project-a/resources/Workload/ack-loss-test"
        proxy = MODULE.ack_drop_proxy(
            f"http://127.0.0.1:{upstream.server_port}",
            path,
            state,
            expected_method="DELETE",
            accepted_statuses=(202, 204),
        )
        proxy_thread = threading.Thread(target=proxy.serve_forever, daemon=True)
        proxy_thread.start()
        request_finished = threading.Event()
        client_error: list[BaseException] = []

        def send_delete() -> None:
            req = urllib_request.Request(
                f"http://127.0.0.1:{proxy.server_port}{path}",
                data=b"",
                headers={
                    "Idempotency-Key": "f" * 32,
                    "If-Match": '"1"',
                    "Authorization": "Bearer test-token",
                },
                method="DELETE",
            )
            try:
                with urllib_request.urlopen(req, timeout=5) as response:
                    response.read()
            except (URLError, TimeoutError, ConnectionError, OSError) as exc:
                client_error.append(exc)
            finally:
                request_finished.set()

        client_thread = threading.Thread(target=send_delete, daemon=True)
        client_thread.start()
        try:
            self.assertTrue(state.upstream_finished.wait(timeout=5))
            self.assertEqual(state.backend_status, 202)
            self.assertEqual(
                upstream_requests,
                [("DELETE", "f" * 32, '"1"')],
            )
            # The provider has committed the delete intent, but the caller must
            # not see the 202 until the fault injector is released.
            self.assertFalse(request_finished.is_set())
            state.release_connection.set()
            self.assertTrue(request_finished.wait(timeout=5))
            self.assertTrue(client_error, "the dropped ACK should surface as a transport error")
        finally:
            state.release_connection.set()
            proxy.shutdown()
            proxy.server_close()
            proxy_thread.join(timeout=5)
            upstream.shutdown()
            upstream.server_close()
            upstream_thread.join(timeout=5)



class WorkloadObservationWaitTests(unittest.TestCase):
    def test_pods_ready_is_a_valid_current_generation_running_observation(self) -> None:
        snapshot = {
            "metadata": {"generation": 1},
            "status": {
                "syncState": "Synced",
                "observedGeneration": 1,
                "conditions": [
                    {"type": "QuotaReserved", "status": "True"},
                    {"type": "Admitted", "status": "True"},
                    {"type": "PodsReady", "status": "True"},
                ],
            },
        }
        with (
            patch.object(MODULE, "json_call", return_value=(200, snapshot)),
            patch.object(MODULE.time, "monotonic", side_effect=[0.0, 0.0, 2.0]),
            patch.object(MODULE.time, "sleep"),
        ):
            self.assertEqual(
                MODULE.wait_for_workload(
                    "http://127.0.0.1:8080",
                    "ack-loss-test",
                    1,
                    timeout=1,
                    namespace="project-a",
                    context=None,
                ),
                snapshot,
            )

class ProviderAbsenceWaitTests(unittest.TestCase):
    def test_waits_for_pod_termination_after_api_finalization(self) -> None:
        pod = {
            "metadata": {
                "name": "job-ack-loss-test-abc",
                "uid": "pod-uid",
                "deletionTimestamp": "2026-10-24T02:00:00Z",
            },
            "status": {"phase": "Running"},
        }
        pod_queries = 0

        def list_resources(namespace: str, kind: str, selector: str, context: str | None) -> dict:
            nonlocal pod_queries
            if kind == "jobs":
                return {"items": []}
            pod_queries += 1
            return {"items": [pod]} if pod_queries == 1 else {"items": []}

        with (
            patch.object(MODULE, "kubectl_json", side_effect=list_resources),
            patch.object(MODULE.time, "sleep"),
        ):
            MODULE.wait_for_provider_absent(
                "project-a",
                "ack-loss-test",
                context=None,
                timeout=10,
            )
        self.assertEqual(pod_queries, 2)

    def test_timeout_reports_remaining_pod_deletion_state(self) -> None:
        pod = {
            "metadata": {
                "name": "job-ack-loss-test-abc",
                "uid": "pod-uid",
                "deletionTimestamp": "2026-10-24T02:00:00Z",
            },
            "status": {
                "phase": "Running",
                "containerStatuses": [{"name": "worker", "state": {"terminated": {}}, "restartCount": 0}],
            },
        }

        def list_resources(namespace: str, kind: str, selector: str, context: str | None) -> dict:
            return {"items": [] if kind == "jobs" else [pod]}

        with patch.object(MODULE, "kubectl_json", side_effect=list_resources):
            with self.assertRaisesRegex(RuntimeError, "deletionTimestamp"):
                MODULE.wait_for_provider_absent(
                    "project-a",
                    "ack-loss-test",
                    context=None,
                    timeout=0,
                )

class ProviderPresenceTests(unittest.TestCase):
    def test_requires_real_job_ready_pod_and_kueue_evidence(self) -> None:
        objects = {
            "jobs": {"items": [{
                "metadata": {
                    "name": "job-ack-loss-test",
                    "annotations": {"ai.compute/generation": "1"},
                },
            }]},
            "pods": {"items": [{
                "metadata": {"name": "job-ack-loss-test-abc"},
                "status": {
                    "phase": "Running",
                    "conditions": [{"type": "Ready", "status": "True"}],
                },
            }]},
        }

        def list_resources(namespace: str, kind: str, selector: str, context: str | None) -> dict:
            return objects[kind]

        evidence = [
            "k8s://cluster-a/namespaces/project-a/jobs/job-ack-loss-test",
            "kueue://cluster-a/namespaces/project-a/workloads/job-ack-loss-test",
        ]
        with patch.object(MODULE, "kubectl_json", side_effect=list_resources):
            MODULE.verify_provider_present(
                "project-a", "ack-loss-test", "cluster-a", 1, evidence, context=None
            )

    def test_rejects_job_without_a_running_ready_pod(self) -> None:
        objects = {
            "jobs": {"items": [{
                "metadata": {
                    "name": "job-ack-loss-test",
                    "annotations": {"ai.compute/generation": "1"},
                },
            }]},
            "pods": {"items": [{
                "metadata": {"name": "job-ack-loss-test-abc"},
                "status": {
                    "phase": "Pending",
                    "conditions": [{"type": "Ready", "status": "False"}],
                },
            }]},
        }

        def list_resources(namespace: str, kind: str, selector: str, context: str | None) -> dict:
            return objects[kind]

        evidence = [
            "k8s://cluster-a/namespaces/project-a/jobs/job-ack-loss-test",
            "kueue://cluster-a/namespaces/project-a/workloads/job-ack-loss-test",
        ]
        with patch.object(MODULE, "kubectl_json", side_effect=list_resources):
            with self.assertRaisesRegex(RuntimeError, r"no Running\+Ready Pod"):
                MODULE.verify_provider_present(
                    "project-a", "ack-loss-test", "cluster-a", 1, evidence, context=None
                )

    def test_rejects_missing_kueue_evidence_even_when_job_and_pod_exist(self) -> None:
        objects = {
            "jobs": {"items": [{
                "metadata": {
                    "name": "job-ack-loss-test",
                    "annotations": {"ai.compute/generation": "1"},
                },
            }]},
            "pods": {"items": [{
                "metadata": {"name": "job-ack-loss-test-abc"},
                "status": {
                    "phase": "Running",
                    "conditions": [{"type": "Ready", "status": "True"}],
                },
            }]},
        }

        def list_resources(namespace: str, kind: str, selector: str, context: str | None) -> dict:
            return objects[kind]

        with patch.object(MODULE, "kubectl_json", side_effect=list_resources):
            with self.assertRaisesRegex(RuntimeError, "omits the namespaced Kueue"):
                MODULE.verify_provider_present(
                    "project-a",
                    "ack-loss-test",
                    "cluster-a",
                    1,
                    ["k8s://cluster-a/namespaces/project-a/jobs/job-ack-loss-test"],
                    context=None,
                )

