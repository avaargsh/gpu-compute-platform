#!/usr/bin/env python3
"""Verify a real KServe custom predictor with an explicit loopback HTTP probe.

Offline validation by default; --execute requires an approved local
port-forward and performs KServe GET -> HTTP POST -> KServe GET. This is
data-plane evidence, not a claim of route/port-forward identity, model
quality, or production readiness.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

import kserve_service as kserve

MAX_REQUEST = 64 * 1024
MAX_RESPONSE = 256 * 1024
MAX_EXPECTATION = 4096
HOST = re.compile(r"^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$")
PATH = re.compile(r"^/[A-Za-z0-9_./-]{0,255}$")


class AcceptanceError(ValueError):
    """Any failed gate must block acceptance with no leaked inference content."""


def read_json(path: Path, limit: int) -> tuple[Any, bytes]:
    raw = path.read_bytes()
    if len(raw) == 0 or len(raw) > limit:
        raise AcceptanceError("JSON input size out of bounds")
    try:
        parsed = json.loads(raw, object_pairs_hook=kserve.unique_keys)
    except (ValueError, UnicodeError) as exc:
        raise AcceptanceError("invalid or ambiguous JSON input") from exc
    return parsed, raw


def local_url(value: str) -> str:
    try:
        u = urlsplit(value)
        port = u.port
    except ValueError as exc:
        raise AcceptanceError("invalid local HTTP endpoint") from exc
    if u.scheme != "http" or u.hostname not in {"127.0.0.1", "::1"} or port is None or \
       not 1 <= port <= 65535 or u.netloc not in {f"127.0.0.1:{port}", f"[::1]:{port}"} or \
       u.username is not None or u.password is not None or u.query or u.fragment or \
       not PATH.fullmatch(u.path) or "//" in u.path or "/../" in u.path or u.path.endswith("/.."):
        raise AcceptanceError("endpoint must be an explicit loopback HTTP URL with a simple path")
    return value


def host_header(value: str | None) -> str | None:
    if value is None:
        return None
    if len(value) > 253 or not HOST.fullmatch(value) or any(
        not part or len(part) > 63 or part[0] == "-" or part[-1] == "-"
        for part in value.split(".")
    ):
        raise AcceptanceError("invalid explicitly approved HTTP Host header")
    return value


def expectation(spec: Any) -> tuple[str, str, str | None]:
    if not isinstance(spec, dict):
        raise AcceptanceError("expected response contract must be an object")
    pointer = spec.get("pointer")
    if not isinstance(pointer, str) or not 2 <= len(pointer) <= 200 or \
       not pointer.startswith("/") or pointer.count("/") > 16 or any(
           x in pointer for x in ("\r", "\n", "\0")
       ):
        raise AcceptanceError("expected response must specify a bounded JSON pointer")
    if set(spec) == {"pointer", "nonEmptyString"} and spec["nonEmptyString"] is True:
        return pointer, "nonEmptyString", None
    if set(spec) == {"pointer", "equals"} and isinstance(spec["equals"], str) and \
       0 < len(spec["equals"]) <= 128:
        return pointer, "equals", spec["equals"]
    raise AcceptanceError("expected contract needs nonEmptyString:true or equals:string")


def at_pointer(value: Any, pointer: str) -> Any:
    for raw_part in pointer.split("/")[1:]:
        if re.search(r"~(?![01])", raw_part):
            raise AcceptanceError("invalid JSON pointer escape")
        key = raw_part.replace("~1", "/").replace("~0", "~")
        if isinstance(value, dict) and key in value:
            value = value[key]
        elif isinstance(value, list) and key.isdecimal() and len(key) <= 8 and \
             int(key) < len(value):
            value = value[int(key)]
        else:
            raise AcceptanceError("expected inference response field absent")
    return value


def verify_body(raw: bytes, contract: tuple[str, str, str | None]) -> None:
    try:
        value = json.loads(raw, object_pairs_hook=kserve.unique_keys)
    except (ValueError, UnicodeError) as exc:
        raise AcceptanceError("inference response is not unambiguous JSON") from exc
    if not isinstance(value, dict) or "error" in value:
        raise AcceptanceError("inference response contains an error or invalid object")
    pointer, rule, expected = contract
    actual = at_pointer(value, pointer)
    if rule == "nonEmptyString":
        if not isinstance(actual, str) or not actual.strip():
            raise AcceptanceError("expected nonempty inference output missing")
    elif actual != expected or not isinstance(actual, str):
        raise AcceptanceError("inference response did not match the explicit expectation")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def post_loopback(url: str, payload: bytes, host: str | None) -> bytes:
    headers = {"Content-Type": "application/json", "Accept": "application/json"}
    if host:
        headers["Host"] = host
    request = urllib.request.Request(url, data=payload, headers=headers, method="POST")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect)
    try:
        with opener.open(request, timeout=12) as response:
            if response.status != 200:
                raise AcceptanceError("inference HTTP status was not 200")
            result = response.read(MAX_RESPONSE + 1)
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise AcceptanceError("inference HTTP request failed without trusted response") from exc
    if not result or len(result) > MAX_RESPONSE:
        raise AcceptanceError("inference HTTP response is empty or oversized")
    return result


def verified_controller(manifest: dict[str, Any], context: str) -> dict[str, Any]:
    name, namespace, _ = kserve.validated(manifest)
    result = kserve.kubectl(
        context, namespace,
        ["get", "inferenceservices.serving.kserve.io", name, "-o", "json"]
    )
    check = kserve.observation(result, manifest)
    if check["status"] != "CONTROLLER_REPORTED_READY_NOT_HTTP_PROVEN":
        raise AcceptanceError("KServe identity/spec/Ready has not passed controller preflight")
    return result


def same_identity(before: dict[str, Any], after: dict[str, Any]) -> None:
    left, right = before["metadata"], after["metadata"]
    if left.get("uid") != right.get("uid") or \
       left.get("generation") != right.get("generation") or \
       before.get("spec") != after.get("spec"):
        raise AcceptanceError("KServe identity/generation/spec changed while probing")


def execute(manifest: dict[str, Any], request: bytes, contract: tuple[str, str, str | None],
            context: str, url: str, host: str | None) -> dict[str, Any]:
    first = verified_controller(manifest, context)
    body = post_loopback(url, request, host)
    verify_body(body, contract)
    second = verified_controller(manifest, context)
    same_identity(first, second)
    name, namespace, project = kserve.validated(manifest)
    # Never include the raw request or response, headers, bearer tokens,
    # credentials or inference output in the acceptance artifact.
    return {
        "status": "HTTP_SCHEMA_ACCEPTED_LOCAL_ROUTE_UNBOUND",
        "claim": "A real HTTP 200 JSON response satisfied the explicit contract",
        "kserveControllerReady": True,
        "service": name, "namespace": namespace, "declaredProject": project,
        "kserveUID": second["metadata"]["uid"],
        "kserveGeneration": second["metadata"].get("generation"),
        "kserveStatusObservedGenerationAdvisory": (
            second.get("status", {}).get("observedGeneration")
        ),
        "requestSHA256": hashlib.sha256(request).hexdigest(),
        "responseSHA256": hashlib.sha256(body).hexdigest(),
        "responseBytes": len(body),
        "expectedJSONPointer": contract[0],
        "localEndpointHost": "loopback",
        "portForwardTargetVerified": False,
        "modelQualityVerified": False,
        "productionEndpointVerified": False,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--request", type=Path, required=True,
                        help="JSON POST input artifact, max 64KiB")
    parser.add_argument("--expect", type=Path, required=True,
                        help="JSON pointer output contract, max 4KiB")
    parser.add_argument("--execute", action="store_true",
                        help="explicit GET/POST/GET acceptance; otherwise offline plan only")
    parser.add_argument("--context")
    parser.add_argument("--url", help="operator-approved loopback port-forward HTTP URL")
    parser.add_argument("--host-header", help="explicit routing Host for local ingress")
    args = parser.parse_args(argv)
    try:
        manifest, manifest_bytes = read_json(args.manifest, kserve.MAX_JSON)
        if not isinstance(manifest, dict):
            raise AcceptanceError("Serving manifest must be a JSON object")
        name, namespace, project = kserve.validated(manifest)
        body, request_bytes = read_json(args.request, MAX_REQUEST)
        if not isinstance(body, dict) or not body:
            raise AcceptanceError("inference request must be a nonempty JSON object")
        expected, _ = read_json(args.expect, MAX_EXPECTATION)
        contract = expectation(expected)
        # Even offline mode validates supplied URL/host, not their accessibility.
        url = local_url(args.url) if args.url else None
        host = host_header(args.host_header)
        if not args.execute:
            if args.context or url or host:
                raise AcceptanceError("network flags are only permitted with --execute")
            print(json.dumps({
                "status": "PLAN_ONLY_NO_NETWORK",
                "name": name, "namespace": namespace, "project": project,
                "manifestSHA256": hashlib.sha256(manifest_bytes).hexdigest(),
                "requestSHA256": hashlib.sha256(request_bytes).hexdigest(),
                "expectedJSONPointer": contract[0],
                "requires": "explicit kubeconfig context, local port-forward and --execute",
            }))
            return 0
        if not args.context or not kserve.CTX.fullmatch(args.context) or not url:
            raise AcceptanceError("--execute needs safe --context and local --url")
        report = execute(manifest, request_bytes, contract, args.context, url, host)
        print(json.dumps(report, sort_keys=True))
        return 0
    except (AcceptanceError, kserve.InvalidServing, OSError, ValueError) as exc:
        print("KServe HTTP acceptance not proven: " + str(exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
