"""KServe HTTP evidence CLI: offline, identity, status and actual loopback tests."""
from __future__ import annotations

import copy
import io
import json
import tempfile
import threading
import unittest
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

import kserve_http_acceptance as h
from test_kserve_service import D, observed

GOOD_BODY = json.dumps({"object": "chat.completion", "choices": [
    {"message": {"content": "hello world"}}]}).encode()
GOOD_REQUEST = b'{"model":"test","messages":[{"role":"user","content":"hello"}]}'
CONTRACT = ("/choices/0/message/content", "nonEmptyString", None)


class ProbeHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        self.server.last_body = self.rfile.read(length)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(GOOD_BODY)))
        self.end_headers()
        self.wfile.write(GOOD_BODY)

    def log_message(self, format, *args):
        pass


class KServeAcceptanceTests(unittest.TestCase):
    def fixture(self, content: bytes):
        with tempfile.NamedTemporaryFile(delete=False, suffix=".json") as file:
            file.write(content)
        path = Path(file.name)
        self.addCleanup(path.unlink, missing_ok=True)
        return path

    def test_plan_never_calls_k8s_or_http(self):
        manifest = self.fixture(json.dumps(D).encode())
        request = self.fixture(GOOD_REQUEST)
        expect = self.fixture(b'{"pointer":"/choices/0/message/content","nonEmptyString":true}')
        output = io.StringIO()
        with patch.object(h.kserve, "kubectl") as cmd, patch.object(h, "post_loopback") as http, redirect_stdout(output):
            self.assertEqual(h.main([str(manifest), "--request", str(request), "--expect", str(expect)]), 0)
        cmd.assert_not_called()
        http.assert_not_called()
        verdict = json.loads(output.getvalue())
        self.assertEqual(verdict["status"], "PLAN_ONLY_NO_NETWORK")
        self.assertEqual(verdict["expectedJSONPointer"], CONTRACT[0])
        self.assertNotIn("rawRequest", verdict)
        self.assertNotIn("response", verdict)

    def test_status_then_http_then_status_and_digest_no_raw_content(self):
        calls = []
        def get(*args):
            calls.append("get")
            return observed()
        def post(*args):
            calls.append("post")
            return GOOD_BODY
        with patch.object(h.kserve, "kubectl", side_effect=get), patch.object(h, "post_loopback", side_effect=post):
            result = h.execute(D, GOOD_REQUEST, CONTRACT, "kind-gpu", "http://127.0.0.1:8080/v1/chat/completions", None)
        self.assertEqual(calls, ["get", "post", "get"])
        self.assertEqual(result["status"], "HTTP_SCHEMA_ACCEPTED_LOCAL_ROUTE_UNBOUND")
        self.assertTrue(result["kserveControllerReady"])
        self.assertFalse(result["portForwardTargetVerified"])
        self.assertFalse(result["productionEndpointVerified"])
        self.assertEqual(len(result["requestSHA256"]), 64)
        self.assertEqual(len(result["responseSHA256"]), 64)
        self.assertNotIn("hello world", json.dumps(result))
        self.assertNotIn("messages", json.dumps(result))

    def test_stale_or_foreign_kserve_before_request_blocks_http(self):
        for mutate in (
            lambda d: d["metadata"].update(uid=""),
            lambda d: d["metadata"].update(namespace="foreign"),
            lambda d: d["status"]["conditions"][0].update(status="False"),
            lambda d: d["spec"]["predictor"]["containers"][0].update(image="foreign"),
        ):
            bad = observed()
            mutate(bad)
            with patch.object(h.kserve, "kubectl", return_value=bad), patch.object(h, "post_loopback") as post:
                with self.assertRaises(h.AcceptanceError):
                    h.execute(D, GOOD_REQUEST, CONTRACT, "kind-gpu", "http://127.0.0.1:8080/v1/models", None)
                post.assert_not_called()

    def test_replacement_and_spec_change_between_gets_fail_closed(self):
        new_uid = observed()
        new_uid["metadata"]["uid"] = "uid-replaced"
        for changed in (new_uid,):
            with patch.object(h.kserve, "kubectl", side_effect=[observed(), changed]), patch.object(h, "post_loopback", return_value=GOOD_BODY):
                with self.assertRaises(h.AcceptanceError):
                    h.execute(D, GOOD_REQUEST, CONTRACT, "ctx", "http://127.0.0.1:8080/v1/models", None)
        new_generation = observed()
        new_generation["metadata"]["generation"] = 2
        with patch.object(h.kserve, "kubectl", side_effect=[observed(), new_generation]), patch.object(h, "post_loopback", return_value=GOOD_BODY):
            with self.assertRaises(h.AcceptanceError):
                h.execute(D, GOOD_REQUEST, CONTRACT, "ctx", "http://127.0.0.1:8080/v1/models", None)

    def test_bad_inference_response_never_accepted(self):
        for body in (
            b'', b'{}', b'{"error":"invalid"}', b'{"choices":[]}',
            b'{"choices":[{"message":{"content":""}}]}',
            b'{"choices":[{"message":{"content":null}}]}',
            b'{"choices":[{"message":{"content":"a","content":"b"}}]}',
            b'{"choices":[{"message":{"content":"a"}}]}' + b' ' * h.MAX_RESPONSE,
        ):
            with self.subTest(body=body[:35]):
                with self.assertRaises(h.AcceptanceError):
                    h.verify_body(body, CONTRACT)

    def test_explicit_exact_value_match(self):
        spec = h.expectation({"pointer": "/object", "equals": "chat.completion"})
        h.verify_body(GOOD_BODY, spec)
        with self.assertRaises(h.AcceptanceError):
            h.verify_body(GOOD_BODY, h.expectation({"pointer": "/object", "equals": "text.completion"}))
        for bad in ({"pointer": "/a", "equals": 1},
                    {"pointer": "/a", "nonEmptyString": False},
                    {"pointer": "/a", "nonEmptyString": True, "equals": "x"},
                    {"pointer": "a", "nonEmptyString": True},
                    {"pointer": "/a/~2", "nonEmptyString": True}):
            with self.subTest(bad=bad):
                try:
                    contract = h.expectation(bad)
                except h.AcceptanceError:
                    continue
                with self.assertRaises(h.AcceptanceError):
                    h.at_pointer({}, contract[0])

    def test_reject_external_ssrf_proxy_redirect_and_url_ambiguity(self):
        for url in (
            "http://example.com:80/predict", "https://127.0.0.1:8000/predict",
            "http://127.0.0.1:8888@other.com/path", "http://localhost:8000/path",
            "http://[::ffff:127.0.0.1]:8000/path", "http://127.0.0.1:80/path?a=1",
            "http://127.0.0.1:8000/../secret", "http://127.0.0.1:0/path",
            "http://127.0.0.1:8000/path#frag", "http://127.0.0.1:8000//path",
        ):
            with self.subTest(url=url), self.assertRaises(h.AcceptanceError):
                h.local_url(url)
        self.assertEqual(h.local_url("http://127.0.0.1:8888/v1/chat/completions"),
                         "http://127.0.0.1:8888/v1/chat/completions")
        for host in ("evil.com\r\nOther: yes", "-bad.host", "bad.host:8080", "UPPERCASE"):
            with self.assertRaises(h.AcceptanceError):
                h.host_header(host)

    def test_actual_local_http_post_with_proxy_disabled_and_host(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), ProbeHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        url = h.local_url(f"http://127.0.0.1:{server.server_port}/v1/chat/completions")
        with patch.dict("os.environ", {"HTTP_PROXY": "http://invalid.example:1234"}):
            body = h.post_loopback(url, GOOD_REQUEST, "model-a.research.local")
        h.verify_body(body, CONTRACT)
        self.assertEqual(server.last_body, GOOD_REQUEST)

    def test_cli_explicit_probe_requires_all_flags(self):
        manifest = self.fixture(json.dumps(D).encode())
        request = self.fixture(GOOD_REQUEST)
        expect = self.fixture(b'{"pointer":"/object","equals":"chat.completion"}')
        flags = [str(manifest), "--request", str(request), "--expect", str(expect)]
        self.assertEqual(h.main(flags + ["--execute"]), 2)
        self.assertEqual(h.main(flags + ["--url", "http://127.0.0.1:9000/v1/models"]), 2)
        with patch.object(h.kserve, "kubectl", return_value=observed()), patch.object(h, "post_loopback", return_value=GOOD_BODY):
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(h.main(flags + ["--execute", "--context", "kind-gpu",
                                               "--url", "http://127.0.0.1:9000/v1/models"]), 0)
            verdict = json.loads(output.getvalue())
            self.assertEqual(verdict["status"], "HTTP_SCHEMA_ACCEPTED_LOCAL_ROUTE_UNBOUND")

    def test_upstream_generation_field_is_advisory_only(self):
        live = observed()
        live["metadata"]["generation"] = 2
        live["status"]["observedGeneration"] = 1
        with patch.object(h.kserve, "kubectl", return_value=live), patch.object(h, "post_loopback", return_value=GOOD_BODY):
            result = h.execute(D, GOOD_REQUEST, CONTRACT, "ctx", "http://127.0.0.1:8080/v1/models", None)
        self.assertEqual(result["kserveStatusObservedGenerationAdvisory"], 1)
        self.assertEqual(result["kserveGeneration"], 2)
        self.assertFalse(result["portForwardTargetVerified"])


if __name__ == "__main__":
    unittest.main()
