import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import kserve_service as k

D = {
    "apiVersion": "serving.kserve.io/v1beta1", "kind": "InferenceService",
    "metadata": {"name": "model-a", "namespace": "research",
                 "annotations": {"ai.compute/project-id": "research"}},
    "spec": {"predictor": {"minReplicas": 1, "maxReplicas": 1,
                           "containers": [{"name": "kserve-container",
                                          "image": "registry.example/llm@sha256:" + "a" * 64,
                                          "args": ["--model", "/models/a"],
                                          "ports": [{"containerPort": 8000, "protocol": "TCP"}],
                                          "resources": {"requests": {"cpu": "1", "memory": "16Gi", "nvidia.com/gpu": 1},
                                                        "limits": {"cpu": "1", "memory": "16Gi", "nvidia.com/gpu": 1}}}]}}
}


def observed(data=D, ready="True"):
    value = copy.deepcopy(data)
    value["metadata"].update(uid="uid-1", resourceVersion="rv-1", generation=1)
    value["status"] = {"conditions": [{"type": "Ready", "status": ready, "reason": "PredictorReady"}]}
    return value


class KServeServiceTests(unittest.TestCase):
    def fixture(self, value=D):
        file = tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False)
        json.dump(value, file)
        file.close()
        self.addCleanup(lambda: Path(file.name).unlink(missing_ok=True))
        return file.name

    def test_plan_is_offline(self):
        with patch.object(k.subprocess, "run") as run:
            self.assertEqual(k.main([self.fixture()]), 0)
        run.assert_not_called()

    def test_explicit_create_never_apply_or_delete(self):
        with patch.object(k.subprocess, "run", return_value=CompletedProcess([], 0, json.dumps(observed()), "")) as run:
            self.assertEqual(k.main([self.fixture(), "--context", "kind-gpu", "--create"]), 0)
            self.assertEqual(run.call_args.args[0],
                             ["kubectl", "--context", "kind-gpu", "--namespace", "research",
                              "create", "-f", "-", "-o", "json"])
            self.assertEqual(json.loads(run.call_args.kwargs["input"]), D)

    def test_status_only_reads_exact_service(self):
        with patch.object(k.subprocess, "run", return_value=CompletedProcess([], 0, json.dumps(observed(ready="False")), "")) as run:
            self.assertEqual(k.main([self.fixture(), "--context", "kind-gpu", "--status"]), 0)
            self.assertEqual(run.call_args.args[0][-4:],
                             ["inferenceservices.serving.kserve.io", "model-a", "-o", "json"])
            self.assertIsNone(run.call_args.kwargs["input"])

    def test_status_reports_not_http_proof(self):
        self.assertEqual(k.observation(observed(), D)["status"],
                         "CONTROLLER_REPORTED_READY_NOT_HTTP_PROVEN")
        self.assertFalse(k.observation(observed(ready="False"), D)["reportedReady"])

    def test_absent_status_and_untrusted_identity_are_not_success(self):
        for mutate in [
            lambda d: d.pop("status"),
            lambda d: d["metadata"].update(namespace="other"),
            lambda d: d["metadata"].update(uid=""),
            lambda d: d["metadata"]["annotations"].update({"ai.compute/project-id": "foreign"}),
            lambda d: d["spec"]["predictor"]["containers"][0].update(image="foreign/image"),
            lambda d: d["status"]["conditions"].append({"type": "Ready", "status": "False"}),
        ]:
            data = observed()
            mutate(data)
            self.assertFalse(k.observation(data, D)["reportedReady"], data)

    def test_bad_image_unsupported_modes_and_injected_metadata_rejected(self):
        for change in [
            lambda d: d["spec"]["predictor"]["containers"][0].update(image="latest"),
            lambda d: d["metadata"].update(namespace="../other"),
            lambda d: d["spec"]["predictor"].update(maxReplicas=3),
            lambda d: d["spec"]["predictor"]["containers"][0]["resources"]["limits"].update({"nvidia.com/gpu": 0}),
            lambda d: d["spec"]["predictor"]["containers"][0].update(command=["/bin/sh", "-c", "unsafe"]),
            lambda d: d["metadata"]["annotations"].update({"some-other": "injected"}),
        ]:
            data = copy.deepcopy(D)
            change(data)
            with self.assertRaises(k.InvalidServing):
                k.validated(data)

    def test_cluster_operations_need_explicit_context(self):
        self.assertEqual(k.main([self.fixture(), "--create"]), 2)

    def test_duplicate_json_key_rejected(self):
        p = Path(self.fixture())
        p.write_text(p.read_text().replace('"kind": "InferenceService"',
                                            '"kind": "Other", "kind": "InferenceService"'))
        self.assertEqual(k.main([str(p)]), 2)


if __name__ == "__main__":
    unittest.main()
