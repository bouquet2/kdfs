package controller

import (
	"fmt"
	"strings"

	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/sidecar"
)

func reconfigureURL(podIP string) string {
	return network.HTTPURL("http", podIP, "9810", "/reconfigure")
}

func validateEndpoint(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("missing endpoint")
	}
	host, port, err := network.SplitEndpoint(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint: %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("endpoint host is empty")
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("endpoint port is empty")
	}
	return nil
}

func validateEngineSidecarStatus(status sidecar.Status) error {
	if status.Role != "engine" {
		return fmt.Errorf("unexpected sidecar role %q", status.Role)
	}
	if strings.TrimSpace(status.SubsystemNQN) == "" {
		return fmt.Errorf("engine sidecar status missing subsystemNQN")
	}
	return validateEndpoint(status.Endpoint)
}

func validateReplicaSidecarStatus(status sidecar.Status) error {
	if status.Role != "replica" {
		return fmt.Errorf("unexpected sidecar role %q", status.Role)
	}
	if strings.TrimSpace(status.ReplicaNQN) == "" {
		return fmt.Errorf("replica sidecar status missing replicaNQN")
	}
	return validateEndpoint(status.Endpoint)
}
