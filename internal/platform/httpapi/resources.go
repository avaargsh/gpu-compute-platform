package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type ResourceAPI struct {
	store     agentstore.Store
	placement PlacementResolver
}

func NewResourceAPI(store agentstore.Store, placement PlacementResolver) *ResourceAPI {
	return &ResourceAPI{store: store, placement: placement}
}

func (a *ResourceAPI) UpsertComputePool(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	var in domain.ComputePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid compute pool", http.StatusBadRequest)
		return
	}
	if resourceID == "" || in.Metadata.ID == "" || in.Metadata.Generation <= 0 || in.ProjectID == "" {
		http.Error(w, "resource id, projectId and positive generation are required", http.StatusBadRequest)
		return
	}
	if in.Metadata.ID != resourceID {
		http.Error(w, "resource id must match path", http.StatusBadRequest)
		return
	}
	if err := validateComputePoolSpec(in.Spec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	projectPlacement, err := a.placement.ResolveProject(r.Context(), in.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	poolPlacement, err := a.placement.ResolvePool(r.Context(), in.Metadata.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if projectPlacement.ClusterID != poolPlacement.ClusterID {
		http.Error(w, "project and pool bindings target different clusters", http.StatusConflict)
		return
	}
	spec := map[string]any{
		"provider":            poolPlacement.Provider,
		"projectID":           in.ProjectID,
		"namespace":           projectPlacement.Namespace,
		"accelerators":        in.Spec.Accelerators,
		"acceleratorBindings": in.Spec.AcceleratorBindings,
		"scheduling":          in.Spec.Scheduling,
	}
	if err := a.store.UpsertDesired(r.Context(), poolPlacement.ClusterID, agent.DesiredResource{
		Kind: "ComputePool", ID: in.Metadata.ID, Generation: in.Metadata.Generation, Spec: spec,
	}); err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) ||
			errors.Is(err, agentstore.ErrDesiredDeleting) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ResourceAPI) UpsertWorkload(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	var in domain.Workload
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid workload", http.StatusBadRequest)
		return
	}
	if resourceID == "" || in.Metadata.ID == "" || in.Metadata.Generation <= 0 || in.ProjectID == "" || in.PoolID == "" {
		http.Error(w, "resource id, projectId, poolId and positive generation are required", http.StatusBadRequest)
		return
	}
	if in.Metadata.ID != resourceID {
		http.Error(w, "resource id must match path", http.StatusBadRequest)
		return
	}
	projectPlacement, err := a.placement.ResolveProject(r.Context(), in.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	poolPlacement, err := a.placement.ResolvePool(r.Context(), in.PoolID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if projectPlacement.ClusterID != poolPlacement.ClusterID {
		http.Error(w, "project and pool bindings target different clusters", http.StatusConflict)
		return
	}

	spec := map[string]any{
		"provider":    poolPlacement.Provider,
		"projectID":   in.ProjectID,
		"poolID":      in.PoolID,
		"namespace":   projectPlacement.Namespace,
		"image":       in.Spec.Image,
		"command":     in.Spec.Command,
		"accelerator": in.Spec.Accelerator,
	}

	// The Store owns replay, identity and parent-lifecycle admission as one
	// atomic operation. Do not preflight here: a lookup followed by a write would
	// reopen TOCTOU races with concurrent finalization or cross-cluster create.
	err = a.store.CreateWorkloadDesired(
		r.Context(),
		poolPlacement.ClusterID,
		in.PoolID,
		in.Spec.Accelerator.Class,
		agent.DesiredResource{
			Kind: "Workload", ID: in.Metadata.ID, Generation: in.Metadata.Generation, Spec: spec,
		},
	)
	if err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) ||
			errors.Is(err, agentstore.ErrIdentityConflict) ||
			errors.Is(err, agentstore.ErrDesiredNotDeleting) ||
			errors.Is(err, agentstore.ErrDesiredDeleting) ||
			errors.Is(err, agentstore.ErrComputePoolNotFound) ||
			errors.Is(err, agentstore.ErrComputePoolDeleting) ||
			errors.Is(err, agentstore.ErrAcceleratorBindingNotFound) ||
			errors.Is(err, agentstore.ErrProviderIdentityMismatch) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func desiredSpecEqual(a, b map[string]any) (bool, error) {
	left, err := json.Marshal(a)
	if err != nil {
		return false, err
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	return string(left) == string(right), nil
}

type workloadView struct {
	Metadata  domain.Metadata     `json:"metadata"`
	ProjectID domain.ID           `json:"projectId"`
	PoolID    domain.ID           `json:"poolId"`
	Spec      domain.WorkloadSpec `json:"spec"`
	Status    resourceState       `json:"status"`
}

func projectWorkload(desired agent.DesiredResource) (workloadView, error) {
	raw, err := json.Marshal(desired.Spec)
	if err != nil {
		return workloadView{}, err
	}
	var projected struct {
		ProjectID   domain.ID                 `json:"projectID"`
		PoolID      domain.ID                 `json:"poolID"`
		Image       string                    `json:"image"`
		Command     []string                  `json:"command"`
		Accelerator domain.AcceleratorRequest `json:"accelerator"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		return workloadView{}, err
	}
	return workloadView{
		Metadata:  domain.Metadata{ID: desired.ID, Generation: desired.Generation},
		ProjectID: projected.ProjectID,
		PoolID:    projected.PoolID,
		Spec:      domain.WorkloadSpec{Image: projected.Image, Command: projected.Command, Accelerator: projected.Accelerator},
	}, nil
}

func (a *ResourceAPI) GetWorkload(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	if resourceID == "" {
		http.Error(w, "resource id is required", http.StatusBadRequest)
		return
	}
	clusterID, desired, found, err := a.store.LocateDesired(r.Context(), "Workload", resourceID)
	if err != nil {
		if errors.Is(err, agentstore.ErrIdentityConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	poolID := domain.ID(fmt.Sprint(desired.Spec["poolID"]))
	if poolID == "" {
		http.Error(w, "workload pool identity is missing", http.StatusInternalServerError)
		return
	}
	placement, err := a.placement.ResolvePool(r.Context(), poolID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if placement.ClusterID != clusterID {
		http.Error(w, "workload desired state and pool binding target different clusters", http.StatusConflict)
		return
	}
	observation, observed, err := a.store.GetObservation(r.Context(), clusterID, "Workload", resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := projectWorkload(desired)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out.Status = projectResourceState(desired.Generation, observed, observation.ObservedGeneration, observation.Conditions, observation.EvidenceRefs)
	out.Status.Kind = desired.Kind
	out.Status.ID = desired.ID
	writeJSON(w, http.StatusOK, out)
}

type computePoolView struct {
	Metadata  domain.Metadata        `json:"metadata"`
	ProjectID domain.ID              `json:"projectId"`
	Spec      domain.ComputePoolSpec `json:"spec"`
	Status    resourceState          `json:"status"`
}

func projectComputePool(desired agent.DesiredResource) (computePoolView, error) {
	raw, err := json.Marshal(desired.Spec)
	if err != nil {
		return computePoolView{}, err
	}
	var projected struct {
		ProjectID           domain.ID                   `json:"projectID"`
		Accelerators        []domain.AcceleratorRequest `json:"accelerators"`
		AcceleratorBindings []domain.AcceleratorBinding `json:"acceleratorBindings"`
		Scheduling          domain.SchedulingPolicy     `json:"scheduling"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		return computePoolView{}, err
	}
	return computePoolView{
		Metadata:  domain.Metadata{ID: desired.ID, Generation: desired.Generation},
		ProjectID: projected.ProjectID,
		Spec: domain.ComputePoolSpec{
			Accelerators: projected.Accelerators, AcceleratorBindings: projected.AcceleratorBindings, Scheduling: projected.Scheduling,
		},
	}, nil
}

func (a *ResourceAPI) GetComputePool(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	if resourceID == "" {
		http.Error(w, "resource id is required", http.StatusBadRequest)
		return
	}
	placement, err := a.placement.ResolvePool(r.Context(), resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	desired, found, err := a.store.GetDesired(r.Context(), placement.ClusterID, "ComputePool", resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	observation, observed, err := a.store.GetObservation(r.Context(), placement.ClusterID, "ComputePool", resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := projectComputePool(desired)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out.Status = projectResourceState(desired.Generation, observed, observation.ObservedGeneration, observation.Conditions, observation.EvidenceRefs)
	out.Status.Kind = desired.Kind
	out.Status.ID = desired.ID
	writeJSON(w, http.StatusOK, out)
}


func validateComputePoolSpec(spec domain.ComputePoolSpec) error {
	bindings := make(map[string]domain.AcceleratorBinding, len(spec.AcceleratorBindings))
	for _, binding := range spec.AcceleratorBindings {
		if binding.Class == "" || binding.Flavor == "" {
			return fmt.Errorf("accelerator binding class and flavor are required")
		}
		mode := binding.AllocationMode
		if mode == "" {
			mode = domain.AcceleratorAllocationExtendedResource
		}
		switch mode {
		case domain.AcceleratorAllocationExtendedResource:
			if binding.ResourceName == "" {
				return fmt.Errorf("extended-resource binding %q requires resourceName", binding.Class)
			}
			if binding.DRA != nil {
				return fmt.Errorf("extended-resource binding %q must not include dra settings", binding.Class)
			}
		case domain.AcceleratorAllocationDRA:
			return fmt.Errorf("allocation mode %q is not enabled in the current product surface", mode)
		default:
			return fmt.Errorf("unsupported accelerator allocation mode %q", mode)
		}
		if binding.Partition != nil {
			if binding.Partition.Kind != domain.AcceleratorPartitionMIG || binding.Partition.Profile == "" {
				return fmt.Errorf("unsupported accelerator partition for %q", binding.Class)
			}
		}
		if _, exists := bindings[binding.Class]; exists {
			return fmt.Errorf("duplicate accelerator binding %q", binding.Class)
		}
		bindings[binding.Class] = binding
	}

	for _, accelerator := range spec.Accelerators {
		if accelerator.Class == "" || accelerator.Quota <= 0 {
			return fmt.Errorf("accelerator class and positive quota are required")
		}
		if _, ok := bindings[accelerator.Class]; !ok {
			return fmt.Errorf("accelerator binding not found: %s", accelerator.Class)
		}
	}
	return nil
}
