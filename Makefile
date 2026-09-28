SHELL := /bin/bash

.PHONY: migrate compose-migrate test contract-test kind-up install-kueue e2e-golden

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

e2e-golden:
	./scripts/e2e/golden-path.sh
