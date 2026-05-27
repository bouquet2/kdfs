//go:build linux

package csi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/xfs"
	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
)

var osWriteFile = os.WriteFile

var ensureNVMeStagedVolume = func(d *Driver, targetPath, endpoint, nqn string, readonly bool) error {
	return d.ensureNVMeStaged(targetPath, endpoint, nqn, readonly)
}

func (d *Driver) mounter() Mounter {
	if d.Mounter == nil {
		return ExecMounter{}
	}
	return d.Mounter
}

func (d *Driver) NodeStageVolume(ctx context.Context, req *csipb.NodeStageVolumeRequest) (*csipb.NodeStageVolumeResponse, error) {
	logger.Info().Str("protocol", req.PublishContext["protocol"]).Interface("publish_context", req.PublishContext).Msg("stage volume")
	switch req.PublishContext["protocol"] {
	case "nfs":
		return d.stageNFS(ctx, req)
	default:
		return d.stageNVMe(ctx, req)
	}
}

func (d *Driver) stageNFS(ctx context.Context, req *csipb.NodeStageVolumeRequest) (*csipb.NodeStageVolumeResponse, error) {
	server := req.PublishContext["server"]
	export := req.PublishContext["export"]
	if server == "" {
		return nil, fmt.Errorf("publish context requires server")
	}

	os.MkdirAll(req.StagingTargetPath, 0755)
	source := nfsSource(server, export)
	opts := []string{"nfsvers=3", "port=2049", "mountport=2049", "proto=tcp", "hard", "nolock", "noresvport", "timeo=10", "retrans=2"}
	logger.Info().Str("source", source).Str("target", req.StagingTargetPath).Str("fstype", "nfs").Strs("options", opts).Msg("mounting NFS staging volume")
	if err := d.mounter().Mount(source, req.StagingTargetPath, "nfs", opts); err != nil {
		logger.Error().Err(err).Str("source", source).Str("target", req.StagingTargetPath).Msg("mount NFS staging volume failed")
		return nil, fmt.Errorf("mount nfs: %w", err)
	}
	logger.Info().Str("target", req.StagingTargetPath).Msg("mounted NFS staging volume")
	return &csipb.NodeStageVolumeResponse{}, nil
}

func nfsSource(server, export string) string {
	if ip := net.ParseIP(server); ip != nil && ip.To4() == nil {
		return "[" + server + "]:" + export
	}
	return server + ":" + export
}

func (d *Driver) stageNVMe(ctx context.Context, req *csipb.NodeStageVolumeRequest) (*csipb.NodeStageVolumeResponse, error) {
	endpoint := req.PublishContext["endpoint"]
	nqn := req.PublishContext["nqn"]
	if endpoint == "" || nqn == "" {
		return nil, fmt.Errorf("publish context requires endpoint and nqn")
	}
	if err := ensureNVMeStagedVolume(d, req.StagingTargetPath, endpoint, nqn, stageReadonly(req.GetVolumeCapability())); err != nil {
		return nil, fmt.Errorf("stage: %w", err)
	}
	return &csipb.NodeStageVolumeResponse{}, nil
}

func (d *Driver) NodePublishVolume(ctx context.Context, req *csipb.NodePublishVolumeRequest) (*csipb.NodePublishVolumeResponse, error) {
	logger.Info().Str("staging_path", req.StagingTargetPath).Str("target_path", req.TargetPath).Interface("capability", req.VolumeCapability).Interface("publish_context", req.PublishContext).Msg("publish volume")

	if req.PublishContext["endpoint"] != "" && req.PublishContext["nqn"] != "" {
		if err := ensureNVMeStagedVolume(d, req.StagingTargetPath, req.PublishContext["endpoint"], req.PublishContext["nqn"], req.GetReadonly() || stageReadonly(req.GetVolumeCapability())); err != nil {
			return nil, fmt.Errorf("publish: ensureNVMeStaged failed: %w", err)
		}
	}

	if err := d.mounter().BindMount(req.StagingTargetPath, req.TargetPath, req.GetReadonly()); err != nil {
		logger.Error().Err(err).Str("staging_path", req.StagingTargetPath).Str("target_path", req.TargetPath).Msg("bind mount failed")
		return nil, err
	}
	logger.Info().Str("target_path", req.TargetPath).Msg("bind mount succeeded")
	return &csipb.NodePublishVolumeResponse{}, nil
}

func stageReadonly(cap *csipb.VolumeCapability) bool {
	if cap == nil || cap.AccessMode == nil {
		return false
	}
	return cap.AccessMode.Mode == csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
}

// Connects to the NVMe-oF subsystem, waits for the device node, and mounts it at the target path.
func (d *Driver) ensureNVMeStaged(targetPath, endpoint, nqn string, readonly bool) error {
	addr, port, err := network.SplitEndpoint(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}

	// Validate NQN format
	if !strings.HasPrefix(nqn, "nqn") {
		return fmt.Errorf("invalid NQN: must start with nqn")
	}

	if _, statErr := os.Stat("/dev/nvme-fabrics"); statErr != nil {
		return fmt.Errorf("NVMe-oF not available: /dev/nvme-fabrics missing: %w", statErr)
	}
	if err := d.ensureNVMeHostNQN(); err != nil {
		return fmt.Errorf("ensure host NQN: %w", err)
	}

	device, existingErr := findExistingNVMEDevice(nqn)
	if existingErr == nil {
		logger.Info().Str("device", device).Msg("found existing NVMe device, skipping connect")
		mountErr := d.mounter().Mount(device, targetPath, "xfs", mountOptions(readonly))
		if mountErr == nil {
			return nil
		}
		logger.Warn().Err(mountErr).Str("device", device).Msg("existing NVMe device mount failed, cleaning stale connections")
		clearNVMeSubsystem(nqn)
		time.Sleep(3 * time.Second)
		device = ""
		existingErr = fmt.Errorf("stale device, will reconnect")
	}

	connStr := fmt.Sprintf("transport=tcp,traddr=%s,trsvcid=%s,nqn=%s", addr, port, nqn)
	for retry := 0; retry < 3; retry++ {
		if err := osWriteFile("/dev/nvme-fabrics", []byte(connStr), 0600); err != nil {
			if strings.Contains(err.Error(), "operation already in progress") && retry < 2 {
				logger.Warn().Err(err).Msg("NVMe connect in progress, waiting for stale controller cleanup")
				clearNVMeSubsystem(nqn)
				time.Sleep(3 * time.Second)
				continue
			}
			return fmt.Errorf("write to /dev/nvme-fabrics: %w", err)
		}
		break
	}

	for range 30 {
		if dev, err := findExistingNVMEDevice(nqn); err == nil {
			device = dev
			break
		}
		time.Sleep(time.Second)
	}

	if _, err := os.Stat(device); err != nil {
		return fmt.Errorf("nvme device %s did not appear after connect: %w", device, err)
	}

	d.mounter().Unmount(targetPath)
	os.RemoveAll(targetPath)

	mount := func() error {
		return d.mounter().Mount(device, targetPath, "xfs", mountOptions(readonly))
	}
	if readonly {
		if err := mount(); err != nil {
			return err
		}
		logger.Info().Str("device", device).Str("target", targetPath).Msg("mounted NVMe device")
		return nil
	}
	formatted, err := xfs.EnsureMounted(device, mount, func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	})
	if err != nil {
		return err
	}
	if formatted {
		logger.Warn().Str("device", device).Msg("mount failed, attempted mkfs.xfs")
		logger.Info().Str("device", device).Msg("mkfs.xfs succeeded")
	}
	logger.Info().Str("device", device).Str("target", targetPath).Msg("mounted NVMe device")
	return nil
}

func mountOptions(readonly bool) []string {
	if readonly {
		return []string{"ro", "nouuid"}
	}
	return nil
}

func (d *Driver) ensureNVMeHostNQN() error {
	if strings.TrimSpace(d.NodeID) == "" {
		return fmt.Errorf("node ID is required")
	}
	hostNQN := names.HostNQN(d.NodeID) + "\n"
	if err := osWriteFile("/etc/nvme/hostnqn", []byte(hostNQN), 0644); err != nil {
		return err
	}
	return nil
}

func ensureBlockDeviceNode(devPath string, major, minor int) error {
	if err := os.Remove(devPath); err != nil && !os.IsNotExist(err) {
		// If the path is a directory or other unexpected entry, remove it recursively.
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrInvalid) {
			return err
		}
		if rmErr := os.RemoveAll(devPath); rmErr != nil {
			return rmErr
		}
	}
	if err := unix.Mknod(devPath, 0660|unix.S_IFBLK, int(unix.Mkdev(uint32(major), uint32(minor)))); err != nil {
		return err
	}
	return nil
}

// Locates a live NVMe namespace for the given NQN and ensures the block device node exists.
func findExistingNVMEDevice(nqn string) (string, error) {
	entries, err := os.ReadDir("/sys/devices/virtual/nvme-subsystem")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		subsysNQN, err := os.ReadFile(filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name(), "subsysnqn"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(subsysNQN)) != nqn {
			continue
		}
		nsEntries, err := os.ReadDir(filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name()))
		if err != nil {
			continue
		}
		controllerLive := false
		for _, ns := range nsEntries {
			if !strings.HasPrefix(ns.Name(), "nvme") || strings.Contains(ns.Name()[4:], "n") {
				continue
			}
			state, err := os.ReadFile(filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name(), ns.Name(), "state"))
			if err == nil && strings.TrimSpace(string(state)) == "live" {
				controllerLive = true
				break
			}
		}
		if !controllerLive {
			return "", fmt.Errorf("NVMe controller for %s is not live", nqn)
		}
		for _, ns := range nsEntries {
			if !strings.HasPrefix(ns.Name(), "nvme") || !strings.Contains(ns.Name()[4:], "n") {
				continue
			}
			devPath := filepath.Join("/dev", ns.Name())
			if majMin, err := os.ReadFile(filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name(), ns.Name(), "dev")); err == nil {
				parts := strings.SplitN(strings.TrimSpace(string(majMin)), ":", 2)
				if len(parts) == 2 {
					major, _ := strconv.Atoi(parts[0])
					minor, _ := strconv.Atoi(parts[1])
					logger.Info().Str("device", devPath).Int("major", major).Int("minor", minor).Msg("creating NVMe block device node")
					if err := ensureBlockDeviceNode(devPath, major, minor); err != nil {
						return "", err
					}
					return devPath, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no existing NVMe device for NQN %s", nqn)
}

func clearNVMeSubsystem(nqn string) {
	entries, err := os.ReadDir("/sys/devices/virtual/nvme-subsystem")
	if err != nil {
		return
	}
	for _, entry := range entries {
		subsysNQN, err := os.ReadFile(filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name(), "subsysnqn"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(subsysNQN)) != nqn {
			continue
		}
		nsEntries, err := os.ReadDir(filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name()))
		if err != nil {
			continue
		}
		for _, ns := range nsEntries {
			if !strings.HasPrefix(ns.Name(), "nvme") || strings.Contains(ns.Name()[4:], "n") {
				continue
			}
			deletePath := filepath.Join("/sys/devices/virtual/nvme-subsystem", entry.Name(), ns.Name(), "delete_controller")
			if err := os.WriteFile(deletePath, []byte("1"), 0200); err != nil {
				logger.Warn().Str("controller", ns.Name()).Err(err).Msg("failed to delete stale NVMe controller")
			} else {
				logger.Info().Str("controller", ns.Name()).Msg("deleted stale NVMe controller")
			}
		}
	}
}

func (d *Driver) NodeUnpublishVolume(ctx context.Context, req *csipb.NodeUnpublishVolumeRequest) (*csipb.NodeUnpublishVolumeResponse, error) {
	logger.Info().Str("target_path", req.TargetPath).Msg("unpublish volume")
	if err := d.mounter().Unmount(req.TargetPath); err != nil {
		logger.Warn().Err(err).Str("target_path", req.TargetPath).Msg("unmount failed during unpublish")
		// Not an error if already unmounted — allows cleanup to proceed
	}
	return &csipb.NodeUnpublishVolumeResponse{}, nil
}

func (d *Driver) NodeUnstageVolume(ctx context.Context, req *csipb.NodeUnstageVolumeRequest) (*csipb.NodeUnstageVolumeResponse, error) {
	logger.Info().Str("staging_path", req.StagingTargetPath).Msg("unstage volume")
	if err := d.mounter().Unmount(req.StagingTargetPath); err != nil {
		logger.Warn().Err(err).Str("staging_path", req.StagingTargetPath).Msg("unmount failed during unstage")
		// Not an error if not mounted (e.g. NVMe-oF fallback on kind) — allows cleanup to proceed
	}
	return &csipb.NodeUnstageVolumeResponse{}, nil
}
