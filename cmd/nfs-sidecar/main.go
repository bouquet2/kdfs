package main

import (
	"os"

	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/sidecar"
)

func main() {
	logger := logging.Component("nfs-sidecar")
	addr := os.Getenv("KDFS_NFS_BIND")
	if addr == "" {
		addr = ":2049"
	}
	exportPath := os.Getenv("KDFS_NFS_EXPORT")
	if exportPath == "" {
		logger.Fatal().Msg("KDFS_NFS_EXPORT not set")
	}
	logger.Info().Str("addr", addr).Str("export_path", exportPath).Msg("starting NFS server")
	if err := sidecar.ServeNFS(exportPath, addr); err != nil {
		logger.Fatal().Err(err).Msg("NFS server exited")
	}
}
