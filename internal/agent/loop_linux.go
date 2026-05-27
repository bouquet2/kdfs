//go:build linux

package agent

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func ensureLoopDeviceNode(path string, major, minor int) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale loop device %s: %w", path, err)
	}
	return unix.Mknod(path, 0660|unix.S_IFBLK, int(unix.Mkdev(uint32(major), uint32(minor))))
}

func loopDeviceNumbers(index int) (int, int, error) {
	devPath := fmt.Sprintf("/sys/class/block/loop%d/dev", index)
	data, err := os.ReadFile(devPath)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", devPath, err)
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("parse %s: unexpected device %q", devPath, strings.TrimSpace(string(data)))
	}
	major, minor := 0, 0
	if _, err := fmt.Sscanf(strings.Join(parts, ":"), "%d:%d", &major, &minor); err != nil {
		return 0, 0, fmt.Errorf("parse %s: %w", devPath, err)
	}
	return major, minor, nil
}

// Attaches a backing file to a free loop device and returns the device path, cleaning up on failure.
func attachLoopDevice(path string) (string, error) {
	backingFile, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open backing file %q: %w", path, err)
	}
	defer backingFile.Close()

	control, err := os.OpenFile("/dev/loop-control", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open /dev/loop-control: %w", err)
	}
	defer control.Close()

	index, err := unix.IoctlRetInt(int(control.Fd()), unix.LOOP_CTL_GET_FREE)
	if err != nil {
		return "", fmt.Errorf("get free loop device: %w", err)
	}

	devicePath := fmt.Sprintf("/dev/loop%d", index)
	major, minor, err := loopDeviceNumbers(index)
	if err != nil {
		return "", err
	}
	if err := ensureLoopDeviceNode(devicePath, major, minor); err != nil {
		return "", fmt.Errorf("ensure %s: %w", devicePath, err)
	}
	loopDevice, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", devicePath, err)
	}
	defer loopDevice.Close()

	if err := unix.IoctlSetInt(int(loopDevice.Fd()), unix.LOOP_SET_FD, int(backingFile.Fd())); err != nil {
		return "", fmt.Errorf("attach backing file to %s: %w", devicePath, err)
	}
	clearLoop := true
	defer func() {
		if clearLoop {
			_ = unix.IoctlSetInt(int(loopDevice.Fd()), unix.LOOP_CLR_FD, 0)
		}
	}()

	info := &unix.LoopInfo64{}
	copy(info.File_name[:], []byte(path))
	if err := unix.IoctlLoopSetStatus64(int(loopDevice.Fd()), info); err != nil {
		return "", fmt.Errorf("configure %s: %w", devicePath, err)
	}

	clearLoop = false
	return devicePath, nil
}
