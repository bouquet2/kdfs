//go:build linux

package main

import "github.com/bouquet2/kdfs/internal/agent"

func newReplicaServer() (replicaServer, error) {
	return &agent.Server{}, nil
}
