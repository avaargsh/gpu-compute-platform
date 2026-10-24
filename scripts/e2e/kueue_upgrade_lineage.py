#!/usr/bin/env python3
"""Classify the storage evidence of a disposable Kueue upgrade rehearsal.

Kueue v0.19.6 serves v1beta1 API but normally stores objects as v1beta2.
A passed upgrade of v1beta1 *requests* does not demonstrate that legacy
v1beta1 *etcd storage* was migrated. This checker records that distinction.
"""

import argparse
import json
from pathlib import Path
import sys

SUFFIX = ".kueue.x-k8s.io"
CORE = frozenset(
    n + SUFFIX for n in ("resourceflavors", "clusterqueues", "localqueues", "workloads")
)


def _versions(snapshot):
    if not isinstance(snapshot, dict) or not isinstance(snapshot.get("items"), list):
        raise ValueError("invalid_crd_list")
    result = {}
    for item in snapshot["items"]:
        if not isinstance(item, dict):
            raise ValueError("invalid_crd_item")
        name = item.get("metadata", {}).get("name")
        if not isinstance(name, str) or not name.endswith(SUFFIX):
            continue
        if name in result:
            raise ValueError("duplicate_crd:" + name)
        status = item.get("status", {})
        stored = status.get("storedVersions")
        if not isinstance(stored, list) or not stored or any(
            not isinstance(x, str) for x in stored
        ):
            raise ValueError("invalid_stored_versions:" + name)
        result[name] = stored
    return result


def classify(before, after, *, require_legacy=False):
    """Evaluate CRD status lineage; never infer raw persisted-object versions."""
    report = {
        "result": "BLOCKED",
        "legacy_crd_status_markers": [],
        "legacy_stored_object_rewrite": "not_assessed",
        "rollback_compatibility": "not_assessed",
        "mixed_version_rollout": "not_assessed",
        "blockers": [],
    }
    try:
        original = _versions(before)
        current = _versions(after)
    except (TypeError, AttributeError, ValueError) as exc:
        report["blockers"].append(str(exc))
        return report
    for name in sorted(CORE):
        if name not in original or name not in current:
            report["blockers"].append("core_crd_missing:" + name)
    for name, versions in sorted(original.items()):
        if "v1beta1" in versions:
            report["legacy_crd_status_markers"].append(name)
        if name not in current:
            report["blockers"].append("crd_removed:" + name)
    for name, versions in sorted(current.items()):
        if versions != ["v1beta2"]:
            report["blockers"].append("post_storage_versions_not_v1beta2_only:" + name)
    report["scenario"] = (
        "legacy_crd_status_marker_observed"
        if report["legacy_crd_status_markers"]
        else "no_legacy_crd_status_marker_observed"
    )
    if require_legacy and not report["legacy_crd_status_markers"]:
        report["blockers"].append("requested_legacy_storage_scenario_not_observed")
    report["blockers"].sort()
    if not report["blockers"]:
        report["result"] = "PASS"
    return report


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--evidence-dir", type=Path, required=True)
    p.add_argument(
        "--require-legacy", action="store_true",
        help="Block if no CRD reported a legacy v1beta1 storedVersions marker",
    )
    args = p.parse_args(argv)
    try:
        before = json.loads((args.evidence_dir / "crds-before.json").read_text())
        after = json.loads((args.evidence_dir / "crds-after.json").read_text())
        result = classify(before, after, require_legacy=args.require_legacy)
    except (OSError, UnicodeError, ValueError) as exc:
        result = {
            "result": "BLOCKED",
            "blockers": ["unreadable_or_invalid_crd_evidence"],
            "legacy_stored_object_rewrite": "not_assessed",
            "rollback_compatibility": "not_assessed",
            "mixed_version_rollout": "not_assessed",
        }
    output = json.dumps(result, indent=2, sort_keys=True) + "\n"
    print(output, end="")
    try:
        (args.evidence_dir / "storage-lineage.json").write_text(output)
    except OSError:
        print("Cannot write lineage evidence", file=sys.stderr)
        return 2
    return 0 if result["result"] == "PASS" else 2


if __name__ == "__main__":
    sys.exit(main())
