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
    p.add_argument("--expected-version", choices=("v1beta1", "v1beta2"), required=True)
    p.add_argument("--evidence-file", type=Path, required=True)
    args = p.parse_args(argv)
    report = {"result": "BLOCKED", "scope": "isolated_kind_fixture_only"}
    try:
        response = json.load(sys.stdin)
        report = inspect(response, expected_version=args.expected_version)
    except (ValueError, TypeError, KeyError, json.JSONDecodeError) as exc:
        # No raw data in error messages; preserve only a code.
        report["reason"] = "unexpected_or_unreadable_etcd_fixture"
    output = json.dumps(report, indent=2, sort_keys=True) + "\n"
    args.evidence_file.write_text(output, encoding="utf-8")
    print(output, end="")
    return 0 if report["result"] == "PASS" else 2


if __name__ == "__main__":
    sys.exit(main())
