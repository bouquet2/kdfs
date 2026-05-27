//go:build !linux

package main

import (
	"errors"
)

func newReplicaServer() (replicaServer, error) {
	return nil, errors.New("node-agent is only supported on Linux")
}
