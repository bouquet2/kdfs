//go:build linux

package csi

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

var unixMount = unix.Mount

func (ExecMounter) Mount(source, target, fsType string, options []string) error {
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	if fsType == "nfs" || fsType == "nfs4" {
		return mountNFSViaHelper(source, target, fsType, options)
	}
	data := strings.Join(options, ",")
	return unixMount(source, target, fsType, 0, data)
}

var mountCommand = func(args ...string) ([]byte, error) {
	return exec.Command(args[0], args[1:]...).CombinedOutput()
}

func mountNFSViaHelper(source, target, fsType string, options []string) error {
	data := strings.Join(options, ",")
	out, err := mountCommand("mount", "-t", fsType, "-o", data, source, target)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (ExecMounter) BindMount(source, target string, readonly bool) error {
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	flags := uintptr(unix.MS_BIND)
	if readonly {
		flags |= uintptr(unix.MS_RDONLY)
	}
	return unix.Mount(source, target, "", flags, "")
}

func (ExecMounter) Unmount(target string) error {
	return unix.Unmount(target, 0)
}
