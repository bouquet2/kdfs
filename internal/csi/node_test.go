//go:build linux

package csi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
)

func TestEnsureNVMeHostNQNWritesExpectedValue(t *testing.T) {
	t.Setenv("NQN_AUTHORITY", "nqn.2026-05.cluster-b.example")
	driver := &Driver{NodeID: "worker-1"}

	var gotPath string
	var gotData []byte
	var gotPerm os.FileMode
	prev := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		gotPath = name
		gotData = append([]byte(nil), data...)
		gotPerm = perm
		return nil
	}
	defer func() { osWriteFile = prev }()

	if err := driver.ensureNVMeHostNQN(); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/etc/nvme/hostnqn" {
		t.Fatalf("path = %q", gotPath)
	}
	if string(gotData) != "nqn.2026-05.cluster-b.example:krea.to:host-worker-1\n" {
		t.Fatalf("data = %q", string(gotData))
	}
	if gotPerm != 0644 {
		t.Fatalf("perm = %v", gotPerm)
	}
}

type fakeMounter struct{ calls []string }

func (f *fakeMounter) Mount(source, target, fsType string, options []string) error {
	f.calls = append(f.calls, "mount:"+source+":"+target+":"+fsType)
	return nil
}

func (f *fakeMounter) BindMount(source, target string, readonly bool) error {
	f.calls = append(f.calls, fmt.Sprintf("bind:%s:%s:ro=%v", source, target, readonly))
	return nil
}

func (f *fakeMounter) Unmount(target string) error {
	f.calls = append(f.calls, "unmount:"+target)
	return nil
}

func TestNodeStageAndPublish(t *testing.T) {
	prevEnsure := ensureNVMeStagedVolume
	defer func() { ensureNVMeStagedVolume = prevEnsure }()
	ensureNVMeStagedVolume = func(_ *Driver, targetPath, endpoint, nqn string, readonly bool) error {
		if endpoint != "worker-1:4420" {
			t.Fatalf("endpoint = %q", endpoint)
		}
		if nqn != "nqn.2026-05.krea.to:volume-pvc-1234" {
			t.Fatalf("nqn = %q", nqn)
		}
		if readonly {
			t.Fatal("expected read-write stage")
		}
		return os.MkdirAll(targetPath, 0755)
	}
	mounter := &fakeMounter{}
	driver := &Driver{NodeID: "worker-1", Mounter: mounter}
	staging := t.TempDir() + "/globalmount"
	target := t.TempDir() + "/podmount"
	os.MkdirAll(target, 0755)
	_, err := driver.NodeStageVolume(context.Background(), &csipb.NodeStageVolumeRequest{VolumeId: "pvc-1234", StagingTargetPath: staging, PublishContext: map[string]string{"endpoint": "worker-1:4420", "nqn": "nqn.2026-05.krea.to:volume-pvc-1234"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = driver.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{VolumeId: "pvc-1234", StagingTargetPath: staging, TargetPath: target})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bind:" + staging + ":" + target + ":ro=false"}
	if !reflect.DeepEqual(mounter.calls, want) {
		t.Fatalf("calls = %#v", mounter.calls)
	}
}

func TestNodePublishROXIsReadOnly(t *testing.T) {
	mounter := &fakeMounter{}
	driver := &Driver{NodeID: "worker-1", Mounter: mounter}
	staging := t.TempDir() + "/globalmount"
	target := t.TempDir() + "/podmount"
	os.MkdirAll(target, 0755)

	_, err := driver.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{
		VolumeId:          "pvc-1234",
		StagingTargetPath: staging,
		TargetPath:        target,
		Readonly:          true,
		VolumeCapability:  &csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bind:" + staging + ":" + target + ":ro=true"}
	if !reflect.DeepEqual(mounter.calls, want) {
		t.Fatalf("calls = %#v", mounter.calls)
	}
}

func TestStageReadonlyMode(t *testing.T) {
	if !stageReadonly(&csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY}}) {
		t.Fatal("expected ROX capability to stage readonly")
	}
	if stageReadonly(&csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}}) {
		t.Fatal("expected single-node writer capability to stage read-write")
	}
	if stageReadonly(nil) {
		t.Fatal("expected nil capability to stage read-write")
	}
}

func TestMountOptionsReadOnlyAddsNoUUID(t *testing.T) {
	opts := mountOptions(true)
	if !reflect.DeepEqual(opts, []string{"ro", "nouuid"}) {
		t.Fatalf("opts = %#v", opts)
	}
	if mountOptions(false) != nil {
		t.Fatalf("expected nil options for read-write")
	}
}

func TestStageNFSFormatsIPv6Source(t *testing.T) {
	mounter := &fakeMounter{}
	driver := &Driver{Mounter: mounter}
	staging := t.TempDir() + "/globalmount"

	_, err := driver.stageNFS(context.Background(), &csipb.NodeStageVolumeRequest{
		StagingTargetPath: staging,
		PublishContext:    map[string]string{"server": "fd00::50", "export": "/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mount:[fd00::50]:/:" + staging + ":nfs"}
	if !reflect.DeepEqual(mounter.calls, want) {
		t.Fatalf("calls = %#v", mounter.calls)
	}
}

func TestEnsureNVMeStagedRejectsMalformedEndpoint(t *testing.T) {
	driver := &Driver{NodeID: "worker-1"}
	err := driver.ensureNVMeStaged(t.TempDir(), "fd00::50:4420", "nqn.2026-05.krea.to:volume-pvc-1234", false)
	if err == nil {
		t.Fatal("expected invalid endpoint error")
	}
	if !strings.Contains(err.Error(), "invalid endpoint") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureNVMeStagedAcceptsCanonicalIPv6Endpoint(t *testing.T) {
	driver := &Driver{NodeID: "worker-1"}
	err := driver.ensureNVMeStaged(t.TempDir(), "[fd00::50]:4420", "bad-nqn", false)
	if err == nil {
		t.Fatal("expected invalid NQN error")
	}
	if err.Error() != "invalid NQN: must start with nqn" {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureBlockDeviceNodeReplacesStaleNode(t *testing.T) {
	root := t.TempDir()
	devPath := filepath.Join(root, "nvme1n1")
	if err := os.WriteFile(devPath, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ensureBlockDeviceNode(devPath, 259, 2); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(devPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeDevice == 0 {
		t.Fatalf("expected %s to be a device node, mode=%v", devPath, info.Mode())
	}
}
