package domain

type Cluster struct {
	Metadata Metadata      `json:"metadata"`
	Spec     ClusterSpec   `json:"spec"`
	Status   ClusterStatus `json:"status"`
}

type ClusterSpec struct {
	Provider string `json:"provider"`
}

type ClusterStatus struct {
	Connected    bool                `json:"connected"`
	Capabilities ClusterCapabilities `json:"capabilities"`
}

type SchedulerCapability struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ClusterCapabilities struct {
	Kueue          bool                  `json:"kueue"`
	Serving        bool                  `json:"serving"`
	Schedulers     []SchedulerCapability `json:"schedulers,omitempty"`
	DRAAPIAvailable   bool                  `json:"draApiAvailable"`
	DRAAPIVersion  string                `json:"draApiVersion,omitempty"`
	Accelerators   []string              `json:"accelerators,omitempty"`
}

type ProjectBinding struct {
	Metadata  Metadata `json:"metadata"`
	ProjectID ID       `json:"projectId"`
	ClusterID ID       `json:"clusterId"`
	Namespace string   `json:"namespace"`
}

type ClusterBinding struct {
	Metadata  Metadata `json:"metadata"`
	PoolID    ID       `json:"poolId"`
	ClusterID ID       `json:"clusterId"`
	Provider  string   `json:"provider"`
}
