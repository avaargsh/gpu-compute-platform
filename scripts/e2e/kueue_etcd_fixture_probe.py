#!/usr/bin/env python3
"""Assert a disposable kind etcd CRD fixture's *persisted bytes* API version.

Input is etcdctl get --write-out=json (base64 value). The probe does not issue
kubectl/etcd writes, and never saves object bytes in evidence artifacts.
"""

import argparse
import base64
import hashlib
import json
import re
import sys
from pathlib import Path

RESOURCE_KEY = "/registry/resourceflavors.kueue.x-k8s.io/rehearsal-flavor"
ENCODED_VERSION = re.compile(rb'"apiVersion"\s*:\s*"(kueue\.x-k8s\.io/v1beta[12])"')


def discover_key(payload):
    """Find exactly one disposable ResourceFlavor fixture key, without values."""
    if not isinstance(payload, dict) or not isinstance(payload.get("kvs"), list):
        raise ValueError("invalid_etcdctl_response")
    candidates = []
    for entry in payload["kvs"]:
        try:
            key = base64.b64decode(entry["key"], validate=True).decode("utf-8")
        except (KeyError, ValueError, UnicodeError) as exc:
            raise ValueError("invalid_etcd_key") from exc
        if (
            key.startswith("/registry/")
            and key.endswith("/rehearsal-flavor")
            and "resourceflavors" in key
        ):
            candidates.append(key)
    if len(candidates) != 1:
        raise ValueError("expected_one_disposable_fixture_key")
    return candidates[0]


def inspect(payload, *, expected_version, key=RESOURCE_KEY):
    if not isinstance(payload, dict):
        raise ValueError("invalid_etcdctl_response")
    matches = []
    for entry in payload.get("kvs", []):
        try:
            decoded_key = base64.b64decode(entry["key"], validate=True).decode("utf-8")
        except (KeyError, ValueError, UnicodeError) as exc:
            raise ValueError("invalid_etcd_key") from exc
        if decoded_key == key:
            matches.append(entry)
    if len(matches) != 1:
        raise ValueError("expected_one_disposable_fixture_key")
    try:
        data = base64.b64decode(matches[0]["value"], validate=True)
    except (KeyError, ValueError) as exc:
        raise ValueError("invalid_etcd_value") from exc
    # Kueue CRDs are stored as JSON; Kubernetes may wrap it in an etcd prefix.
    found = ENCODED_VERSION.search(data)
    if found is None:
        raise ValueError("storage_api_version_not_extractable")
    observed = found.group(1).decode("ascii")
    if observed != "kueue.x-k8s.io/" + expected_version:
        raise ValueError("unexpected_persisted_version:" + observed)
    return {
        "result": "PASS",
        "scope": "isolated_kind_fixture_only",
        "etcd_key": key,
        "observed_persisted_api_version": observed,
        "value_sha256": hashlib.sha256(data).hexdigest(),
        "proof_type": "direct_etcd_value",
    }


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--discover-key", action="store_true",
                   help="Input: etcdctl get /registry/ --prefix --keys-only -w json")
    p.add_argument("--key", help="Exact discovered disposable etcd key for verification")
    p.add_argument("--expected-version", choices=("v1beta1", "v1beta2"))
    p.add_argument("--evidence-file", type=Path)
    args = p.parse_args(argv)
    if args.discover_key:
        try:
            print(discover_key(json.load(sys.stdin)))
            return 0
        except (ValueError, TypeError, KeyError, json.JSONDecodeError):
            print("BLOCKED: cannot find exactly one disposable fixture key", file=sys.stderr)
            return 2
    if not args.expected_version or not args.evidence_file or not args.key:
        p.error("--key, --expected-version and --evidence-file required for verification")
    report = {"result": "BLOCKED", "scope": "isolated_kind_fixture_only"}
    try:
        response = json.load(sys.stdin)
        report = inspect(response, expected_version=args.expected_version, key=args.key)
    except (ValueError, TypeError, KeyError, json.JSONDecodeError) as exc:
        # Emit only finite, predefined diagnostic classes, never raw etcd bytes.
        reason = str(exc)
        known_reasons = (
            "invalid_etcdctl_response", "invalid_etcd_key", "invalid_etcd_value",
            "expected_one_disposable_fixture_key", "storage_api_version_not_extractable",
            "unexpected_persisted_version:kueue.x-k8s.io/v1beta1",
            "unexpected_persisted_version:kueue.x-k8s.io/v1beta2",
        )
        report["reason"] = reason if reason in known_reasons else "invalid_etcdctl_json"
    output = json.dumps(report, indent=2, sort_keys=True) + "\n"
    args.evidence_file.write_text(output, encoding="utf-8")
    print(output, end="")
    return 0 if report["result"] == "PASS" else 2


if __name__ == "__main__":
    sys.exit(main())
