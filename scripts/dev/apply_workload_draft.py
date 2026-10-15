#!/usr/bin/env python3
"""Apply a Portal Workload draft to a *loopback-only* Go development API.

This is a developer tool, not a browser-side authorization or production API.
The existing Go control plane owns Project/Pool binding and Workload lifecycle.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

SAFE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
MAX_FILE_BYTES = 64 * 1024


class InvalidDraft(ValueError):
    """The artifact does not conform to the v0.1 Workload contract."""


def no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise InvalidDraft(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def load_draft(path: Path) -> dict[str, object]:
    raw = path.read_bytes()
    if len(raw) > MAX_FILE_BYTES:
        raise InvalidDraft("draft exceeds 64 KiB")
    payload = json.loads(raw, object_pairs_hook=no_duplicate_keys)
    if not isinstance(payload, dict):
        raise InvalidDraft("draft must be a JSON object")
    if set(payload) != {"metadata", "projectId", "poolId", "spec"}:
        raise InvalidDraft("unexpected Workload top-level fields")
    metadata, spec = payload["metadata"], payload["spec"]
    if not isinstance(metadata, dict) or not isinstance(spec, dict):
        raise InvalidDraft("metadata and spec must be objects")
    identity = metadata.get("id")
    if not isinstance(identity, str) or not SAFE_ID.fullmatch(identity):
        raise InvalidDraft("invalid Workload ID")
    if type(metadata.get("generation")) is not int or metadata["generation"] != 1:
        raise InvalidDraft("v0.1 new Workload generation must be 1")
    for field in ("projectId", "poolId"):
        value = payload[field]
        if not isinstance(value, str) or not SAFE_ID.fullmatch(value):
            raise InvalidDraft(f"invalid {field}")
    if set(spec) != {"image", "command", "accelerator"}:
        raise InvalidDraft("unsupported Workload spec fields")
    image = spec["image"]
    if not isinstance(image, str) or not image or len(image) > 512 or any(c.isspace() for c in image):
        raise InvalidDraft("invalid container image")
    cmd = spec["command"]
    if (not isinstance(cmd, list) or not 1 <= len(cmd) <= 64 or
        not all(isinstance(a, str) and 0 < len(a) <= 4096 and "\\0" not in a for a in cmd)):
        raise InvalidDraft("command must be a nonempty JSON argv string array")
    accelerator = spec["accelerator"]
    if not isinstance(accelerator, dict) or set(accelerator) != {"class", "quota"}:
        raise InvalidDraft("invalid accelerator request")
    cls, quota = accelerator["class"], accelerator["quota"]
    if not isinstance(cls, str) or not SAFE_ID.fullmatch(cls):
        raise InvalidDraft("invalid accelerator class")
    if type(quota) is not int or quota < 1 or quota > 256:
        raise InvalidDraft("invalid GPU count")
    return payload


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        raise InvalidDraft("unexpected redirect: refusing to forward Workload payload")


def apply(port: int, payload: dict[str, object]) -> int:
    identity = payload["metadata"]["id"]
    url = f"http://127.0.0.1:{port}/api/v1/workloads/{identity}"
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        method="PUT",
        headers={"Accept": "application/json", "Content-Type": "application/json"},
    )
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(request, timeout=10) as response:
            if response.status != 204:
                print(f"HTTP {response.status}: inspect Go API response", file=sys.stderr)
                return 1
            print(f"Local Go API accepted Workload Desired: {identity}")
            print("This is NOT proof of Kueue admission, GPU execution, or task success.")
            return 0
    except urllib.error.HTTPError as exc:
        print(f"Go API rejected Workload: HTTP {exc.code}", file=sys.stderr)
        return 1
    except (urllib.error.URLError, TimeoutError) as exc:
        print(f"Cannot reach loopback Go API: {type(exc).__name__}", file=sys.stderr)
        return 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("draft", type=Path, help="JSON exported by Portal Workload composer")
    parser.add_argument("--port", type=int, default=8080, help="Go loopback dev API port (default 8080)")
    parser.add_argument("--apply", action="store_true", help="Perform PUT; default only validates and previews")
    args = parser.parse_args(argv)
    if not 1 <= args.port <= 65535:
        parser.error("port must be between 1 and 65535")
    try:
        payload = load_draft(args.draft)
    except (InvalidDraft, ValueError, UnicodeError, OSError) as exc:
        print(f"Invalid Workload draft: {exc}", file=sys.stderr)
        return 2
    print("Workload:", payload["metadata"]["id"], "Project:", payload["projectId"],
          "Pool:", payload["poolId"], "GPU:", payload["spec"]["accelerator"])
    if not args.apply:
        print("DRY RUN: no API request. Add --apply to submit to 127.0.0.1 only.")
        return 0
    return apply(args.port, payload)


if __name__ == "__main__":
    raise SystemExit(main())
