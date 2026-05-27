package main

import (
	"net/http"
	"os"
	"time"

	"github.com/bouquet2/kdfs/internal/dashboard"
	"github.com/bouquet2/kdfs/internal/logging"
	"k8s.io/client-go/rest"
)

func main() {
	logger := logging.Component("dashboard")
	namespace := os.Getenv("KDFS_NAMESPACE")
	if namespace == "" {
		namespace = "kdfs"
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to get in-cluster config")
	}

	cl, err := dashboard.NewClient(cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create dashboard client")
	}

	mux := dashboard.NewMux(cl, namespace)

	addr := ":8080"
	logger.Info().Str("addr", addr).Msg("dashboard listening")
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Fatal().Err(srv.ListenAndServe()).Msg("dashboard server exited")
}
