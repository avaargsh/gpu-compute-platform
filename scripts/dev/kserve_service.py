#!/usr/bin/env python3
"""Developer-only KServe custom predictor deployment from a strict JSON artifact.

Plan is offline; explicit create uses kubectl CREATE only, status uses GET only.
No control-plane Serving ownership, endpoint readiness or inference is implied.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path
from typing import Any

DNS = re.compile(r"^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$")
LABEL = re.compile(r"^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$")
PROJECT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
IMAGE = re.compile(r"^[^\s\x00-\x1f]+@sha256:[0-9a-f]{64}$")
CTX = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.:@/-]{0,127}$")
MAX_JSON = 64 * 1024


class InvalidServing(ValueError):
    pass


def unique_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise InvalidServing("duplicate JSON key")
        result[key] = value
    return result


def load(path: Path) -> dict[str, Any]:
    raw = path.read_bytes()
    if len(raw) > MAX_JSON:
        raise InvalidServing("Serving artifact exceeds 64 KiB")
    source = json.loads(raw, object_pairs_hook=unique_keys)
    if not isinstance(source, dict):
        raise InvalidServing("Serving artifact must be a JSON object")
    return source


def validated(source: dict[str, Any]) -> tuple[str, str, str]:
    if set(source) != {"apiVersion", "kind", "metadata", "spec"} or \
       source["apiVersion"] != "serving.kserve.io/v1beta1" or source["kind"] != "InferenceService":
        raise InvalidServing("expected KServe v1beta1 InferenceService manifest")
    meta, spec = source["metadata"], source["spec"]
    if not isinstance(meta, dict) or set(meta) != {"name", "namespace", "annotations"}:
        raise InvalidServing("metadata must contain name, namespace and project annotation")
    name, namespace, notes = meta["name"], meta["namespace"], meta["annotations"]
    if not isinstance(name, str) or len(name) > 253 or not DNS.fullmatch(name) or \
       not isinstance(namespace, str) or len(namespace) > 63 or not LABEL.fullmatch(namespace):
        raise InvalidServing("invalid Kubernetes Serving name or namespace")
    if not isinstance(notes, dict) or set(notes) != {"ai.compute/project-id"}:
        raise InvalidServing("project annotation required")
    project = notes["ai.compute/project-id"]
    if not isinstance(project, str) or not PROJECT.fullmatch(project):
        raise InvalidServing("invalid declared project")
    if not isinstance(spec, dict) or set(spec) != {"predictor"}:
        raise InvalidServing("only a single custom predictor is supported")
    predictor = spec["predictor"]
    if not isinstance(predictor, dict) or set(predictor) != {"minReplicas", "maxReplicas", "containers"} or \
       predictor["minReplicas"] != 1 or predictor["maxReplicas"] != 1 or \
       not isinstance(predictor["containers"], list) or len(predictor["containers"]) != 1:
        raise InvalidServing("v0.1 supports one custom predictor replica only")
    c = predictor["containers"][0]
    if not isinstance(c, dict) or set(c) != {"name", "image", "args", "ports", "resources"} or \
       c["name"] != "kserve-container" or not isinstance(c["image"], str) or not IMAGE.fullmatch(c["image"]):
        raise InvalidServing("custom predictor requires digest-pinned kserve-container")
    if not isinstance(c["args"], list) or len(c["args"]) > 48 or \
       not all(isinstance(a, str) and 0 < len(a) <= 2048 and "\0" not in a for a in c["args"]):
        raise InvalidServing("args must be a bounded argv string array")
    if c["ports"] != [{"containerPort": 8000, "protocol": "TCP"}]:
        raise InvalidServing("v0.1 serving only exposes TCP 8000")
    resources = c["resources"]
    if not isinstance(resources, dict) or set(resources) != {"requests", "limits"} or \
       resources["requests"] != resources["limits"]:
        raise InvalidServing("requests and limits must match")
    expected_keys = {"cpu", "memory", "nvidia.com/gpu"}
    limits = resources["limits"]
    if not isinstance(limits, dict) or set(limits) != expected_keys or \
       limits["cpu"] != "1" or limits["memory"] != "16Gi" or \
       type(limits["nvidia.com/gpu"]) is not int or not 1 <= limits["nvidia.com/gpu"] <= 8:
        raise InvalidServing("only explicit 1 CPU, 16Gi memory and 1..8 whole GPUs are supported")
    return name, namespace, project


def kubectl(context: str, namespace: str, cmd: list[str],
            payload: dict[str, Any] | None = None) -> dict[str, Any]:
    argv = ["kubectl", "--context", context, "--namespace", namespace, *cmd]
    body = None if payload is None else json.dumps(payload, separators=(",", ":"))
    try:
        result = subprocess.run(argv, input=body, text=True, capture_output=True,
                                timeout=25, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise InvalidServing("kubectl failed: " + type(exc).__name__) from exc
    if result.returncode != 0:
        raise InvalidServing("kubectl failed (exit " + str(result.returncode) + ")")
    if len(result.stdout) > 256 * 1024:
        raise InvalidServing("Kubernetes response too large")
    try:
        value = json.loads(result.stdout)
    except ValueError as exc:
        raise InvalidServing("kubectl result is not JSON") from exc
    if not isinstance(value, dict):
        raise InvalidServing("kubectl result must be an object")
    return value


def observation(obj: dict[str, Any], expected: dict[str, Any]) -> dict[str, Any]:
    name, namespace, project = validated(expected)
    meta = obj.get("metadata")
    if obj.get("apiVersion") != expected["apiVersion"] or obj.get("kind") != "InferenceService" or \
       not isinstance(meta, dict) or meta.get("name") != name or meta.get("namespace") != namespace or \
       not meta.get("uid") or not meta.get("resourceVersion") or meta.get("deletionTimestamp") or \
       not isinstance(meta.get("annotations"), dict) or \
       meta["annotations"].get("ai.compute/project-id") != project:
        return {"status": "IDENTITY_MISMATCH", "reportedReady": False}
    spec = obj.get("spec")
    if not isinstance(spec, dict) or spec.get("predictor") != expected["spec"]["predictor"]:
        return {"status": "SPEC_DRIFT", "reportedReady": False}
    status = obj.get("status")
    conditions = status.get("conditions") if isinstance(status, dict) else None
    if not isinstance(conditions, list):
        return {"status": "STATUS_UNKNOWN", "reportedReady": False}
    ready = [c.get("status") for c in conditions if isinstance(c, dict) and c.get("type") == "Ready"]
    if len(ready) != 1 or ready[0] not in {"True", "False", "Unknown"}:
        return {"status": "READY_CONDITION_UNKNOWN", "reportedReady": False}
    return {
        "status": "CONTROLLER_REPORTED_READY_NOT_HTTP_PROVEN" if ready[0] == "True"
        else "CONTROLLER_NOT_READY",
        "reportedReady": ready[0] == "True", "uid": meta["uid"],
        "generation": meta.get("generation"), "reason": next(
            (c.get("reason", "") for c in conditions
             if isinstance(c, dict) and c.get("type") == "Ready"), ""),
    }


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("manifest", type=Path)
    p.add_argument("--context")
    mode = p.add_mutually_exclusive_group()
    mode.add_argument("--create", action="store_true", help="CREATE only, never apply/patch/delete")
    mode.add_argument("--status", action="store_true", help="GET KServe object and report controller status")
    args = p.parse_args(argv)
    try:
        source = load(args.manifest)
        name, namespace, project = validated(source)
        if not args.create and not args.status:
            print(json.dumps({"status": "PLAN_ONLY", "name": name,
                              "namespace": namespace, "projectId": project,
                              "manifest": source}, indent=2))
            return 0
        if not args.context or not CTX.fullmatch(args.context):
            raise InvalidServing("--context must explicitly name a local Kubernetes context")
        actual = kubectl(args.context, namespace,
                         ["create", "-f", "-", "-o", "json"] if args.create
                         else ["get", "inferenceservices.serving.kserve.io", name, "-o", "json"],
                         source if args.create else None)
        verdict = observation(actual, source)
        # A newly created KServe object can legitimately have no status yet.
        # A matching observed identity and predictor are enough to report
        # CREATE acceptance, but absolutely not a ready model endpoint.
        if args.create and verdict["status"] in {
            "STATUS_UNKNOWN", "READY_CONDITION_UNKNOWN", "CONTROLLER_NOT_READY",
            "CONTROLLER_REPORTED_READY_NOT_HTTP_PROVEN"
        }:
            verdict = {"status": "CREATED_NOT_READY_PROVEN",
                       "reportedReady": False, "uid": actual["metadata"]["uid"]}
        print(json.dumps({"name": name, "namespace": namespace, "projectId": project,
                          "operation": "create" if args.create else "status", **verdict}))
        return 0 if verdict["status"] in {
            "CREATED_NOT_READY_PROVEN", "CONTROLLER_REPORTED_READY_NOT_HTTP_PROVEN",
            "CONTROLLER_NOT_READY"
        } else 1
    except (InvalidServing, ValueError, UnicodeError, OSError) as exc:
        print("KServe Serving not verified: " + str(exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
