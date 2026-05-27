package agent

import "context"

type FakeClient struct {
	Created []CreateReplicaRequest
	Err     error
}

func (f *FakeClient) CreateReplica(ctx context.Context, req CreateReplicaRequest) (CreateReplicaResponse, error) {
	f.Created = append(f.Created, req)
	if f.Err != nil {
		return CreateReplicaResponse{}, f.Err
	}
	return CreateReplicaResponse{DevicePath: "/dev/loop0", State: ReplicaStateReady}, nil
}

func (f *FakeClient) DeleteReplica(ctx context.Context, req DeleteReplicaRequest) error { return f.Err }

func (f *FakeClient) GetReplica(ctx context.Context, req GetReplicaRequest) (GetReplicaResponse, error) {
	if f.Err != nil {
		return GetReplicaResponse{}, f.Err
	}
	return GetReplicaResponse{State: ReplicaStateReady, Size: "10Gi"}, nil
}
