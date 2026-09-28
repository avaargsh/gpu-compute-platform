package kueue

import (
	"context"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type Provider struct {
	client Client
}

func NewProvider(client Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) ReconcilePool(ctx context.Context, projection baseprovider.PoolProjection) (baseprovider.PoolObservation, error) {
	if p.client == nil {
		return baseprovider.PoolObservation{}, fmt.Errorf("kueue client is required")
	}

	resources, err := ProjectPool(projection)
	if err != nil {
		return baseprovider.PoolObservation{}, err
	}

	for _, flavor := range resources.Flavors {
		if err := p.client.ApplyResourceFlavor(ctx, flavor); err != nil {
			return baseprovider.PoolObservation{}, fmt.Errorf("apply resource flavor %s: %w", flavor.Name, classifyProviderError(err))
		}
	}
	if err := p.client.ApplyClusterQueue(ctx, resources.ClusterQueue); err != nil {
		return baseprovider.PoolObservation{}, fmt.Errorf("apply cluster queue: %w", classifyProviderError(err))
	}
	if err := p.client.ApplyLocalQueue(ctx, resources.LocalQueue); err != nil {
		return baseprovider.PoolObservation{}, fmt.Errorf("apply local queue: %w", classifyProviderError(err))
	}

	return baseprovider.PoolObservation{
		ObservedGeneration: projection.Generation,
		Conditions: []domain.Condition{
			{
				Type:               "Ready",
				Status:             "True",
				Reason:             "ResourcesApplied",
				Message:            "compute pool resources reconciled",
				LastTransitionTime: time.Now().UTC(),
			},
		},
		EvidenceRefs: []string{
			fmt.Sprintf("k8s://%s/clusterqueue/%s", projection.ClusterID, resources.ClusterQueue.Name),
			fmt.Sprintf("k8s://%s/namespaces/%s/localqueue/%s", projection.ClusterID, resources.LocalQueue.Namespace, resources.LocalQueue.Name),
		},
	}, nil
}

func (p *Provider) DeletePool(ctx context.Context, projection baseprovider.PoolProjection) (baseprovider.DeletionObservation, error) {
	if p.client == nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("kueue client is required")
	}
	resources, err := ProjectPool(projection)
	if err != nil {
		return baseprovider.DeletionObservation{}, err
	}

	evidence := []string{
		fmt.Sprintf("k8s://%s/clusterqueue/%s", projection.ClusterID, resources.ClusterQueue.Name),
		fmt.Sprintf("k8s://%s/namespaces/%s/localqueue/%s", projection.ClusterID, resources.LocalQueue.Namespace, resources.LocalQueue.Name),
	}
	allGone := true

	gone, err := p.client.DeleteLocalQueue(ctx, resources.LocalQueue.Namespace, resources.LocalQueue.Name)
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("delete local queue: %w", classifyProviderError(err))
	}
	allGone = allGone && gone

	gone, err = p.client.DeleteClusterQueue(ctx, resources.ClusterQueue.Name)
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("delete cluster queue: %w", classifyProviderError(err))
	}
	allGone = allGone && gone

	// ResourceFlavor is cluster-scoped and may be shared by multiple ComputePools.
	// A pool finalizer must only delete pool-owned resources. Shared flavor garbage
	// collection requires explicit ownership/reference tracking at the control plane.
	return baseprovider.DeletionObservation{Gone: allGone, EvidenceRefs: evidence}, nil
}
