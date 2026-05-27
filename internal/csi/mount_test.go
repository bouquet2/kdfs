//go:build linux

package csi

import (
	"errors"
	"os"
	"testing"
)

func TestExecMounterMountUsesHelperForNFS(t *testing.T) {
	target := t.TempDir() + "/target"

	prev := mountCommand
	defer func() { mountCommand = prev }()
	mountCommand = func(args ...string) ([]byte, error) {
		if len(args) != 5 || args[0] != "-t" || args[1] != "nfs" || args[2] != "-o" || args[3] != "vers=4.1,soft" || args[4] != target {
			t.Fatalf("args = %#v", args)
		}
		return nil, nil
	}

	err := (ExecMounter{}).Mount("server:/export", target, "nfs", []string{"vers=4.1", "soft"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target dir not created: %v", err)
	}
}

func TestExecMounterMountReturnsErrorOnHelperFailure(t *testing.T) {
	prev := mountCommand
	defer func() { mountCommand = prev }()
	mountCommand = func(args ...string) ([]byte, error) {
		return []byte("mount.nfs: timed out"), errors.New("exit status 32")
	}

	err := (ExecMounter{}).Mount("server:/export", "/tmp/target", "nfs", []string{"vers=3"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecMounterMountUsesSyscallForOtherFS(t *testing.T) {
	target := t.TempDir() + "/target"

	called := false
	prev := unixMount
	unixMount = func(source, target, fsType string, flags uintptr, data string) error {
		called = true
		if source != "/dev/sda1" {
			t.Fatalf("source = %q", source)
		}
		if fsType != "xfs" {
			t.Fatalf("fsType = %q", fsType)
		}
		if flags != 0 {
			t.Fatalf("flags = %d", flags)
		}
		if data != "ro" {
			t.Fatalf("data = %q", data)
		}
		return nil
	}
	defer func() { unixMount = prev }()

	err := (ExecMounter{}).Mount("/dev/sda1", target, "xfs", []string{"ro"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected unixMount to be called")
	}
}
