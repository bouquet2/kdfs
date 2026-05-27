package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bouquet2/kdfs/internal/agent"
)

type fakeReplicaServer struct {
	create func(context.Context, agent.CreateReplicaRequest) (agent.CreateReplicaResponse, error)
	delete func(context.Context, agent.DeleteReplicaRequest) error
	get    func(context.Context, agent.GetReplicaRequest) (agent.GetReplicaResponse, error)
}

func (f fakeReplicaServer) CreateReplica(ctx context.Context, req agent.CreateReplicaRequest) (agent.CreateReplicaResponse, error) {
	if f.create == nil {
		return agent.CreateReplicaResponse{}, errors.New("unexpected create")
	}
	return f.create(ctx, req)
}

func (f fakeReplicaServer) DeleteReplica(ctx context.Context, req agent.DeleteReplicaRequest) error {
	if f.delete == nil {
		return errors.New("unexpected delete")
	}
	return f.delete(ctx, req)
}

func (f fakeReplicaServer) GetReplica(ctx context.Context, req agent.GetReplicaRequest) (agent.GetReplicaResponse, error) {
	if f.get == nil {
		return agent.GetReplicaResponse{}, errors.New("unexpected get")
	}
	return f.get(ctx, req)
}

func TestCreateReplicaHandler(t *testing.T) {
	t.Parallel()

	var got agent.CreateReplicaRequest
	h := handler(fakeReplicaServer{
		create: func(ctx context.Context, req agent.CreateReplicaRequest) (agent.CreateReplicaResponse, error) {
			got = req
			return agent.CreateReplicaResponse{DevicePath: "/dev/loop7", State: agent.ReplicaStateReady}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/replicas/create", strings.NewReader(`{"path":"/var/lib/kdfs/pvc/vol.img","size":"10Gi"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got.Path != "/var/lib/kdfs/pvc/vol.img" || got.Size != "10Gi" {
		t.Fatalf("unexpected request: %+v", got)
	}
	if body := rr.Body.String(); !strings.Contains(body, "/dev/loop7") || !strings.Contains(body, string(agent.ReplicaStateReady)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestGetReplicaHandler(t *testing.T) {
	t.Parallel()

	var got agent.GetReplicaRequest
	h := handler(fakeReplicaServer{
		get: func(ctx context.Context, req agent.GetReplicaRequest) (agent.GetReplicaResponse, error) {
			got = req
			return agent.GetReplicaResponse{State: agent.ReplicaStateReady, Size: "10Gi"}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/replicas/get", strings.NewReader(`{"path":"/var/lib/kdfs/pvc/vol.img"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got.Path != "/var/lib/kdfs/pvc/vol.img" {
		t.Fatalf("unexpected request: %+v", got)
	}
	if body := rr.Body.String(); !strings.Contains(body, string(agent.ReplicaStateReady)) || !strings.Contains(body, "10Gi") {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestDeleteReplicaHandler(t *testing.T) {
	t.Parallel()

	var got agent.DeleteReplicaRequest
	h := handler(fakeReplicaServer{
		delete: func(ctx context.Context, req agent.DeleteReplicaRequest) error {
			got = req
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/replicas/delete", strings.NewReader(`{"path":"/var/lib/kdfs/pvc/vol.img"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if got.Path != "/var/lib/kdfs/pvc/vol.img" {
		t.Fatalf("unexpected request: %+v", got)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rr.Body.String())
	}
}

func TestReplicaHandlerRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	h := handler(fakeReplicaServer{})
	req := httptest.NewRequest(http.MethodPost, "/replicas/create", strings.NewReader(`{"path":`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
