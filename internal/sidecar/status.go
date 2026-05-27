package sidecar

import (
	"encoding/json"
	"net/http"
)

type Status struct {
	Role         string `json:"role"`
	Endpoint     string `json:"endpoint,omitempty"`
	SubsystemNQN string `json:"subsystemNQN,omitempty"`
	ReplicaNQN   string `json:"replicaNQN,omitempty"`
}

func WriteStatusHTTP(w http.ResponseWriter, status Status) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}
