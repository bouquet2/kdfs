package sidecar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteStatusHTTPWritesJSONStatus(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteStatusHTTP(rec, Status{
		Role:         "replica",
		Endpoint:     "[2001:db8::10]:4421",
		ReplicaNQN:   "nqn.2026-05.krea.to:replica-pvc-1234-0",
		SubsystemNQN: "nqn.2026-05.krea.to:volume-pvc-1234",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	var status Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Role != "replica" || status.Endpoint != "[2001:db8::10]:4421" || status.ReplicaNQN == "" || status.SubsystemNQN == "" {
		t.Fatalf("status = %#v", status)
	}
}
