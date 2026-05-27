package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/sidecar"
	"github.com/bouquet2/kdfs/internal/spdk"
)

func defaultNetworkPolicies() network.RolePolicies {
	return network.RolePolicies{
		Default: network.Policy{
			BindAddress:      "podIP",
			AdvertiseAddress: "podIP",
			PreferredFamily:  network.FamilyAuto,
		},
		Engine:  network.Policy{Port: "4420"},
		Replica: network.Policy{Port: "4421"},
	}
}

func ensureDataFile(path string, size int64) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

// Orchestrates sidecar startup, health serving, SPDK readiness, and engine/replica setup based on mode.
func main() {
	logger := logging.Component("sidecar")
	mode := flag.String("mode", "engine", "sidecar mode: engine or replica")
	addr := flag.String("health-bind-address", ":9810", "health address")
	flag.Parse()

	if *mode != "engine" && *mode != "replica" {
		logger.Fatal().Str("mode", *mode).Msg("unsupported sidecar mode")
	}

	volumeName := os.Getenv("KDFS_VOLUME_NAME")
	if volumeName == "" {
		logger.Fatal().Msg("KDFS_VOLUME_NAME not set")
	}

	listenAddr := os.Getenv("KDFS_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = "0.0.0.0"
	}

	runtimeValues := network.RuntimeValues{
		PodIP:    os.Getenv("KDFS_POD_IP"),
		HostIP:   os.Getenv("KDFS_HOST_IP"),
		NodeName: os.Getenv("KDFS_NODE_NAME"),
	}
	policies := defaultNetworkPolicies()
	if rawPolicy := os.Getenv("KDFS_NETWORK_POLICY"); rawPolicy != "" {
		if err := json.Unmarshal([]byte(rawPolicy), &policies); err != nil {
			logger.Fatal().Err(err).Msg("failed to parse KDFS_NETWORK_POLICY")
		}
	}

	ready := false

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if ready {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready"))
		}
	})
	go func() {
		srv := &http.Server{
			Addr:              *addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      2 * time.Minute,
			IdleTimeout:       30 * time.Second,
		}
		logger.Fatal().Err(srv.ListenAndServe()).Str("addr", *addr).Msg("sidecar health server exited")
	}()

	var client spdk.Client
	for range 30 {
		c, err := spdk.NewUnixClient("/var/tmp/spdk.sock")
		if err == nil {
			client = c
			break
		}
		logger.Info().Err(err).Msg("waiting for SPDK socket")
		time.Sleep(2 * time.Second)
	}
	if client == nil {
		logger.Fatal().Msg("failed to connect to SPDK after 30 attempts")
	}

	for {
		ctx := context.Background()
		if err := client.Health(ctx); err != nil {
			logger.Info().Err(err).Msg("waiting for SPDK readiness")
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}
	logger.Info().Msg("SPDK is ready, configuring stack")

	switch *mode {
	case "engine":
		resolvedPolicy, err := network.ResolvePolicy("engine", policies, runtimeValues)
		if err != nil {
			logger.Fatal().Err(err).Msg("resolve engine network policy failed")
		}
		localPath := os.Getenv("KDFS_LOCAL_PATH")
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			logger.Warn().Err(err).Str("path", filepath.Dir(localPath)).Msg("failed to create local path directory")
		}
		if err := ensureDataFile(localPath, 1073741824); err != nil {
			logger.Fatal().Err(err).Str("path", localPath).Int64("size", 1073741824).Msg("failed to initialize local path")
		}

		var replicas []storagev1alpha1.ReplicaAttachment
		replicasJSON := os.Getenv("KDFS_REPLICAS")
		if replicasJSON != "" {
			if err := json.Unmarshal([]byte(replicasJSON), &replicas); err != nil {
				logger.Fatal().Err(err).Msg("failed to parse KDFS_REPLICAS")
			}
		}

		allowedHostsRaw := os.Getenv("KDFS_ALLOWED_HOSTS")
		var allowedHosts []string
		if allowedHostsRaw != "" {
			allowedHosts = strings.Split(allowedHostsRaw, ",")
		}

		config := sidecar.EngineConfig{
			VolumeName:   volumeName,
			LocalPath:    localPath,
			Replicas:     replicas,
			Listener:     spdk.Listener{TrType: "TCP", AdrFam: resolvedPolicy.AdrFam, TrAddr: resolvedPolicy.BindAddress, TrSvcID: resolvedPolicy.Port},
			Endpoint:     network.JoinEndpoint(resolvedPolicy.AdvertiseAddress, resolvedPolicy.Port),
			AllowedHosts: allowedHosts,
		}
		engine, err := sidecar.ConfigureEngine(context.Background(), client, config)
		if err != nil {
			logger.Fatal().Err(err).Msg("engine setup failed")
		}
		mux.HandleFunc("/reconfigure", engine.ReconfigureHTTP)
		mux.HandleFunc("/status", engine.StatusHTTP)
	case "replica":
		resolvedPolicy, err := network.ResolvePolicy("replica", policies, runtimeValues)
		if err != nil {
			logger.Fatal().Err(err).Msg("resolve replica network policy failed")
		}
		dataPath := os.Getenv("KDFS_DATA_PATH")
		if err := os.MkdirAll(filepath.Dir(dataPath), 0755); err != nil {
			logger.Warn().Err(err).Str("path", filepath.Dir(dataPath)).Msg("failed to create replica data directory")
		}
		if err := ensureDataFile(dataPath, 1073741824); err != nil {
			logger.Fatal().Err(err).Str("path", dataPath).Int64("size", 1073741824).Msg("failed to initialize replica data file")
		}

		replicaIndexStr := os.Getenv("KDFS_REPLICA_INDEX")
		replicaIndex := 0
		if replicaIndexStr != "" {
			if idx, err := strconv.Atoi(replicaIndexStr); err == nil {
				replicaIndex = idx
			}
		}

		config := sidecar.ReplicaConfig{
			VolumeName:   volumeName,
			ReplicaIndex: replicaIndex,
			DataPath:     dataPath,
			Listener:     spdk.Listener{TrType: "TCP", AdrFam: resolvedPolicy.AdrFam, TrAddr: resolvedPolicy.BindAddress, TrSvcID: resolvedPolicy.Port},
			Endpoint:     network.JoinEndpoint(resolvedPolicy.AdvertiseAddress, resolvedPolicy.Port),
		}
		var replica *sidecar.Replica
		for {
			result, err := sidecar.ConfigureReplica(context.Background(), client, config)
			if err != nil {
				logger.Warn().Err(err).Int("replica_index", replicaIndex).Msg("replica setup failed, retrying")
				time.Sleep(5 * time.Second)
				continue
			}
			replica = sidecar.NewReplica(result)
			break
		}
		mux.HandleFunc("/status", replica.StatusHTTP)
	}

	logger.Info().Str("mode", *mode).Msg("SPDK stack configured successfully")
	ready = true
	select {}
}
