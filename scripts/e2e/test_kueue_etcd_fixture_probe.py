"""Test that version claims come from encoded etcd bytes, not CRD metadata."""
import base64
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from kueue_etcd_fixture_probe import inspect, RESOURCE_KEY


def encoded(version, *, key=RESOURCE_KEY):
    raw = b"\x00k8s\x00" + (
        '{"apiVersion":"kueue.x-k8s.io/' + version
        + '","kind":"ResourceFlavor","metadata":{"name":"rehearsal-flavor"}}'
    ).encode()
    return {"kvs": [
        {"key": base64.b64encode(key.encode()).decode(),
         "value": base64.b64encode(raw).decode()}
    ]}


class EtcdStorageEvidenceTests(unittest.TestCase):
    def test_actual_encoded_v1beta1_is_detected(self):
        r = inspect(encoded("v1beta1"), expected_version="v1beta1")
        self.assertEqual(r["result"], "PASS")
        self.assertEqual(r["proof_type"], "direct_etcd_value")
        self.assertEqual(r["observed_persisted_api_version"], "kueue.x-k8s.io/v1beta1")

    def test_changed_etcd_value_proves_v1beta2(self):
        r = inspect(encoded("v1beta2"), expected_version="v1beta2")
        self.assertEqual(r["result"], "PASS")
        self.assertEqual(len(r["value_sha256"]), 64)

    def test_only_v1beta1_served_api_is_not_enough(self):
        with self.assertRaisesRegex(ValueError, "unexpected_persisted_version"):
            inspect(encoded("v1beta2"), expected_version="v1beta1")

    def test_missing_key_is_blocked(self):
        with self.assertRaisesRegex(ValueError, "expected_one_disposable_fixture_key"):
            inspect(encoded("v1beta1", key="/registry/other"), expected_version="v1beta1")

    def test_duplicate_key_is_blocked(self):
        sample = encoded("v1beta1")
        sample["kvs"].append(sample["kvs"][0])
        with self.assertRaisesRegex(ValueError, "expected_one_disposable_fixture_key"):
            inspect(sample, expected_version="v1beta1")

    def test_bad_payload_is_blocked(self):
        with self.assertRaises(ValueError):
            inspect({"kvs": [{"key": "@@@"}]}, expected_version="v1beta1")

    def test_missing_api_version_is_blocked(self):
        x = encoded("v1beta1")
        x["kvs"][0]["value"] = base64.b64encode(b'{"kind":"ResourceFlavor"}').decode()
        with self.assertRaisesRegex(ValueError, "storage_api_version_not_extractable"):
            inspect(x, expected_version="v1beta1")


if __name__ == "__main__":
    unittest.main()
