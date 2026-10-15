#!/usr/bin/env python3
"""Developer-only Workspace PVC bootstrap from a Portal Workload draft.

Plan without contacting Kubernetes; explicit create-only and status checks
never mutate an existing claim or delete a PVC.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path
from typing import Any

from apply_workload_draft import InvalidDraft, load_draft

DNS_LABEL = re.compile(r"^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$")
PROJECT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
SIZE = re.compile(r"^[1-9][0-9]{0,3}(?:Mi|Gi|Ti)$")
CONTEXT = re.compile(r"^[^\s\x00-\x1f]{1,255}$")
CLASS = re.compile(r"^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$")
ACCESS = ("ReadWriteOnce", "ReadWriteMany", "ReadWriteOncePod")


def manifest(draft: dict[str, Any], args: argparse.Namespace) -> dict[str, Any]:
    if len(args.namespace) > 63 or not DNS_LABEL.fullmatch(args.namespace):
        raise InvalidDraft("namespace must be a Kubernetes DNS label")
    if not SIZE.fullmatch(args.size):
        raise InvalidDraft("size must be 1..9999 Mi/Gi/Ti, e.g. 20Gi")
    if len(args.storage_class) > 253 or not CLASS.fullmatch(args.storage_class):
        raise InvalidDraft("storage-class must be a valid Kubernetes DNS subdomain")
    if args.access_mode not in ACCESS:
        raise InvalidDraft("unsupported access mode")
    if args.create or args.status:
        if not args.context or args.context.startswith("-") or not CONTEXT.fullmatch(args.context):
            raise InvalidDraft("--context must explicitly select the Kubernetes cluster")
    project = draft.get("projectId")
    if not isinstance(project, str) or not PROJECT.fullmatch(project):
        raise InvalidDraft("invalid projectId")
    workspace = draft["spec"].get("workspace")
    if not isinstance(workspace, dict) or not workspace.get("claimName"):
        raise InvalidDraft("Workload draft must contain workspace.claimName")
    return {
        "apiVersion": "v1",
        "kind": "PersistentVolumeClaim",
        "metadata": {
            "name": workspace["claimName"], "namespace": args.namespace,
            "annotations": {"ai.compute/project-id": project},
            "labels": {"app.kubernetes.io/part-of": "gpu-compute-platform"},
        },
        "spec": {
            "accessModes": [args.access_mode],
            "storageClassName": args.storage_class,
            "resources": {"requests": {"storage": args.size}},
        },
    }


def kubectl(args: argparse.Namespace, command: list[str],
            payload: dict[str, Any] | None = None) -> dict[str, Any]:
    argv = ["kubectl", "--context", args.context,
            "--namespace", args.namespace, *command]
    body = None if payload is None else json.dumps(payload, separators=(",", ":"))
    try:
        result = subprocess.run(argv, input=body, text=True,
                                capture_output=True, timeout=25, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise InvalidDraft(f"kubectl could not complete: {type(exc).__name__}") from exc
    if result.returncode != 0:
        # Never echo provider stderr, which may expose credentials/paths.
        raise InvalidDraft(f"kubectl exited with status {result.returncode}")
    if len(result.stdout) > 256 * 1024:
        raise InvalidDraft("oversized kubectl response")
    try:
        obj = json.loads(result.stdout)
    except (ValueError, UnicodeError) as exc:
        raise InvalidDraft("kubectl response is not JSON") from exc
    if not isinstance(obj, dict):
        raise InvalidDraft("kubectl response must be an object")
    return obj


def observed_bound(obj: dict[str, Any], wanted: dict[str, Any]) -> tuple[bool, str]:
    meta, spec, status = obj.get("metadata"), obj.get("spec"), obj.get("status")
    if (obj.get("apiVersion") != "v1" or obj.get("kind") != "PersistentVolumeClaim"
        or not isinstance(meta, dict) or not isinstance(spec, dict)
        or meta.get("name") != wanted["metadata"]["name"]
        or meta.get("namespace") != wanted["metadata"]["namespace"]
        or not meta.get("uid") or meta.get("deletionTimestamp")
        or not isinstance(meta.get("annotations"), dict)
        or meta["annotations"].get("ai.compute/project-id") !=
           wanted["metadata"]["annotations"]["ai.compute/project-id"]
        or spec.get("storageClassName") != wanted["spec"]["storageClassName"]
        or not isinstance(spec.get("accessModes"), list)
        or wanted["spec"]["accessModes"][0] not in spec["accessModes"]):
        return False, "PVCIdentityOrPolicyMismatch"
    if not isinstance(status, dict) or status.get("phase") != "Bound":
        return False, "PVCNotBound"
    return True, "PVCBoundOnly"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("draft", type=Path, help="Portal Workload draft with workspace.claimName")
    parser.add_argument("--namespace", required=True, help="ProjectBinding Kubernetes namespace")
    parser.add_argument("--storage-class", required=True, help="existing Kubernetes StorageClass")
    parser.add_argument("--size", default="20Gi", help="PVC requested size")
    parser.add_argument("--access-mode", choices=ACCESS, default="ReadWriteOnce")
    parser.add_argument("--context", help="explicit kubectl cluster context for create/status")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--create", action="store_true", help="create only; never replace existing PVC")
    mode.add_argument("--status", action="store_true", help="read PVC identity and Bound phase")
    args = parser.parse_args(argv)
    try:
        expected = manifest(load_draft(args.draft), args)
        if not args.create and not args.status:
            print(json.dumps(expected, indent=2))
            print("PLAN ONLY: no Kubernetes call; namespace binding is not independently verified.",
                  file=sys.stderr)
            return 0
        claim = expected["metadata"]["name"]
        obj = (kubectl(args, ["create", "-f", "-", "-o", "json"], expected)
               if args.create else kubectl(args, ["get", "pvc", claim, "-o", "json"]))
        ready, reason = observed_bound(obj, expected)
        print(json.dumps({
            "projectId": expected["metadata"]["annotations"]["ai.compute/project-id"],
            "namespace": args.namespace, "claimName": claim,
            "status": reason, "bound": ready,
            "operation": "create" if args.create else "status",
        }))
        return 0 if ready else 1
    except (InvalidDraft, ValueError, UnicodeError, OSError) as exc:
        print(f"Workspace PVC blocked: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
