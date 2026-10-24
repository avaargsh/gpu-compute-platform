#!/usr/bin/env python3
"""Inject a real lost HTTP ACK into the authenticated Management API path.

This is an opt-in acceptance test, not a mock/unit fixture. The loopback proxy
waits until the upstream Management API has fully returned HTTP 204 (the
PostgreSQL Desired + operation-receipt transaction has committed), holds that
response, lets the parent SIGKILL the caller process, and then closes the
socket without forwarding the ACK. The parent recovers using the original
Idempotency-Key, verifies Desired/receipt state, checks Kueue observations,
and releases the Workload.

Only Python's standard library is required. All endpoints must be loopback.
"""

from __future__ import annotations

import base64
import http.client
import json
import os
import re
import signal
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import secrets
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, NoReturn

ID_RE = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
IMAGE_DIGEST_RE = re.compile(r"@sha256:[0-9a-f]{64}$")
REQUEST_DIGEST_RE = re.compile(r"^[0-9a-f]{64}$")
POLL_SECONDS = 2


def fail(message: str) -> NoReturn:
    raise RuntimeError(message)


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        fail(f"required environment variable is missing: {name}")
    return value


def safe_id(name: str, value: str) -> str:
    if not ID_RE.fullmatch(value):
        fail(f"{name} must be a lowercase DNS-safe identifier (max 63 characters)")
    return value


def local_http_base(name: str, default: str) -> str:
    raw = os.environ.get(name, default).strip().rstrip("/")
    parsed = urllib.parse.urlsplit(raw)
    if (
        parsed.scheme != "http"
        or parsed.hostname != "127.0.0.1"
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
    ):
        fail(f"{name} must be an http://127.0.0.1[:port] loopback URL with no path")
    if parsed.port is not None and not (1 <= parsed.port <= 65535):
        fail(f"{name} contains an invalid port")
    return raw


def timeout_seconds() -> int:
    value = int(os.environ.get("ACCEPTANCE_TIMEOUT_SECONDS", "240"))
    if value < 15 or value > 1800:
        fail("ACCEPTANCE_TIMEOUT_SECONDS must be between 15 and 1800")
    return value


def http_call(
    base_url: str,
    method: str,
    path: str,
    token: str,
    *,
    request_id: str | None = None,
    if_match: str | None = None,
    body: bytes | None = None,
    timeout: int = 20,
) -> tuple[int, bytes]:
    headers = {"Cache-Control": "no-store"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if body is not None:
        headers["Content-Type"] = "application/json"
    if request_id:
        headers["Idempotency-Key"] = request_id
    if if_match:
        headers["If-Match"] = if_match
    request = urllib.request.Request(base_url + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as response:
        return response.code, response.read()
    except (urllib.error.URLError, TimeoutError, ConnectionError, OSError):
        # A transport failure does not reveal whether a mutation committed.
        # Return an explicit unknown status so write callers can reconcile by
        # the original operation receipt; read callers fail closed on status 0.
        return 0, b""


def json_call(base_url: str, path: str, token: str) -> tuple[int, dict[str, Any] | None]:
    status, body = http_call(base_url, "GET", path, token)
    if status != 200:
        return status, None
    try:
        value = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        fail(f"GET {path} returned non-JSON success")
    if not isinstance(value, dict):
        fail(f"GET {path} returned a non-object JSON document")
    return status, value


class FaultState:
    def __init__(self) -> None:
        self.upstream_finished = threading.Event()
        self.release_connection = threading.Event()
        self.backend_status: int | None = None
        self.error: str | None = None


def ack_drop_proxy(
    base_url: str,
    expected_path: str,
    state: FaultState,
    *,
    expected_method: str = "PUT",
    accepted_statuses: tuple[int, ...] = (204,),
) -> ThreadingHTTPServer:
    """Forward one mutation, withhold its commit ACK, and drop the caller socket."""
    if expected_method not in {"PUT", "DELETE"}:
        fail("fault proxy only supports PUT and DELETE")
    if not accepted_statuses or any(status < 200 or status >= 300 for status in accepted_statuses):
        fail("accepted_statuses must contain one or more successful HTTP statuses")
    upstream = urllib.parse.urlsplit(base_url)

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, _format: str, *_args: Any) -> None:
            # Never log Authorization, request body, or provider data.
            return

        def _handle_mutation(self, method: str) -> None:
            if method != expected_method:
                state.error = f"fault proxy received unexpected method {method}"
                state.upstream_finished.set()
                self.send_error(405)
                return
            if self.path != expected_path:
                state.error = "fault proxy received an unexpected request path"
                state.upstream_finished.set()
                self.send_error(404)
                return
            try:
                length_header = self.headers.get("Content-Length", "0")
                length = int(length_header)
                if length < 0 or length > 256 * 1024:
                    raise ValueError("invalid or oversized request body")
                body = self.rfile.read(length)
                forward_headers: dict[str, str] = {}
                for header in (
                    "Authorization",
                    "Idempotency-Key",
                    "If-Match",
                    "Content-Type",
                    "Origin",
                ):
                    value = self.headers.get(header)
                    if value is not None:
                        forward_headers[header] = value
                forward_headers["Content-Length"] = str(len(body))
                connection = http.client.HTTPConnection(
                    upstream.hostname,
                    upstream.port or 80,
                    timeout=30,
                )
                try:
                    connection.request(method, self.path, body=body, headers=forward_headers)
                    response = connection.getresponse()
                    response_body = response.read()  # Drain commit response before faulting.
                    state.backend_status = response.status
                    if response.status in accepted_statuses:
                        # Management has committed the mutation. Hold its ACK until
                        # the parent SIGKILLs the caller, then close without forwarding.
                        state.upstream_finished.set()
                        if not state.release_connection.wait(timeout=60):
                            state.error = "timed out waiting for parent to release ACK-drop barrier"
                        self.close_connection = True
                        return
                    self.send_response(response.status)
                    for key, value in response.getheaders():
                        if key.lower() not in {
                            "connection", "keep-alive", "proxy-authenticate",
                            "proxy-authorization", "te", "trailers",
                            "transfer-encoding", "upgrade", "content-length",
                        }:
                            self.send_header(key, value)
                    self.send_header("Content-Length", str(len(response_body)))
                    self.end_headers()
                    if response_body and response.status not in (204, 304):
                        self.wfile.write(response_body)
                    state.upstream_finished.set()
                finally:
                    connection.close()
            except Exception as exc:  # Surface category without credentials.
                state.error = f"fault proxy upstream failure: {type(exc).__name__}"
                state.upstream_finished.set()
                try:
                    self.send_error(502)
                except OSError:
                    pass

        def do_PUT(self) -> None:  # noqa: N802
            self._handle_mutation("PUT")

        def do_DELETE(self) -> None:  # noqa: N802
            self._handle_mutation("DELETE")

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    return server


def run_faulted_caller(
    base_url: str,
    path: str,
    request_id: str,
    body: bytes,
    timeout: int,
    *,
    method: str = "PUT",
    if_match: str | None = None,
    accepted_statuses: tuple[int, ...] = (204,),
) -> int:
    """SIGKILL the caller after a real upstream mutation ACK is withheld."""
    state = FaultState()
    server = ack_drop_proxy(
        base_url,
        path,
        state,
        expected_method=method,
        accepted_statuses=accepted_statuses,
    )
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()
    proxy_url = f"http://127.0.0.1:{server.server_port}"
    encoded_body = base64.b64encode(body).decode("ascii")
    child = subprocess.Popen(
        [
            sys.executable,
            os.path.abspath(__file__),
            "--client",
            "--url",
            proxy_url,
            "--path",
            path,
            "--request-id",
            request_id,
            "--body-base64",
            encoded_body,
            "--method",
            method,
            "--if-match",
            if_match or "",
        ],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=os.environ.copy(),
    )
    try:
        if not state.upstream_finished.wait(timeout=timeout):
            child.kill()
            child.wait(timeout=10)
            fail(state.error or "upstream Management API did not answer before timeout")
        if state.error:
            fail(state.error)
        if state.backend_status not in accepted_statuses:
            child.wait(timeout=10)
            fail(
                f"expected upstream status in {accepted_statuses}, "
                f"got {state.backend_status}"
            )
        if child.poll() is not None:
            fail("caller exited before the ACK-drop barrier; the fault injection is invalid")
        child.kill()  # Real process-boundary crash while the HTTP response is withheld.
        return_code = child.wait(timeout=10)
        if return_code != -signal.SIGKILL:
            fail(f"caller process was not SIGKILLed as required (exit={return_code})")
        state.release_connection.set()  # Proxy closes downstream without forwarding ACK.
        return return_code
    finally:
        state.release_connection.set()
        if child.poll() is None:
            child.kill()
            child.wait(timeout=10)
        server.shutdown()
        server.server_close()
        server_thread.join(timeout=5)


def receipt_path(cluster: str, project: str, request_id: str) -> str:
    return (
        f"/api/v1/management/clusters/{cluster}/projects/{project}"
        f"/operations/{request_id}"
    )


def resource_path(cluster: str, project: str, workload: str) -> str:
    return (
        f"/api/v1/management/clusters/{cluster}/projects/{project}"
        f"/resources/Workload/{workload}"
    )


def verify_receipt(
    receipt: dict[str, Any],
    *,
    request_id: str,
    cluster: str,
    project: str,
    workload: str,
    action: str,
    expected_generation: int,
) -> int:
    required = {
        "requestId": request_id,
        "status": "desired-committed",
        "clusterId": cluster,
        "projectId": project,
        "kind": "Workload",
        "resourceId": workload,
        "action": action,
        "expectedGeneration": expected_generation,
    }
    for key, expected in required.items():
        if receipt.get(key) != expected:
            fail(f"operation receipt mismatch for field {key}")
    generation = receipt.get("resultingGeneration")
    if not isinstance(generation, int) or generation < 1:
        fail("operation receipt has no positive resultingGeneration")
    digest = receipt.get("requestDigest")
    if not isinstance(digest, str) or not REQUEST_DIGEST_RE.fullmatch(digest) or digest == "0" * 64:
        fail("operation receipt requestDigest is not a valid nonzero lowercase SHA-256 hex digest")
    return generation


def receipt_snapshot_matches(
    before: dict[str, Any],
    after: dict[str, Any],
    *,
    expected_generation: int,
) -> bool:
    """Ensure replay/conflict handling did not mutate the committed operation receipt."""
    immutable_fields = (
        "requestId",
        "status",
        "clusterId",
        "projectId",
        "kind",
        "resourceId",
        "action",
        "expectedGeneration",
        "resultingGeneration",
        "requestDigest",
        "committedAt",
    )
    return (
        isinstance(before, dict)
        and isinstance(after, dict)
        and after.get("resultingGeneration") == expected_generation
        and all(after.get(field) == before.get(field) for field in immutable_fields)
    )


def desired_matches_create(
    desired: dict[str, Any],
    *,
    cluster: str,
    project: str,
    workload: str,
    generation: int,
    image: str,
    pool: str,
    command: list[str],
    accelerator_class: str,
    quota: int,
) -> bool:
    """Return true only when Desired still matches this operation's exact commit."""
    if not isinstance(desired, dict) or type(generation) is not int or generation < 1:
        return False
    spec = desired.get("spec")
    if not isinstance(spec, dict):
        return False
    return (
        desired.get("id") == workload
        and desired.get("clusterId") == cluster
        and desired.get("projectId") == project
        and type(desired.get("generation")) is int
        and desired.get("generation") == generation
        and desired.get("deleting") is not True
        and spec.get("image") == image
        and spec.get("poolID") == pool
        and spec.get("command") == command
        and spec.get("accelerator") == {"class": accelerator_class, "quota": quota}
    )


def print_provider_diagnostics(namespace: str, context: str | None) -> None:
    """Dump non-secret Kueue/Job/Pod state when current-generation observation stalls."""
    commands = (
        (
            "namespaced Job/Pod/Kueue state",
            ["-n", namespace, "get", "jobs,pods,workloads.kueue.x-k8s.io,localqueues", "-o", "wide"],
        ),
        ("cluster-scoped Kueue state", ["get", "clusterqueues,resourceflavors", "-o", "wide"]),
        ("recent namespace events", ["-n", namespace, "get", "events", "--sort-by=.lastTimestamp"]),
    )
    for title, args in commands:
        command = ["kubectl"]
        if context:
            command.extend(["--context", context])
        command.extend(args)
        print(f"--- Provider diagnostics: {title} ---", file=sys.stderr, flush=True)
        try:
            result = subprocess.run(
                command,
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                timeout=30,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            print(f"diagnostic command failed: {type(exc).__name__}", file=sys.stderr, flush=True)
            continue
        if result.stdout:
            print(result.stdout[-16000:], file=sys.stderr, flush=True)
        if result.stderr:
            print(result.stderr[-4000:], file=sys.stderr, flush=True)


def wait_for_workload(
    control_plane_url: str,
    workload: str,
    expected_generation: int,
    timeout: int,
    namespace: str,
    context: str | None,
) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    path = f"/api/v1/workloads/{workload}"
    last_status: dict[str, Any] = {}
    while time.monotonic() < deadline:
        status_code, data = json_call(control_plane_url, path, "")
        # The canonical Cluster Agent/status API is loopback-local in this test
        # and is intentionally not the source of Management API authorization.
        if status_code == 200 and data is not None:
            last_status = data
            metadata = data.get("metadata") or {}
            status = data.get("status") or {}
            conditions = status.get("conditions") or []
            true_conditions = {
                condition.get("type")
                for condition in conditions
                if condition.get("status") in (True, "True", "TRUE")
            }
            if metadata.get("generation") != expected_generation:
                fail("observed Workload generation does not match committed receipt")
            if (
                status.get("syncState") == "Synced"
                and "QuotaReserved" in true_conditions
                and "Admitted" in true_conditions
                and ({"Ready", "PodsReady", "Succeeded"} & true_conditions)
            ):
                return data
        time.sleep(POLL_SECONDS)
    status = last_status.get("status") or {}
    diagnostic = {
        "metadata": last_status.get("metadata"),
        "syncState": status.get("syncState"),
        "desiredGeneration": status.get("desiredGeneration"),
        "observedGeneration": status.get("observedGeneration"),
        "conditions": status.get("conditions"),
        "evidenceRefs": status.get("evidenceRefs"),
    }
    print(
        "Last Workload observation before timeout: "
        + json.dumps(diagnostic, sort_keys=True, ensure_ascii=True),
        file=sys.stderr,
        flush=True,
    )
    print_provider_diagnostics(namespace, context)
    fail("timed out waiting for current-generation Kueue/provider observations")


def kubectl_json(namespace: str, kind: str, selector: str, context: str | None) -> dict[str, Any]:
    command = ["kubectl"]
    if context:
        command.extend(["--context", context])
    command.extend(["-n", namespace, "get", kind, "-l", selector, "-o", "json"])
    completed = subprocess.run(
        command,
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=30,
    )
    if completed.returncode != 0:
        fail(f"kubectl provider absence check failed for {kind}: exit={completed.returncode}")
    try:
        value = json.loads(completed.stdout)
    except ValueError:
        fail(f"kubectl returned invalid JSON for {kind}")
    if not isinstance(value, dict) or not isinstance(value.get("items"), list):
        fail(f"kubectl returned an invalid list for {kind}")
    return value


def verify_provider_present(
    namespace: str,
    workload: str,
    cluster: str,
    expected_generation: int,
    evidence_refs: list[Any],
    context: str | None,
) -> None:
    """Require real Job/Ready Pod evidence before issuing the cleanup DELETE."""
    selector = f"ai.compute/workload=job-{workload}"
    jobs = kubectl_json(namespace, "jobs", selector, context)["items"]
    if len(jobs) != 1:
        fail(f"expected exactly one provider Job before deletion, found {len(jobs)}")
    job = jobs[0]
    metadata = job.get("metadata") or {}
    annotations = metadata.get("annotations") or {}
    if metadata.get("name") != f"job-{workload}":
        fail(f"provider Job name does not match the Workload: {metadata.get('name')!r}")
    if annotations.get("ai.compute/generation") != str(expected_generation):
        fail("provider Job generation annotation does not match the original create receipt")

    pods = kubectl_json(namespace, "pods", selector, context)["items"]
    ready_running_pods = []
    for pod in pods:
        status = pod.get("status") or {}
        ready = any(
            condition.get("type") == "Ready"
            and condition.get("status") in (True, "True", "TRUE")
            for condition in (status.get("conditions") or [])
            if isinstance(condition, dict)
        )
        if status.get("phase") == "Running" and ready:
            ready_running_pods.append(pod)
    if not ready_running_pods:
        diagnostics = provider_resource_diagnostics("pods", pods)
        fail(
            "provider Job exists but no Running+Ready Pod proves execution before deletion: "
            f"{json.dumps(diagnostics, sort_keys=True, ensure_ascii=True)}"
        )

    refs = [ref for ref in evidence_refs if isinstance(ref, str)]
    expected_job_ref = f"k8s://{cluster}/namespaces/{namespace}/jobs/job-{workload}"
    expected_kueue_prefix = f"kueue://{cluster}/namespaces/{namespace}/workloads/"
    if expected_job_ref not in refs:
        fail(f"current-generation evidence omits the concrete Job reference {expected_job_ref}")
    if not any(ref.startswith(expected_kueue_prefix) for ref in refs):
        fail("current-generation evidence omits the namespaced Kueue Workload reference")


def verify_provider_absent(namespace: str, workload: str, context: str | None) -> None:
    """Fail immediately if a supposedly unused Workload ID already owns provider objects."""
    selector = f"ai.compute/workload=job-{workload}"
    for kind in ("jobs", "pods"):
        value = kubectl_json(namespace, kind, selector, context)
        if value["items"]:
            fail(f"provider resources already exist: {len(value['items'])} {kind} match Workload")


def provider_resource_diagnostics(kind: str, items: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Summarize only lifecycle fields needed to diagnose provider cleanup."""
    diagnostics: list[dict[str, Any]] = []
    for item in items[:20]:
        metadata = item.get("metadata") or {}
        status = item.get("status") or {}
        summary: dict[str, Any] = {
            "kind": kind,
            "name": metadata.get("name"),
            "uid": metadata.get("uid"),
            "deletionTimestamp": metadata.get("deletionTimestamp"),
            "labels": {
                key: value
                for key, value in (metadata.get("labels") or {}).items()
                if key == "ai.compute/workload"
            },
            "generationAnnotation": (metadata.get("annotations") or {}).get("ai.compute/generation"),
        }
        if kind == "pods":
            # Include attribution identity in a cleanup failure report. Labels
            # alone are not ownership proof, so retain the API-server owner UID
            # and controller bit needed to distinguish a Job-owned Pod from a
            # foreign same-label Pod without dumping the whole object.
            summary["ownerReferences"] = [
                {
                    "apiVersion": owner.get("apiVersion"),
                    "kind": owner.get("kind"),
                    "name": owner.get("name"),
                    "uid": owner.get("uid"),
                    "controller": owner.get("controller"),
                }
                for owner in (metadata.get("ownerReferences") or [])
                if isinstance(owner, dict)
            ]
            summary["phase"] = status.get("phase")
            summary["reason"] = status.get("reason")
            summary["conditions"] = [
                {"type": condition.get("type"), "status": condition.get("status"), "reason": condition.get("reason")}
                for condition in (status.get("conditions") or [])
                if isinstance(condition, dict)
            ]
            summary["containerStates"] = [
                {
                    "name": container.get("name"),
                    "state": list((container.get("state") or {}).keys()),
                    "restartCount": container.get("restartCount"),
                }
                for container in (status.get("containerStatuses") or [])
                if isinstance(container, dict)
            ]
        else:
            summary["active"] = status.get("active")
            summary["succeeded"] = status.get("succeeded")
            summary["failed"] = status.get("failed")
            summary["conditions"] = [
                {"type": condition.get("type"), "status": condition.get("status"), "reason": condition.get("reason")}
                for condition in (status.get("conditions") or [])
                if isinstance(condition, dict)
            ]
        diagnostics.append(summary)
    if len(items) > 20:
        diagnostics.append({"kind": kind, "truncatedCount": len(items) - 20})
    return diagnostics


def wait_for_provider_absent(
    namespace: str,
    workload: str,
    context: str | None,
    timeout: float,
) -> None:
    """Wait boundedly for Kubernetes foreground/graceful deletion to finish."""
    selector = f"ai.compute/workload=job-{workload}"
    deadline = time.monotonic() + max(0.0, timeout)
    last_items: dict[str, list[dict[str, Any]]] = {"jobs": [], "pods": []}
    while True:
        for kind in ("jobs", "pods"):
            value = kubectl_json(namespace, kind, selector, context)
            last_items[kind] = value["items"]
        if not last_items["jobs"] and not last_items["pods"]:
            return

        remaining = deadline - time.monotonic()
        if remaining <= 0:
            diagnostics = {
                kind: provider_resource_diagnostics(kind, items)
                for kind, items in last_items.items()
                if items
            }
            fail(
                "provider cleanup timed out; Kubernetes resources still match Workload "
                f"after {timeout:.1f}s: {json.dumps(diagnostics, sort_keys=True, ensure_ascii=True)}"
            )
        time.sleep(min(POLL_SECONDS, remaining))


def cleanup_workload(
    base_url: str,
    control_plane_url: str,
    token: str,
    cluster: str,
    project: str,
    workload: str,
    namespace: str,
    context: str | None,
    timeout: int,
    expected_image: str,
    expected_pool: str,
    expected_generation: int,
    expected_command: list[str],
    expected_accelerator_class: str,
    expected_quota: int,
) -> tuple[str, int]:
    path = resource_path(cluster, project, workload)
    status_code, desired = json_call(base_url, path, token)
    if status_code == 404:
        verify_provider_absent(namespace, workload, context)
        fail("Workload disappeared before this test issued its DELETE; release acceptance is inconclusive")
    if status_code != 200 or desired is None:
        fail(f"cannot safely inspect Workload before cleanup (HTTP {status_code})")
    if not desired_matches_create(
        desired,
        cluster=cluster,
        project=project,
        workload=workload,
        generation=expected_generation,
        image=expected_image,
        pool=expected_pool,
        command=expected_command,
        accelerator_class=expected_accelerator_class,
        quota=expected_quota,
    ):
        fail("cleanup refused: Desired identity/generation/spec no longer matches the Workload created by this run")
    # Never adopt the current generation as cleanup authority. Delete only the
    # exact generation authorized by the original durable create receipt.
    generation = expected_generation
    delete_id = secrets.token_hex(16)
    run_faulted_caller(
        base_url,
        path,
        delete_id,
        b"",
        timeout,
        method="DELETE",
        if_match=f'"{generation}"',
        accepted_statuses=(202, 204),
    )
    print(f"PASS: DELETE intent committed then ACK dropped; releaseRequestId={delete_id}; expectedGeneration={generation}; caller=SIGKILL")

    code, delete_receipt = json_call(base_url, receipt_path(cluster, project, delete_id), token)
    if code != 200 or delete_receipt is None:
        fail(f"lost DELETE ACK could not be recovered from its receipt (HTTP {code})")
    delete_result_generation = verify_receipt(
        delete_receipt,
        request_id=delete_id,
        cluster=cluster,
        project=project,
        workload=workload,
        action="delete-request",
        expected_generation=generation,
    )

    # Explicitly replay the same release operation ID. It must not create a
    # second tombstone/operation or change the immutable receipt snapshot.
    replay_status, _ = http_call(
        base_url,
        "DELETE",
        path,
        token,
        request_id=delete_id,
        if_match=f'"{generation}"',
        timeout=30,
    )
    if replay_status not in (202, 204):
        fail(f"same-key DELETE replay returned HTTP {replay_status}")
    replay_code, replay_receipt = json_call(
        base_url, receipt_path(cluster, project, delete_id), token
    )
    if (
        replay_code != 200
        or replay_receipt is None
        or not receipt_snapshot_matches(
            delete_receipt,
            replay_receipt,
            expected_generation=delete_result_generation,
        )
    ):
        fail("same-key DELETE replay changed or obscured the original operation receipt")
    print(f"PASS: same-key DELETE replay reused the single committed release receipt; releaseRequestId={delete_id}; resultingGeneration={delete_result_generation}")
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        management_status, current = json_call(base_url, path, token)
        runtime_status, _ = json_call(control_plane_url, f"/api/v1/workloads/{workload}", "")
        if management_status == 404 and runtime_status == 404:
            # API finalization may complete just before Kubernetes finishes a
            # graceful Pod deletion. Keep the same bounded cleanup deadline and
            # require actual Job/Pod absence; never accept 404 alone as proof.
            wait_for_provider_absent(
                namespace,
                workload,
                context,
                timeout=max(0.0, deadline - time.monotonic()),
            )
            return delete_id, generation
        if management_status not in (200, 404) or runtime_status not in (200, 404):
            fail("unexpected API error while waiting for cleanup/finalization")
        if current is not None and current.get("deleting") is not True:
            fail("deletion was not durably projected while waiting for finalization")
        time.sleep(POLL_SECONDS)
    fail("cleanup timed out; Workload/tombstone must remain available for reconciliation")


def main() -> int:
    base_url = local_http_base("MANAGEMENT_BASE_URL", "http://127.0.0.1:8081")
    control_plane_url = local_http_base("CONTROL_PLANE_BASE_URL", "http://127.0.0.1:8080")
    token = required_env("OIDC_ACCESS_TOKEN")
    cluster = safe_id("CLUSTER_ID", required_env("CLUSTER_ID"))
    project = safe_id("PROJECT_ID", required_env("PROJECT_ID"))
    pool = safe_id("POOL_ID", required_env("POOL_ID"))
    namespace = safe_id("NAMESPACE", required_env("NAMESPACE"))
    accelerator_class = required_env("ACCELERATOR_CLASS")
    if len(accelerator_class) > 128:
        fail("ACCELERATOR_CLASS is too long")
    image = required_env("IMAGE")
    if not IMAGE_DIGEST_RE.search(image):
        fail("IMAGE must be immutable and end in @sha256:<64 lowercase hex>")
    quota = int(os.environ.get("ACCELERATOR_QUOTA", "1"))
    if quota < 1:
        fail("ACCELERATOR_QUOTA must be positive")
    timeout = timeout_seconds()
    context = os.environ.get("KUBECTL_CONTEXT", "").strip() or None

    request_id = secrets.token_hex(16)
    workload = safe_id("WORKLOAD_ID", os.environ.get("WORKLOAD_ID", f"ack-loss-{request_id[:12]}"))
    command_raw = os.environ.get("COMMAND_JSON", '["/bin/sh","-c","sleep 3600"]')
    command = json.loads(command_raw)
    if not isinstance(command, list) or not command or not all(isinstance(item, str) for item in command):
        fail("COMMAND_JSON must be a non-empty JSON array of strings")
    body = json.dumps(
        {
            "expectedGeneration": 0,
            "poolId": pool,
            "spec": {
                "image": image,
                "command": command,
                "accelerator": {"class": accelerator_class, "quota": quota},
            },
        },
        separators=(",", ":"),
    ).encode("utf-8")
    create_path = resource_path(cluster, project, workload)
    receipt_url = receipt_path(cluster, project, request_id)

    # Never let an override cause this harness to delete a pre-existing object.
    # Require both Desired absence and absence of same-label provider objects
    # before attempting the create.
    existing_status, _ = json_call(base_url, create_path, token)
    if existing_status != 404:
        fail(f"refusing to reuse Workload ID {workload}: initial Management GET returned HTTP {existing_status}")
    verify_provider_absent(namespace, workload, context)
    print(f"Starting lost-ACK acceptance: workloadId={workload}; requestId={request_id}")

    may_have_committed = False
    receipt_proven = False
    cleanup_authorized = False
    cleanup_generation: int | None = None
    try:
        # From this point an exception/transport error cannot prove that the
        # server did not commit. The finally block only deletes after proving
        # ownership through this exact request's durable operation receipt.
        may_have_committed = True
        run_faulted_caller(base_url, create_path, request_id, body, timeout)
        code, receipt = json_call(base_url, receipt_url, token)
        if code != 200 or receipt is None:
            fail(f"lost-ACK recovery could not read owner-scoped receipt (HTTP {code})")
        generation = verify_receipt(
            receipt,
            request_id=request_id,
            cluster=cluster,
            project=project,
            workload=workload,
            action="create",
            expected_generation=0,
        )
        receipt_proven = True
        code, desired = json_call(base_url, create_path, token)
        if code != 200 or desired is None:
            fail(f"committed Desired state is not readable after lost ACK (HTTP {code})")
        if not desired_matches_create(
            desired,
            cluster=cluster,
            project=project,
            workload=workload,
            generation=generation,
            image=image,
            pool=pool,
            command=command,
            accelerator_class=accelerator_class,
            quota=quota,
        ):
            fail("authorized Desired state does not exactly match the operation receipt/request")
        cleanup_authorized = True
        cleanup_generation = generation
        print(f"PASS: lost ACK recovered by original requestId={request_id}; generation={generation}")

        replay_status, _ = http_call(
            base_url,
            "PUT",
            create_path,
            token,
            request_id=request_id,
            body=body,
            timeout=30,
        )
        if replay_status != 204:
            fail(f"identical replay with original Idempotency-Key returned HTTP {replay_status}")
        code2, replayed_receipt = json_call(base_url, receipt_url, token)
        code3, replayed_desired = json_call(base_url, create_path, token)
        if code2 != 200 or replayed_receipt is None or code3 != 200 or replayed_desired is None:
            fail("receipt or Desired state became unavailable after identical replay")
        generation2 = verify_receipt(
            replayed_receipt,
            request_id=request_id,
            cluster=cluster,
            project=project,
            workload=workload,
            action="create",
            expected_generation=0,
        )
        if (
            generation2 != generation
            or not receipt_snapshot_matches(receipt, replayed_receipt, expected_generation=generation)
            or replayed_desired.get("generation") != generation
        ):
            fail("same-key replay changed the authoritative receipt or created a new generation")
        print("PASS: identical retry reused the single Desired commit and operation receipt")

        # Reusing the original key for a different request must conflict and
        # must not mutate the already committed Workload.
        tampered_payload = json.loads(body)
        tampered_payload["spec"]["image"] = (
            "registry.invalid/replay-conflict@sha256:" + ("0" * 64)
        )
        tampered_body = json.dumps(tampered_payload, separators=(",", ":")).encode("utf-8")
        conflict_status, _ = http_call(
            base_url,
            "PUT",
            create_path,
            token,
            request_id=request_id,
            body=tampered_body,
            timeout=30,
        )
        if conflict_status != 409:
            fail(f"same requestId with changed body returned HTTP {conflict_status}, expected 409")
        code, unchanged = json_call(base_url, create_path, token)
        receipt_status_after_conflict, receipt_after_conflict = json_call(base_url, receipt_url, token)
        if (
            code != 200
            or unchanged is None
            or not desired_matches_create(
                unchanged,
                cluster=cluster,
                project=project,
                workload=workload,
                generation=generation,
                image=image,
                pool=pool,
                command=command,
                accelerator_class=accelerator_class,
                quota=quota,
            )
        ):
            fail("idempotency-key conflict changed or obscured the committed Workload")
        if (
            receipt_status_after_conflict != 200
            or receipt_after_conflict is None
            or not receipt_snapshot_matches(receipt, receipt_after_conflict, expected_generation=generation)
        ):
            fail("idempotency-key conflict changed or obscured the original operation receipt")
        print("PASS: changed-body replay rejected with 409; Desired and receipt remained unchanged")

        observed = wait_for_workload(
            control_plane_url, workload, generation, timeout, namespace, context
        )
        status = observed.get("status") or {}
        verify_provider_present(
            namespace,
            workload,
            cluster,
            generation,
            status.get("evidenceRefs") or [],
            context,
        )
        print(
            "PASS: provider status synchronized; "
            f"syncState={status.get('syncState')} generation={generation}"
        )
        print("PASS: concrete Job, Running+Ready Pod and Kueue evidence verified before DELETE")
    finally:
        if may_have_committed and not receipt_proven:
            # A lost connection or test-process error may happen after commit.
            # Reconcile by the original ID; never delete based only on matching
            # names, labels, image or pool.
            probe_status, probe_receipt = json_call(base_url, receipt_url, token)
            if probe_status == 200 and probe_receipt is not None:
                try:
                    recovered_generation = verify_receipt(
                        probe_receipt,
                        request_id=request_id,
                        cluster=cluster,
                        project=project,
                        workload=workload,
                        action="create",
                        expected_generation=0,
                    )
                except Exception as exc:
                    fail(
                        f"cannot prove ownership for workloadId={workload} requestId={request_id}; "
                        f"automatic cleanup refused ({type(exc).__name__})"
                    )
                probe_code, probe_desired = json_call(base_url, create_path, token)
                if probe_code != 200 or probe_desired is None or not desired_matches_create(
                    probe_desired,
                    cluster=cluster,
                    project=project,
                    workload=workload,
                    generation=recovered_generation,
                    image=image,
                    pool=pool,
                    command=command,
                    accelerator_class=accelerator_class,
                    quota=quota,
                ):
                    fail(
                        f"receipt found but exact Desired identity/generation/spec could not be reconciled; "
                        f"preserve for manual review (workloadId={workload}, requestId={request_id})"
                    )
                receipt_proven = True
                cleanup_authorized = True
                cleanup_generation = recovered_generation
                print(f"PASS: recovered cleanup authority from original receipt for workloadId={workload}")
            elif probe_status == 404:
                print(
                    f"RECEIPT UNAVAILABLE (HTTP 404): skipped destructive cleanup; "
                    f"reconcile workloadId={workload}, requestId={request_id} before retrying"
                )
            else:
                fail(
                    f"commit outcome is unknown and owner receipt is unavailable (HTTP {probe_status}); "
                    f"no delete attempted; reconcile workloadId={workload}, requestId={request_id}"
                )
        if cleanup_authorized and cleanup_generation is not None:
            release_request_id, released_generation = cleanup_workload(
                base_url,
                control_plane_url,
                token,
                cluster,
                project,
                workload,
                namespace,
                context,
                timeout,
                image,
                pool,
                expected_generation=cleanup_generation,
                expected_command=command,
                expected_accelerator_class=accelerator_class,
                expected_quota=quota,
            )
            print(f"PASS: release finalized; releaseRequestId={release_request_id}; generation={released_generation}; Management/ControlPlane=404; Job/Pod independently absent")
    return 0


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--client":
        try:
            # Remaining flags are parsed strictly to keep caller arguments deterministic.
            import argparse

            parser = argparse.ArgumentParser()
            parser.add_argument("--client", action="store_true")
            parser.add_argument("--url", required=True)
            parser.add_argument("--path", required=True)
            parser.add_argument("--request-id", required=True)
            parser.add_argument("--body-base64", required=True)
            parser.add_argument("--method", choices=("PUT", "DELETE"), default="PUT")
            parser.add_argument("--if-match", default="")
            args = parser.parse_args(sys.argv[1:])
            token = required_env("OIDC_ACCESS_TOKEN")
            headers = {
                "Authorization": f"Bearer {token}",
                "Idempotency-Key": args.request_id,
            }
            body = base64.b64decode(args.body_base64, validate=True)
            if args.method == "PUT":
                headers["Content-Type"] = "application/json"
            if args.if_match:
                headers["If-Match"] = args.if_match
            req = urllib.request.Request(
                args.url + args.path,
                data=body,
                headers=headers,
                method=args.method,
            )
            with urllib.request.urlopen(req, timeout=90) as response:
                response.read()
            sys.exit(0)
        except Exception:
            sys.exit(1)
    try:
        sys.exit(main())
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        sys.exit(1)
