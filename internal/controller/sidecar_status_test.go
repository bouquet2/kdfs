package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bouquet2/kdfs/internal/sidecar"
)

func TestHTTPSidecarStatusClientGetStatus(t *testing.T) {
	client := HTTPSidecarStatusClient{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://[fd00::21]:9810/status" {
			t.Fatalf("url = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"endpoint":"[fd00::21]:4421","replicaNQN":"nqn.replica"}`)),
			Header:     make(http.Header),
		}, nil
	})}}

	status, err := client.GetStatus(context.Background(), "fd00::21")
	if err != nil {
		t.Fatal(err)
	}
	if status.Endpoint != "[fd00::21]:4421" || status.ReplicaNQN != "nqn.replica" {
		t.Fatalf("status = %#v", status)
	}
}

func TestHTTPSidecarStatusClientGetStatusReturnsErrorOnNonOK(t *testing.T) {
	client := HTTPSidecarStatusClient{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("bad gateway")), Header: make(http.Header)}, nil
	})}}

	_, err := client.GetStatus(context.Background(), "10.0.0.5")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPSidecarStatusClientGetStatusDefaultsTimeout(t *testing.T) {
	if got := sidecarStatusHTTPClient(nil); got == nil || got.Timeout != 5*time.Second {
		t.Fatalf("default client = %#v", got)
	}
	custom := &http.Client{Timeout: 2 * time.Second}
	if got := sidecarStatusHTTPClient(custom); got != custom {
		t.Fatalf("expected custom client passthrough, got %#v", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type fakeSidecarStatusClient struct {
	status sidecar.Status
	err    error
	podIPs []string
}

func (f *fakeSidecarStatusClient) GetStatus(_ context.Context, podIP string) (sidecar.Status, error) {
	f.podIPs = append(f.podIPs, podIP)
	if f.err != nil {
		return sidecar.Status{}, f.err
	}
	return f.status, nil
}

var _ SidecarStatusClient = (*fakeSidecarStatusClient)(nil)
