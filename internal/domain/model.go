package domain

import "time"

type ID string

type Metadata struct {
	ID         ID        `json:"id"`
	Name       string    `json:"name"`
	Generation int64     `json:"generation"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Tenant struct {
	Metadata Metadata `json:"metadata"`
}

type Project struct {
	Metadata Metadata `json:"metadata"`
	TenantID ID       `json:"tenantId"`
}

type ComputePool struct {
	Metadata  Metadata        `json:"metadata"`
	ProjectID ID              `json:"projectId"`
	Spec      ComputePoolSpec `json:"spec"`
	Status    ResourceStatus  `json:"status"`
}

type ComputePoolSpec struct {
	Accelerators        []AcceleratorRequest `json:"accelerators,omitempty"`
	AcceleratorBindings []AcceleratorBinding `json:"acceleratorBindings,omitempty"`
	Scheduling          SchedulingPolicy     `json:"scheduling"`
}

type AcceleratorRequest struct {
	Class string `json:"class"`
	Quota int64  `json:"quota"`
}

const (
	AcceleratorAllocationExtendedResource = "extended-resource"
	AcceleratorAllocationDRA              = "dra"
)

// AcceleratorBinding resolves a portable accelerator class into the concrete
// allocation mechanism, resource and Kueue flavor exposed by a compute pool.
type AcceleratorBinding struct {
	Class          string                `json:"class"`
	AllocationMode string                `json:"allocationMode,omitempty"`
	ResourceName   string                `json:"resourceName"`
	Flavor         string                `json:"flavor"`
	NodeLabels     map[string]string     `json:"nodeLabels,omitempty"`
	Partition      *AcceleratorPartition `json:"partition,omitempty"`
}

type AcceleratorPartition struct {
	Kind    string `json:"kind"`
	Profile string `json:"profile"`
}

const AcceleratorPartitionMIG = "mig"

type SchedulingPolicy struct {
	Mode string `json:"mode"`
}

type Workload struct {
	Metadata  Metadata       `json:"metadata"`
	ProjectID ID             `json:"projectId"`
	PoolID    ID             `json:"poolId"`
	Spec      WorkloadSpec   `json:"spec"`
	Status    ResourceStatus `json:"status"`
}

type WorkloadSpec struct {
	Image       string             `json:"image"`
	Command     []string           `json:"command,omitempty"`
	Accelerator AcceleratorRequest `json:"accelerator"`
}

type Serving struct {
	Metadata  Metadata       `json:"metadata"`
	ProjectID ID             `json:"projectId"`
	PoolID    ID             `json:"poolId"`
	Spec      ServingSpec    `json:"spec"`
	Status    ResourceStatus `json:"status"`
}

type ServingSpec struct {
	Model       string             `json:"model"`
	Replicas    int32              `json:"replicas"`
	Accelerator AcceleratorRequest `json:"accelerator"`
}

type ResourceStatus struct {
	ObservedGeneration int64       `json:"observedGeneration"`
	Conditions         []Condition `json:"conditions,omitempty"`
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}
