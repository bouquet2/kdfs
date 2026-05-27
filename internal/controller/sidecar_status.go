package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/sidecar"
)

type SidecarStatusClient interface {
	GetStatus(ctx context.Context, podIP string) (sidecar.Status, error)
}

type HTTPSidecarStatusClient struct {
	Client *http.Client
}

func (c HTTPSidecarStatusClient) GetStatus(ctx context.Context, podIP string) (sidecar.Status, error) {
	client := sidecarStatusHTTPClient(c.Client)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, network.HTTPURL("http", podIP, "9810", "/status"), nil)
	if err != nil {
		return sidecar.Status{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return sidecar.Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sidecar.Status{}, fmt.Errorf("sidecar status returned %d", resp.StatusCode)
	}
	var status sidecar.Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return sidecar.Status{}, err
	}
	return status, nil
}

func sidecarStatusHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 5 * time.Second}
}
