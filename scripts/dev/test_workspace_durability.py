"""No-cluster unit tests for real writer/delete/reader PVC evidence flow."""
from __future__ import annotations

import argparse
import copy
import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import workspace_durability as wd  # noqa: E402

DRAFT = {
    "metadata": {"id": "train-1", "generation": 1},
    "projectId": "research", "poolId": "pool-1",
    "spec": {"image": "registry.example/train:v1", "command": ["python", "train.py"],
             "accelerator": {"class": "h100", "quota": 1},
             "workspace": {"claimName": "train-data"}},
}
IMAGE = "registry.example/shell@sha256:" + "a" * 64


def args():
    return argparse.Namespace(
        namespace="research", storage_class="standard", size="20Gi",
        access_mode="ReadWriteOnce", context="kind-gpu",
        image=IMAGE, timeout=60, execute=True,
    )


class DurabilityTests(unittest.TestCase):
    def draft_file(self):
        tmp = tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False)
        json.dump(DRAFT, tmp)
        tmp.close()
        self.addCleanup(lambda: Path(tmp.name).unlink(missing_ok=True))
        return tmp.name

    def test_plan_does_not_contact_cluster(self):
        with patch.object(wd.subprocess, "run") as proc:
            self.assertEqual(wd.main([
                self.draft_file(), "--namespace", "research",
                "--storage-class", "standard", "--context", "kind-gpu",
                "--image", IMAGE]), 0)
        proc.assert_not_called()

    def test_pinned_image_and_explicit_execute_required(self):
        base = [self.draft_file(), "--namespace", "research",
                "--storage-class", "standard", "--context", "kind-gpu"]
        self.assertEqual(wd.main(base + ["--image", "busybox:latest"]), 2)
        self.assertEqual(wd.main(base + ["--image", IMAGE, "--timeout", "3"]), 2)

    def test_jobs_share_pvc_but_do_not_mount_service_credentials(self):
        cfg = args()
        w = wd.workload_job(cfg, "pvc-write-test", "train-data",
                            "/workspace/test.bin", "payload", "abc", False)
        r = wd.workload_job(cfg, "pvc-read-test", "train-data",
                            "/workspace/test.bin", "payload", "abc", True)
        for job in [w, r]:
            spec = job["spec"]["template"]["spec"]
            self.assertFalse(spec["automountServiceAccountToken"])
            self.assertEqual(spec["volumes"][0]["persistentVolumeClaim"]["claimName"], "train-data")
            self.assertEqual(spec["containers"][0]["volumeMounts"][0]["mountPath"], "/workspace")
            self.assertFalse(spec["containers"][0]["securityContext"]["allowPrivilegeEscalation"])
        self.assertNotIn("CHECKPOINT_VALUE", str(r))

    def test_full_sequence_deletes_writer_before_reader_and_preserves_uid(self):
        cfg = args()
        expected = wd.pvc_manifest(DRAFT, cfg)
        events = []
        def fake_pvc(config, wanted, initial=None):
            events.append("pvc")
            self.assertEqual(wanted, expected)
            self.assertIn(initial, (None, "pvc-uid"))
            return "pvc-uid"
        def fake_create(config, job):
            kind = "reader" if job["metadata"]["name"].startswith("pvc-read-") else "writer"
            events.append("create-" + kind)
            return kind + "-uid"
        def fake_delete(config, name):
            events.append("delete-" + ("reader" if name.startswith("pvc-read-") else "writer"))
        def fake_kubectl(config, cmd, payload=None, timeout=30):
            self.assertEqual(cmd[0], "logs")
            events.append("reader-logs")
            # Reproduce the reader's stdout based on the expected SHA of
            # the known writer payload from the prepared manifest.
            return digest + "  " + checkpoint + "\n"
        with patch.object(wd.secrets, "token_hex", side_effect=["0123456789abcd", "f" * 32]), \
             patch.object(wd, "pvc_uid", side_effect=fake_pvc), \
             patch.object(wd, "create_wait", side_effect=fake_create), \
             patch.object(wd, "delete_job", side_effect=fake_delete), \
             patch.object(wd, "kubectl", side_effect=fake_kubectl):
            checkpoint = "/workspace/.gpu-compute-durability/0123456789abcd.bin"
            digest = hashlib.sha256(("gpu-compute-pvc-durability:" + "f" * 32).encode()).hexdigest()
            receipt = wd.run(cfg, expected)
        self.assertEqual(receipt["result"], "CHECKPOINT_BYTES_RETAINED_AFTER_JOB_DELETION")
        self.assertEqual(receipt["pvcUid"], "pvc-uid")
        self.assertEqual(events, [
            "pvc", "create-writer", "delete-writer", "pvc",
            "create-reader", "reader-logs", "pvc", "delete-reader", "pvc",
        ])
        self.assertEqual(receipt["sha256"], digest)

    def test_reader_wrong_digest_is_not_success(self):
        cfg, expected = args(), wd.pvc_manifest(DRAFT, args())
        with patch.object(wd.secrets, "token_hex", side_effect=["0123456789abcd", "f" * 32]), \
             patch.object(wd, "pvc_uid", return_value="pvc-uid"), \
             patch.object(wd, "create_wait", return_value="job-uid"), \
             patch.object(wd, "delete_job"), \
             patch.object(wd, "kubectl", return_value=("0" * 64 + "  /workspace/.gpu-compute-durability/0123456789abcd.bin\n")):
            with self.assertRaises(wd.InvalidDraft):
                wd.run(cfg, expected)

    def test_pvc_uid_replacement_fails_before_reader(self):
        cfg, expected = args(), wd.pvc_manifest(DRAFT, args())
        with patch.object(wd.secrets, "token_hex", side_effect=["0123456789abcd", "f" * 32]), \
             patch.object(wd, "pvc_uid", side_effect=["original", wd.InvalidDraft("PVC UID changed")]), \
             patch.object(wd, "create_wait", return_value="job-uid"), \
             patch.object(wd, "delete_job"):
            with self.assertRaises(wd.InvalidDraft):
                wd.run(cfg, expected)

    def test_mutating_webhook_cannot_redirect_job_to_foreign_pvc(self):
        config = args()
        requested = wd.workload_job(config, "pvc-write-test", "train-data",
                                    "/workspace/test.bin", "payload", "digest", False)
        admitted = copy.deepcopy(requested)
        admitted["metadata"]["uid"] = "uid-123"
        admitted["spec"]["template"]["spec"]["volumes"][0][
            "persistentVolumeClaim"]["claimName"] = "foreign-data"
        with patch.object(wd, "obj", return_value=admitted), \
             patch.object(wd, "kubectl") as cli:
            with self.assertRaises(wd.InvalidDraft):
                wd.create_wait(config, requested)
        cli.assert_not_called()

    def test_kubectl_never_executes_shell_or_deletes_pvc(self):
        cfg = args()
        calls = []
        def fake_run(argv, **kwargs):
            calls.append(argv)
            return wd.subprocess.CompletedProcess(argv, 0, json.dumps({"kind": "Job", "metadata": {
                "name": "pvc-write-1", "namespace": "research", "uid": "job-uid"}}), "")
        with patch.object(wd.subprocess, "run", side_effect=fake_run):
            wd.obj(cfg, ["create", "-f", "-", "-o", "json"], {"kind": "Job"})
            wd.delete_job(cfg, "pvc-write-1")
        self.assertEqual(calls[0][0:5], ["kubectl", "--context", "kind-gpu", "--namespace", "research"])
        self.assertIn(["delete", "job", "pvc-write-1", "--wait=true", "--cascade=foreground", "--timeout=60s"],
                      [call[5:] for call in calls])
        self.assertTrue(all("pvc" not in call[5:] for call in calls))


if __name__ == "__main__":
    unittest.main()
