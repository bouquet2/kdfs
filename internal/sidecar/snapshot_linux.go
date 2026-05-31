//go:build linux

package sidecar

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/reflink"
)

type reflinkSnapshotter struct {
	volumeName string
	localPath  string
}

func NewReflinkSnapshotter(volumeName, localPath string) Snapshotter {
	return &reflinkSnapshotter{volumeName: volumeName, localPath: localPath}
}

func (s *reflinkSnapshotter) CreateSnapshot(ctx context.Context, snapshotID string) (string, int64, error) {
	src := s.localPath
	dst := filepath.Join(filepath.Dir(s.localPath), "snapshot-"+snapshotID+".img")

	stat, err := os.Stat(src)
	if err != nil {
		return "", 0, fmt.Errorf("stat source: %w", err)
	}

	if _, err := os.Stat(dst); err == nil {
		stat2, _ := os.Stat(dst)
		return names.SnapshotFilePath(s.volumeName, snapshotID), stat2.Size(), nil
	}

	if err := reflink.FileOrCopy(src, dst); err != nil {
		return "", 0, fmt.Errorf("snapshot: %w", err)
	}

	return names.SnapshotFilePath(s.volumeName, snapshotID), stat.Size(), nil
}
