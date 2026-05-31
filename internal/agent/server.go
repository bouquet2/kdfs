//go:build linux

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bouquet2/kdfs/internal/reflink"
	"github.com/bouquet2/kdfs/internal/xfs"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

type Server struct {
	Runner           Runner
	MkdirAll         func(path string, perm os.FileMode) error
	TruncateFile     func(path string, size int64) error
	AttachLoopDevice func(path string) (string, error)
	RemoveFile       func(path string) error
	StatFile         func(path string) error
	CopyFile         func(src, dst string) error
}

func (s *Server) runner() Runner {
	if s.Runner == nil {
		return ExecRunner{}
	}
	return s.Runner
}

func (s *Server) mkdirAll(path string, perm os.FileMode) error {
	if s.MkdirAll != nil {
		return s.MkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (s *Server) truncateFile(path string, size int64) error {
	if s.TruncateFile != nil {
		return s.TruncateFile(path, size)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

func (s *Server) attachLoopDevice(path string) (string, error) {
	if s.AttachLoopDevice != nil {
		return s.AttachLoopDevice(path)
	}
	return attachLoopDevice(path)
}

func (s *Server) removeFile(path string) error {
	if s.RemoveFile != nil {
		return s.RemoveFile(path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) statFile(path string) error {
	if s.StatFile != nil {
		return s.StatFile(path)
	}
	_, err := os.Stat(path)
	return err
}

func parseReplicaSize(size string) (int64, error) {
	quantity, err := resource.ParseQuantity(size)
	if err != nil {
		return 0, fmt.Errorf("parse replica size %q: %w", size, err)
	}
	value := quantity.Value()
	if value < 0 {
		return 0, fmt.Errorf("parse replica size %q: negative size", size)
	}
	return value, nil
}

func (s *Server) CreateReplica(ctx context.Context, req CreateReplicaRequest) (CreateReplicaResponse, error) {
	_ = ctx
	if err := s.mkdirAll(filepath.Dir(req.Path), 0755); err != nil {
		return CreateReplicaResponse{}, err
	}

	if req.SnapshotSource != "" {
		s.removeFile(req.Path)
		if err := s.copyFile(req.SnapshotSource, req.Path); err != nil {
			return CreateReplicaResponse{}, fmt.Errorf("snapshot restore: %w", err)
		}
	} else {
		size, err := parseReplicaSize(req.Size)
		if err != nil {
			return CreateReplicaResponse{}, err
		}
		if err := s.truncateFile(req.Path, size); err != nil {
			return CreateReplicaResponse{}, err
		}
	}

	device, err := s.attachLoopDevice(req.Path)
	if err != nil {
		return CreateReplicaResponse{}, err
	}
	runner := s.runner()
	if err := xfs.Format(device, runner.Run); err != nil {
		return CreateReplicaResponse{}, err
	}
	return CreateReplicaResponse{DevicePath: device, State: ReplicaStateReady}, nil
}

func (s *Server) copyFile(src, dst string) error {
	if s.CopyFile != nil {
		return s.CopyFile(src, dst)
	}
	return reflink.FileOrCopy(src, dst)
}

func (s *Server) DeleteReplica(ctx context.Context, req DeleteReplicaRequest) error {
	_ = ctx
	return s.removeFile(req.Path)
}

func (s *Server) DeleteSnapshot(ctx context.Context, req DeleteSnapshotRequest) error {
	_ = ctx
	return s.removeFile(req.Path)
}

func (s *Server) GetReplica(ctx context.Context, req GetReplicaRequest) (GetReplicaResponse, error) {
	_ = ctx
	if err := s.statFile(req.Path); err != nil {
		return GetReplicaResponse{State: ReplicaStateMissing}, nil
	}
	return GetReplicaResponse{State: ReplicaStateReady}, nil
}
