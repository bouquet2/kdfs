//go:build linux

package agent

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
)

type fakeRunner struct{ commands [][]string }

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, append([]string{name}, args...))
	return nil, nil
}

func TestCreateReplicaUsesNativeSetupAndFormatsLoopDevice(t *testing.T) {
	runner := &fakeRunner{}
	var mkdirPath string
	var truncatePath string
	var truncateSize int64
	var attachedPath string
	server := &Server{
		Runner: runner,
		MkdirAll: func(path string, _ os.FileMode) error {
			mkdirPath = path
			return nil
		},
		TruncateFile: func(path string, size int64) error {
			truncatePath = path
			truncateSize = size
			return nil
		},
		AttachLoopDevice: func(path string) (string, error) {
			attachedPath = path
			return "/dev/loop7", nil
		},
	}
	resp, err := server.CreateReplica(context.Background(), CreateReplicaRequest{Path: "/var/lib/kdfs/pvc-1234/vol.img", Size: "10Gi"})
	if err != nil {
		t.Fatal(err)
	}
	if mkdirPath != "/var/lib/kdfs/pvc-1234" {
		t.Fatalf("mkdir path = %q", mkdirPath)
	}
	if truncatePath != "/var/lib/kdfs/pvc-1234/vol.img" {
		t.Fatalf("truncate path = %q", truncatePath)
	}
	if truncateSize != 10*1024*1024*1024 {
		t.Fatalf("truncate size = %d", truncateSize)
	}
	if attachedPath != "/var/lib/kdfs/pvc-1234/vol.img" {
		t.Fatalf("attach path = %q", attachedPath)
	}
	if resp.DevicePath != "/dev/loop7" || resp.State != ReplicaStateReady {
		t.Fatalf("response = %#v", resp)
	}
	want := [][]string{
		{"mkfs.xfs", "-f", "-s", "size=4096", "-m", "crc=0,finobt=0,rmapbt=0,reflink=0", "-i", "nrext64=0,maxpct=25", "-i", "bigtime=0,inobtcount=0", "/dev/loop7"},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v", runner.commands)
	}
}

func TestDeleteReplicaUsesNativeRemove(t *testing.T) {
	var removed string
	server := &Server{RemoveFile: func(path string) error {
		removed = path
		return nil
	}}
	if err := server.DeleteReplica(context.Background(), DeleteReplicaRequest{Path: "/var/lib/kdfs/pvc-1234/vol.img"}); err != nil {
		t.Fatal(err)
	}
	if removed != "/var/lib/kdfs/pvc-1234/vol.img" {
		t.Fatalf("removed = %q", removed)
	}
}

func TestGetReplicaUsesNativeStat(t *testing.T) {
	server := &Server{StatFile: func(path string) error {
		if path != "/var/lib/kdfs/pvc-1234/vol.img" {
			t.Fatalf("stat path = %q", path)
		}
		return nil
	}}
	resp, err := server.GetReplica(context.Background(), GetReplicaRequest{Path: "/var/lib/kdfs/pvc-1234/vol.img"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != ReplicaStateReady {
		t.Fatalf("response = %#v", resp)
	}
}

func TestGetReplicaReturnsMissingWhenStatFails(t *testing.T) {
	server := &Server{StatFile: func(string) error { return errors.New("missing") }}
	resp, err := server.GetReplica(context.Background(), GetReplicaRequest{Path: "/var/lib/kdfs/pvc-1234/vol.img"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != ReplicaStateMissing {
		t.Fatalf("response = %#v", resp)
	}
}

func TestCreateReplicaFromSnapshotUsesCopyFile(t *testing.T) {
	var mkdirPath string
	var attachedPath string
	var copiedSrc, copiedDst string
	server := &Server{
		Runner: &fakeRunner{},
		MkdirAll: func(path string, _ os.FileMode) error {
			mkdirPath = path
			return nil
		},
		AttachLoopDevice: func(path string) (string, error) {
			attachedPath = path
			return "/dev/loop7", nil
		},
		CopyFile: func(src, dst string) error {
			copiedSrc = src
			copiedDst = dst
			return nil
		},
	}
	resp, err := server.CreateReplica(context.Background(), CreateReplicaRequest{
		Path:           "/var/lib/kdfs/pvc-1234/vol.img",
		Size:           "10Gi",
		SnapshotSource: "/var/lib/kdfs/snapshots/snap-abc.img",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mkdirPath != "/var/lib/kdfs/pvc-1234" {
		t.Fatalf("mkdir path = %q", mkdirPath)
	}
	if copiedSrc != "/var/lib/kdfs/snapshots/snap-abc.img" {
		t.Fatalf("copy src = %q", copiedSrc)
	}
	if copiedDst != "/var/lib/kdfs/pvc-1234/vol.img" {
		t.Fatalf("copy dst = %q", copiedDst)
	}
	if attachedPath != "/var/lib/kdfs/pvc-1234/vol.img" {
		t.Fatalf("attach path = %q", attachedPath)
	}
	if resp.DevicePath != "/dev/loop7" || resp.State != ReplicaStateReady {
		t.Fatalf("response = %#v", resp)
	}
}
