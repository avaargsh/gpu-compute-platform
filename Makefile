SHELL := /bin/bash

.PHONY: fmt fmt-check test vet build acceptance-contract kind-up install-kueue install-fake-gpu e2e-golden

fmt:
	bash scripts/go-format.sh

fmt-check:
	bash scripts/go-format.sh --check

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/control-plane
	go build ./cmd/cluster-agent

acceptance-contract:
	go test ./internal/agent -run 'TestRunner(RestartReplaysDesiredSafely|BacksOffRetryableFailureAndResetsOnNewGeneration|SkipsProviderWhenLeaseIsContended|DeletionReplaysAfterFinalObservationFailure|DeletionReplaysAfterFinalizeFailure)$'
	go test ./internal/store/postgres -run 'TestPostgres(ReconcileLeaseOwnershipAndExpiry|LeaseTakeoverFencesStaleReportAndFinalize|FinalizeDesiredAtomicallyCleansRuntimeState|TombstoneFencesOldGenerationAndAllowsNewerRecreate|StateSurvivesStoreReconstruction|RestartPreservesDeletionIntentAndTombstoneFence|CreateWorkloadDesiredRejectsConcurrentPoolDelete|CreateWorkloadDesiredPreservesLostAckReplay|FinalizeDesiredOwnedIsIdempotentAfterLostAck|UpsertCannotCancelDeletionLifecycle|RecreateWaitsForFinalizationReceiptAndConsumesTombstone|OwnedFinalizePrefersCurrentDesiredOverOlderTombstone)$'
	go test ./internal/platform/httpapi -run 'TestResourceAPI(RequiresDeleteRecreateForWorkloadChanges|RejectsStaleWorkloadGenerationWithoutRollback)$'

kind-up:
	kind get clusters | grep -qx kind-golden || kind create cluster --name kind-golden --wait 120s

install-kueue:
	kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/latest/download/manifests.yaml
	kubectl wait --for=condition=Available deployment/kueue-controller-manager -n kueue-system --timeout=180s

install-fake-gpu:
	bash scripts/e2e/install-fake-gpu.sh

e2e-golden:
	bash scripts/e2e/go-kind-kueue-golden.sh
