"""Fail-closed regression tests for read-only Kueue storage preflight."""

import contextlib
import copy
import io
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import kueue_storage_preflight as preflight


def crd(plural, *, stored=None, served=True, storage=True):
    return {
        "metadata": {"name": plural + ".kueue.x-k8s.io"},
        "spec": {
            "versions": [
                {"name": "v1beta1", "served": True, "storage": not storage},
                {"name": "v1beta2", "served": served, "storage": storage},
            ]
        },
        "status": {"storedVersions": ["v1beta2"] if stored is None else stored},
    }


def valid_snapshot():
    return {"kind": "List", "items": [
        crd("resourceflavors"), crd("clusterqueues"),
        crd("localqueues"), crd("workloads"), crd("admissionchecks")
    ]}


class StoragePreflightTests(unittest.TestCase):
    def test_all_kueue_crds_have_only_v1beta2_stored(self):
        result = preflight.audit_crds(valid_snapshot())
        self.assertEqual(result["result"], "PASS")
        self.assertEqual(len(result["checked_crds"]), 5)
        self.assertEqual(result["blockers"], [])

    def test_v1beta1_stored_version_blocks_even_when_v1beta2_is_served(self):
        snapshot = valid_snapshot()
        snapshot["items"][3]["status"]["storedVersions"] = ["v1beta1", "v1beta2"]
        result = preflight.audit_crds(snapshot)
        self.assertEqual(result["result"], "BLOCKED")
        self.assertIn("unmigrated_stored_versions:workloads.kueue.x-k8s.io", result["blockers"])

    def test_non_core_kueue_crd_with_stale_storage_also_blocks(self):
        snapshot = valid_snapshot()
        snapshot["items"][4]["status"]["storedVersions"] = ["v1beta1"]
        self.assertIn(
            "unmigrated_stored_versions:admissionchecks.kueue.x-k8s.io",
            preflight.audit_crds(snapshot)["blockers"],
        )

    def test_missing_core_crd_blocks(self):
        snapshot = valid_snapshot()
        snapshot["items"] = snapshot["items"][:3]
        result = preflight.audit_crds(snapshot)
        self.assertIn("missing_core_crd:workloads.kueue.x-k8s.io", result["blockers"])

    def test_no_kueue_crds_blocks(self):
        result = preflight.audit_crds({"items": [{"metadata": {"name": "jobs.example.org"}}]})
        self.assertEqual(result["result"], "BLOCKED")
        self.assertIn("no_kueue_crds_found", result["blockers"])

    def test_non_served_v1beta2_blocks(self):
        snapshot = valid_snapshot()
        snapshot["items"][0] = crd("resourceflavors", served=False)
        result = preflight.audit_crds(snapshot)
        self.assertIn("v1beta2_not_served:resourceflavors.kueue.x-k8s.io", result["blockers"])

    def test_non_storage_v1beta2_blocks(self):
        snapshot = valid_snapshot()
        snapshot["items"][0] = crd("resourceflavors", storage=False)
        result = preflight.audit_crds(snapshot)
        self.assertIn("v1beta2_not_storage_version:resourceflavors.kueue.x-k8s.io", result["blockers"])

    def test_missing_stored_versions_is_not_treated_as_clean(self):
        snapshot = valid_snapshot()
        del snapshot["items"][0]["status"]
        result = preflight.audit_crds(snapshot)
        self.assertIn("unreadable_stored_versions:resourceflavors.kueue.x-k8s.io", result["blockers"])

    def test_malformed_snapshot_is_blocked(self):
        self.assertEqual(preflight.audit_crds({"items": {}})["result"], "BLOCKED")
        self.assertEqual(preflight.audit_crds(None)["result"], "BLOCKED")

    def test_duplicate_crd_names_are_blocked(self):
        snapshot = valid_snapshot()
        snapshot["items"].append(copy.deepcopy(snapshot["items"][0]))
        result = preflight.audit_crds(snapshot)
        self.assertIn("duplicate_crd:resourceflavors.kueue.x-k8s.io", result["blockers"])

    def test_live_read_only_uses_explicit_context(self):
        with patch.object(preflight.subprocess, "run", return_value=subprocess.CompletedProcess(
            args=[], returncode=0, stdout=json.dumps(valid_snapshot()), stderr=""
        )) as run:
            result = preflight.load_snapshot(context="staging-verified")
        self.assertEqual(result["kind"], "List")
        args = run.call_args.args[0]
        self.assertEqual(args, [
            "kubectl", "--context", "staging-verified", "get",
            "customresourcedefinitions.apiextensions.k8s.io", "-o", "json",
        ])
        self.assertEqual(run.call_args.kwargs["timeout"], 30)

    def test_missing_explicit_context_cannot_access_live_cluster(self):
        with patch.object(preflight.subprocess, "run") as run:
            with self.assertRaises(preflight.PreflightReadError):
                preflight.load_snapshot()
        run.assert_not_called()

    def test_kubectl_permission_failure_is_blocked(self):
        with patch.object(preflight.subprocess, "run", return_value=subprocess.CompletedProcess(
            args=[], returncode=1, stdout="", stderr="token=should-not-leak"
        )):
            with self.assertRaisesRegex(preflight.PreflightReadError, "kubectl_read_failed_exit_1") as err:
                preflight.load_snapshot(context="prod")
        self.assertNotIn("should-not-leak", str(err.exception))

    def test_offline_snapshot_cli_writes_evidence_and_nonzero_on_stale_version(self):
        snapshot = valid_snapshot()
        snapshot["items"][1]["status"]["storedVersions"] = ["v1beta1", "v1beta2"]
        with tempfile.TemporaryDirectory() as directory:
            source = pathlib.Path(directory) / "crds.json"
            report_file = pathlib.Path(directory) / "report.json"
            source.write_text(json.dumps(snapshot), encoding="utf-8")
            with patch.object(preflight.subprocess, "run") as run:
                with contextlib.redirect_stdout(io.StringIO()):
                    code = preflight.main(["--snapshot", str(source), "--report-file", str(report_file)])
            run.assert_not_called()
            self.assertEqual(code, 2)
            report = json.loads(report_file.read_text(encoding="utf-8"))
            self.assertEqual(report["result"], "BLOCKED")
            self.assertEqual(report["source"], "offline_snapshot")


if __name__ == "__main__":
    unittest.main()
