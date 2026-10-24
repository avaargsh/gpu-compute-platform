SHELL := /bin/bash

.PHONY: fmt fmt-check test test-parallel-isolated vet build supply-chain-check acceptance-contract stage-b-volcano-contract kind-up install-kueue install-fake-gpu e2e-golden

fmt:
	bash scripts/go-format.sh

fmt-check:
	bash scripts/go-format.sh --check

test:
	# Safe local default when one TEST_POSTGRES_DSN is shared across packages.
	go test -p 1 ./...

# CI provisions independent databases for Management and Store packages.
# Do not use package-level concurrency with a shared DSN.
test-parallel-isolated:
	@printenv TEST_POSTGRES_DSN | grep -q . || (echo "TEST_POSTGRES_DSN is required" >&2; exit 1)
	@printenv TEST_POSTGRES_STORE_DSN | grep -q . || (echo "TEST_POSTGRES_STORE_DSN is required" >&2; exit 1)
	go test -p 4 ./...

vet:
	go vet ./...

build:
	go build ./cmd/control-plane
	go build ./cmd/cluster-agent
	go build ./cmd/management-api

supply-chain-check:
	bash scripts/verify-production-images.sh

acceptance-contract:
	go test ./internal/agent -run 'Test(LifecycleRegistersDiscoveredCapabilities|Runner(RestartReplaysDesiredSafely|BacksOffRetryableFailureAndResetsOnNewGeneration|SkipsProviderWhenLeaseIsContended|DeletionReplaysAfterFinalObservationFailure|DeletionReplaysAfterFinalizeFailure))$$'
	go test ./internal/cluster -run 'TestProductionRuntimeRegistersOnlyAcceptedKueueProvider$$'
	go test ./internal/provider/kueue -run 'TestApplyJob(ReplaysAfterLostCreateAckWithoutDuplicate|AdoptsSameGenerationAfterCreateRace|CreateRaceRejectsDifferentGeneration)$$'
	go test ./internal/store/postgres -run 'TestPostgres(AgentRegistrationPersistsCapabilitiesAndHeartbeat|ReconcileLeaseOwnershipAndExpiry|LeaseTakeoverFencesStaleReportAndFinalize|FinalizeDesiredAtomicallyCleansRuntimeState|TombstoneFencesOldGenerationAndAllowsNewerRecreate|StateSurvivesStoreReconstruction|RestartPreservesDeletionIntentAndTombstoneFence|CreateWorkloadDesiredRejectsConcurrentPoolDelete|CreateWorkloadDesiredPreservesLostAckReplay|FinalizeDesiredOwnedIsIdempotentAfterLostAck|UpsertCannotCancelDeletionLifecycle|RecreateWaitsForFinalizationReceiptAndConsumesTombstone|OwnedFinalizePrefersCurrentDesiredOverOlderTombstone|LifecycleRaceStressPreservesDeletionFence|LeaseClaimStressHasSingleLiveOwner)$$'
	go test ./internal/platform/httpapi -run 'Test(ClusterStatusReturnsRegisteredCapabilities|ResourceAPI(RequiresDeleteRecreateForWorkloadChanges|RejectsStaleWorkloadGenerationWithoutRollback))$$'

stage-b-volcano-contract:
	go test ./internal/provider/volcano -run 'Test(Project|Volcano|Classify|Ensure|Dynamic|Observe|Delete)'

kind-up:
	kind get clusters | grep -qx kind-golden || kind create cluster --name kind-golden --wait 120s

install-kueue:
	kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/latest/download/manifests.yaml
	kubectl wait --for=condition=Available deployment/kueue-controller-manager -n kueue-system --timeout=180s

install-fake-gpu:
	bash scripts/e2e/install-fake-gpu.sh

e2e-golden:
	bash scripts/e2e/go-kind-kueue-golden.sh
