package kueue

type ResourceFlavor struct {
	Name         string
	ResourceName string
	NodeLabels   map[string]string
}

type ResourceQuota struct {
	Flavor   string
	Resource string
	Nominal  int64
}

type ClusterQueue struct {
	Name   string
	Quotas []ResourceQuota
}

type LocalQueue struct {
	Name         string
	Namespace    string
	ClusterQueue string
}

type PoolResources struct {
	Flavors      []ResourceFlavor
	ClusterQueue ClusterQueue
	LocalQueue   LocalQueue
}
