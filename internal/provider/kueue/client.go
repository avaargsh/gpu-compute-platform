package kueue

import "context"

type Client interface {
	ApplyResourceFlavor(context.Context, ResourceFlavor) error
	ApplyClusterQueue(context.Context, ClusterQueue) error
	ApplyLocalQueue(context.Context, LocalQueue) error
}
