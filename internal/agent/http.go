package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *HTTPClient) CreateReplica(ctx context.Context, req CreateReplicaRequest) (CreateReplicaResponse, error) {
	var resp CreateReplicaResponse
	if err := c.doJSON(ctx, "/replicas/create", req, &resp); err != nil {
		return CreateReplicaResponse{}, err
	}
	return resp, nil
}

func (c *HTTPClient) DeleteReplica(ctx context.Context, req DeleteReplicaRequest) error {
	return c.doJSON(ctx, "/replicas/delete", req, nil)
}

func (c *HTTPClient) GetReplica(ctx context.Context, req GetReplicaRequest) (GetReplicaResponse, error) {
	var resp GetReplicaResponse
	if err := c.doJSON(ctx, "/replicas/get", req, &resp); err != nil {
		return GetReplicaResponse{}, err
	}
	return resp, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, path string, reqBody any, respBody any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("perform request for %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request %s failed with status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if respBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response for %s: %w", path, err)
	}
	return nil
}
