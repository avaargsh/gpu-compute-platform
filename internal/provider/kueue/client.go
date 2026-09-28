package kueue

import "context"

type JobObservation struct {
	Phase     string
	Admitted  bool
	PodsReady bool
	Failed    bool
	Message   string
}

type Client interface {
	ApplyResourceFlavor(context.Context, ResourceFlavor) error
	ApplyClusterQueue(context.Context, ClusterQueue) error
	ApplyLocalQueue(context.Context, LocalQueue) error
	ApplyJob(context.Context, Job) error
	ObserveJob(context.Context, string, string) (JobObservation, error)
}
