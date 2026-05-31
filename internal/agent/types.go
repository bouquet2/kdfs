package agent

import "context"

type ReplicaState string

const (
	ReplicaStateMissing ReplicaState = "Missing"
	ReplicaStateReady   ReplicaState = "Ready"
)

type CreateReplicaRequest struct {
	Path           string
	Size           string
	SnapshotSource string `json:"snapshotSource,omitempty"`
}

type CreateReplicaResponse struct {
	DevicePath string
	State      ReplicaState
}

type DeleteReplicaRequest struct {
	Path string
}

type GetReplicaRequest struct {
	Path string
}

type DeleteSnapshotRequest struct {
	Path string `json:"path"`
}

type GetReplicaResponse struct {
	State ReplicaState
	Size  string
}

type Client interface {
	CreateReplica(ctx context.Context, req CreateReplicaRequest) (CreateReplicaResponse, error)
	DeleteReplica(ctx context.Context, req DeleteReplicaRequest) error
	GetReplica(ctx context.Context, req GetReplicaRequest) (GetReplicaResponse, error)
	DeleteSnapshot(ctx context.Context, req DeleteSnapshotRequest) error
}
