"""Mocked kubectl contract tests; no cluster required."""
from __future__ import annotations

import argparse
import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import workspace_pvc as ws  # noqa: E402

DRAFT = {
    "metadata": {"id": "train-1", "generation": 1},
    "projectId": "research", "poolId": "pool-a",
    "spec": {
        "image": "example/train:v1", "command": ["python", "train.py"],
        "accelerator": {"class": "h100", "quota": 1},
        "workspace": {"claimName": "research-pvc"},
    },
}


def settings(**overrides):
    values = dict(namespace="research-ns", storage_class="standard", size="20Gi",
                  access_mode="ReadWriteOnce", context="kind-gpu-dev",
                  create=False, status=False)
    values.update(overrides)
    return argparse.Namespace(**values)


def bound(wanted):
    obj = copy.deepcopy(wanted)
    obj["metadata"]["uid"] = "pvc-uid-123"
    obj["status"] = {"phase": "Bound"}
    return obj


class WorkspaceTests(unittest.TestCase):
    def fixture(self, content=DRAFT):
        file = tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False)
        json.dump(content, file)
        file.close()
        self.addCleanup(lambda: Path(file.name).unlink(missing_ok=True))
        return file.name

    def flags(self, *extra):
        return [self.fixture(), "--namespace", "research-ns",
                "--storage-class", "standard", *extra]

    def test_plan_has_no_kube_side_effects(self):
        with patch.object(ws.subprocess, "run") as call:
            self.assertEqual(ws.main(self.flags()), 0)
        call.assert_not_called()

    def test_manifest_uses_draft_project_and_claim(self):
        obj = ws.manifest(DRAFT, settings())
        self.assertEqual(obj["metadata"]["name"], "research-pvc")
        self.assertEqual(obj["metadata"]["namespace"], "research-ns")
        self.assertEqual(obj["metadata"]["annotations"]["ai.compute/project-id"], "research")
        self.assertEqual(obj["spec"]["resources"]["requests"]["storage"], "20Gi")

    def test_create_only_no_apply_replace_or_delete(self):
        wanted = ws.manifest(DRAFT, settings())
        result = CompletedProcess([], 0, json.dumps(bound(wanted)), "")
        with patch.object(ws.subprocess, "run", return_value=result) as call:
            self.assertEqual(ws.main(self.flags("--context", "kind-gpu-dev", "--create")), 0)
        self.assertEqual(call.call_args.args[0], [
            "kubectl", "--context", "kind-gpu-dev", "--namespace", "research-ns",
            "create", "-f", "-", "-o", "json",
        ])
        self.assertEqual(json.loads(call.call_args.kwargs["input"]), wanted)

    def test_status_reads_exact_claim(self):
        wanted = ws.manifest(DRAFT, settings())
        result = CompletedProcess([], 0, json.dumps(bound(wanted)), "")
        with patch.object(ws.subprocess, "run", return_value=result) as call:
            self.assertEqual(ws.main(self.flags("--context", "kind-gpu-dev", "--status")), 0)
        self.assertEqual(call.call_args.args[0][-4:], [
            "pvc", "research-pvc", "-o", "json",
        ])

    def test_foreign_pending_deleting_and_mismatched_not_ready(self):
        wanted = ws.manifest(DRAFT, settings())
        for change in [
            lambda o: o["status"].update(phase="Pending"),
            lambda o: o["metadata"].update(namespace="foreign"),
            lambda o: o["metadata"].pop("uid"),
            lambda o: o["metadata"].update(deletionTimestamp="2026-10-15T00:00:00Z"),
            lambda o: o["metadata"]["annotations"].update({"ai.compute/project-id": "other"}),
            lambda o: o["spec"].update(storageClassName="foreign"),
        ]:
            obj = bound(wanted)
            change(obj)
            self.assertFalse(ws.observed_bound(obj, wanted)[0])

    def test_rejects_bad_namespace_size_and_missing_context(self):
        for kw in [
            dict(namespace="../other"), dict(size="0Gi"), dict(size="100000Gi"),
            dict(storage_class="bad name"), dict(create=True, context=None),
        ]:
            with self.subTest(kw=kw), self.assertRaises(ws.InvalidDraft):
                ws.manifest(DRAFT, settings(**kw))

    def test_rejects_missing_workspace(self):
        obj = copy.deepcopy(DRAFT)
        obj["spec"].pop("workspace")
        with self.assertRaises(ws.InvalidDraft):
            ws.manifest(obj, settings())

    def test_kube_error_is_not_successful(self):
        with patch.object(ws.subprocess, "run",
                          return_value=CompletedProcess([], 1, "", "secret token")):
            self.assertEqual(ws.main(self.flags("--context", "kind-gpu-dev", "--create")), 2)


if __name__ == "__main__":
    unittest.main()
