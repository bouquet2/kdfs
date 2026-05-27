package sidecar

import (
	"context"
	"net/http"

	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/spdk"
)

type ReplicaConfig struct {
	VolumeName   string
	ReplicaIndex int
	DataPath     string
	Listener     spdk.Listener
	Endpoint     string
}

type ReplicaResult struct {
	NQN      string
	Endpoint string
}

type Replica struct {
	result ReplicaResult
}

func NewReplica(result ReplicaResult) *Replica {
	return &Replica{result: result}
}

func (r *Replica) Status() Status {
	return Status{Role: "replica", Endpoint: r.result.Endpoint, ReplicaNQN: r.result.NQN}
}

func (r *Replica) StatusHTTP(w http.ResponseWriter, _ *http.Request) {
	WriteStatusHTTP(w, r.Status())
}

func ConfigureReplica(ctx context.Context, client spdk.Client, config ReplicaConfig) (ReplicaResult, error) {
	nqn := names.ReplicaNQN(config.VolumeName, config.ReplicaIndex)
	for _, step := range []func() error{
		func() error { return client.CreateAIOBdev(ctx, "aio0", config.DataPath, 4096) },
		func() error { return client.CreateTransport(ctx, "tcp") },
		func() error { return client.CreateSubsystem(ctx, nqn, "kdfs-vol", true) },
		func() error { return client.AddNamespace(ctx, nqn, spdk.Namespace{BdevName: "aio0"}) },
		func() error {
			return client.AddListener(ctx, nqn, config.Listener)
		},
	} {
		if err := step(); err != nil {
			return ReplicaResult{}, err
		}
	}
	return ReplicaResult{NQN: nqn, Endpoint: config.Endpoint}, nil
}
