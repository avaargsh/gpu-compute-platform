from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "verify-attestation-ref.sh"
WORKFLOW = ROOT / ".github" / "workflows" / "go-security-supply-chain.yml"


class ReleaseAttestationRefPolicyTests(unittest.TestCase):
    def run_policy(self, event: str, ref: str, allowed: bool) -> None:
        result = subprocess.run(
            ["bash", str(SCRIPT), event, ref],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(
            result.returncode == 0,
            allowed,
            msg=f"event={event!r} ref={ref!r} rc={result.returncode} stderr={result.stderr!r}",
        )

    def test_manual_attestation_is_allowed_only_from_master(self) -> None:
        self.run_policy("workflow_dispatch", "refs/heads/master", True)
        self.run_policy("workflow_dispatch", "refs/heads/feature", False)

    def test_version_tags_are_allowed_but_other_tags_are_rejected(self) -> None:
        self.run_policy("push", "refs/tags/v0.1.0", True)
        self.run_policy("workflow_dispatch", "refs/tags/v1.2.3", True)
        self.run_policy("push", "refs/tags/preview", False)

    def test_master_push_does_not_trigger_manual_attestation_policy(self) -> None:
        self.run_policy("push", "refs/heads/master", False)

    def test_workflow_job_condition_enforces_trusted_refs(self) -> None:
        source = WORKFLOW.read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^  release-attestation:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:|\Z)",
            source,
        )
        self.assertIsNotNone(match, "release-attestation job must exist")
        condition = re.search(r"(?m)^    if: (.+)$", match.group("body"))
        self.assertIsNotNone(condition, "release-attestation job must have an explicit ref guard")
        self.assertIn("refs/tags/v", condition.group(1))
        self.assertIn("refs/heads/master", condition.group(1))
        self.assertIn("github.event_name == 'workflow_dispatch'", condition.group(1))

    def test_release_attestation_checks_out_source_before_ref_guard(self) -> None:
        source = WORKFLOW.read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^  release-attestation:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:|\Z)",
            source,
        )
        self.assertIsNotNone(match, "release-attestation job must exist")
        body = match.group("body")
        checkout = re.search(r"(?m)^      - uses: actions/checkout@v4$", body)
        guard = re.search(r"(?m)^      - name: Enforce trusted ref before attestation$", body)
        self.assertIsNotNone(checkout, "release-attestation must checkout its own workspace")
        self.assertIsNotNone(guard, "release-attestation must run the trusted-ref guard")
        self.assertLess(checkout.start(), guard.start(), "checkout must precede the local ref guard")


if __name__ == "__main__":
    unittest.main()
