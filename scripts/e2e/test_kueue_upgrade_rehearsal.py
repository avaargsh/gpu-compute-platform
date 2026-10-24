"""Safety gates for the opt-in, ephemeral Kueue in-place upgrade rehearsal."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("kueue_inplace_upgrade_kind.sh")


class KueueUpgradeRehearsalTests(unittest.TestCase):
    def run_guard(self, *, opt_in="YES", cluster="kueue-upgrade-test-001", node="kindest/node:v1.34.0"):
        with tempfile.TemporaryDirectory() as temp:
            evidence = Path(temp) / "evidence"
            env = os.environ.copy()
            env.update({
                "ALLOW_EPHEMERAL_KUEUE_UPGRADE": opt_in,
                "CLUSTER_ID": cluster,
                "KIND_NODE_IMAGE": node,
                "EVIDENCE_DIR": str(evidence),
            })
            run = subprocess.run(["bash", str(SCRIPT)], capture_output=True, text=True, env=env, timeout=10)
            self.assertFalse(evidence.exists(), "a denied invocation must not allocate an evidence directory")
            return run

    def test_requires_an_explicit_opt_in_before_any_cluster_operations(self):
        result = self.run_guard(opt_in="NO")
        self.assertEqual(result.returncode, 2)
        self.assertIn("explicit opt-in", result.stderr)

    def test_refuses_non_ephemeral_cluster_names(self):
        for name in ("prod", "kind-golden", "kueue-upgrade-", "kueue-upgrade-PROD"):
            with self.subTest(name=name):
                result = self.run_guard(cluster=name)
                self.assertEqual(result.returncode, 2)
                self.assertIn("disposable cluster name", result.stderr)

    def test_disallows_unqualified_kubernetes_node_image(self):
        result = self.run_guard(node="kindest/node:v1.35.8")
        self.assertEqual(result.returncode, 2)
        self.assertIn("frozen node image only", result.stderr)

    def test_invalid_legacy_fixture_mode_fails_before_any_mutation(self):
        with tempfile.TemporaryDirectory() as temp:
            evidence = Path(temp) / "evidence"
            env = os.environ.copy()
            env.update({
                "ALLOW_EPHEMERAL_KUEUE_UPGRADE": "YES",
                "LEGACY_ETCD_FIXTURE": "NOPE",
                "CLUSTER_ID": "kueue-upgrade-test-001",
                "KIND_NODE_IMAGE": "kindest/node:v1.34.0",
                "EVIDENCE_DIR": str(evidence),
            })
            p = subprocess.run(["bash", str(SCRIPT)], capture_output=True, text=True, env=env, timeout=10)
            self.assertEqual(p.returncode, 2)
            self.assertIn("LEGACY_ETCD_FIXTURE must be YES or NO", p.stderr)
            self.assertFalse(evidence.exists())

    def test_real_legacy_fixture_proof_must_bracket_official_migration(self):
        data = SCRIPT.read_text()
        self.assertLess(data.index('probe_disposable_etcd v1beta1'),
                        data.index('bash "$tmp/migrate.sh"'))
        self.assertLess(data.index('bash "$tmp/migrate.sh"'),
                        data.index('probe_disposable_etcd v1beta2'))
        self.assertLess(data.index('probe_disposable_etcd v1beta2'),
                        data.index('storedVersions":["v1beta2"]'))
        self.assertIn('--expected-version "$expected"', data)
        self.assertIn('LEGACY_ETCD_FIXTURE:-NO', data)

    def test_shell_syntax_is_valid(self):
        check = subprocess.run(["bash", "-n", str(SCRIPT)], capture_output=True, text=True, timeout=5)
        self.assertEqual(check.returncode, 0, check.stderr)

    def test_embedded_migration_is_from_pinned_official_commit(self):
        data = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("f0e95cea51b5591e0177fbe0fa06a299f36db548", data)
        self.assertIn("ede30226c5521585aa9aefc261ffb69af88afd38", data)
        self.assertLess(data.index('bash "$tmp/migrate.sh"'), data.index("releases/download/v0.20.3/manifests.yaml"))
        self.assertIn('KUBECONFIG="$tmp/kubeconfig"', data)
        self.assertIn('kind get clusters | grep -Fxq "$CLUSTER_ID"', data)


if __name__ == "__main__":
    unittest.main()
