"""Storage evidence cannot be misrepresented as proof of legacy etcd rewrite."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from kueue_upgrade_lineage import classify, main


def snapshot(legacy=False):
    return {
        "items": [
            {
                "metadata": {"name": n + ".kueue.x-k8s.io"},
                "status": {
                    "storedVersions": ["v1beta1", "v1beta2"] if legacy else ["v1beta2"]
                },
            }
            for n in ("resourceflavors", "clusterqueues", "localqueues", "workloads")
        ]
    }


class StorageLineageTests(unittest.TestCase):
    def test_current_baseline_upgrade_must_not_claim_legacy_migration(self):
        r = classify(snapshot(), snapshot())
        self.assertEqual(r["result"], "PASS")
        self.assertEqual(r["scenario"], "no_legacy_crd_status_marker_observed")
        self.assertEqual(r["legacy_stored_object_rewrite"], "not_assessed")
        self.assertEqual(r["rollback_compatibility"], "not_assessed")

    def test_require_legacy_blocks_current_baseline(self):
        r = classify(snapshot(), snapshot(), require_legacy=True)
        self.assertEqual(r["result"], "BLOCKED")
        self.assertIn("requested_legacy_storage_scenario_not_observed", r["blockers"])

    def test_legacy_metadata_marker_does_not_claim_etcd_proof(self):
        r = classify(snapshot(legacy=True), snapshot(), require_legacy=True)
        self.assertEqual(r["result"], "PASS")
        self.assertEqual(len(r["legacy_crd_status_markers"]), 4)
        self.assertEqual(r["legacy_stored_object_rewrite"], "not_assessed")

    def test_post_upgrade_stale_version_blocks(self):
        r = classify(snapshot(legacy=True), snapshot(legacy=True))
        self.assertEqual(r["result"], "BLOCKED")
        self.assertTrue(any("post_storage_versions_not_v1beta2_only" in e for e in r["blockers"]))

    def test_missing_core_crd_blocks(self):
        before = snapshot()
        before["items"].pop()
        r = classify(before, snapshot())
        self.assertEqual(r["result"], "BLOCKED")
        self.assertTrue(any("core_crd_missing:workloads" in e for e in r["blockers"]))

    def test_duplicate_crds_block(self):
        before = snapshot()
        before["items"].append(dict(before["items"][0]))
        self.assertEqual(classify(before, snapshot())["result"], "BLOCKED")

    def test_cli_preserves_machine_readable_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "crds-before.json").write_text(json.dumps(snapshot()))
            (d / "crds-after.json").write_text(json.dumps(snapshot()))
            self.assertEqual(main(["--evidence-dir", str(d)]), 0)
            report = json.loads((d / "storage-lineage.json").read_text())
            self.assertEqual(report["scenario"], "no_legacy_crd_status_marker_observed")


if __name__ == "__main__":
    unittest.main()
