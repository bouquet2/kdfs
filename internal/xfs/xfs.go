package xfs

import (
	"fmt"
)

type Runner func(name string, args ...string) ([]byte, error)

func Format(device string, run Runner) error {
	out, err := run("mkfs.xfs", "-f", "-s", "size=4096", device)
	if err != nil {
		return fmt.Errorf("mkfs.xfs %s failed: %w output=%s", device, err, string(out))
	}
	return nil
}

func EnsureMounted(device string, mount func() error, run Runner) (formatted bool, err error) {
	if err := mount(); err != nil {
		if formatErr := Format(device, run); formatErr != nil {
			return false, formatErr
		}
		if mountErr := mount(); mountErr != nil {
			return true, fmt.Errorf("mount %s after mkfs failed: %w", device, mountErr)
		}
		return true, nil
	}
	return false, nil
}
