//go:build !linux

package sidecar

import (
	"context"
	"fmt"
)

func NewReflinkSnapshotter(volumeName, localPath string) Snapshotter {
	return &stubSnapshotter{volumeName: volumeName}
}

type stubSnapshotter struct {
	volumeName string
}

func (s *stubSnapshotter) CreateSnapshot(_ context.Context, _ string) (string, int64, error) {
	return "", 0, fmt.Errorf("reflink snapshots not supported on this platform")
}
