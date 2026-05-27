package sidecar

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"github.com/go-git/go-billy/v5"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
	"github.com/willscott/memphis"
)

type statFSHandler struct {
	nfs.Handler
	exportPath string
}

func (h *statFSHandler) FSStat(ctx context.Context, f billy.Filesystem, s *nfs.FSStat) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(h.exportPath, &stat); err == nil {
		s.TotalSize = stat.Blocks * uint64(stat.Bsize)
		s.FreeSize = stat.Bfree * uint64(stat.Bsize)
		s.AvailableSize = stat.Bavail * uint64(stat.Bsize)
		s.TotalFiles = stat.Files
		s.FreeFiles = stat.Ffree
		s.AvailableFiles = stat.Ffree
	}
	return nil
}

func ServeNFS(exportPath, listenAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	fs := memphis.FromOS(exportPath)
	bfs := fs.AsBillyFS(0, 0)
	handler := nfshelper.NewNullAuthHandler(bfs)
	handler = nfshelper.NewCachingHandler(handler, 1024)
	handler = &statFSHandler{Handler: handler, exportPath: exportPath}

	return nfs.Serve(listener, handler)
}
