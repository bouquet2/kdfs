package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Snapshotter interface {
	CreateSnapshot(ctx context.Context, snapshotID string) (snapshotPath string, sizeBytes int64, err error)
}

type snapshotReq struct {
	SnapshotID string `json:"snapshotID"`
}

type snapshotResp struct {
	SnapshotPath string `json:"snapshotPath"`
	SizeBytes    int64  `json:"sizeBytes"`
}

func SnapshotHTTP(snapshotter Snapshotter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req snapshotReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
			return
		}
		if req.SnapshotID == "" {
			http.Error(w, "snapshotID is required", http.StatusBadRequest)
			return
		}
		path, size, err := snapshotter.CreateSnapshot(r.Context(), req.SnapshotID)
		if err != nil {
			http.Error(w, fmt.Sprintf("snapshot failed: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshotResp{SnapshotPath: path, SizeBytes: size})
	}
}
