//go:build linux

package reflink

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// FileOrCopy attempts to reflink (clone) src to dst using FICLONE. If the
// filesystem does not support reflinks it falls back to a byte-by-byte copy.
func FileOrCopy(src, dst string) error {
	srcFd, err := unix.Open(src, unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("reflink open source: %w", err)
	}
	defer unix.Close(srcFd)

	dstFd, err := unix.Open(dst, unix.O_CREAT|unix.O_WRONLY|unix.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("reflink create destination: %w", err)
	}

	if err := unix.IoctlFileClone(int(dstFd), int(srcFd)); err != nil {
		unix.Close(dstFd)
		_ = os.Remove(dst)
		return CopyFile(dst, src)
	}

	return unix.Close(dstFd)
}

// CopyFile copies file contents with io.Copy. Used as fallback when reflink
// is not supported by the underlying filesystem.
func CopyFile(dstPath, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("copy open source: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("copy open destination: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}
	return nil
}
