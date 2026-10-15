"""Tests for local-only Workload intent application, no live cluster required."""
from __future__ import annotations

import json
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import apply_workload_draft as target  # noqa: E402

VALID = {
    "metadata": {"id": "train-001", "generation": 1},
    "projectId": "research", "poolId": "pool-a",
    "spec": {
        "image": "registry.example/train@sha256:deadbeef",
        "command": ["python", "train.py"],
        "accelerator": {"class": "h100-80g", "quota": 1},
    },
}


class TestDraft(unittest.TestCase):
    def file(self, data: str) -> Path:
        tmp = tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False)
        tmp.write(data)
        tmp.close()
        self.addCleanup(lambda: Path(tmp.name).unlink(missing_ok=True))
        return Path(tmp.name)

    def test_dry_run_never_contacts_network(self):
        path = self.file(json.dumps(VALID))
        self.assertEqual(target.main([str(path)]), 0)

    def test_rejects_duplicate_fields_and_forged_identity(self):
        path = self.file('{"metadata":{"id":"a","id":"b","generation":1},"projectId":"research","poolId":"p","spec":{}}')
        with self.assertRaises(target.InvalidDraft):
            target.load_draft(path)
        forged = json.loads(json.dumps(VALID))
        forged["metadata"]["id"] = "../oops"
        with self.assertRaises(target.InvalidDraft):
            target.load_draft(self.file(json.dumps(forged)))

    def test_rejects_shell_string_and_invalid_gpu_count(self):
        invalid = json.loads(json.dumps(VALID))
        invalid["spec"]["command"] = "python train.py"
        with self.assertRaises(target.InvalidDraft):
            target.load_draft(self.file(json.dumps(invalid)))
        invalid["spec"]["command"] = ["python", "train.py"]
        invalid["spec"]["accelerator"]["quota"] = True
        with self.assertRaises(target.InvalidDraft):
            target.load_draft(self.file(json.dumps(invalid)))

    def test_rejects_actual_nul_in_command_argv(self):
        invalid = json.loads(json.dumps(VALID))
        invalid["spec"]["command"] = ["python", "bad" + chr(0) + "argv"]
        with self.assertRaises(target.InvalidDraft):
            target.load_draft(self.file(json.dumps(invalid)))

    def test_workspace_pvc_roundtrip_and_invalid_name(self):
        valid = json.loads(json.dumps(VALID))
        valid["spec"]["workspace"] = {"claimName": "training-data.v1"}
        parsed = target.load_draft(self.file(json.dumps(valid)))
        self.assertEqual(parsed["spec"]["workspace"]["claimName"], "training-data.v1")
        for bad in ("", "../escape", "UPPER", "invalid/name", "bad..claim"):
            valid["spec"]["workspace"] = {"claimName": bad}
            with self.assertRaises(target.InvalidDraft):
                target.load_draft(self.file(json.dumps(valid)))

    def test_explicit_apply_puts_to_local_go_endpoint_only(self):
        received = {}
        class Handler(BaseHTTPRequestHandler):
            def do_PUT(self):
                received["path"] = self.path
                received["body"] = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                self.send_response(204)
                self.end_headers()
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            rc = target.main([str(self.file(json.dumps(VALID))), "--port", str(server.server_port), "--apply"])
            self.assertEqual(rc, 0)
            self.assertEqual(received["path"], "/api/v1/workloads/train-001")
            self.assertEqual(received["body"], VALID)
        finally:
            server.shutdown()
            thread.join()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
