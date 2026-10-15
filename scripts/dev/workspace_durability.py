#!/usr/bin/env python3
"""Explicit two-Job PVC byte-retention experiment on an existing Kubernetes cluster.

Requires an existing Bound PVC and a trusted pinned POSIX-shell/sha256sum image.
No PVC creation, patching, deleting, model serving or GPU admission occurs.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import secrets
import subprocess
import sys
from pathlib import Path
from typing import Any

from apply_workload_draft import InvalidDraft, load_draft
from workspace_pvc import manifest as expected_pvc, observed_bound, CONTEXT

IMAGE = re.compile(r"^[^\s\x00-\x1f]+@sha256:[0-9a-f]{64}$")
TIMEOUT_MIN, TIMEOUT_MAX = 30, 1800


def kubectl(args: argparse.Namespace, tokens: list[str],
            payload: dict[str, Any] | None = None, timeout: int = 30) -> str:
    command = ["kubectl", "--context", args.context, "--namespace", args.namespace, *tokens]
    content = None if payload is None else json.dumps(payload, separators=(",", ":"))
    try:
        result = subprocess.run(command, input=content, text=True,
                                capture_output=True, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise InvalidDraft(f"kubectl operation did not complete: {type(exc).__name__}") from exc
    if result.returncode != 0:
        # No raw provider stderr or potentially sensitive Pod output.
        raise InvalidDraft(f"kubectl operation failed (exit {result.returncode})")
    if len(result.stdout) > 256 * 1024:
        raise InvalidDraft("kubectl response exceeds 256 KiB")
    return result.stdout


def obj(args: argparse.Namespace, tokens: list[str],
        payload: dict[str, Any] | None = None, timeout: int = 30) -> dict[str, Any]:
    try:
        result = json.loads(kubectl(args, tokens, payload, timeout))
    except (ValueError, UnicodeError) as exc:
        raise InvalidDraft("kubectl result must be JSON") from exc
    if not isinstance(result, dict):
        raise InvalidDraft("kubectl result must be an object")
    return result


def pvc_uid(args: argparse.Namespace, wanted: dict[str, Any],
            initial: str | None = None) -> str:
    observed = obj(args, ["get", "pvc", wanted["metadata"]["name"], "-o", "json"])
    valid, reason = observed_bound(observed, wanted)
    if not valid:
        raise InvalidDraft(f"PVC not usable: {reason}")
    uid = observed["metadata"]["uid"]
    if not isinstance(uid, str) or not uid:
        raise InvalidDraft("PVC UID missing")
    if initial is not None and uid != initial:
        raise InvalidDraft("PVC UID changed between writer and reader")
    return uid


def workload_job(args: argparse.Namespace, name: str, claim: str, path: str,
                 value: str, digest: str, reader: bool) -> dict[str, Any]:
    shell_script = (
        'test -f "$CHECKPOINT_PATH"; '
        'printf "%s  %s\n" "$EXPECTED_SHA256" "$CHECKPOINT_PATH" | sha256sum -c -; '
        'sha256sum "$CHECKPOINT_PATH"'
        if reader else
        'mkdir -p "$(dirname "$CHECKPOINT_PATH")"; '
        'printf "%s" "$CHECKPOINT_VALUE" > "$CHECKPOINT_PATH"; '
        'printf "%s  %s\n" "$EXPECTED_SHA256" "$CHECKPOINT_PATH" | sha256sum -c -'
    )
    env = [
        {"name": "CHECKPOINT_PATH", "value": path},
        {"name": "EXPECTED_SHA256", "value": digest},
    ]
    if not reader:
        env.append({"name": "CHECKPOINT_VALUE", "value": value})
    return {
        "apiVersion": "batch/v1", "kind": "Job",
        "metadata": {
            "name": name, "namespace": args.namespace,
            "labels": {"app.kubernetes.io/part-of": "gpu-compute-platform",
                       "ai.compute/durability-check": "true"},
        },
        "spec": {
            "backoffLimit": 0,
            "template": {"spec": {
                "restartPolicy": "Never", "automountServiceAccountToken": False,
                "securityContext": {"runAsNonRoot": True, "runAsUser": 10001,
                                    "runAsGroup": 10001, "fsGroup": 10001,
                                    "seccompProfile": {"type": "RuntimeDefault"}},
                "volumes": [{"name": "workspace", "persistentVolumeClaim": {
                    "claimName": claim}}],
                "containers": [{
                    "name": "probe", "image": args.image,
                    "command": ["/bin/sh", "-ec", shell_script],
                    "env": env,
                    "volumeMounts": [{"name": "workspace", "mountPath": "/workspace"}],
                    "resources": {"requests": {"cpu": "25m", "memory": "32Mi"},
                                  "limits": {"cpu": "250m", "memory": "128Mi"}},
                    "securityContext": {
                        "allowPrivilegeEscalation": False,
                        "readOnlyRootFilesystem": True,
                        "capabilities": {"drop": ["ALL"]},
                    },
                }],
            }},
        },
    }


def create_wait(args: argparse.Namespace, wanted: dict[str, Any]) -> str:
    returned = obj(args, ["create", "-f", "-", "-o", "json"], wanted)
    meta = returned.get("metadata")
    if (returned.get("kind") != "Job" or not isinstance(meta, dict)
        or meta.get("name") != wanted["metadata"]["name"]
        or meta.get("namespace") != args.namespace or not meta.get("uid")):
        raise InvalidDraft("created Job identity does not match request")
    kubectl(args, ["wait", "--for=condition=complete",
                   "job/" + meta["name"], "--timeout=" + str(args.timeout) + "s"],
            timeout=args.timeout + 20)
    return str(meta["uid"])


def delete_job(args: argparse.Namespace, name: str) -> None:
    # Only freshly generated unguessable probe names; never touch PVC.
    kubectl(args, ["delete", "job", name, "--wait=true",
                   "--cascade=foreground", "--timeout=" + str(args.timeout) + "s"],
            timeout=args.timeout + 20)


def run(args: argparse.Namespace, wanted: dict[str, Any]) -> dict[str, Any]:
    uid = pvc_uid(args, wanted)
    ident = secrets.token_hex(7)
    checkpoint_path = "/workspace/.gpu-compute-durability/" + ident + ".bin"
    payload = "gpu-compute-pvc-durability:" + secrets.token_hex(32)
    digest = hashlib.sha256(payload.encode("utf-8")).hexdigest()
    writer = "pvc-write-" + ident
    reader = "pvc-read-" + ident
    wjob = workload_job(args, writer, wanted["metadata"]["name"],
                        checkpoint_path, payload, digest, False)
    rjob = workload_job(args, reader, wanted["metadata"]["name"],
                        checkpoint_path, payload, digest, True)
    wuid = create_wait(args, wjob)
    # The source Job must disappear before the distinct reader Pod starts.
    delete_job(args, writer)
    pvc_uid(args, wanted, uid)
    ruid = create_wait(args, rjob)
    logs = kubectl(args, ["logs", "job/" + reader], timeout=30)
    # The separate reader runs sha256sum -c and prints the independently
    # recomputed digest; do not accept a mere JobComplete condition as proof.
    observed = [line.split()[0] for line in logs.splitlines()
                if len(line.split()) == 2 and
                line.split()[1] == checkpoint_path and
                re.fullmatch(r"[0-9a-f]{64}", line.split()[0])]
    if observed != [digest]:
        raise InvalidDraft("reader did not independently report exact checkpoint SHA-256")
    pvc_uid(args, wanted, uid)
    delete_job(args, reader)
    pvc_uid(args, wanted, uid)
    return {
        "result": "CHECKPOINT_BYTES_RETAINED_AFTER_JOB_DELETION",
        "namespace": args.namespace,
        "projectId": wanted["metadata"]["annotations"]["ai.compute/project-id"],
        "pvc": wanted["metadata"]["name"], "pvcUid": uid,
        "writerJobUid": wuid, "readerJobUid": ruid,
        "sha256": digest, "path": checkpoint_path,
        "scope": "storage-retention-only-not-GPU-or-Kueue-evidence",
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("draft", type=Path)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--storage-class", required=True)
    parser.add_argument("--size", default="20Gi")
    parser.add_argument("--access-mode", default="ReadWriteOnce")
    parser.add_argument("--context", required=True)
    parser.add_argument("--image", required=True,
                        help="digest-pinned shell image with sha256sum and mkdir")
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--execute", action="store_true",
                        help="really create/delete two temporary Jobs; PVC is never deleted")
    args = parser.parse_args(argv)
    try:
        if (not args.context or args.context.startswith("-") or not CONTEXT.fullmatch(args.context)
            or not IMAGE.fullmatch(args.image)
            or not TIMEOUT_MIN <= args.timeout <= TIMEOUT_MAX):
            raise InvalidDraft("invalid context, image digest, or timeout")
        # Reuses existing v0.1 draft and PVC validation; never trusts a
        # client-chosen namespace as proof of the server-side ProjectBinding.
        wanted = expected_pvc(load_draft(args.draft), args)
        if not args.execute:
            print(json.dumps({
                "result": "PLAN_ONLY", "namespace": args.namespace,
                "pvc": wanted["metadata"]["name"],
                "sequence": ["verify Bound PVC UID", "writer Job", "delete writer Job",
                             "recheck PVC UID", "reader Job verifies SHA-256",
                             "delete reader Job", "verify unchanged PVC UID"],
            }, indent=2))
            return 0
        print(json.dumps(run(args, wanted), sort_keys=True))
        return 0
    except (InvalidDraft, ValueError, UnicodeError, OSError) as exc:
        print(f"Workspace durability NOT PROVEN: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
