package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/bouquet2/kdfs/internal/agent"
	"github.com/bouquet2/kdfs/internal/logging"
)

type replicaServer interface {
	CreateReplica(context.Context, agent.CreateReplicaRequest) (agent.CreateReplicaResponse, error)
	DeleteReplica(context.Context, agent.DeleteReplicaRequest) error
	GetReplica(context.Context, agent.GetReplicaRequest) (agent.GetReplicaResponse, error)
	DeleteSnapshot(context.Context, agent.DeleteSnapshotRequest) error
}

func handler(server replicaServer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/replicas/create", createReplicaHandler(server))
	mux.HandleFunc("/replicas/delete", deleteReplicaHandler(server))
	mux.HandleFunc("/replicas/get", getReplicaHandler(server))
	mux.HandleFunc("/snapshots/delete", deleteSnapshotHandler(server))
	return mux
}

func createReplicaHandler(server replicaServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req agent.CreateReplicaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := server.CreateReplica(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func deleteReplicaHandler(server replicaServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req agent.DeleteReplicaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := server.DeleteReplica(r.Context(), req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func deleteSnapshotHandler(server replicaServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req agent.DeleteSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := server.DeleteSnapshot(r.Context(), req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func getReplicaHandler(server replicaServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req agent.GetReplicaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := server.GetReplica(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func main() {
	logger := logging.Component("node-agent")
	addr := os.Getenv("KDFS_NODE_AGENT_ADDR")
	if addr == "" {
		addr = ":9808"
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	server, err := newReplicaServer()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize node-agent server")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler(server),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal().Err(err).Str("addr", addr).Msg("node-agent server exited")
	}
}
