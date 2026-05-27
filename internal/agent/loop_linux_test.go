//go:build linux

package agent

import (
	"os"
	"testing"
)

func TestEnsureLoopDeviceNodeReplacesStaleNode(t *testing.T) {
	path := t.TempDir() + "/loop8"
	if err := os.WriteFile(path, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLoopDeviceNode(path, 7, 8); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeDevice == 0 {
		t.Fatalf("mode = %v, want device", info.Mode())
	}
}
