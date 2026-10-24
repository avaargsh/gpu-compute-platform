#!/usr/bin/env python3
"""Read-only Kueue CRD storage-version preflight for the v0.20 upgrade boundary.

This is NOT a storage migration tool and passing is NOT approval to upgrade.
No Kubernetes write verbs or shell commands are issued.
"""

import argparse
import datetime
import json
import pathlib
import subprocess
import sys

GROUP_SUFFIX = ".kueue.x-k8s.io"
TARGET_VERSION = "v1beta2"
CORE_CRDS = frozenset(
    {
        "resourceflavors.kueue.x-k8s.io",
        "clusterqueues.kueue.x-k8s.io",
        "localqueues.kueue.x-k8s.io",
        "workloads.kueue.x-k8s.io",
    }
)


class PreflightReadError(Exception):
    """A read failed, so safe upgrade preparation cannot be established."""


def audit_crds(document):
    """Audit all installed Kueue CRDs without claiming object migration is proven."""
    report = {"result": "BLOCKED", "checked_crds": [], "blockers": []}
    blockers = report["blockers"]
    if not isinstance(document, dict) or not isinstance(document.get("items"), list):
        blockers.append("invalid_crd_list_json")
        return report

    observed = set()
    for item in document["items"]:
        if not isinstance(item, dict):
            blockers.append("malformed_crd_item")
            continue
        metadata = item.get("metadata")
        name = metadata.get("name") if isinstance(metadata, dict) else None
        if not isinstance(name, str) or not name.endswith(GROUP_SUFFIX):
            continue
        if name in observed:
            blockers.append("duplicate_crd:" + name)
            continue
        observed.add(name)
        spec = item.get("spec")
        status = item.get("status")
        versions = spec.get("versions") if isinstance(spec, dict) else None
        stored = status.get("storedVersions") if isinstance(status, dict) else None
        entry = {"name": name, "served_v1beta2": False, "storage_v1beta2": False, "stored_versions": []}
        if isinstance(versions, list) and all(isinstance(v, dict) for v in versions):
            current = [v for v in versions if v.get("name") == TARGET_VERSION]
            entry["served_v1beta2"] = len(current) == 1 and current[0].get("served") is True
            entry["storage_v1beta2"] = (
                len(current) == 1 and current[0].get("storage") is True
                and sum(v.get("storage") is True for v in versions) == 1
            )
        if isinstance(stored, list) and all(isinstance(v, str) for v in stored):
            entry["stored_versions"] = sorted(stored)
        else:
            blockers.append("unreadable_stored_versions:" + name)
        if not entry["served_v1beta2"]:
            blockers.append("v1beta2_not_served:" + name)
        if not entry["storage_v1beta2"]:
            blockers.append("v1beta2_not_storage_version:" + name)
        if entry["stored_versions"] != [TARGET_VERSION]:
            blockers.append("unmigrated_stored_versions:" + name)
        report["checked_crds"].append(entry)

    for missing in sorted(CORE_CRDS - observed):
        blockers.append("missing_core_crd:" + missing)
    if not observed:
        blockers.append("no_kueue_crds_found")
    report["checked_crds"].sort(key=lambda row: row["name"])
    report["blockers"].sort()
    if not blockers:
        report["result"] = "PASS"
    return report


def load_snapshot(*, context=None, snapshot=None):
    """Load a fixture or issue one explicit-context, read-only kubectl request."""
    if snapshot is not None:
        try:
            return json.loads(pathlib.Path(snapshot).read_text(encoding="utf-8"))
        except (OSError, UnicodeError, json.JSONDecodeError) as exc:
            raise PreflightReadError("snapshot_unreadable_or_invalid") from exc
    if not context:
        raise PreflightReadError("explicit_kubernetes_context_required")
    cmd = [
        "kubectl", "--context", context, "get",
        "customresourcedefinitions.apiextensions.k8s.io", "-o", "json",
    ]
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=False, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise PreflightReadError("kubectl_read_unavailable_or_timed_out") from exc
    if result.returncode != 0:
        # Do not echo kubectl stderr; auth provider messages can contain secrets.
        raise PreflightReadError("kubectl_read_failed_exit_" + str(result.returncode))
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise PreflightReadError("kubectl_returned_invalid_json") from exc


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", help="Explicit kubeconfig context to audit (required for live reads)")
    parser.add_argument("--snapshot", help="Offline Kubernetes CRD List JSON; does not contact a cluster")
    parser.add_argument("--report-file", help="Optional JSON evidence output path")
    args = parser.parse_args(argv)

    if args.snapshot and args.context:
        parser.error("--snapshot and --context must not be combined")
    report = {
        "schema_version": 1,
        "checked_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source": "offline_snapshot" if args.snapshot else "live_explicit_context",
        "context": args.context,
    }
    try:
        report.update(audit_crds(load_snapshot(context=args.context, snapshot=args.snapshot)))
    except PreflightReadError as exc:
        report.update({"result": "BLOCKED", "checked_crds": [], "blockers": [str(exc)]})

    output = json.dumps(report, indent=2, sort_keys=True) + "\n"
    print(output, end="")
    if args.report_file:
        try:
            pathlib.Path(args.report_file).write_text(output, encoding="utf-8")
        except OSError:
            print("Cannot write evidence report; preflight blocked.", file=sys.stderr)
            return 2
    return 0 if report["result"] == "PASS" else 2


if __name__ == "__main__":
    sys.exit(main())
