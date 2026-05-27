package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientCreateReplica(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/replicas/create" {
			t.Fatalf("path = %s, want %s", r.URL.Path, "/replicas/create")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"devicePath":"/dev/loop7","state":"Ready"}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	resp, err := client.CreateReplica(context.Background(), CreateReplicaRequest{Path: "/var/lib/kdfs/pvc/vol.img", Size: "10Gi"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DevicePath != "/dev/loop7" || resp.State != ReplicaStateReady {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHTTPClientDeleteReplica(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/replicas/delete" {
			t.Fatalf("path = %s, want %s", r.URL.Path, "/replicas/delete")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	if err := client.DeleteReplica(context.Background(), DeleteReplicaRequest{Path: "/var/lib/kdfs/pvc/vol.img"}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClientGetReplica(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/replicas/get" {
			t.Fatalf("path = %s, want %s", r.URL.Path, "/replicas/get")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"Ready","size":"10Gi"}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	resp, err := client.GetReplica(context.Background(), GetReplicaRequest{Path: "/var/lib/kdfs/pvc/vol.img"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != ReplicaStateReady || resp.Size != "10Gi" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHTTPClientReturnsServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	_, err := client.CreateReplica(context.Background(), CreateReplicaRequest{Path: "/var/lib/kdfs/pvc/vol.img", Size: "10Gi"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want server body", err)
	}
}
