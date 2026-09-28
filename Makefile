SHELL := /bin/bash

.PHONY: fmt fmt-check install-hooks migrate compose-migrate test contract-test kind-up install-kueue install-fake-gpu e2e-golden e2e-gpu-golden e2e-up e2e-gpu-up

fmt:
	bash scripts/go-format.sh

fmt-check:
	bash scripts/go-format.sh --check

install-hooks:
	bash scripts/install-git-hooks.sh

migrate:
	uv run alembic upgrade head

compose-migrate:
	docker compose run --rm app alembic upgrade head

test:
	uv run pytest -q

contract-test:
	uv run pytest -q tests/test_control_plane_contracts.py tests/test_control_plane_v2_contracts.py tests/test_workload_gates.py tests/test_compute_pool.py tests/test_compute_pool_reconcile.py tests/test_kueue_adapter.py tests/test_kueue_scheduler_provider.py

kind-up:
	kind get clusters | grep -qx ai-compute || kind create cluster --name ai-compute --wait 120s

install-kueue:
	kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/latest/download/manifests.yaml
	kubectl wait --for=condition=Available deployment/kueue-controller-manager -n kueue-system --timeout=180s

install-fake-gpu:
	bash scripts/e2e/install-fake-gpu.sh

e2e-golden:
	bash scripts/e2e/golden-path.sh

e2e-gpu-golden:
	bash scripts/e2e/fake-gpu-golden.sh

e2e-up:
	./scripts/e2e/up.sh

e2e-gpu-up:
	bash scripts/e2e/gpu-up.sh
