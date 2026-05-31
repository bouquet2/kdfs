package sidecar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSnapshotter struct {
	path string
	size int64
	err  error
}

func (f *fakeSnapshotter) CreateSnapshot(_ context.Context, _ string) (string, int64, error) {
	return f.path, f.size, f.err
}

func TestSnapshotHTTP_Valid(t *testing.T) {
	snapshotter := &fakeSnapshotter{path: "/var/lib/kdfs/vol/snapshot-abc.img", size: 10737418240}
	handler := SnapshotHTTP(snapshotter)
	body := strings.NewReader(`{"snapshotID": "abc"}`)
	req := httptest.NewRequest(http.MethodPost, "/snapshot", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp snapshotResp
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.SnapshotPath != "/var/lib/kdfs/vol/snapshot-abc.img" {
		t.Fatal("wrong path")
	}
	if resp.SizeBytes != 10737418240 {
		t.Fatal("wrong size")
	}
}

func TestSnapshotHTTP_MissingID(t *testing.T) {
	handler := SnapshotHTTP(&fakeSnapshotter{})
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/snapshot", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSnapshotHTTP_WrongMethod(t *testing.T) {
	handler := SnapshotHTTP(&fakeSnapshotter{})
	req := httptest.NewRequest(http.MethodGet, "/snapshot", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
